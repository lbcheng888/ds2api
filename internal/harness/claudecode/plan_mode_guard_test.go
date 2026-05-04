package claudecode

import (
	"testing"

	"ds2api/internal/toolcall"
)

func TestDetectInvalidPlanModeTransitionBlocksSwitchModePlan(t *testing.T) {
	got := DetectInvalidPlanModeTransition(PlanModeGuardInput{
		FinalPrompt: "<｜User｜>请继续修复<｜Assistant｜>",
		ToolNames:   []string{"switch_mode", "Read", "Bash"},
		Calls: []toolcall.ParsedToolCall{{
			Name:  "switch_mode",
			Input: map[string]any{"target_mode_id": "plan"},
		}},
	})
	if !got.Blocked || got.Reason != PlanModeGuardExecutionRequestReason {
		t.Fatalf("expected switch_mode(plan) to be blocked for execution request, got %#v", got)
	}
}

func TestDetectInvalidPlanModeTransitionAllowsNormalPlanRequest(t *testing.T) {
	got := DetectInvalidPlanModeTransition(PlanModeGuardInput{
		FinalPrompt: "<｜User｜>请先制定一个实现方案<｜Assistant｜>",
		ToolNames:   []string{"EnterPlanMode", "Read", "Bash"},
		Calls:       []toolcall.ParsedToolCall{{Name: "EnterPlanMode", Input: map[string]any{}}},
	})
	if got.Blocked {
		t.Fatalf("expected EnterPlanMode to remain valid for a planning request, got %#v", got)
	}
}

func TestDetectInvalidPlanModeTransitionBlocksPlanModeBugReport(t *testing.T) {
	got := DetectInvalidPlanModeTransition(PlanModeGuardInput{
		FinalPrompt: "<｜User｜>连续进入Plan mode<｜Assistant｜>",
		ToolNames:   []string{"EnterPlanMode", "Read", "Bash"},
		Calls:       []toolcall.ParsedToolCall{{Name: "EnterPlanMode", Input: map[string]any{}}},
	})
	if !got.Blocked || got.Reason != PlanModeGuardBugReportReason {
		t.Fatalf("expected plan mode bug report to block EnterPlanMode, got %#v", got)
	}
}
