package claudecode

import "testing"

func TestExtractTeamAgentStateAllowsOnlyLiveTaskOutputs(t *testing.T) {
	prompt := `
Task Output runabcdef
Task is still running.
Task Output doneabcdef
Task completed.
Task Output missabcdef
Error: No task found with ID: missabcdef
`
	got := ExtractTeamAgentState(prompt)
	allowed := got.AllowedTaskOutputIDSet()
	if _, ok := allowed["runabcdef"]; !ok {
		t.Fatalf("running task not allowed: %#v", got)
	}
	if _, ok := allowed["doneabcdef"]; !ok {
		t.Fatalf("completed task not allowed: %#v", got)
	}
	if _, ok := allowed["missabcdef"]; ok {
		t.Fatalf("missing task should not be allowed: %#v", got)
	}
}

func TestExtractTeamAgentStateAllowsLatestTaskNotificationIDs(t *testing.T) {
	prompt := `<｜User｜>
<task-notification task_id="notify1234"><status>completed</status></task-notification>
<｜Assistant｜>`
	got := ExtractTeamAgentState(prompt)
	allowed := got.AllowedTaskOutputIDSet()
	if _, ok := allowed["notify1234"]; !ok {
		t.Fatalf("notification id not allowed: %#v", got)
	}
}
