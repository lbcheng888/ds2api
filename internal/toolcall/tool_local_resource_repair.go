package toolcall

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func rewriteLocalResourceReadCallForAvailable(call ParsedToolCall, availableToolNames []string) (ParsedToolCall, bool) {
	if !isReadMCPResourceToolName(call.Name) {
		return call, false
	}
	path := localFilePathFromResourceInput(call.Input)
	if strings.TrimSpace(path) == "" {
		return call, false
	}
	if name, ok := preferredLocalReadToolName(availableToolNames); ok {
		return ParsedToolCall{Name: name, Input: localReadToolInput(name, path, call.Input)}, true
	}
	if name, ok := preferredShellReadToolName(availableToolNames); ok {
		return ParsedToolCall{Name: name, Input: shellReadToolInput(name, path, call.Input)}, true
	}
	return call, false
}

func rewriteUnavailableLocalReadFileCallForAvailable(call ParsedToolCall, availableToolNames []string) (ParsedToolCall, bool) {
	if !isReadFileToolName(call.Name) {
		return call, false
	}
	if _, ok := resolveToolNameForAvailable(call.Name, availableToolNames); ok {
		return call, false
	}
	path := localPathFromReadFileInput(call.Input)
	if strings.TrimSpace(path) == "" {
		return call, false
	}
	if name, ok := preferredLocalReadToolName(availableToolNames); ok {
		return ParsedToolCall{Name: name, Input: localReadToolInput(name, path, call.Input)}, true
	}
	if name, ok := preferredShellReadToolName(availableToolNames); ok {
		return ParsedToolCall{Name: name, Input: shellReadToolInput(name, path, call.Input)}, true
	}
	return call, false
}

func RewriteCallsForAvailableTools(calls []ParsedToolCall, availableToolNames []string) []ParsedToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(calls))
	for _, call := range calls {
		if call.Input == nil {
			call.Input = map[string]any{}
		}
		if rewritten, ok := rewriteUnavailableLocalReadFileCallForAvailable(call, availableToolNames); ok {
			call = rewritten
		}
		if rewritten, ok := rewriteLocalResourceReadCallForAvailable(call, availableToolNames); ok {
			call = rewritten
		}
		out = append(out, call)
	}
	return out
}

func isReadMCPResourceToolName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "read_mcp_resource" || strings.HasSuffix(lower, ".read_mcp_resource")
}

func isReadFileToolName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "read_file", "readfile":
		return true
	default:
		return false
	}
}

func localPathFromReadFileInput(input map[string]any) string {
	for _, key := range []string{"path", "filePath", "file_path", "filepath"} {
		value, ok := inputValueForKnownRequiredField(input, key)
		if !ok {
			continue
		}
		path, _ := value.(string)
		path = normalizeToolPathString(strings.TrimSpace(path))
		if path != "" {
			return path
		}
	}
	return ""
}

func localFilePathFromResourceInput(input map[string]any) string {
	for _, key := range []string{"uri", "url"} {
		value, ok := inputValueForKnownRequiredField(input, key)
		if !ok {
			continue
		}
		raw, _ := value.(string)
		if path := parseLocalFileURI(raw); path != "" {
			return path
		}
		if path := parseCodexSkillURI(raw); path != "" {
			return path
		}
		if path := parseLocalDiskPath(raw); path != "" {
			return path
		}
	}
	if nested, ok := input["tool_parameters"].(map[string]any); ok {
		return localFilePathFromResourceInput(nested)
	}
	return ""
}

func parseLocalFileURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "file") {
		return ""
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return ""
	}
	path := u.Path
	if unescaped, err := url.PathUnescape(path); err == nil {
		path = unescaped
	}
	return normalizeToolPathString(path)
}

func parseCodexSkillURI(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "skill") {
		return ""
	}
	skillName := strings.TrimSpace(u.Host)
	if unescaped, err := url.PathUnescape(skillName); err == nil {
		skillName = unescaped
	}
	if skillName == "" {
		return ""
	}
	rel := strings.TrimPrefix(u.Path, "/")
	if unescaped, err := url.PathUnescape(rel); err == nil {
		rel = unescaped
	}
	if strings.TrimSpace(rel) == "" {
		rel = "SKILL.md"
	}
	rel = filepath.Clean(rel)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return normalizeToolPathString(filepath.Join(home, ".codex", "skills", skillName, rel))
}

func parseLocalDiskPath(raw string) string {
	raw = normalizeToolPathString(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		raw = filepath.Join(home, strings.TrimPrefix(raw, "~/"))
	}
	if !filepath.IsAbs(raw) {
		return ""
	}
	return filepath.Clean(raw)
}

func preferredLocalReadToolName(availableToolNames []string) (string, bool) {
	for _, want := range []string{"Read", "read", "read_file"} {
		if name, ok := findAvailableToolName(availableToolNames, want); ok {
			return name, true
		}
	}
	return "", false
}

func preferredShellReadToolName(availableToolNames []string) (string, bool) {
	for _, want := range []string{"exec_command", "Bash", "bash", "execute_command"} {
		if name, ok := findAvailableToolName(availableToolNames, want); ok {
			return name, true
		}
	}
	return "", false
}

func findAvailableToolName(availableToolNames []string, want string) (string, bool) {
	if len(availableToolNames) == 0 {
		return "", false
	}
	for _, candidate := range availableToolNames {
		if strings.EqualFold(strings.TrimSpace(candidate), want) {
			return strings.TrimSpace(candidate), true
		}
	}
	return "", false
}

func localReadToolInput(name, path string, original map[string]any) map[string]any {
	input := map[string]any{}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read":
		if strings.TrimSpace(name) == "Read" {
			input["file_path"] = path
		} else {
			input["filePath"] = path
		}
	case "read_file":
		input["path"] = path
	default:
		input["file_path"] = path
	}
	copyOptionalToolInput(input, original, "offset")
	copyOptionalToolInput(input, original, "limit")
	return input
}

func shellReadToolInput(name, path string, original map[string]any) map[string]any {
	cmd := fmt.Sprintf("sed -n '%s' %s", shellReadLineRange(original), shellQuote(path))
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "exec_command":
		return map[string]any{"cmd": cmd}
	default:
		return map[string]any{
			"command":     cmd,
			"description": "Read local file",
		}
	}
}

func shellReadLineRange(input map[string]any) string {
	offset := nonNegativeIntInput(input, "offset")
	limit := positiveIntInput(input, "limit", 200)
	start := offset + 1
	end := start + limit - 1
	return fmt.Sprintf("%d,%dp", start, end)
}

func positiveIntInput(input map[string]any, key string, fallback int) int {
	n := nonNegativeIntInput(input, key)
	if n <= 0 {
		return fallback
	}
	return n
}

func nonNegativeIntInput(input map[string]any, key string) int {
	value, ok := inputValueForKnownRequiredField(input, key)
	if !ok {
		return 0
	}
	switch v := value.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func copyOptionalToolInput(dst map[string]any, src map[string]any, key string) {
	value, ok := inputValueForKnownRequiredField(src, key)
	if !ok || isEmptyKnownRequiredValue(value) {
		return
	}
	dst[key] = value
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
