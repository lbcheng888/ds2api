package protocol

import "testing"

func TestExtractTaskStatesFromTaskOutputWindows(t *testing.T) {
	text := `Task Output(non-blocking) completed_task_1
Done.
Task Output(non-blocking) running_task_2
Task is still running.
Task Output(non-blocking) running_task_3
仍在运行。`

	states := ExtractTaskStates(text)
	running := TaskIDsWithStatus(states, TaskStatusRunning)
	if len(running) != 2 || running[0] != "running_task_2" || running[1] != "running_task_3" {
		t.Fatalf("unexpected running task ids: %#v states=%#v", running, states)
	}
	completed := TaskIDsWithStatus(states, TaskStatusCompleted)
	if len(completed) != 1 || completed[0] != "completed_task_1" {
		t.Fatalf("unexpected completed task ids: %#v states=%#v", completed, states)
	}
}

func TestExtractTaskStatesFromNotification(t *testing.T) {
	text := `<task-notification task_id="task_a"><status>completed</status></task-notification>
<task-notification><task-id>task_b</task-id><status>running</status></task-notification>`

	states := ExtractTaskStates(text)
	completed := TaskIDsWithStatus(states, TaskStatusCompleted)
	running := TaskIDsWithStatus(states, TaskStatusRunning)
	if len(completed) != 1 || completed[0] != "task_a" {
		t.Fatalf("unexpected completed ids: %#v states=%#v", completed, states)
	}
	if len(running) != 1 || running[0] != "task_b" {
		t.Fatalf("unexpected running ids: %#v states=%#v", running, states)
	}
}

func TestExtractTaskStatesMissingOverridesEarlierRunning(t *testing.T) {
	text := `Task Output(non-blocking) stale_task_1
Task is still running.
Task Output stale_task_1
Error: No task found with ID: stale_task_1`

	states := ExtractTaskStates(text)
	if got := TaskIDsWithStatus(states, TaskStatusRunning); len(got) != 0 {
		t.Fatalf("expected no running ids after missing result, got %#v states=%#v", got, states)
	}
	missing := TaskIDsWithStatus(states, TaskStatusMissing)
	if len(missing) != 1 || missing[0] != "stale_task_1" {
		t.Fatalf("unexpected missing ids: %#v states=%#v", missing, states)
	}
}
