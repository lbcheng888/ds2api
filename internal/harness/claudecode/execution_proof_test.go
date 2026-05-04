package claudecode

import (
	"testing"

	"ds2api/internal/toolcall"
)

func TestEvaluateExecutionProof(t *testing.T) {
	tests := []struct {
		name        string
		finalText   string
		toolCalls   []toolcall.ParsedToolCall
		toolNames   []string
		wantMissing bool
		wantReason  ExecutionProofReason
	}{
		{
			name:        "chinese write claim without write evidence",
			finalText:   "已修改 internal/harness/claudecode/execution_proof.go。",
			wantMissing: true,
			wantReason:  ExecutionProofReasonWriteClaimWithoutEvidence,
		},
		{
			name:      "chinese write claim with edit evidence",
			finalText: "已修改 execution_proof.go。",
			toolCalls: []toolcall.ParsedToolCall{{
				Name:  "Edit",
				Input: map[string]any{"file_path": "execution_proof.go"},
			}},
		},
		{
			name:      "apply patch name proves write claim",
			finalText: "已写入新的辅助层。",
			toolNames: []string{"apply_patch"},
		},
		{
			name:        "english write claim without evidence",
			finalText:   "Fixed the final output execution-proof checks.",
			wantMissing: true,
			wantReason:  ExecutionProofReasonWriteClaimWithoutEvidence,
		},
		{
			name:        "chinese test claim without run evidence",
			finalText:   "测试通过。",
			toolNames:   []string{"Edit"},
			wantMissing: true,
			wantReason:  ExecutionProofReasonRunClaimWithoutEvidence,
		},
		{
			name:      "english test claim with bash evidence",
			finalText: "All tests passed.",
			toolNames: []string{"Bash"},
		},
		{
			name:      "exec command call proves run claim",
			finalText: "已运行 go test ./internal/harness/claudecode。",
			toolCalls: []toolcall.ParsedToolCall{{
				Name:  "exec_command",
				Input: map[string]any{"cmd": "go test ./internal/harness/claudecode"},
			}},
		},
		{
			name:      "run-only exec command does not prove write claim",
			finalText: "已修改执行证据实现。",
			toolCalls: []toolcall.ParsedToolCall{{
				Name:  "exec_command",
				Input: map[string]any{"cmd": "go test ./internal/harness/claudecode"},
			}},
			wantMissing: true,
			wantReason:  ExecutionProofReasonWriteClaimWithoutEvidence,
		},
		{
			name:        "chinese agent running claim without agent evidence",
			finalText:   "已启动 3 个子代理，正在运行中。",
			toolNames:   []string{"Bash"},
			wantMissing: true,
			wantReason:  ExecutionProofReasonAgentClaimWithoutEvidence,
		},
		{
			name:      "english agents running with agent evidence",
			finalText: "3 agents are running.",
			toolCalls: []toolcall.ParsedToolCall{{
				Name:  "Agent",
				Input: map[string]any{"prompt": "inspect"},
			}},
		},
		{
			name:        "chinese future plan is unexecuted commitment",
			finalText:   "准备运行测试验证。",
			wantMissing: true,
			wantReason:  ExecutionProofReasonUnexecutedCommitment,
		},
		{
			name:        "english future plan is unexecuted commitment",
			finalText:   "I will edit the file now.",
			wantMissing: true,
			wantReason:  ExecutionProofReasonUnexecutedCommitment,
		},
		{
			name:      "pure summary is allowed",
			finalText: "这个辅助层只根据最终文本和当前回合工具证据做确定性判断。",
		},
		{
			name:      "negated test statement is allowed",
			finalText: "未运行测试，当前只给出实现建议。",
		},
		{
			name:      "negated write statement is allowed",
			finalText: "没有修改任何文件。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateExecutionProof(ExecutionProofInput{
				FinalText: tt.finalText,
				ToolCalls: tt.toolCalls,
				ToolNames: tt.toolNames,
			})
			if got.MissingEvidence != tt.wantMissing || got.Reason != tt.wantReason {
				t.Fatalf("got %#v, want missing=%v reason=%q", got, tt.wantMissing, tt.wantReason)
			}
		})
	}
}
