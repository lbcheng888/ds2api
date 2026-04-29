package claudecode

import (
	"encoding/json"
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ds2api/internal/protocol"
	"ds2api/internal/toolcall"
)

type FinalOutputInput struct {
	FinalPrompt         string
	Text                string
	Thinking            string
	ToolNames           []string
	ToolSchemas         toolcall.ParameterSchemas
	AllowMetaAgentTools bool
	ContentFilter       bool
}

type FinalOutputResult struct {
	Text              string
	PreservedThinking string
	Changed           bool
	Reason            string
	ToolCall          bool
}

func RepairFinalOutput(in FinalOutputInput) FinalOutputResult {
	out := FinalOutputResult{Text: in.Text}
	if repaired := SynthesizeReadToolCallTextFromIncompleteReadIntent(in.FinalPrompt, out.Text, in.ToolNames); repaired != "" {
		out.Text = repaired
		out.Changed = true
		out.Reason = "read_intent_from_incomplete_call"
		out.ToolCall = true
		recordRepair(out.Reason)
		return out
	}
	if stripped, changed := StripEmptyToolCallContainerNoise(out.Text); changed {
		out.Text = stripped
		out.Changed = true
		out.Reason = "empty_tool_container_noise"
	}
	if repaired := SynthesizeTaskOutputToolCallTextFromAgentWaiting(in.FinalPrompt, out.Text, in.ToolNames, in.AllowMetaAgentTools); repaired != "" {
		out.Text = repaired
		out.Changed = true
		out.Reason = "agent_waiting_task_output"
		out.ToolCall = true
	}
	if !in.ContentFilter && strings.TrimSpace(out.Text) == "" {
		if repaired := SynthesizeTaskOutputToolCallTextFromTaskNotification(in.FinalPrompt, in.ToolNames, in.AllowMetaAgentTools); repaired != "" {
			out.Text = repaired
			out.Changed = true
			out.Reason = "task_notification_task_output"
			out.ToolCall = true
		} else if promoted := ExecutableToolCallTextFromThinking(in.Thinking, in.ToolNames, in.ToolSchemas, in.AllowMetaAgentTools); promoted != "" {
			out.PreservedThinking = "[Note: the following tool calls were extracted from the model's thinking content]\n" + in.Thinking
			out.Text = promoted
			out.Changed = true
			out.Reason = "thinking_tool_call"
			out.ToolCall = true
		} else if repaired := SynthesizeAgentToolCallTextFromLaunchPromise(in.FinalPrompt, in.Thinking, in.ToolNames, in.AllowMetaAgentTools); repaired != "" {
			out.PreservedThinking = "[Note: the following tool calls were extracted from the model's thinking content]\n" + in.Thinking
			out.Text = repaired
			out.Changed = true
			out.Reason = "thinking_agent_launch"
			out.ToolCall = true
		}
	}
	if repaired := SynthesizeAgentToolCallTextFromLaunchPromise(in.FinalPrompt, out.Text, in.ToolNames, in.AllowMetaAgentTools); repaired != "" {
		out.Text = repaired
		out.Changed = true
		out.Reason = "agent_launch_promise"
		out.ToolCall = true
	}
	if out.Changed {
		recordRepair(out.Reason)
	}
	return out
}

var promptFileReferencePattern = regexp.MustCompile(`(?i)(/Users/[^\s"'<>` + "`" + `]+|(?:[A-Za-z0-9_.-]+/)+[A-Za-z0-9_.-]+\.(?:cheng|md|json|go|js|jsx|ts|tsx|py|c|h|toml|yaml|yml))`)
var promptWorkingDirectoryPattern = regexp.MustCompile(`(?im)(?:Primary working directory|Working directory):\s*([^\s<]+)`)
var namedToolCallPattern = regexp.MustCompile(`(?is)<tool_call\b[^>]*\bname\s*=\s*["']([^"']+)["'][^>]*>(.*?)</tool_call>`)
var toolArgsTagPattern = regexp.MustCompile(`(?is)<tool_args\b[^>]*>(.*?)</tool_args>`)
var systemReminderBlockPattern = regexp.MustCompile(`(?is)<system-reminder\b[^>]*>.*?</system-reminder>`)

func SynthesizeReadToolCallTextFromIncompleteReadIntent(finalPrompt, finalText string, toolNames []string) string {
	readToolName, ok := findReadToolName(toolNames)
	if !ok || !containsIncompleteReadToolCall(finalText, toolNames) {
		return ""
	}
	path := requestedFilePathForReadRepair(finalPrompt, finalText)
	if path == "" {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML([]toolcall.ParsedToolCall{{
		Name: readToolName,
		Input: map[string]any{
			"file_path": path,
			"limit":     "200",
		},
	}})
}

func ExecutableToolCallTextFromThinking(finalThinking string, toolNames []string, schemas toolcall.ParameterSchemas, allowMetaAgentTools bool) string {
	if strings.TrimSpace(finalThinking) == "" {
		return ""
	}
	detected := toolcall.ParseStandaloneToolCallsDetailed(finalThinking, toolNames)
	if len(detected.Calls) == 0 {
		return ""
	}
	if !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(detected.Calls) {
		return toolcall.MetaAgentToolBlockedMessage()
	}
	calls := toolcall.NormalizeCallsForSchemasWithMeta(detected.Calls, schemas, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML(calls)
}

func FormatParsedToolCallsAsPromptXML(calls []toolcall.ParsedToolCall) string {
	if len(calls) == 0 {
		return ""
	}
	blocks := make([]string, 0, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		input := call.Input
		if input == nil {
			input = map[string]any{}
		}
		blocks = append(blocks, formatCanonicalToolCall(name, input))
	}
	if len(blocks) == 0 {
		return ""
	}
	return "<tool_calls>\n" + strings.Join(blocks, "\n") + "\n</tool_calls>"
}

func formatCanonicalToolCall(name string, input map[string]any) string {
	var b strings.Builder
	b.WriteString("<tool_call>\n")
	b.WriteString("<tool_name>")
	b.WriteString(html.EscapeString(name))
	b.WriteString("</tool_name>\n")
	b.WriteString("<parameters>")
	keys := sortedMapKeys(input)
	for _, key := range keys {
		b.WriteString("<")
		b.WriteString(html.EscapeString(key))
		b.WriteString(">")
		b.WriteString(canonicalToolParameterValue(key, input[key]))
		b.WriteString("</")
		b.WriteString(html.EscapeString(key))
		b.WriteString(">")
	}
	b.WriteString("</parameters>\n")
	b.WriteString("</tool_call>")
	return b.String()
}

func sortedMapKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func canonicalToolParameterValue(key string, value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		if strings.EqualFold(strings.TrimSpace(key), "limit") {
			return html.EscapeString(strconv.Quote(v))
		}
		return html.EscapeString(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return html.EscapeString(strconv.Quote(fmt.Sprint(v)))
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return html.EscapeString(inputString(v))
		}
		return html.EscapeString(string(b))
	}
}

func containsIncompleteReadToolCall(text string, toolNames []string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	parsed := toolcall.ParseStandaloneToolCallsDetailed(text, toolNames)
	for _, call := range parsed.Calls {
		if !isReadToolName(call.Name) {
			continue
		}
		if readCallFilePath(call.Input) == "" {
			return true
		}
	}
	for _, match := range namedToolCallPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 3 || !isReadToolName(match[1]) {
			continue
		}
		body := match[2]
		bodyLower := strings.ToLower(body)
		if strings.Contains(bodyLower, "file_path") || strings.Contains(bodyLower, "filepath") || strings.Contains(bodyLower, "file-path") {
			continue
		}
		argsMatch := toolArgsTagPattern.FindStringSubmatch(body)
		if len(argsMatch) >= 2 {
			args := strings.TrimSpace(argsMatch[1])
			return args == "" || args == "{}"
		}
		return true
	}
	return false
}

func findReadToolName(toolNames []string) (string, bool) {
	for _, want := range []string{"Read", "read", "read_file"} {
		for _, name := range toolNames {
			if strings.EqualFold(strings.TrimSpace(name), want) {
				return strings.TrimSpace(name), true
			}
		}
	}
	return "", false
}

func isReadToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "read_file", "readfile":
		return true
	default:
		return false
	}
}

func readCallFilePath(input map[string]any) string {
	for _, key := range []string{"file_path", "filePath", "filepath", "path"} {
		if value := strings.TrimSpace(inputString(input[key])); value != "" {
			return value
		}
	}
	return ""
}

func requestedFilePathForReadRepair(finalPrompt, finalText string) string {
	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	latestUser = systemReminderBlockPattern.ReplaceAllString(latestUser, " ")
	for _, source := range []string{finalText, latestUser} {
		if path := firstFileReference(source, finalPrompt); path != "" {
			return path
		}
	}
	return ""
}

func firstFileReference(text, finalPrompt string) string {
	for _, match := range promptFileReferencePattern.FindAllString(text, -1) {
		path := cleanPromptFileReference(match)
		if path == "" {
			continue
		}
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
		cwd := workingDirectoryFromPrompt(finalPrompt)
		if cwd == "" {
			continue
		}
		return filepath.Clean(filepath.Join(cwd, path))
	}
	return ""
}

func cleanPromptFileReference(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "`'\".,;:)]}")
	if path == "" || strings.Contains(path, "<") || strings.Contains(path, ">") {
		return ""
	}
	if strings.Contains(path, "path/to/") || strings.Contains(path, "/path/to/") {
		return ""
	}
	return path
}

func workingDirectoryFromPrompt(finalPrompt string) string {
	matches := promptWorkingDirectoryPattern.FindAllStringSubmatch(finalPrompt, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		if len(matches[i]) < 2 {
			continue
		}
		path := cleanPromptFileReference(matches[i][1])
		if filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
	}
	return ""
}

func SynthesizeAgentToolCallTextFromLaunchPromise(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) string {
	calls := SynthesizeAgentToolCallsFromLaunchPromise(finalPrompt, finalText, toolNames, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML(calls)
}

func determineAgentLaunchCount(finalPrompt string) int {
	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	latestUser = systemReminderBlockPattern.ReplaceAllString(latestUser, " ")

	// Count distinct file references
	fileMatches := promptFileReferencePattern.FindAllString(latestUser, -1)
	fileSet := make(map[string]struct{}, len(fileMatches))
	for _, m := range fileMatches {
		fileSet[m] = struct{}{}
	}
	fileCount := len(fileSet)

	// Check for complexity keywords
	lower := strings.ToLower(latestUser)
	hasImplement := strings.Contains(lower, "implement")
	hasTest := strings.Contains(lower, "test")
	hasRefactor := strings.Contains(lower, "refactor") || strings.Contains(lower, "restructure")

	// Complex: 6+ files or includes refactor/restructure or has test requirement
	if fileCount >= 6 || hasRefactor || hasTest {
		return 4
	}
	// Moderate: 3-5 files or includes implement/fix
	if fileCount >= 3 || hasImplement {
		return 2
	}
	// Simple: 1-2 files, no complex keywords
	return 1
}

func SynthesizeAgentToolCallsFromLaunchPromise(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) []toolcall.ParsedToolCall {
	if !allowMetaAgentTools {
		return nil
	}
	if RecentPromptHasBackgroundAgentLaunch(finalPrompt) && !LatestUserRequestsAdditionalAgentLaunch(finalPrompt) {
		return nil
	}
	if !LooksLikeUnexecutedAgentLaunch(finalText, finalPrompt, allowMetaAgentTools) {
		return nil
	}
	if looksLikeInvalidLegacyToolCallSyntax(finalText) {
		return nil
	}
	if toolcall.LooksLikeToolCallSyntax(finalText) {
		return nil
	}
	toolName, ok := FindBackgroundAgentToolName(toolNames)
	if !ok {
		return nil
	}
	request := compactPromptText(html.UnescapeString(LatestUserPromptBlock(finalPrompt)))
	if request == "" {
		request = compactPromptText(finalText)
	}
	if request == "" {
		return nil
	}
	allCalls := []toolcall.ParsedToolCall{
		newAgentLaunchCall(toolName, "Map implementation route", "Explore", request, "Map the implementation route, current blockers, key files, and the smallest sequence of executable steps. Read-only analysis; do not edit files or commit."),
		newAgentLaunchCall(toolName, "Review code risks", "code-reviewer", request, "Review likely correctness, compatibility, and tool-call protocol risks for this request. Read-only analysis; report concrete file/path references where possible."),
		newAgentLaunchCall(toolName, "Design end-state", "design", request, "Design the target end-state and rollout strategy. Focus on architecture, operational stability, and verification. Read-only analysis; no file edits."),
		newAgentLaunchCall(toolName, "Plan verification", "Explore", request, "Find the local verification commands, tests, and observability checks needed to prove the work. Read-only analysis; report commands and expected signals."),
	}
	count := determineAgentLaunchCount(finalPrompt)
	if count > len(allCalls) {
		count = len(allCalls)
	}
	return allCalls[:count]
}

func newAgentLaunchCall(toolName, description, subagentType, request, instruction string) toolcall.ParsedToolCall {
	return toolcall.ParsedToolCall{
		Name: toolName,
		Input: map[string]any{
			"description":       description,
			"prompt":            "User request:\n" + request + "\n\n" + instruction + "\nKeep the report concise and actionable.",
			"subagent_type":     subagentType,
			"run_in_background": true,
		},
	}
}

func SynthesizeTaskOutputToolCallTextFromTaskNotification(finalPrompt string, toolNames []string, allowMetaAgentTools bool) string {
	calls := SynthesizeTaskOutputToolCallsFromTaskNotification(finalPrompt, toolNames, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML(calls)
}

func SynthesizeTaskOutputToolCallTextFromAgentWaiting(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) string {
	calls := SynthesizeTaskOutputToolCallsFromAgentWaiting(finalPrompt, finalText, toolNames, allowMetaAgentTools)
	if len(calls) == 0 {
		return ""
	}
	return FormatParsedToolCallsAsPromptXML(calls)
}

func SynthesizeTaskOutputToolCallsFromTaskNotification(finalPrompt string, toolNames []string, allowMetaAgentTools bool) []toolcall.ParsedToolCall {
	if !allowMetaAgentTools {
		return nil
	}
	toolName, ok := FindTaskOutputToolName(toolNames)
	if !ok {
		return nil
	}
	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	if !strings.Contains(strings.ToLower(latestUser), "<task-notification") {
		return nil
	}
	ids := ExtractTaskNotificationIDs(latestUser)
	if len(ids) == 0 {
		return nil
	}
	calls := make([]toolcall.ParsedToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, toolcall.ParsedToolCall{
			Name: toolName,
			Input: map[string]any{
				"task_id": id,
				"block":   false,
				"timeout": 5000,
			},
		})
	}
	return calls
}

func SynthesizeTaskOutputToolCallsFromAgentWaiting(finalPrompt, finalText string, toolNames []string, allowMetaAgentTools bool) []toolcall.ParsedToolCall {
	if !allowMetaAgentTools || !LooksLikeAgentWaitingText(finalText) {
		return nil
	}
	if toolcall.LooksLikeToolCallSyntax(finalText) {
		return nil
	}
	toolName, ok := FindTaskOutputToolName(toolNames)
	if !ok {
		return nil
	}
	states := protocol.ExtractTaskStates(finalPrompt)
	ids := protocol.TaskIDsWithStatus(states, protocol.TaskStatusRunning)
	if len(ids) == 0 {
		return nil
	}
	if len(ids) > 4 {
		ids = ids[len(ids)-4:]
	}
	calls := make([]toolcall.ParsedToolCall, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, toolcall.ParsedToolCall{
			Name: toolName,
			Input: map[string]any{
				"task_id": id,
				"block":   false,
				"timeout": 5000,
			},
		})
	}
	return calls
}

func InvalidTaskOutputIDs(calls []toolcall.ParsedToolCall, finalPrompt string) []string {
	if len(calls) == 0 {
		return nil
	}
	allowed := allowedTaskOutputIDSet(finalPrompt)
	invalid := []string{}
	for _, call := range calls {
		if CanonicalTaskOutputToolName(call.Name) != "taskoutput" {
			continue
		}
		id := taskOutputIDFromInput(call.Input)
		if id == "" {
			continue
		}
		if _, ok := allowed[id]; !ok {
			invalid = append(invalid, id)
		}
	}
	return invalid
}

func FilterInvalidTaskOutputCalls(calls []toolcall.ParsedToolCall, finalPrompt string) []toolcall.ParsedToolCall {
	filtered, _ := FilterInvalidTaskOutputCallsWithReport(calls, finalPrompt)
	return filtered
}

func FilterInvalidTaskOutputCallsWithReport(calls []toolcall.ParsedToolCall, finalPrompt string) ([]toolcall.ParsedToolCall, []string) {
	if len(calls) == 0 {
		return nil, nil
	}
	allowed := allowedTaskOutputIDSet(finalPrompt)
	out := make([]toolcall.ParsedToolCall, 0, len(calls))
	dropped := []string{}
	for _, call := range calls {
		if CanonicalTaskOutputToolName(call.Name) != "taskoutput" {
			out = append(out, call)
			continue
		}
		id := taskOutputIDFromInput(call.Input)
		if id == "" {
			dropped = append(dropped, "")
			continue
		}
		if _, ok := allowed[id]; ok {
			out = append(out, call)
			continue
		}
		dropped = append(dropped, id)
	}
	return out, dropped
}

func InvalidTaskOutputNotice(droppedIDs []string) string {
	if len(droppedIDs) == 0 {
		return ""
	}
	return "Background result unavailable."
}

func HasAllowedTaskOutputIDs(finalPrompt string) bool {
	return len(allowedTaskOutputIDSet(finalPrompt)) > 0
}

func taskOutputIDFromInput(input map[string]any) string {
	for _, key := range []string{"task_id", "taskId", "tool_id", "toolId", "toolID"} {
		if id := protocol.CleanTaskID(inputString(input[key])); id != "" {
			return id
		}
	}
	return ""
}

// CompleteToolCallsWithSchemaDefaults injects default values from schema for
// missing optional parameters on Read tool calls (limit, offset, etc.).
func CompleteToolCallsWithSchemaDefaults(calls []toolcall.ParsedToolCall, schemas toolcall.ParameterSchemas) []toolcall.ParsedToolCall {
	if len(calls) == 0 || len(schemas) == 0 {
		return calls
	}
	out := make([]toolcall.ParsedToolCall, len(calls))
	copy(out, calls)
	for i, call := range out {
		if !isReadToolName(call.Name) {
			continue
		}
		defaults := toolcall.SchemaPropertyDefaults(schemas, call.Name)
		if len(defaults) == 0 {
			continue
		}
		if out[i].Input == nil {
			out[i].Input = make(map[string]any)
		}
		for key, defValue := range defaults {
			if _, exists := out[i].Input[key]; !exists {
				out[i].Input[key] = defValue
			}
		}
	}
	return out
}
