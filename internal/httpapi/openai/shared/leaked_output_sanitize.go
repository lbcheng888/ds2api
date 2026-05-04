package shared

import (
	"regexp"
	"strings"

	"ds2api/internal/toolcall"
)

var emptyJSONFencePattern = regexp.MustCompile("(?is)```json\\s*```")
var leakedToolCallArrayPattern = regexp.MustCompile(`(?is)\[\{\s*"function"\s*:\s*\{[\s\S]*?\}\s*,\s*"id"\s*:\s*"call[^"]*"\s*,\s*"type"\s*:\s*"function"\s*}\]`)
var leakedToolResultBlobPattern = regexp.MustCompile(`(?is)<\s*\|\s*tool\s*\|\s*>\s*\{[\s\S]*?"tool_call_id"\s*:\s*"call[^"]*"\s*}`)
var leakedHistoryTranscriptMarkerPattern = regexp.MustCompile(`(?im)^\s*===\s*\d+\.\s*(?:SYSTEM|USER|ASSISTANT|TOOL)\s*===\s*$`)
var leakedToolCallIDLinePattern = regexp.MustCompile(`(?im)^\s*\[tool_call_id\s*=\s*call[^\]]*\]\s*$`)

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

var leakedAgentWrapperTagPattern = regexp.MustCompile(`(?is)</?(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentWrapperPlusResultOpenPattern = regexp.MustCompile(`(?is)<(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>\s*<result>`)
var leakedAgentResultPlusWrapperClosePattern = regexp.MustCompile(`(?is)</result>\s*</(?:attempt_completion|ask_followup_question|new_task)\b[^>]*>`)
var leakedAgentResultTagPattern = regexp.MustCompile(`(?is)</?result>`)

func sanitizeLeakedOutput(text string) string {
	if text == "" {
		return text
	}
	out := emptyJSONFencePattern.ReplaceAllString(text, "")
	out = leakedToolCallArrayPattern.ReplaceAllString(out, "")
	out = leakedToolResultBlobPattern.ReplaceAllString(out, "")
	out, _ = stripLeakedHistoryTranscriptSuffix(out)
	out = stripDanglingThinkSuffix(out)
	out = leakedThinkTagPattern.ReplaceAllString(out, "")
	out = leakedBOSMarkerPattern.ReplaceAllString(out, "")
	out = leakedMetaMarkerPattern.ReplaceAllString(out, "")
	out = stripLeakedToolCallWrapperBlocks(out)
	out = sanitizeLeakedAgentXMLBlocks(out)
	return out
}

func stripLeakedHistoryTranscriptSuffix(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	idx := -1
	if loc := leakedHistoryTranscriptMarkerPattern.FindStringIndex(text); loc != nil {
		idx = loc[0]
	}
	if loc := leakedToolCallIDLinePattern.FindStringIndex(text); loc != nil && (idx < 0 || loc[0] < idx) {
		idx = loc[0]
	}
	if idx < 0 {
		return text, false
	}
	return strings.TrimRight(text[:idx], " \t\r"), true
}

func stripLeakedToolCallWrapperBlocks(text string) string {
	if text == "" {
		return text
	}
	var b strings.Builder
	pos := 0
	for pos < len(text) {
		tag, ok := toolcall.FindToolMarkupTagOutsideIgnored(text, pos)
		if !ok {
			b.WriteString(text[pos:])
			break
		}
		if tag.Start > pos {
			b.WriteString(text[pos:tag.Start])
		}
		if tag.Closing || tag.Name != "tool_calls" {
			b.WriteString(text[tag.Start : tag.End+1])
			pos = tag.End + 1
			continue
		}
		closeTag, ok := toolcall.FindMatchingToolMarkupClose(text, tag)
		if !ok {
			b.WriteString(text[tag.Start : tag.End+1])
			pos = tag.End + 1
			continue
		}
		pos = closeTag.End + 1
	}
	return b.String()
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
