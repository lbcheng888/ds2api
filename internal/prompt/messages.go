package prompt

import (
	textclean "ds2api/internal/textclean"
	"ds2api/internal/toolcall"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var markdownImagePattern = regexp.MustCompile(`!\[(.*?)\]\((.*?)\)`)
var emptyToolParametersPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?parameters\b[^>]*/>|<(?:[a-z0-9_:-]+:)?parameters\b[^>]*>\s*</(?:[a-z0-9_:-]+:)?parameters>`)
var toolCallsBlockPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_calls\b[^>]*>.*?</(?:[a-z0-9_:-]+:)?tool_calls>`)
var toolCallBlockPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_call\b[^>]*>.*?</(?:[a-z0-9_:-]+:)?tool_call>`)
var knownToolNameWithoutParametersPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_name\b[^>]*>\s*(agent|task|taskcreate|taskupdate|todowrite|todo_write|bash|read|grep|glob|edit|multiedit)\s*</(?:[a-z0-9_:-]+:)?tool_name>`)

const (
	beginSentenceMarker   = "<｜begin▁of▁sentence｜>"
	systemMarker          = "<｜System｜>"
	userMarker            = "<｜User｜>"
	assistantMarker       = "<｜Assistant｜>"
	toolMarker            = "<｜Tool｜>"
	endSentenceMarker     = "<｜end▁of▁sentence｜>"
	endToolResultsMarker  = "<｜end▁of▁toolresults｜>"
	endInstructionsMarker = "<｜end▁of▁instructions｜>"
)

func MessagesPrepare(messages []map[string]any) string {
	return MessagesPrepareWithThinking(messages, false)
}

func MessagesPrepareWithThinking(messages []map[string]any, thinkingEnabled bool) string {
	type block struct {
		Role string
		Text string
	}
	processed := make([]block, 0, len(messages))
	if thinkingEnabled {
		if instruction := buildConversationContinuityInstructions(thinkingEnabled); strings.TrimSpace(instruction) != "" {
			processed = append(processed, block{Role: "system", Text: instruction})
		}
	}
	for _, m := range messages {
		role, _ := m["role"].(string)
		text := NormalizeContent(m["content"])
		if isTaskTrackingReminderText(text) {
			continue
		}
		if role != "user" && isInternalMetaAgentBlockedText(text) {
			continue
		}
		if role == "tool" && isInvalidToolParameterErrorText(text) {
			continue
		}
		if role == "assistant" {
			text = stripInvalidEmptyParameterToolCallBlocks(text)
			text = textclean.SanitizeLeakedOutput(text)
		}
		if role != "user" && strings.TrimSpace(text) == "" {
			continue
		}
		processed = append(processed, block{Role: role, Text: text})
	}
	if len(processed) == 0 {
		return ""
	}
	merged := make([]block, 0, len(processed))
	for _, msg := range processed {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			merged[len(merged)-1].Text += "\n\n" + msg.Text
			continue
		}
		merged = append(merged, msg)
	}
	parts := make([]string, 0, len(merged)+2)
	parts = append(parts, beginSentenceMarker)
	lastRole := ""
	lastNonSystemRole := ""
	for _, m := range merged {
		lastRole = m.Role
		if m.Role != "system" && strings.TrimSpace(m.Text) != "" {
			lastNonSystemRole = m.Role
		}
		switch m.Role {
		case "assistant":
			parts = append(parts, formatRoleBlock(assistantMarker, m.Text, endSentenceMarker))
		case "tool":
			if strings.TrimSpace(m.Text) != "" {
				parts = append(parts, formatRoleBlock(toolMarker, m.Text, endToolResultsMarker))
			}
		case "system":
			if text := strings.TrimSpace(m.Text); text != "" {
				parts = append(parts, formatRoleBlock(systemMarker, text, endInstructionsMarker))
			}
		case "user":
			parts = append(parts, formatRoleBlock(userMarker, m.Text, ""))
		default:
			if strings.TrimSpace(m.Text) != "" {
				parts = append(parts, m.Text)
			}
		}
	}
	if thinkingEnabled && lastNonSystemRole == "tool" {
		parts = append(parts, formatRoleBlock(systemMarker, postToolVisibleAnswerInstruction(), endInstructionsMarker))
	}
	if thinkingEnabled && lastRole != "assistant" {
		parts = append(parts, formatRoleBlock(systemMarker, finalAssistantTurnInstruction(), endInstructionsMarker))
	}
	if lastRole != "assistant" {
		parts = append(parts, assistantMarker)
	}
	out := strings.Join(parts, "")
	return markdownImagePattern.ReplaceAllString(out, `[${1}](${2})`)
}

// formatRoleBlock produces a single concatenated block: marker + text + endMarker.
// No whitespace is inserted between marker and text so role boundaries stay
// compact and predictable for downstream parsers.
func formatRoleBlock(marker, text, endMarker string) string {
	out := marker + text
	if strings.TrimSpace(endMarker) != "" {
		out += endMarker
	}
	return out
}

func postToolVisibleAnswerInstruction() string {
	return "Tool results are complete. If more work is required, call the next needed tool now in this same assistant response. TaskCreate, TaskUpdate, TodoWrite, and TodoRead only update the client task UI; they are not implementation progress, so after any task-list update you must immediately call real work tools such as Read, Grep, Glob, Bash, Edit, MultiEdit, Agent, or TaskOutput. If waiting for background agents or task notifications, call TaskOutput now with the concrete task_id values. If no further tool is required, answer the user now in visible assistant content. Do not stop with future-tense setup text like 'I'll implement' or 'let me run' unless it is followed by a tool call. Do not ask broad next-step questions like 'do you want me to continue' after concrete findings exist; choose the highest-priority actionable fix and proceed when the user asked to optimize, improve, fix, continue, or proceed. Do not put the final answer only in reasoning_content. Ignore task-tracking reminders unless the user explicitly requested task tracking."
}

func finalAssistantTurnInstruction() string {
	return "Next assistant response contract: if you intend to use any tool, emit the complete <tool_calls> XML block now in this response. Do not emit only TaskCreate, TaskUpdate, TodoWrite, or TodoRead; task tracking is not work. If you intend to launch Agent or subagent work, emit real Agent tool calls now so the client can show agent progress; text such as 'I will launch agents' or '我将启动多个 Agent' without tool_calls is invalid. If the latest input is <task-notification> or you are waiting for background agents, emit TaskOutput tool calls now with the concrete task_id values; reasoning-only waiting text is invalid. If no tool is needed, provide the final visible answer now."
}

func isInternalMetaAgentBlockedText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, toolcall.MetaAgentToolBlockedMessage())
}

func isInvalidToolParameterErrorText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, "invalid tool parameters") {
		return true
	}
	if strings.Contains(lower, "tool was called with invalid arguments") {
		return true
	}
	if !strings.Contains(lower, "inputvalidationerror") {
		return false
	}
	return strings.Contains(lower, "required parameter") ||
		strings.Contains(lower, "missing") ||
		strings.Contains(lower, "invalid_type") ||
		strings.Contains(lower, "expected")
}

func stripInvalidEmptyParameterToolCallBlocks(text string) string {
	if !emptyToolParametersPattern.MatchString(text) && !knownToolNameWithoutParametersPattern.MatchString(text) {
		return text
	}
	out := toolCallsBlockPattern.ReplaceAllStringFunc(text, func(block string) string {
		if isInvalidEmptyParameterToolCallBlock(block) {
			return ""
		}
		return block
	})
	out = toolCallBlockPattern.ReplaceAllStringFunc(out, func(block string) string {
		if isInvalidEmptyParameterToolCallBlock(block) {
			return ""
		}
		return block
	})
	return strings.TrimSpace(out)
}

func isInvalidEmptyParameterToolCallBlock(block string) bool {
	if emptyToolParametersPattern.MatchString(block) {
		return true
	}
	lower := strings.ToLower(block)
	return !strings.Contains(lower, "<parameters") && knownToolNameWithoutParametersPattern.MatchString(block)
}

func isTaskTrackingReminderText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if !strings.Contains(lower, "system-reminder") {
		return false
	}
	return strings.Contains(lower, "taskcreate") &&
		strings.Contains(lower, "taskupdate") &&
		strings.Contains(lower, "task tools")
}

func buildConversationContinuityInstructions(thinkingEnabled bool) string {
	lines := []string{
		"Continue the conversation from the full prior context and the latest tool results.",
		"Treat earlier messages as binding context; answer the user's current request as a continuation, not a restart.",
		"If the latest user message explicitly chooses a previously offered direction or says to continue, treat that as sufficient confirmation and proceed without asking to confirm the same strategy again.",
		"Treat short execution requests such as 'optimize', 'improve', 'fix it', 'continue', 'proceed', '请优化', '继续', '按建议推进', or '直接改' as authorization to pick the highest-priority actionable item from prior findings and start executing it.",
		"When work remains and tools are available, continue by calling the next needed tool in the same response; do not finish a turn with only future-tense setup text such as 'I'll implement' or 'let me run'.",
		"Do not spend a turn only creating or updating task lists. TaskCreate, TaskUpdate, TodoWrite, and TodoRead are UI bookkeeping, not code progress; ignore task-tracking reminders and use concrete file/shell/edit tools.",
		"When receiving <task-notification> from background agents, either summarize completed results in visible text or immediately call TaskOutput for remaining task_id values in this same response; do not put the waiting decision only in reasoning.",
		"After a review or exploration produced concrete findings, do not end with broad questions like 'which direction should I take' or 'need me to go deeper'; either implement the top actionable fix with tools or provide a concise final answer if no tool work remains.",
	}
	if thinkingEnabled {
		lines = append(lines, "Keep reasoning internal. Do not leave the final user-facing answer only in reasoning; always provide the answer in visible assistant content.")
	}
	return strings.Join(lines, "\n")
}

func NormalizeContent(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			typeStr, _ := m["type"].(string)
			typeStr = strings.ToLower(strings.TrimSpace(typeStr))
			if typeStr == "text" || typeStr == "output_text" || typeStr == "input_text" {
				if txt, ok := m["text"].(string); ok && txt != "" {
					parts = append(parts, txt)
					continue
				}
				if txt, ok := m["content"].(string); ok && txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}
