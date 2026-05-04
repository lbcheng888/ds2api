package claudecode

import (
	"strings"
	"unicode"

	"ds2api/internal/toolcall"
)

type ExecutionProofInput struct {
	FinalText string
	ToolCalls []toolcall.ParsedToolCall
	ToolNames []string
}

type ExecutionProofReason string

const (
	ExecutionProofReasonUnexecutedCommitment      ExecutionProofReason = "unexecuted_commitment"
	ExecutionProofReasonWriteClaimWithoutEvidence ExecutionProofReason = "write_claim_without_write_tool"
	ExecutionProofReasonRunClaimWithoutEvidence   ExecutionProofReason = "run_claim_without_run_tool"
	ExecutionProofReasonAgentClaimWithoutEvidence ExecutionProofReason = "agent_claim_without_agent_tool"
)

type ExecutionProofResult struct {
	MissingEvidence bool
	Reason          ExecutionProofReason
}

func EvaluateExecutionProof(in ExecutionProofInput) ExecutionProofResult {
	finalText := strings.TrimSpace(in.FinalText)
	if finalText == "" {
		return ExecutionProofResult{}
	}
	lower := executionProofNormalizedLower(finalText)

	evidence := collectExecutionProofEvidence(in)
	if looksLikeExecutionProofAgentClaim(finalText, lower) && !evidence.agent {
		return missingExecutionProof(ExecutionProofReasonAgentClaimWithoutEvidence)
	}
	if looksLikeExecutionProofFutureCommitment(finalText, lower) {
		return missingExecutionProof(ExecutionProofReasonUnexecutedCommitment)
	}
	if looksLikeExecutionProofRunClaim(lower) && !evidence.run {
		return missingExecutionProof(ExecutionProofReasonRunClaimWithoutEvidence)
	}
	if looksLikeExecutionProofWriteClaim(lower) && !evidence.write {
		return missingExecutionProof(ExecutionProofReasonWriteClaimWithoutEvidence)
	}
	return ExecutionProofResult{}
}

func missingExecutionProof(reason ExecutionProofReason) ExecutionProofResult {
	return ExecutionProofResult{
		MissingEvidence: true,
		Reason:          reason,
	}
}

type executionProofEvidence struct {
	write bool
	run   bool
	agent bool
}

func collectExecutionProofEvidence(in ExecutionProofInput) executionProofEvidence {
	var out executionProofEvidence
	for _, name := range in.ToolNames {
		out.merge(executionProofEvidenceForTool(name, "", false))
	}
	for _, call := range in.ToolCalls {
		command, hasCommand := executionProofCommandInput(call.Input)
		out.merge(executionProofEvidenceForTool(call.Name, command, hasCommand))
	}
	return out
}

func (e *executionProofEvidence) merge(next executionProofEvidence) {
	e.write = e.write || next.write
	e.run = e.run || next.run
	e.agent = e.agent || next.agent
}

func executionProofEvidenceForTool(name, command string, hasCommand bool) executionProofEvidence {
	key := canonicalExecutionProofToolName(name)
	var out executionProofEvidence
	if toolcall.IsBackgroundAgentToolName(name) {
		out.agent = true
	}
	if executionProofIsWriteToolName(key) {
		out.write = true
	}
	if key == "execcommand" {
		out.run = true
		out.write = !hasCommand || executionProofShellCommandWrites(command)
		return out
	}
	if executionProofIsShellToolName(key) {
		out.run = true
		out.write = executionProofShellCommandWrites(command)
		return out
	}
	if executionProofIsRunToolName(key) {
		out.run = true
	}
	return out
}

func executionProofIsWriteToolName(key string) bool {
	switch key {
	case "edit", "multiedit", "multiwrite", "write", "applypatch", "applydiff", "patch", "update", "strreplaceeditor", "strreplace", "notebookedit", "filewrite", "createfile":
		return true
	default:
		return false
	}
}

func executionProofIsShellToolName(key string) bool {
	switch key {
	case "bash", "shell", "sh", "terminal":
		return true
	default:
		return false
	}
}

func executionProofIsRunToolName(key string) bool {
	switch key {
	case "test", "gotest", "pytest", "unittest", "build", "compile", "testsim", "buildsim", "buildrunsim":
		return true
	default:
		return strings.Contains(key, "test") || strings.Contains(key, "build") || strings.Contains(key, "compile")
	}
}

func executionProofCommandInput(input map[string]any) (string, bool) {
	for _, key := range []string{"command", "cmd", "script"} {
		if value, ok := input[key]; ok {
			if s, ok := value.(string); ok {
				return s, true
			}
		}
	}
	for _, key := range []string{"args", "argv"} {
		if value, ok := input[key]; ok {
			return joinExecutionProofArgs(value), true
		}
	}
	return "", false
}

func joinExecutionProofArgs(value any) string {
	switch v := value.(type) {
	case []string:
		return strings.Join(v, " ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func executionProofShellCommandWrites(command string) bool {
	lower := strings.ToLower(strings.Join(strings.Fields(command), " "))
	if lower == "" {
		return false
	}
	for _, phrase := range []string{
		"apply_patch",
		"applypatch",
		"gofmt -w",
		"sed -i",
		"perl -pi",
		"cat >",
		"cat <<",
		"tee ",
		"| tee",
		">>",
		"> ",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	for _, word := range []string{"mv", "cp", "rm", "mkdir", "touch", "chmod"} {
		if executionProofContainsShellWord(lower, word) {
			return true
		}
	}
	return false
}

func looksLikeExecutionProofFutureCommitment(text, lower string) bool {
	if containsAny(lower, []string{
		"will not ",
		"won't ",
		"do not plan to ",
		"don't plan to ",
		"未运行",
		"没有运行",
		"没运行",
		"未执行",
		"未测试",
		"不会",
		"不准备",
		"不打算",
		"无需",
		"不用",
	}) {
		return false
	}
	if looksLikeFutureToolAction(text) {
		return true
	}
	if LooksLikeUnexecutedAgentLaunch(text, "", true) {
		return true
	}
	if !containsAny(lower, []string{
		"i will ",
		"i'll ",
		"i’ll ",
		"let me ",
		"i am going to ",
		"i'm going to ",
		"going to ",
		"将要",
		"我将",
		"我会",
		"准备",
		"打算",
		"计划",
		"接下来",
		"下一步",
		"马上",
		"现在开始",
	}) {
		return false
	}
	return containsAny(lower, []string{
		" edit",
		" write",
		" modify",
		" fix",
		" update",
		" implement",
		" create",
		" run",
		" test",
		" compile",
		" build",
		" verify",
		" launch",
		" start",
		" read",
		" inspect",
		" search",
		"修改",
		"写入",
		"修复",
		"更新",
		"实现",
		"创建",
		"运行",
		"执行",
		"测试",
		"编译",
		"构建",
		"验证",
		"启动",
		"代理",
		"读取",
		"检查",
		"搜索",
	})
}

func looksLikeExecutionProofAgentClaim(text, lower string) bool {
	if containsAny(lower, []string{"未启动", "没有启动", "没启动", "not launched", "did not launch", "haven't launched"}) {
		return false
	}
	if !strings.Contains(lower, "agent") && !strings.Contains(text, "代理") {
		return false
	}
	return containsAny(lower, []string{
		"agents running",
		"agents are running",
		"agent is running",
		"agents launched",
		"agent launched",
		"launched agents",
		"launched agent",
		"started agents",
		"started agent",
		"background agents running",
		"已启动",
		"已经启动",
		"启动了",
		"启动中",
		"运行中",
		"代理已",
		"子代理已",
	})
}

func looksLikeExecutionProofRunClaim(lower string) bool {
	if containsAny(lower, []string{
		"未运行",
		"没有运行",
		"没运行",
		"未执行",
		"没有执行",
		"没执行",
		"未测试",
		"测试未运行",
		"测试未通过",
		"not run tests",
		"did not run tests",
		"haven't run tests",
		"tests not run",
		"not executed",
		"not pass",
		"failed",
	}) {
		return false
	}
	if containsAny(lower, []string{
		"测试通过",
		"测试已通过",
		"已通过测试",
		"编译通过",
		"构建通过",
		"验证通过",
		"go test 通过",
		"npm run build 通过",
		"已运行",
		"已经运行",
		"已执行",
		"已经执行",
		"跑过测试",
		"已跑测试",
		"tests pass",
		"tests passed",
		"test passed",
		"all tests passed",
		"go test passed",
		"build passed",
		"build succeeded",
		"compiled successfully",
		"ran tests",
		"ran the tests",
		"executed tests",
		"verified with",
	}) {
		return true
	}
	return false
}

func looksLikeExecutionProofWriteClaim(lower string) bool {
	if containsAny(lower, []string{
		"未修改",
		"没有修改",
		"没修改",
		"未写入",
		"没有写入",
		"没写入",
		"未修复",
		"没有修复",
		"没修复",
		"无需修改",
		"不需要修改",
		"not modified",
		"not changed",
		"not fixed",
		"no changes",
		"did not modify",
		"did not change",
		"did not fix",
		"haven't modified",
		"haven't changed",
	}) {
		return false
	}
	if containsAny(lower, []string{
		"已修改",
		"已经修改",
		"已写入",
		"已经写入",
		"已修复",
		"已经修复",
		"已更新",
		"已经更新",
		"已实现",
		"已经实现",
		"已添加",
		"已经添加",
		"已新增",
		"已经新增",
		"已创建",
		"已经创建",
		"写好了",
		"改好了",
		"修好了",
		"修改完成",
		"修复完成",
		"实现完成",
		"写入完成",
		"更新完成",
		"改动完成",
	}) {
		return true
	}
	return executionProofContainsAnyEnglishWord(lower, []string{
		"modified",
		"changed",
		"updated",
		"fixed",
		"implemented",
		"wrote",
		"written",
		"created",
		"added",
		"patched",
		"saved",
		"applied",
	})
}

func executionProofNormalizedLower(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func canonicalExecutionProofToolName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if r == '_' || r == '-' || r == ' ' || r == '.' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func executionProofContainsAnyEnglishWord(text string, words []string) bool {
	for _, word := range words {
		if executionProofContainsEnglishWord(text, word) {
			return true
		}
	}
	return false
}

func executionProofContainsEnglishWord(text, word string) bool {
	start := 0
	for {
		idx := strings.Index(text[start:], word)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !executionProofEnglishTokenByte(text[idx-1])
		afterIdx := idx + len(word)
		afterOK := afterIdx >= len(text) || !executionProofEnglishTokenByte(text[afterIdx])
		if beforeOK && afterOK {
			return true
		}
		start = idx + len(word)
	}
}

func executionProofContainsShellWord(command, word string) bool {
	for _, field := range strings.FieldsFunc(command, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ';' || r == '&' || r == '|'
	}) {
		field = strings.TrimLeft(field, "(")
		if field == word {
			return true
		}
	}
	return false
}

func executionProofEnglishTokenByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}
