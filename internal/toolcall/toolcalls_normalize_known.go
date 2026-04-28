package toolcall

import (
	"fmt"
	"strings"
)

const maxExpandedReadCalls = 16

func expandKnownClientToolCalls(call ParsedToolCall) []ParsedToolCall {
	name := strings.TrimSpace(call.Name)
	if !strings.EqualFold(name, "Read") {
		return []ParsedToolCall{call}
	}
	expanded, ok := expandReadPathListCall(call)
	if !ok {
		return []ParsedToolCall{call}
	}
	return expanded
}

func expandReadPathListCall(call ParsedToolCall) ([]ParsedToolCall, bool) {
	input := call.Input
	if len(input) == 0 {
		return nil, false
	}
	raw, ok := firstReadPathListValue(input)
	if !ok {
		return nil, false
	}
	items, ok := normalizeReadPathItems(raw)
	if !ok || len(items) == 0 || len(items) > maxExpandedReadCalls {
		return nil, false
	}

	out := make([]ParsedToolCall, 0, len(items))
	for i, item := range items {
		path := strings.TrimSpace(item.path)
		if path == "" {
			continue
		}
		nextInput := cloneSchemaMap(input)
		removeReadPathAliases(nextInput)
		nextInput["file_path"] = path
		if value, ok := readIndexedValue(input["offset"], i); ok {
			nextInput["offset"] = value
		}
		if value, ok := readIndexedValue(input["limit"], i); ok {
			nextInput["limit"] = value
		}
		for key, value := range item.extra {
			if strings.TrimSpace(key) == "" {
				continue
			}
			nextInput[key] = value
		}
		out = append(out, ParsedToolCall{Name: call.Name, Input: nextInput})
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

type readPathItem struct {
	path  string
	extra map[string]any
}

func firstReadPathListValue(input map[string]any) (any, bool) {
	for _, key := range []string{"file_path", "filePath", "filepath", "path", "paths", "files", "file_paths", "filePaths"} {
		value, ok := input[key]
		if !ok || !isReadPathListValue(value) {
			continue
		}
		return value, true
	}
	return nil, false
}

func isReadPathListValue(value any) bool {
	switch v := value.(type) {
	case []any:
		return len(v) > 0
	case []string:
		return len(v) > 0
	default:
		return false
	}
}

func normalizeReadPathItems(value any) ([]readPathItem, bool) {
	switch v := value.(type) {
	case []string:
		out := make([]readPathItem, 0, len(v))
		for _, path := range v {
			out = append(out, readPathItem{path: normalizeToolPathString(path)})
		}
		return out, true
	case []any:
		out := make([]readPathItem, 0, len(v))
		for _, item := range v {
			normalized, ok := normalizeOneReadPathItem(item)
			if !ok {
				return nil, false
			}
			out = append(out, normalized)
		}
		return out, true
	default:
		return nil, false
	}
}

func normalizeOneReadPathItem(value any) (readPathItem, bool) {
	switch v := value.(type) {
	case string:
		return readPathItem{path: normalizeToolPathString(v)}, true
	case map[string]any:
		path, ok := scalarReadPathFromMap(v)
		if !ok {
			return readPathItem{}, false
		}
		extra := map[string]any{}
		for _, key := range []string{"offset", "limit"} {
			if value, ok := v[key]; ok {
				extra[key] = value
			}
		}
		return readPathItem{path: normalizeToolPathString(path), extra: extra}, true
	default:
		path := strings.TrimSpace(fmt.Sprint(v))
		if path == "" || path == "<nil>" {
			return readPathItem{}, false
		}
		return readPathItem{path: normalizeToolPathString(path)}, true
	}
}

func scalarReadPathFromMap(input map[string]any) (string, bool) {
	for _, key := range []string{"file_path", "filePath", "filepath", "path"} {
		value, ok := input[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			path := strings.TrimSpace(v)
			return path, path != ""
		default:
			path := strings.TrimSpace(fmt.Sprint(v))
			return path, path != "" && path != "<nil>"
		}
	}
	return "", false
}

func removeReadPathAliases(input map[string]any) {
	for _, key := range []string{"file_path", "filePath", "filepath", "path", "paths", "files", "file_paths", "filePaths"} {
		delete(input, key)
	}
}

func readIndexedValue(value any, index int) (any, bool) {
	switch v := value.(type) {
	case nil:
		return nil, false
	case []any:
		if index < 0 || index >= len(v) {
			return nil, false
		}
		return v[index], true
	case []string:
		if index < 0 || index >= len(v) {
			return nil, false
		}
		return v[index], true
	default:
		return value, true
	}
}

func isInvalidKnownClientToolCall(name string, input map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "execute_command":
		value, ok := inputValueForKnownRequiredField(input, "command")
		if !ok {
			return false
		}
		command, ok := value.(string)
		if !ok {
			return false
		}
		return isDegenerateShellCommand(command)
	default:
		return false
	}
}

func isDegenerateShellCommand(command string) bool {
	trimmed := strings.TrimSpace(normalizeToolStringValue(command))
	if trimmed == "" {
		return true
	}
	switch trimmed {
	case ">", "<", "|", "||", "&&", "&", ";", "(", ")", "{", "}", "[", "]",
		`"`, `'`, "`", `\"`, `""`, `''`:
		return true
	}
	if len([]rune(trimmed)) <= 3 {
		return strings.Trim(trimmed, " \t\r\n\"'`<>|&;(){}[]") == ""
	}
	return false
}
