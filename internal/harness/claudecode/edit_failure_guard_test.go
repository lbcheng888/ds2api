package claudecode

import "testing"

func TestEvaluateFinalOutputRecoversRepeatedEditFailureWithRead(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: `<｜User｜>继续修改<｜Assistant｜>
<tool_calls>
<tool_call>
<tool_name>Update</tool_name>
<parameters><file_path>src/core/backend/direct_exe_emit.cheng</file_path><old_string>old</old_string><new_string>new</new_string></parameters>
</tool_call>
</tool_calls>
<｜Tool｜>Error editing file<｜end▁of▁toolresults｜><｜Assistant｜>`,
		Text: `<tool_calls>
<tool_call>
<tool_name>Update</tool_name>
<parameters><file_path>src/core/backend/direct_exe_emit.cheng</file_path><old_string>old</old_string><new_string>new</new_string></parameters>
</tool_call>
</tool_calls>`,
		ToolNames: []string{"Read", "Update"},
	})
	if len(got.Calls) != 1 {
		t.Fatalf("expected one recovery call, got %#v", got.Calls)
	}
	if got.Calls[0].Name != "Read" {
		t.Fatalf("expected repeated failed edit to become Read, got %#v", got.Calls[0])
	}
	if got.Calls[0].Input["file_path"] != "src/core/backend/direct_exe_emit.cheng" {
		t.Fatalf("unexpected recovery read path: %#v", got.Calls[0].Input)
	}
}

func TestRecoverEditRetryAfterFailureAllowsEditAfterFreshRead(t *testing.T) {
	got := EvaluateFinalOutput(FinalEvaluationInput{
		FinalPrompt: `<｜User｜>继续修改<｜Assistant｜>
<tool_calls>
<tool_call>
<tool_name>Update</tool_name>
<parameters><file_path>src/core/backend/direct_exe_emit.cheng</file_path><old_string>old</old_string><new_string>new</new_string></parameters>
</tool_call>
</tool_calls>
<｜Tool｜>Error editing file<｜end▁of▁toolresults｜><｜Assistant｜>
<tool_calls>
<tool_call>
<tool_name>Read</tool_name>
<parameters><file_path>src/core/backend/direct_exe_emit.cheng</file_path></parameters>
</tool_call>
</tool_calls>
<｜Tool｜>fresh file text<｜end▁of▁toolresults｜><｜Assistant｜>`,
		Text: `<tool_calls>
<tool_call>
<tool_name>Update</tool_name>
<parameters><file_path>src/core/backend/direct_exe_emit.cheng</file_path><old_string>fresh</old_string><new_string>new</new_string></parameters>
</tool_call>
</tool_calls>`,
		ToolNames: []string{"Read", "Update"},
	})
	if len(got.Calls) != 1 {
		t.Fatalf("expected one call, got %#v", got.Calls)
	}
	if got.Calls[0].Name != "Update" {
		t.Fatalf("expected edit after fresh read to remain Update, got %#v", got.Calls[0])
	}
}
