package claudecode

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"ds2api/internal/toolcall"
)

const ExplorationGuardDuplicateReason = "duplicate_exploration_tool_call"

var explorationGuardHistoryHeaderPattern = regexp.MustCompile(`(?m)^===\s+\d+\.\s+([A-Z]+)\s+===\s*$`)

type ExplorationGuardInput struct {
	FinalPrompt string
	Calls       []toolcall.ParsedToolCall
}

type ExplorationGuardResult struct {
	Blocked           bool
	Reason            string
	DuplicateKeys     []string
	RecoveryDirective string
}

func DetectRepeatedExploration(in ExplorationGuardInput) ExplorationGuardResult {
	seen := map[string]struct{}{}
	for _, call := range explorationGuardHistoryCalls(in.FinalPrompt) {
		for _, key := range explorationGuardKeys(call) {
			seen[key] = struct{}{}
		}
	}

	duplicateSet := map[string]struct{}{}
	duplicates := make([]string, 0)
	for _, call := range in.Calls {
		for _, key := range explorationGuardKeys(call) {
			if _, exists := seen[key]; exists {
				if _, added := duplicateSet[key]; !added {
					duplicateSet[key] = struct{}{}
					duplicates = append(duplicates, key)
				}
				continue
			}
			seen[key] = struct{}{}
		}
	}
	if len(duplicates) == 0 {
		return ExplorationGuardResult{}
	}
	return ExplorationGuardResult{
		Blocked:           true,
		Reason:            ExplorationGuardDuplicateReason,
		DuplicateKeys:     duplicates,
		RecoveryDirective: generateRecoveryDirective(duplicates),
	}
}

func explorationGuardHistoryCalls(finalPrompt string) []toolcall.ParsedToolCall {
	segments := explorationGuardCurrentTurnAssistantSegments(finalPrompt)
	segments = append(segments, explorationGuardHistoryTranscriptAssistantSegments(finalPrompt)...)
	if len(segments) == 0 {
		return nil
	}
	var calls []toolcall.ParsedToolCall
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			continue
		}
		parsed := toolcall.ParseStandaloneToolCallsDetailed(segment, nil)
		calls = append(calls, parsed.Calls...)
	}
	return calls
}

func explorationGuardHistoryTranscriptAssistantSegments(finalPrompt string) []string {
	const historyTitle = "# DS2API_HISTORY.txt"
	if !strings.Contains(finalPrompt, historyTitle) {
		return nil
	}
	historyText := finalPrompt
	if idx := strings.LastIndex(historyText, "<｜begin▁of▁sentence｜>"); idx >= 0 {
		historyText = historyText[:idx]
	}
	matches := explorationGuardHistoryHeaderPattern.FindAllStringSubmatchIndex(historyText, -1)
	if len(matches) == 0 {
		return nil
	}
	first := 0
	for i, match := range matches {
		if len(match) >= 4 && strings.EqualFold(historyText[match[2]:match[3]], "USER") {
			first = i
		}
	}
	segments := make([]string, 0)
	for i := first; i < len(matches); i++ {
		match := matches[i]
		if len(match) < 4 || !strings.EqualFold(historyText[match[2]:match[3]], "ASSISTANT") {
			continue
		}
		end := len(historyText)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		if segment := strings.TrimSpace(historyText[match[1]:end]); segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func explorationGuardCurrentTurnAssistantSegments(finalPrompt string) []string {
	const assistantMarker = "<｜Assistant｜>"
	const nextRolePrefix = "<｜"
	turn := latestConversationTurnBlock(finalPrompt)
	segments := make([]string, 0)
	for {
		start := strings.Index(turn, assistantMarker)
		if start < 0 {
			return segments
		}
		tail := turn[start+len(assistantMarker):]
		end := strings.Index(tail, nextRolePrefix)
		if end < 0 {
			segments = append(segments, tail)
			return segments
		}
		segments = append(segments, tail[:end])
		turn = tail[end:]
	}
}

func explorationGuardKeys(call toolcall.ParsedToolCall) []string {
	if toolcall.IsTaskTrackingToolName(call.Name) {
		return nil
	}
	input := call.Input
	if input == nil {
		input = map[string]any{}
	}
	switch explorationGuardCanonicalName(call.Name) {
	case "read", "readfile":
		return explorationGuardSingleKey(explorationGuardReadKey(input))
	case "grep":
		return explorationGuardSingleKey(explorationGuardSearchLikeKey("grep", input, []string{"pattern", "regexp", "regex", "query", "q"},
			[]string{"scope", "glob", "include", "exclude", "type", "output_mode", "outputMode", "case_sensitive", "caseSensitive", "limit", "max_results", "maxResults"}))
	case "search":
		return explorationGuardSingleKey(explorationGuardSearchLikeKey("search", input, []string{"query", "q", "pattern", "regexp", "regex"},
			[]string{"scope", "glob", "include", "exclude", "type", "output_mode", "outputMode", "case_sensitive", "caseSensitive", "limit", "max_results", "maxResults"}))
	case "glob", "globe":
		return explorationGuardSingleKey(explorationGuardSearchLikeKey("glob", input, []string{"pattern", "glob"},
			[]string{"scope", "include", "exclude", "type", "case_sensitive", "caseSensitive", "limit", "max_results", "maxResults"}))
	case "bash", "shell", "terminal", "sh", "execcommand", "executecommand":
		return explorationGuardShellKeys(input)
	case "agent", "task", "subagent", "newtask":
		return explorationGuardSingleKey(explorationGuardAgentKey(input))
	default:
		return nil
	}
}

func explorationGuardSingleKey(key string, ok bool) []string {
	if !ok {
		return nil
	}
	return []string{key}
}

func explorationGuardReadKey(input map[string]any) (string, bool) {
	path := explorationGuardPathValue(input, "path", "filePath", "file_path", "filepath")
	if path == "" {
		return "", false
	}
	return "read|path:" + explorationGuardStringSignature(path) +
		"|offset:" + explorationGuardIntegerFieldSignature(input, "offset") +
		"|limit:" + explorationGuardIntegerFieldSignature(input, "limit"), true
}

func explorationGuardSearchLikeKey(kind string, input map[string]any, patternFields []string, scopeFields []string) (string, bool) {
	pattern, ok := explorationGuardFirstFieldSignature(input, patternFields...)
	if !ok {
		return "", false
	}
	path := explorationGuardPathFieldSignature(input, "path", "file_path", "filePath", "cwd", "workdir", "working_directory")
	scope := explorationGuardScopeSignature(input, scopeFields...)
	return kind + "|pattern:" + pattern + "|path:" + path + "|scope:" + scope, true
}

func explorationGuardShellKeys(input map[string]any) []string {
	command := explorationGuardStringValue(input, "command", "cmd", "script")
	if command == "" {
		return nil
	}
	segments := explorationGuardCanonicalExplorationShellSegments(command)
	if len(segments) == 0 {
		return nil
	}
	cwd := explorationGuardPathFieldSignature(input, "cwd", "workdir", "work_dir", "working_directory")
	keys := make([]string, 0, len(segments))
	for _, segment := range segments {
		keys = append(keys, "shell|cwd:"+cwd+"|command:"+explorationGuardStringSignature(segment))
		if unscopedGit := explorationGuardUnscopedGitInspectionSegment(segment); unscopedGit != "" {
			keys = append(keys, "shell|git:"+explorationGuardStringSignature(unscopedGit))
		}
	}
	return keys
}

func explorationGuardAgentKey(input map[string]any) (string, bool) {
	subagentType := explorationGuardCanonicalText(explorationGuardStringValue(input, "subagent_type", "subagentType", "type"))
	if description := explorationGuardCanonicalText(explorationGuardStringValue(input, "description")); description != "" {
		return "agent|subagent:" + explorationGuardStringSignature(subagentType) +
			"|description:" + explorationGuardStringSignature(description), true
	}
	if prompt := explorationGuardCanonicalText(explorationGuardStringValue(input, "prompt")); prompt != "" {
		return "agent|subagent:" + explorationGuardStringSignature(subagentType) +
			"|prompt:" + explorationGuardStringSignature(prompt), true
	}
	return "", false
}

func explorationGuardCanonicalExplorationShellSegments(command string) []string {
	if explorationGuardShellCommandMayWrite(command) {
		return nil
	}
	segments := explorationGuardShellCommandSegments(command)
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		executable := explorationGuardShellExecutable(segment)
		if executable == "" || executable == "cd" || executable == "echo" {
			continue
		}
		if executable == "git" {
			canonical, ok := explorationGuardCanonicalGitInspectionSegment(segment)
			if !ok {
				return nil
			}
			out = append(out, canonical)
			continue
		}
		if !explorationGuardIsExplorationExecutable(executable) {
			return nil
		}
		out = append(out, explorationGuardCanonicalShellInspectionSegment(segment))
	}
	return out
}

func explorationGuardShellCommandSegments(command string) []string {
	normalized := strings.ReplaceAll(command, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "&&", "\n")
	normalized = strings.ReplaceAll(normalized, ";", "\n")
	parts := strings.Split(normalized, "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func explorationGuardShellExecutable(segment string) string {
	if pipe := strings.Index(segment, "|"); pipe >= 0 {
		segment = segment[:pipe]
	}
	fields := strings.Fields(strings.TrimSpace(segment))
	for len(fields) > 0 {
		token := strings.Trim(fields[0], `"'()`)
		fields = fields[1:]
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if lower == "time" || lower == "command" || lower == "env" || lower == "sudo" || lower == "noglob" {
			continue
		}
		if strings.Contains(token, "=") && !strings.Contains(token, "/") {
			continue
		}
		return strings.ToLower(filepath.Base(token))
	}
	return ""
}

func explorationGuardIsExplorationExecutable(executable string) bool {
	switch strings.ToLower(strings.TrimSpace(executable)) {
	case "rg", "ripgrep", "grep", "egrep", "fgrep", "find", "ls":
		return true
	default:
		return false
	}
}

func explorationGuardCanonicalShellInspectionSegment(segment string) string {
	fields := explorationGuardShellFieldsWithoutNoopRedirection(segment)
	if len(fields) == 0 {
		return strings.TrimSpace(segment)
	}
	return strings.Join(fields, " ")
}

func explorationGuardCanonicalGitInspectionSegment(segment string) (string, bool) {
	fields := explorationGuardShellFieldsWithoutNoopRedirection(segment)
	if len(fields) == 0 {
		return "", false
	}
	gitIndex := explorationGuardShellExecutableFieldIndex(fields)
	if gitIndex < 0 || strings.ToLower(filepath.Base(strings.Trim(fields[gitIndex], `"'()`))) != "git" {
		return "", false
	}
	fields = fields[gitIndex+1:]
	scope := make([]string, 0, 2)
	for len(fields) > 0 {
		token := explorationGuardCleanShellToken(fields[0])
		if token == "" {
			fields = fields[1:]
			continue
		}
		switch {
		case token == "-C" && len(fields) >= 2:
			scope = append(scope, "C="+explorationGuardCleanGitPath(fields[1]))
			fields = fields[2:]
			continue
		case strings.HasPrefix(token, "-C") && len(token) > 2:
			scope = append(scope, "C="+explorationGuardCleanGitPath(token[2:]))
			fields = fields[1:]
			continue
		case token == "--git-dir" && len(fields) >= 2:
			scope = append(scope, "git-dir="+explorationGuardCleanGitPath(fields[1]))
			fields = fields[2:]
			continue
		case strings.HasPrefix(token, "--git-dir="):
			scope = append(scope, "git-dir="+explorationGuardCleanGitPath(strings.TrimPrefix(token, "--git-dir=")))
			fields = fields[1:]
			continue
		case token == "--work-tree" && len(fields) >= 2:
			scope = append(scope, "work-tree="+explorationGuardCleanGitPath(fields[1]))
			fields = fields[2:]
			continue
		case strings.HasPrefix(token, "--work-tree="):
			scope = append(scope, "work-tree="+explorationGuardCleanGitPath(strings.TrimPrefix(token, "--work-tree=")))
			fields = fields[1:]
			continue
		case token == "-c" && len(fields) >= 2:
			fields = fields[2:]
			continue
		case token == "--no-pager" || token == "--paginate":
			fields = fields[1:]
			continue
		case strings.HasPrefix(token, "-"):
			fields = fields[1:]
			continue
		default:
			subcommand := strings.ToLower(token)
			args := explorationGuardCleanShellTokens(fields[1:])
			if !explorationGuardIsReadOnlyGitSubcommand(subcommand, args) {
				return "", false
			}
			parts := []string{"git"}
			parts = append(parts, scope...)
			parts = append(parts, subcommand)
			parts = append(parts, args...)
			return strings.Join(parts, " "), true
		}
	}
	return "", false
}

func explorationGuardIsReadOnlyGitSubcommand(subcommand string, args []string) bool {
	switch subcommand {
	case "status", "log", "show", "rev-parse", "ls-files":
		return true
	case "diff":
		for _, arg := range args {
			if arg == "--output" || strings.HasPrefix(arg, "--output=") {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func explorationGuardUnscopedGitInspectionSegment(segment string) string {
	fields := strings.Fields(segment)
	if len(fields) == 0 || fields[0] != "git" {
		return ""
	}
	out := []string{"git"}
	for i := 1; i < len(fields); i++ {
		field := fields[i]
		if strings.HasPrefix(field, "C=") || strings.HasPrefix(field, "git-dir=") || strings.HasPrefix(field, "work-tree=") {
			continue
		}
		out = append(out, field)
	}
	if len(out) == len(fields) {
		return strings.Join(out, " ")
	}
	if len(out) <= 1 {
		return ""
	}
	return strings.Join(out, " ")
}

func explorationGuardShellExecutableFieldIndex(fields []string) int {
	for i, field := range fields {
		token := explorationGuardCleanShellToken(field)
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)
		if lower == "time" || lower == "command" || lower == "env" || lower == "sudo" || lower == "noglob" {
			continue
		}
		if strings.Contains(token, "=") && !strings.Contains(token, "/") {
			continue
		}
		return i
	}
	return -1
}

func explorationGuardShellFieldsWithoutNoopRedirection(segment string) []string {
	if pipe := strings.Index(segment, "|"); pipe >= 0 {
		segment = segment[:pipe]
	}
	fields := strings.Fields(strings.TrimSpace(segment))
	out := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		field := explorationGuardCleanShellToken(fields[i])
		if explorationGuardIsNoopRedirectionToken(field) {
			continue
		}
		if explorationGuardIsNoopRedirectionOperator(field) && i+1 < len(fields) && explorationGuardCleanShellToken(fields[i+1]) == "/dev/null" {
			i++
			continue
		}
		out = append(out, field)
	}
	return out
}

func explorationGuardCleanShellTokens(fields []string) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		cleaned := explorationGuardCleanShellToken(field)
		if cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func explorationGuardCleanShellToken(token string) string {
	return strings.Trim(strings.TrimSpace(token), `"'()`)
}

func explorationGuardCleanGitPath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "⁄", "/"))
	path = strings.Trim(path, `"'()`)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func explorationGuardIsNoopRedirectionToken(token string) bool {
	switch token {
	case "2>&1", "1>&2", "2>/dev/null", "1>/dev/null", ">/dev/null", "&>/dev/null":
		return true
	default:
		return false
	}
}

func explorationGuardIsNoopRedirectionOperator(token string) bool {
	switch token {
	case ">", "1>", "2>", "&>":
		return true
	default:
		return false
	}
}

func explorationGuardShellCommandMayWrite(command string) bool {
	lower := strings.ToLower(command)
	if explorationGuardHasWriteRedirection(command) {
		return true
	}
	if strings.Contains(lower, "| tee") || strings.Contains(lower, " tee ") ||
		strings.Contains(lower, "apply_patch") || strings.Contains(lower, "gofmt -w") ||
		strings.Contains(lower, "sed -i") || strings.Contains(lower, "perl -pi") ||
		strings.Contains(lower, " -delete") {
		return true
	}
	for _, word := range []string{"rm", "mv", "cp", "mkdir", "touch", "chmod", "chown", "install"} {
		if explorationGuardContainsShellWord(lower, word) {
			return true
		}
	}
	return false
}

func explorationGuardHasWriteRedirection(command string) bool {
	fields := strings.Fields(command)
	for i, field := range fields {
		cleaned := strings.Trim(field, `"'()[]{};`)
		if !strings.Contains(cleaned, ">") {
			continue
		}
		if cleaned == "2>&1" || cleaned == "1>&2" {
			continue
		}
		if strings.Contains(cleaned, "/dev/null") {
			continue
		}
		if (cleaned == ">" || cleaned == "1>" || cleaned == "2>" || cleaned == "&>") && i+1 < len(fields) && strings.Trim(fields[i+1], `"'()[]{};`) == "/dev/null" {
			continue
		}
		return true
	}
	return false
}

func explorationGuardContainsShellWord(command, word string) bool {
	for _, field := range strings.Fields(command) {
		cleaned := strings.Trim(field, `"'()[]{};|&`)
		if strings.ToLower(filepath.Base(cleaned)) == word {
			return true
		}
	}
	return false
}

func explorationGuardFirstFieldSignature(input map[string]any, fields ...string) (string, bool) {
	value, ok := explorationGuardFirstValue(input, fields...)
	if !ok || explorationGuardValueIsEmpty(value) {
		return "", false
	}
	return explorationGuardValueSignature(value), true
}

func explorationGuardPathFieldSignature(input map[string]any, fields ...string) string {
	path := explorationGuardPathValue(input, fields...)
	if path == "" {
		return "<absent>"
	}
	return explorationGuardStringSignature(path)
}

func explorationGuardScopeSignature(input map[string]any, fields ...string) string {
	parts := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, field := range fields {
		canonical := explorationGuardCanonicalField(field)
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		value, ok := explorationGuardFirstValue(input, field)
		if !ok || explorationGuardValueIsEmpty(value) {
			parts = append(parts, canonical+":<absent>")
			continue
		}
		parts = append(parts, canonical+":"+explorationGuardValueSignature(value))
	}
	return strings.Join(parts, ",")
}

func explorationGuardIntegerFieldSignature(input map[string]any, field string) string {
	value, ok := explorationGuardFirstValue(input, field)
	if !ok || explorationGuardValueIsEmpty(value) {
		return "<absent>"
	}
	if normalized, valid := explorationGuardNormalizeInteger(value); valid {
		return strconv.FormatInt(normalized, 10)
	}
	return explorationGuardValueSignature(value)
}

func explorationGuardPathValue(input map[string]any, fields ...string) string {
	value := explorationGuardStringValue(input, fields...)
	value = strings.TrimSpace(strings.ReplaceAll(value, "⁄", "/"))
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

func explorationGuardStringValue(input map[string]any, fields ...string) string {
	value, ok := explorationGuardFirstValue(input, fields...)
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func explorationGuardFirstValue(input map[string]any, fields ...string) (any, bool) {
	if len(input) == 0 {
		return nil, false
	}
	for _, field := range fields {
		if value, ok := input[field]; ok {
			return value, true
		}
		want := explorationGuardCanonicalField(field)
		var found any
		foundKey := ""
		for key, value := range input {
			if explorationGuardCanonicalField(key) != want {
				continue
			}
			if foundKey == "" || key < foundKey {
				found = value
				foundKey = key
			}
		}
		if foundKey != "" {
			return found, true
		}
	}
	return nil, false
}

func explorationGuardNormalizeInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		if uint64(v) > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(v), true
	case float32:
		i := int64(v)
		return i, float32(i) == v
	case float64:
		i := int64(v)
		return i, float64(i) == v
	case json.Number:
		i, err := v.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func explorationGuardValueIsEmpty(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func explorationGuardValueSignature(value any) string {
	b, err := json.Marshal(explorationGuardNormalizeSignatureValue(value))
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(b)
}

func explorationGuardStringSignature(value string) string {
	return explorationGuardValueSignature(value)
}

func explorationGuardNormalizeSignatureValue(value any) any {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = explorationGuardNormalizeSignatureValue(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = explorationGuardNormalizeSignatureValue(child)
		}
		return out
	default:
		return v
	}
}

func explorationGuardCanonicalText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), " "))
}

func explorationGuardCanonicalField(field string) string {
	return explorationGuardCanonicalName(field)
}

func generateRecoveryDirective(duplicateKeys []string) string {
	if len(duplicateKeys) == 0 {
		return ""
	}
	return "检测到重复探索操作（" + strings.Join(duplicateKeys, ", ") + "）。请立即停止搜索，直接基于已有信息进行编辑并提交结果。"
}

func explorationGuardCanonicalName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
