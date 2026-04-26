package prompt

import (
	"strings"
	"testing"
)

func TestNormalizeContentNilReturnsEmpty(t *testing.T) {
	if got := NormalizeContent(nil); got != "" {
		t.Fatalf("expected empty string for nil content, got %q", got)
	}
}

func TestMessagesPrepareNilContentNoNullLiteral(t *testing.T) {
	messages := []map[string]any{
		{"role": "assistant", "content": nil},
		{"role": "user", "content": "ok"},
	}
	got := MessagesPrepare(messages)
	if got == "" {
		t.Fatalf("expected non-empty output")
	}
	if got == "null" {
		t.Fatalf("expected no null literal output, got %q", got)
	}
}

func TestMessagesPrepareUsesTurnSuffixes(t *testing.T) {
	messages := []map[string]any{
		{"role": "system", "content": "System rule"},
		{"role": "user", "content": "Question"},
		{"role": "assistant", "content": "Answer"},
	}
	got := MessagesPrepare(messages)
	if !strings.HasPrefix(got, "<｜begin▁of▁sentence｜>") {
		t.Fatalf("expected begin-of-sentence marker, got %q", got)
	}
	if !strings.Contains(got, "<｜System｜>System rule<｜end▁of▁instructions｜>") {
		t.Fatalf("expected system instructions suffix, got %q", got)
	}
	if !strings.Contains(got, "<｜User｜>Question") {
		t.Fatalf("expected user question, got %q", got)
	}
	if !strings.Contains(got, "<｜Assistant｜>Answer<｜end▁of▁sentence｜>") {
		t.Fatalf("expected assistant sentence suffix, got %q", got)
	}
	if strings.Contains(got, "<think>") || strings.Contains(got, "</think>") {
		t.Fatalf("did not expect think tags in prompt, got %q", got)
	}
}

func TestNormalizeContentArrayFallsBackToContentWhenTextEmpty(t *testing.T) {
	got := NormalizeContent([]any{
		map[string]any{"type": "text", "text": "", "content": "from-content"},
	})
	if got != "from-content" {
		t.Fatalf("expected fallback to content when text is empty, got %q", got)
	}
}

func TestMessagesPrepareWithThinkingAddsContinuityContract(t *testing.T) {
	messages := []map[string]any{{"role": "user", "content": "Question"}}
	gotThinking := MessagesPrepareWithThinking(messages, true)
	gotPlain := MessagesPrepareWithThinking(messages, false)
	if gotThinking == gotPlain {
		t.Fatalf("expected thinking-enabled prompt to include extra continuity instructions")
	}
	if !strings.HasSuffix(gotThinking, "<｜Assistant｜>") {
		t.Fatalf("expected assistant suffix, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "Continue the conversation from the full prior context") {
		t.Fatalf("expected continuity instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "proceed without asking to confirm the same strategy again") {
		t.Fatalf("expected no-repeat-confirmation instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "请优化") || !strings.Contains(gotThinking, "highest-priority actionable item") {
		t.Fatalf("expected optimize/proceed authorization instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "do not finish a turn with only future-tense setup text") {
		t.Fatalf("expected no-future-setup instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "When receiving <task-notification>") {
		t.Fatalf("expected task-notification continuation instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "do not end with broad questions") {
		t.Fatalf("expected no broad follow-up question instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "final user-facing answer only in reasoning") {
		t.Fatalf("expected visible-answer instruction in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "emit real Agent tool calls now so the client can show agent progress") {
		t.Fatalf("expected final-turn Agent tool-call contract in thinking prompt, got %q", gotThinking)
	}
	if !strings.Contains(gotThinking, "emit TaskOutput tool calls now") {
		t.Fatalf("expected final-turn TaskOutput contract in thinking prompt, got %q", gotThinking)
	}
	if strings.LastIndex(gotThinking, "Next assistant response contract") > strings.LastIndex(gotThinking, "<｜Assistant｜>") {
		t.Fatalf("expected final-turn contract before assistant marker, got %q", gotThinking)
	}
	if strings.Contains(gotPlain, "Continue the conversation from the full prior context") {
		t.Fatalf("did not expect thinking-only instruction in plain prompt, got %q", gotPlain)
	}
}

func TestMessagesPrepareWithThinkingAddsPostToolVisibleAnswerContract(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Count files"},
		{"role": "assistant", "content": "<tool_calls>...</tool_calls>"},
		{"role": "tool", "content": "17"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if !strings.Contains(got, "Tool results are complete.") {
		t.Fatalf("expected post-tool visible-answer instruction, got %q", got)
	}
	if !strings.Contains(got, "visible assistant content") {
		t.Fatalf("expected visible assistant content contract, got %q", got)
	}
	if !strings.Contains(got, "call the next needed tool now") {
		t.Fatalf("expected same-turn tool continuation contract, got %q", got)
	}
	if !strings.Contains(got, "call TaskOutput now with the concrete task_id values") {
		t.Fatalf("expected TaskOutput continuation contract, got %q", got)
	}
	if !strings.Contains(got, "Do not ask broad next-step questions") {
		t.Fatalf("expected no broad next-step question contract, got %q", got)
	}
	if !strings.HasSuffix(got, "<｜Assistant｜>") {
		t.Fatalf("expected final assistant marker, got %q", got)
	}
	if strings.Index(got, "<｜Tool｜>17<｜end▁of▁toolresults｜>") > strings.Index(got, "Tool results are complete.") {
		t.Fatalf("expected post-tool instruction after tool result, got %q", got)
	}
}

func TestMessagesPrepareAddsEditErrorRecoveryContract(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Change core_types.cheng"},
		{"role": "assistant", "content": "<tool_calls><tool_call><tool_name>Update</tool_name></tool_call></tool_calls>"},
		{"role": "tool", "content": "Error editing file"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	for _, want := range []string{
		"The latest edit/update tool failed",
		"read the file again",
		"exact current unique old_string",
		"Do not retry the same stale old_string",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected edit recovery instruction %q, got %q", want, got)
		}
	}
	if strings.Index(got, "Error editing file") > strings.Index(got, "The latest edit/update tool failed") {
		t.Fatalf("expected recovery instruction after edit failure result, got %q", got)
	}
}

func TestMessagesPrepareDoesNotKeepEditRecoveryAfterAssistantReply(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Change core_types.cheng"},
		{"role": "assistant", "content": "<tool_calls><tool_call><tool_name>Update</tool_name></tool_call></tool_calls>"},
		{"role": "tool", "content": "Error editing file"},
		{"role": "assistant", "content": "I will read the file again."},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if strings.Contains(got, "The latest edit/update tool failed") {
		t.Fatalf("did not expect stale edit recovery instruction after assistant reply, got %q", got)
	}
}

func TestMessagesPrepareWithThinkingAddsTaskNotificationContract(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "<task-notification><task-id>task_a</task-id><status>completed</status></task-notification>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if !strings.Contains(got, "When receiving <task-notification>") {
		t.Fatalf("expected task-notification continuity contract, got %q", got)
	}
	if !strings.Contains(got, "emit TaskOutput tool calls now with the concrete task_id values") {
		t.Fatalf("expected final TaskOutput contract, got %q", got)
	}
	if !strings.HasSuffix(got, "<｜Assistant｜>") {
		t.Fatalf("expected final assistant marker, got %q", got)
	}
}

func TestMessagesPrepareWithThinkingAddsPostToolContractAfterSystemReminder(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Count files"},
		{"role": "assistant", "content": "<tool_calls>...</tool_calls>"},
		{"role": "tool", "content": "17"},
		{"role": "system", "content": "<system-reminder>consider task tracking</system-reminder>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	reminderIdx := strings.Index(got, "<system-reminder>")
	instructionIdx := strings.LastIndex(got, "Tool results are complete.")
	if reminderIdx < 0 || instructionIdx < 0 || instructionIdx < reminderIdx {
		t.Fatalf("expected post-tool instruction after system reminder, got %q", got)
	}
	if !strings.Contains(got, "Ignore task-tracking reminders") {
		t.Fatalf("expected task reminder override, got %q", got)
	}
}

func TestMessagesPrepareDropsTaskTrackingSystemReminder(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Review code"},
		{"role": "assistant", "content": "<tool_calls>...</tool_calls>"},
		{"role": "tool", "content": "file content"},
		{"role": "user", "content": "<system-reminder>\nThe task tools haven't been used recently. If you're working on tasks that would benefit from tracking progress, consider using TaskCreate to add new tasks and TaskUpdate to update task status.\n</system-reminder>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	for _, bad := range []string{"task tools haven't been used recently", "consider using TaskCreate to add new tasks"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected task tracking reminder text to be dropped, got %q", got)
		}
	}
	if !strings.Contains(got, "Tool results are complete.") {
		t.Fatalf("expected post-tool visible-answer instruction, got %q", got)
	}
	if !strings.Contains(got, "Do not emit only TaskCreate") {
		t.Fatalf("expected task-tracking suppression contract, got %q", got)
	}
}

func TestMessagesPrepareDropsInternalMetaAgentBlockedMessage(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "继续"},
		{"role": "assistant", "content": "Agent/subagent tools are disabled by ds2api for DeepSeek coding-agent compatibility. Continue with direct file tools such as read, grep, glob, and bash."},
		{"role": "assistant", "content": "ready"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if strings.Contains(got, "Agent/subagent tools are disabled") {
		t.Fatalf("expected internal meta-agent blocked message to be dropped, got %q", got)
	}
	if !strings.Contains(got, "ready") {
		t.Fatalf("expected normal assistant content to remain, got %q", got)
	}
}

func TestMessagesPrepareDropsInvalidToolParameterErrorHistory(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Review code"},
		{"role": "assistant", "content": "<tool_calls><tool_call><tool_name>Agent</tool_name><parameters></parameters></tool_call></tool_calls>"},
		{"role": "tool", "content": "InputValidationError: required parameter `description` is missing"},
		{"role": "assistant", "content": "Let me inspect the files directly."},
	}
	got := MessagesPrepareWithThinking(messages, true)
	for _, bad := range []string{"<tool_name>Agent</tool_name>", "InputValidationError", "required parameter"} {
		if strings.Contains(got, bad) {
			t.Fatalf("expected invalid tool history %q to be dropped, got %q", bad, got)
		}
	}
	if !strings.Contains(got, "Let me inspect the files directly.") {
		t.Fatalf("expected normal assistant text to remain, got %q", got)
	}
}

func TestMessagesPrepareStripsOnlyInvalidEmptyParameterToolBlock(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Review code"},
		{"role": "assistant", "content": "I will inspect now.\n<tool_call><tool_name>Bash</tool_name><parameters> \n </parameters></tool_call>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if strings.Contains(got, "<tool_call>") || strings.Contains(got, "<parameters>") {
		t.Fatalf("expected empty-parameter tool block to be stripped, got %q", got)
	}
	if !strings.Contains(got, "I will inspect now.") {
		t.Fatalf("expected assistant prose to remain, got %q", got)
	}
}

func TestMessagesPrepareStripsKnownToolBlockWithoutParameters(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Review code"},
		{"role": "assistant", "content": "Checking.\n<tool_call><tool_name>Bash</tool_name></tool_call>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if strings.Contains(got, "<tool_call>") || strings.Contains(got, "<tool_name>Bash</tool_name>") {
		t.Fatalf("expected known tool block without parameters to be stripped, got %q", got)
	}
	if !strings.Contains(got, "Checking.") {
		t.Fatalf("expected assistant prose to remain, got %q", got)
	}
}

func TestMessagesPrepareSanitizesAssistantLeakedDanglingToolTag(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Review code"},
		{"role": "assistant", "content": "Let me inspect more files.\n\n\ntool_calls>"},
	}
	got := MessagesPrepareWithThinking(messages, true)
	if strings.Contains(got, "tool_calls>") {
		t.Fatalf("expected leaked dangling tool tag to be removed, got %q", got)
	}
	if !strings.Contains(got, "Let me inspect more files.") {
		t.Fatalf("expected assistant text to remain, got %q", got)
	}
}
