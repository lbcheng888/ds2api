package toolcall

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func DedupeParsedToolCalls(calls []ParsedToolCall) []ParsedToolCall {
	out, _ := DedupeParsedToolCallsWithReport(calls)
	return out
}

type DedupeReport struct {
	ToolCallsDropped int
	TodoItemsDropped int
}

func (r *DedupeReport) add(other DedupeReport) {
	r.ToolCallsDropped += other.ToolCallsDropped
	r.TodoItemsDropped += other.TodoItemsDropped
}

func DedupeParsedToolCallsWithReport(calls []ParsedToolCall) ([]ParsedToolCall, DedupeReport) {
	if len(calls) < 2 {
		return calls, DedupeReport{}
	}
	seen := make(map[string]struct{}, len(calls))
	out := make([]ParsedToolCall, 0, len(calls))
	report := DedupeReport{}
	for _, call := range calls {
		key := toolCallDedupKey(call)
		if key != "" {
			if _, ok := seen[key]; ok {
				report.ToolCallsDropped++
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, call)
	}
	if report.ToolCallsDropped == 0 {
		return calls, report
	}
	return out, report
}

func NormalizeKnownToolCallInputValues(name string, input map[string]any) map[string]any {
	out, _ := NormalizeKnownToolCallInputValuesWithReport(name, input)
	return out
}

func NormalizeKnownToolCallInputValuesWithReport(name string, input map[string]any) (map[string]any, DedupeReport) {
	switch canonicalToolPolicyName(name) {
	case "todowrite", "updatetodolist":
		return normalizeTodoWriteInputWithReport(input)
	case "updateplan", "functionsupdateplan":
		return normalizeUpdatePlanInputWithReport(input)
	default:
		return input, DedupeReport{}
	}
}

func NormalizeKnownToolCallInputs(calls []ParsedToolCall) []ParsedToolCall {
	out, _ := NormalizeKnownToolCallInputsWithReport(calls)
	return out
}

func NormalizeKnownToolCallInputsWithReport(calls []ParsedToolCall) ([]ParsedToolCall, DedupeReport) {
	if len(calls) == 0 {
		return calls, DedupeReport{}
	}
	report := DedupeReport{}
	var changed bool
	out := make([]ParsedToolCall, len(calls))
	for i, call := range calls {
		out[i] = call
		if call.Input == nil {
			continue
		}
		normalized, itemReport := NormalizeKnownToolCallInputValuesWithReport(call.Name, call.Input)
		report.add(itemReport)
		if reflect.DeepEqual(normalized, call.Input) {
			continue
		}
		changed = true
		out[i].Input = normalized
	}
	if !changed {
		return calls, report
	}
	return out, report
}

func toolCallDedupKey(call ParsedToolCall) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return ""
	}
	if IsBackgroundAgentToolName(name) {
		if key := backgroundAgentDedupKey(name, call.Input); key != "" {
			return key
		}
	}
	if key := knownToolPrimaryDedupKey(name, call.Input); key != "" {
		return key
	}
	var input any = call.Input
	if call.Input == nil {
		input = map[string]any{}
	}
	args, err := json.Marshal(normalizeDedupValue(input))
	if err != nil {
		return ""
	}
	return canonicalToolPolicyName(name) + "\x00args:" + string(args)
}

func knownToolPrimaryDedupKey(name string, input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	keyName := canonicalToolPolicyName(name)
	switch keyName {
	case "read", "readfile":
		return readToolDedupKey(keyName, input)
	case "grep":
		return grepToolDedupKey(keyName, input)
	case "search":
		return searchToolDedupKey(keyName, input)
	case "glob", "globe":
		return globToolDedupKey(keyName, input)
	case "bash", "executecommand", "execcommand", "terminal", "shell":
		command := canonicalCommandText(firstInputStringForDedup(input, "command", "cmd"))
		if command == "" {
			return ""
		}
		cwd := strings.TrimSpace(firstInputStringForDedup(input, "cwd", "workdir", "work_dir"))
		return keyName + "\x00command:" + cwd + "\x00" + command
	case "taskcreate", "taskupdate":
		task := canonicalDedupText(firstInputStringForDedup(input, "subject", "content", "title", "task", "description"))
		if task == "" {
			return ""
		}
		return keyName + "\x00task:" + task
	default:
		return ""
	}
}

func readToolDedupKey(keyName string, input map[string]any) string {
	path := readPathForDedup(input)
	if path == "" {
		return ""
	}
	return keyName + "\x00file_path:" + dedupValueSignature(path) +
		dedupIntegerFieldSegment(input, "offset", "offset") +
		dedupIntegerFieldSegment(input, "limit", "limit")
}

func grepToolDedupKey(keyName string, input map[string]any) string {
	pattern, ok := firstDedupFieldSignature(input, "pattern", "regexp", "regex", "query", "q")
	if !ok {
		return ""
	}
	return keyName + "\x00pattern:" + pattern +
		dedupPathFieldSegment(input, "path", "path", "file_path", "filePath", "cwd", "workdir", "working_directory") +
		dedupValueFieldSegment(input, "glob", "glob") +
		dedupValueFieldSegment(input, "output_mode", "output_mode", "outputMode") +
		dedupValueFieldSegment(input, "type", "type") +
		dedupValueFieldSegment(input, "case_sensitive", "case_sensitive", "caseSensitive")
}

func searchToolDedupKey(keyName string, input map[string]any) string {
	query, ok := firstDedupFieldSignature(input, "query", "q", "pattern", "regexp", "regex")
	if !ok {
		return ""
	}
	return keyName + "\x00query:" + query +
		dedupPathFieldSegment(input, "path", "path", "file_path", "filePath", "cwd", "workdir", "working_directory") +
		dedupValueFieldSegment(input, "glob", "glob") +
		dedupValueFieldSegment(input, "include", "include") +
		dedupValueFieldSegment(input, "exclude", "exclude") +
		dedupValueFieldSegment(input, "limit", "limit") +
		dedupValueFieldSegment(input, "case_sensitive", "case_sensitive", "caseSensitive")
}

func globToolDedupKey(keyName string, input map[string]any) string {
	pattern, ok := firstDedupFieldSignature(input, "pattern", "glob")
	if !ok {
		return ""
	}
	return keyName + "\x00pattern:" + pattern +
		dedupPathFieldSegment(input, "path", "path", "cwd", "workdir", "working_directory") +
		dedupValueFieldSegment(input, "case_sensitive", "case_sensitive", "caseSensitive")
}

func normalizeTodoWriteInputWithReport(input map[string]any) (map[string]any, DedupeReport) {
	if len(input) == 0 {
		return input, DedupeReport{}
	}
	for key, value := range input {
		if canonicalSchemaPropertyName(key) != "todos" {
			continue
		}
		normalized, dropped, changed := dedupeTodoListValue(value)
		report := DedupeReport{TodoItemsDropped: dropped}
		if !changed {
			return input, report
		}
		out := cloneAnyMap(input)
		out[key] = normalized
		return out, report
	}
	return input, DedupeReport{}
}

func normalizeUpdatePlanInputWithReport(input map[string]any) (map[string]any, DedupeReport) {
	if len(input) == 0 {
		return input, DedupeReport{}
	}
	for key, value := range input {
		if canonicalSchemaPropertyName(key) != "plan" {
			continue
		}
		normalized, dropped, changed := dedupeTodoListValue(value)
		report := DedupeReport{TodoItemsDropped: dropped}
		if !changed {
			return input, report
		}
		out := cloneAnyMap(input)
		out[key] = normalized
		return out, report
	}
	return input, DedupeReport{}
}

func dedupeTodoListValue(value any) (any, int, bool) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return value, 0, false
	}
	completed := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !todoItemIsCompleted(item) {
			continue
		}
		if key := todoItemDedupKey(item); key != "" {
			completed[key] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]any, 0, len(items))
	changed := false
	dropped := 0
	for _, item := range items {
		key := todoItemDedupKey(item)
		if todoItemIsCompleted(item) {
			changed = true
			dropped++
			continue
		}
		if key != "" {
			if _, exists := completed[key]; exists {
				changed = true
				dropped++
				continue
			}
			if _, exists := seen[key]; exists {
				changed = true
				dropped++
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, item)
	}
	if !changed {
		return value, 0, false
	}
	return out, dropped, true
}

func todoItemIsCompleted(item any) bool {
	switch v := item.(type) {
	case string:
		return isCompletedTodoStatus(v)
	case map[string]any:
		for _, field := range []string{"status", "state"} {
			if isCompletedTodoStatus(firstInputStringForDedup(v, field)) {
				return true
			}
		}
		for _, field := range []string{"completed", "done", "resolved"} {
			if b, ok := v[field].(bool); ok && b {
				return true
			}
		}
	}
	return false
}

func isCompletedTodoStatus(value string) bool {
	key := canonicalDedupText(value)
	switch key {
	case "completed", "complete", "done", "finished", "resolved", "closed", "success", "succeeded":
		return true
	default:
		return strings.HasPrefix(key, "[x]") ||
			strings.HasPrefix(key, "- [x]") ||
			strings.HasPrefix(key, "-[x]") ||
			strings.HasPrefix(key, "* [x]") ||
			strings.HasPrefix(key, "*[x]")
	}
}

func todoItemDedupKey(item any) string {
	switch v := item.(type) {
	case string:
		if text := canonicalDedupText(v); text != "" {
			return "text:" + text
		}
	case map[string]any:
		text := canonicalDedupText(firstInputStringForDedup(v, "content", "activeForm", "step", "title", "subject", "task", "description"))
		if text != "" {
			return "item:" + text
		}
	}
	b, err := json.Marshal(normalizeDedupValue(item))
	if err != nil {
		return ""
	}
	if string(b) == "null" || string(b) == "{}" || string(b) == "[]" || string(b) == `""` {
		return ""
	}
	return "json:" + string(b)
}

func firstDedupFieldSignature(input map[string]any, fields ...string) (string, bool) {
	value, ok := firstInputValueForDedup(input, fields...)
	if !ok || isEmptyKnownRequiredValue(value) {
		return "", false
	}
	return dedupValueSignature(value), true
}

func dedupPathFieldSegment(input map[string]any, label string, fields ...string) string {
	raw, ok := firstInputValueForDedup(input, fields...)
	if !ok {
		return "\x00" + label + ":<absent>"
	}
	value, ok := raw.(string)
	if !ok {
		return "\x00" + label + ":<absent>"
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "\x00" + label + ":<absent>"
	}
	return "\x00" + label + ":" + dedupValueSignature(normalizeToolPathString(value))
}

func dedupValueFieldSegment(input map[string]any, label string, fields ...string) string {
	value, ok := firstInputValueForDedup(input, fields...)
	if !ok || isEmptyKnownRequiredValue(value) {
		return "\x00" + label + ":<absent>"
	}
	return "\x00" + label + ":" + dedupValueSignature(value)
}

func dedupIntegerFieldSegment(input map[string]any, label string, fields ...string) string {
	value, ok := firstInputValueForDedup(input, fields...)
	if !ok || isEmptyKnownRequiredValue(value) {
		return "\x00" + label + ":<absent>"
	}
	if normalized, valid := normalizeInteger(value); valid {
		return "\x00" + label + ":" + dedupValueSignature(normalized)
	}
	return "\x00" + label + ":" + dedupValueSignature(value)
}

func firstInputValueForDedup(input map[string]any, fields ...string) (any, bool) {
	for _, field := range fields {
		if value, ok := input[field]; ok {
			return value, true
		}
		want := canonicalSchemaPropertyName(field)
		var found any
		var foundKey string
		var ok bool
		for key, value := range input {
			if canonicalSchemaPropertyName(key) == want {
				if !ok || key < foundKey {
					found = value
					foundKey = key
					ok = true
				}
			}
		}
		if ok {
			return found, true
		}
	}
	return nil, false
}

func readPathForDedup(input map[string]any) string {
	for _, field := range []string{"path", "filePath", "file_path", "filepath"} {
		value, ok := firstInputValueForDedup(input, field)
		if !ok {
			continue
		}
		path, ok := value.(string)
		if !ok {
			continue
		}
		path = normalizeToolPathString(strings.TrimSpace(path))
		if path != "" {
			return path
		}
	}
	return ""
}

func dedupValueSignature(value any) string {
	b, err := json.Marshal(normalizeDedupValue(value))
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(b)
}

func backgroundAgentDedupKey(name string, input map[string]any) string {
	if len(input) == 0 {
		return ""
	}
	description := canonicalDedupText(inputStringForDedup(input, "description"))
	subagentType := canonicalDedupText(inputStringForDedup(input, "subagent_type"))
	if description != "" {
		return canonicalToolPolicyName(name) + "\x00agent-desc:" + subagentType + "\x00" + description
	}
	prompt := canonicalDedupText(inputStringForDedup(input, "prompt"))
	if prompt == "" {
		return ""
	}
	return canonicalToolPolicyName(name) + "\x00agent-prompt:" + subagentType + "\x00" + prompt
}

func firstInputStringForDedup(input map[string]any, fields ...string) string {
	for _, field := range fields {
		if value := inputStringForDedup(input, field); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func inputStringForDedup(input map[string]any, field string) string {
	want := canonicalSchemaPropertyName(field)
	for key, value := range input {
		if canonicalSchemaPropertyName(key) != want {
			continue
		}
		switch v := value.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		default:
			if value == nil {
				return ""
			}
			return fmt.Sprint(value)
		}
	}
	return ""
}

func canonicalDedupText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func canonicalCommandText(s string) string {
	command := strings.TrimSpace(s)
	if command == "" {
		return ""
	}
	return stripTrailingNoopShellRedirections(command)
}

func stripTrailingNoopShellRedirections(command string) string {
	for {
		trimmed := strings.TrimSpace(command)
		updated := strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
		for _, suffix := range []string{
			"2>&1",
			"1>&2",
			"2>/dev/null",
			"1>/dev/null",
			">/dev/null",
			"&>/dev/null",
			"2> /dev/null",
			"1> /dev/null",
			"> /dev/null",
			"&> /dev/null",
		} {
			if strings.HasSuffix(updated, suffix) {
				updated = strings.TrimSpace(strings.TrimSuffix(updated, suffix))
				break
			}
		}
		if updated == trimmed || updated == command {
			return updated
		}
		command = updated
	}
}

func normalizeDedupValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = normalizeDedupValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = normalizeDedupValue(child)
		}
		return out
	default:
		return v
	}
}

func cloneAnyMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
