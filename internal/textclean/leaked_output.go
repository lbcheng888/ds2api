package textclean

import (
	"regexp"
	"strings"
)

var emptyJSONFencePattern = regexp.MustCompile("(?is)```json\\s*```")
var leakedToolCallArrayPattern = regexp.MustCompile(`(?is)\[\{\s*"function"\s*:\s*\{[\s\S]*?\}\s*,\s*"id"\s*:\s*"call[^"]*"\s*,\s*"type"\s*:\s*"function"\s*}\]`)
var leakedToolResultBlobPattern = regexp.MustCompile(`(?is)<\s*\|\s*tool\s*\|\s*>\s*\{[\s\S]*?"tool_call_id"\s*:\s*"call[^"]*"\s*}`)
var leakedDanglingToolTagLinePattern = regexp.MustCompile(`(?im)^\s*/?\s*(?:tool_calls|tool_call|function_calls|function_call|tool_use|invoke)>\s*$\n?`)

var leakedSystemReminderBlockPattern = regexp.MustCompile(`(?is)<system-reminder\b[^>]*>[\s\S]*?</system-reminder>`)
var leakedSystemReminderOpenPattern = regexp.MustCompile(`(?is)<system-reminder\b[^>]*>[\s\S]*$`)
var leakedSystemReminderClosePattern = regexp.MustCompile(`(?is)</system-reminder>`)
var leakedCodexCorePrinciplesPattern = regexp.MustCompile(`(?is)(?:^|\n)\s*(?:As you answer the user's questions,\s*you can use the following context:\s*)?# Codex Core Principles\s+[\s\S]*?Codex is an AI-first coding agent[\s\S]*$`)

var leakedThinkTagPattern = regexp.MustCompile(`(?is)</?\s*think\s*>`)

// leakedBOSMarkerPattern matches DeepSeek BOS markers in BOTH forms:
//   - ASCII underscore: <｜begin_of_sentence｜>
//   - U+2581 variant:   <｜begin▁of▁sentence｜>
var leakedBOSMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*begin[_▁]of[_▁]sentence\s*[｜\|]>`)

// leakedMetaMarkerPattern matches the remaining DeepSeek special tokens in BOTH forms:
//   - ASCII underscore: <｜end_of_sentence｜>, <｜end_of_toolresults｜>, <｜end_of_instructions｜>
//   - U+2581 variant:   <｜end▁of▁sentence｜>, <｜end▁of▁toolresults｜>, <｜end▁of▁instructions｜>
var leakedMetaMarkerPattern = regexp.MustCompile(`(?i)<[｜\|]\s*(?:assistant|tool|end[_▁]of[_▁]sentence|end[_▁]of[_▁]thinking|end[_▁]of[_▁]toolresults|end[_▁]of[_▁]instructions)\s*[｜\|]>`)

// leakedAgentXMLBlockPatterns catch agent-style XML blocks that leak through
// when the sieve fails to capture them. These are applied only to complete
// wrapper blocks so standalone "<result>" examples in normal output remain
// untouched.
var leakedAgentXMLBlockPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<attempt_completion\b[^>]*>(.*?)</attempt_completion>`),
	regexp.MustCompile(`(?is)<ask_followup_question\b[^>]*>(.*?)</ask_followup_question>`),
	regexp.MustCompile(`(?is)<new_task\b[^>]*>(.*?)</new_task>`),
}

// StripDSMLContent removes DSML-format tool call markup from streaming
// visible content. It is called only in streaming output paths (emitFinalContent
// and the no-tool-buffer passthrough), never in tool detection or non-stream
// paths where the markup needs to survive for ParseStandaloneToolCallsDetailed.
//
// Strategy (order matters):
//  1. Complete wrapper blocks: <|DSML|tool_calls> ... </|DSML|tool_calls>
//  2. Complete inner blocks: <|DSML|invoke> ... </|DSML|invoke>
//     and <|DSML|parameter> ... </|DSML|parameter> — these include the
//     parameter text values that would otherwise leak.
//  3. Standalone tag lines as a last resort.
func StripDSMLContent(text string) string {
	if text == "" {
		return text
	}
	out := leakedDSMLWrapperPattern.ReplaceAllString(text, "")
	out = leakedDSMLInnerPairPattern.ReplaceAllString(out, "")
	out = leakedDSMLTagLinePattern.ReplaceAllString(out, "")
	return strings.TrimSpace(out)
}

var leakedDSMLWrapperPattern = regexp.MustCompile(`(?is)<\s*(?:\||｜)?\s*(?:DSML|dsml)\s*(?:\||｜)?\s*\s*tool_calls\b[^>]*>.*?</\s*(?:\||｜)?\s*(?:DSML|dsml)\s*(?:\||｜)?\s*\s*tool_calls\s*>`)
var leakedDSMLInnerPairPattern = regexp.MustCompile(`(?is)<\s*(?:\||｜)?\s*(?:DSML|dsml)\s*(?:\||｜)?\s*\s*(?:invoke|parameter)\b[^>]*>.*?</\s*(?:\||｜)?\s*(?:DSML|dsml)\s*(?:\||｜)?\s*\s*(?:invoke|parameter)\s*>`)
var leakedDSMLTagLinePattern = regexp.MustCompile(`(?im)^\s*</?\s*(?:\||｜)?\s*(?:DSML|dsml)\s*(?:\||｜)?\s*(?:|\s)(?:tool_calls|invoke|parameter)\b[^>]*>\s*$\n?`)

var leakedAgentWrapperTagPattern = regexp.MustCompile(`(?is)</?(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentWrapperPlusResultOpenPattern = regexp.MustCompile(`(?is)<(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>\s*<result>`)
var leakedAgentResultPlusWrapperClosePattern = regexp.MustCompile(`(?is)</result>\s*</(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentResultTagPattern = regexp.MustCompile(`(?is)</?result>`)

type StreamSanitizer struct {
	pendingSystemReminderPrefix string
	pendingInstructionPrefix    string
	insideSystemReminder        bool
	dropRemainder               bool
}

func (s *StreamSanitizer) Sanitize(text string) string {
	if text == "" || s.dropRemainder {
		return ""
	}
	if s.pendingSystemReminderPrefix != "" {
		text = s.pendingSystemReminderPrefix + text
		s.pendingSystemReminderPrefix = ""
	}
	if s.pendingInstructionPrefix != "" {
		text = s.pendingInstructionPrefix + text
		s.pendingInstructionPrefix = ""
	}
	out := s.sanitizeSystemInstructionStream(text)
	if s.dropRemainder || s.insideSystemReminder || out == "" {
		return out
	}
	if idx := partialSystemReminderOpenStart(out); idx >= 0 {
		s.pendingSystemReminderPrefix = out[idx:]
		return out[:idx]
	}
	return out
}

func SanitizeLeakedOutput(text string) string {
	if text == "" {
		return text
	}
	out := emptyJSONFencePattern.ReplaceAllString(text, "")
	out = leakedToolCallArrayPattern.ReplaceAllString(out, "")
	out = leakedToolResultBlobPattern.ReplaceAllString(out, "")
	out = leakedDanglingToolTagLinePattern.ReplaceAllString(out, "")
	out = sanitizeLeakedSystemInstructions(out)
	out = stripDanglingThinkSuffix(out)
	out = leakedThinkTagPattern.ReplaceAllString(out, "")
	out = leakedBOSMarkerPattern.ReplaceAllString(out, "")
	out = leakedMetaMarkerPattern.ReplaceAllString(out, "")
	out = sanitizeLeakedAgentXMLBlocks(out)
	return out
}

func (s *StreamSanitizer) sanitizeSystemInstructionStream(text string) string {
	var out strings.Builder
	rest := text
	for rest != "" {
		if s.insideSystemReminder {
			closeIdx := indexFold(rest, "</system-reminder>")
			if closeIdx < 0 {
				return out.String()
			}
			rest = rest[closeIdx+len("</system-reminder>"):]
			s.insideSystemReminder = false
			continue
		}
		codexIdx := findCodexCoreLeakStart(rest)
		pendingInstructionIdx := findPendingCodexCoreLeakStart(rest)
		openIdx := indexFold(rest, "<system-reminder")
		if codexIdx >= 0 && (openIdx < 0 || codexIdx < openIdx) {
			out.WriteString(rest[:codexIdx])
			s.dropRemainder = true
			return out.String()
		}
		if pendingInstructionIdx >= 0 && (openIdx < 0 || pendingInstructionIdx < openIdx) {
			out.WriteString(rest[:pendingInstructionIdx])
			s.pendingInstructionPrefix = rest[pendingInstructionIdx:]
			return out.String()
		}
		if openIdx < 0 {
			out.WriteString(rest)
			return out.String()
		}
		out.WriteString(rest[:openIdx])
		closeIdx := indexFold(rest[openIdx:], "</system-reminder>")
		if closeIdx < 0 {
			s.insideSystemReminder = true
			return out.String()
		}
		rest = rest[openIdx+closeIdx+len("</system-reminder>"):]
	}
	return out.String()
}

func sanitizeLeakedSystemInstructions(text string) string {
	out := leakedSystemReminderBlockPattern.ReplaceAllString(text, "")
	out = leakedSystemReminderOpenPattern.ReplaceAllString(out, "")
	out = leakedSystemReminderClosePattern.ReplaceAllString(out, "")
	out = leakedCodexCorePrinciplesPattern.ReplaceAllString(out, "")
	return out
}

func findCodexCoreLeakStart(text string) int {
	lower := strings.ToLower(text)
	context := "as you answer the user's questions, you can use the following context:"
	if idx := strings.Index(lower, context); idx >= 0 {
		suffix := lower[idx:]
		if strings.Contains(suffix, "# codex core principles") || strings.Contains(suffix, "codex is an ai-first coding agent") {
			return idx
		}
	}
	header := "# codex core principles"
	idx := strings.Index(lower, header)
	if idx < 0 {
		return -1
	}
	if strings.Contains(lower[idx:], "codex is an ai-first coding agent") {
		return idx
	}
	return -1
}

func findPendingCodexCoreLeakStart(text string) int {
	lower := strings.ToLower(text)
	context := "as you answer the user's questions, you can use the following context:"
	if idx := strings.Index(lower, context); idx >= 0 {
		suffix := lower[idx:]
		if !strings.Contains(suffix, "# codex core principles") && !strings.Contains(suffix, "codex is an ai-first coding agent") {
			return idx
		}
	}
	header := "# codex core principles"
	if idx := strings.Index(lower, header); idx >= 0 && !strings.Contains(lower[idx:], "codex is an ai-first coding agent") {
		return idx
	}
	return -1
}

func indexFold(text string, needle string) int {
	return strings.Index(strings.ToLower(text), strings.ToLower(needle))
}

func partialSystemReminderOpenStart(text string) int {
	last := strings.LastIndex(text, "<")
	if last < 0 {
		return -1
	}
	tail := strings.ToLower(text[last:])
	if len(tail) >= len("<system-reminder") {
		return -1
	}
	if strings.HasPrefix("<system-reminder", tail) {
		return last
	}
	return -1
}

func stripDanglingThinkSuffix(text string) string {
	matches := leakedThinkTagPattern.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	depth := 0
	lastOpen := -1
	for _, loc := range matches {
		tag := strings.ToLower(text[loc[0]:loc[1]])
		compact := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(tag), " ", ""), "\t", "")
		if strings.HasPrefix(compact, "</") {
			if depth > 0 {
				depth--
				if depth == 0 {
					lastOpen = -1
				}
			}
			continue
		}
		if depth == 0 {
			lastOpen = loc[0]
		}
		depth++
	}
	if depth == 0 || lastOpen < 0 {
		return text
	}
	prefix := text[:lastOpen]
	if strings.TrimSpace(prefix) == "" {
		return ""
	}
	return prefix
}

func sanitizeLeakedAgentXMLBlocks(text string) string {
	out := text
	for _, pattern := range leakedAgentXMLBlockPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			submatches := pattern.FindStringSubmatch(match)
			if len(submatches) < 2 {
				return match
			}
			// Preserve the inner text so leaked agent instructions do not erase
			// the actual answer, but strip the wrapper/result markup itself.
			return leakedAgentResultTagPattern.ReplaceAllString(submatches[1], "")
		})
	}
	// Fallback for truncated output streams: strip any dangling wrapper tags
	// that were not part of a complete block replacement. If we detect leaked
	// wrapper tags, strip only adjacent <result> tags to avoid exposing agent
	// markup without altering unrelated user-visible <result> examples.
	if leakedAgentWrapperTagPattern.MatchString(out) {
		out = leakedAgentWrapperPlusResultOpenPattern.ReplaceAllStringFunc(out, func(match string) string {
			return leakedAgentResultTagPattern.ReplaceAllString(match, "")
		})
		out = leakedAgentResultPlusWrapperClosePattern.ReplaceAllStringFunc(out, func(match string) string {
			return leakedAgentResultTagPattern.ReplaceAllString(match, "")
		})
		out = leakedAgentWrapperTagPattern.ReplaceAllString(out, "")
	}
	return out
}
