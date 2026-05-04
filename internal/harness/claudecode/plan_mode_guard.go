package claudecode

import (
	"strings"

	"ds2api/internal/toolcall"
)

const (
	PlanModeGuardAlreadyActiveReason     = "plan_mode_already_active"
	PlanModeGuardExecutionRequestReason  = "execution_request_entered_plan_mode"
	PlanModeGuardBugReportReason         = "plan_mode_bug_report"
	planModeGuardMissingToolMessage      = "Upstream model entered plan mode instead of executing a required tool."
	planModeActiveReminderMaxWindowBytes = 24000
)

type PlanModeGuardInput struct {
	FinalPrompt         string
	Calls               []toolcall.ParsedToolCall
	ToolNames           []string
	AllowMetaAgentTools bool
}

type PlanModeGuardDecision struct {
	Blocked bool
	Reason  string
}

func DetectInvalidPlanModeTransition(in PlanModeGuardInput) PlanModeGuardDecision {
	if !hasEnterPlanModeCall(in.Calls) {
		return PlanModeGuardDecision{}
	}
	if promptIndicatesPlanModeActive(in.FinalPrompt) {
		return blockedPlanModeTransition(PlanModeGuardAlreadyActiveReason)
	}
	if latestUserReportsPlanModeProblem(in.FinalPrompt) {
		return blockedPlanModeTransition(PlanModeGuardBugReportReason)
	}
	if latestUserExplicitlyRequestsPlanning(in.FinalPrompt) &&
		!latestUserExplicitlyRequestsImmediateExecution(in.FinalPrompt) {
		return PlanModeGuardDecision{}
	}
	if IsToolRequiredTurn(ToolRequiredTurnInput{
		FinalPrompt:         in.FinalPrompt,
		ToolNames:           in.ToolNames,
		AllowMetaAgentTools: in.AllowMetaAgentTools,
	}) {
		return blockedPlanModeTransition(PlanModeGuardExecutionRequestReason)
	}
	return PlanModeGuardDecision{}
}

func PlanModeGuardMissingToolMessage() string {
	return planModeGuardMissingToolMessage
}

func planModeGuardDecision(profile string) MissingToolCallDecision {
	recordFailureDecision(profile, MissingToolCallCode)
	return MissingToolCallDecision{
		Blocked: true,
		Message: planModeGuardMissingToolMessage,
		Code:    MissingToolCallCode,
	}
}

func blockedPlanModeTransition(reason string) PlanModeGuardDecision {
	return PlanModeGuardDecision{Blocked: true, Reason: reason}
}

func hasEnterPlanModeCall(calls []toolcall.ParsedToolCall) bool {
	for _, call := range calls {
		if IsEnterPlanModeToolCall(call) {
			return true
		}
	}
	return false
}

func IsEnterPlanModeToolCall(call toolcall.ParsedToolCall) bool {
	switch CanonicalTaskOutputToolName(call.Name) {
	case "enterplanmode":
		return true
	case "switchmode":
		return planModeSwitchTargetsPlan(call.Input)
	default:
		return false
	}
}

func planModeSwitchTargetsPlan(input map[string]any) bool {
	if len(input) == 0 {
		return false
	}
	for _, key := range []string{
		"mode", "target", "target_mode", "targetMode", "target_mode_id", "targetModeId", "mode_id", "modeId",
	} {
		if mode := strings.ToLower(strings.TrimSpace(asExecutionLedgerString(input[key]))); mode == "plan" || mode == "planmode" {
			return true
		}
	}
	return false
}

func promptIndicatesPlanModeActive(finalPrompt string) bool {
	window := latestConversationTurnBlock(finalPrompt)
	if len(window) > planModeActiveReminderMaxWindowBytes {
		window = window[len(window)-planModeActiveReminderMaxWindowBytes:]
	}
	lower := strings.ToLower(window)
	return strings.Contains(lower, "plan mode is active") ||
		strings.Contains(lower, "plan mode still active")
}

func latestUserReportsPlanModeProblem(finalPrompt string) bool {
	latest := strings.ToLower(LatestUserTextFromPrompt(finalPrompt))
	if !strings.Contains(latest, "plan mode") && !strings.Contains(latest, "planmode") &&
		!strings.Contains(latest, "计划模式") {
		return false
	}
	return containsAny(latest, []string{
		"连续",
		"重复",
		"反复",
		"一直",
		"又",
		"再次",
		"卡",
		"停",
		"不执行",
		"只说不做",
		"repeat",
		"repeated",
		"loop",
		"stuck",
	})
}

func latestUserExplicitlyRequestsPlanning(finalPrompt string) bool {
	latest := strings.ToLower(LatestUserTextFromPrompt(finalPrompt))
	return containsAny(latest, []string{
		"plan mode",
		"计划模式",
		"制定方案",
		"设计方案",
		"实现方案",
		"修复方案",
		"优化方案",
		"先做计划",
		"先制定计划",
		"先规划",
		"先给方案",
		"make a plan",
		"create a plan",
		"plan first",
		"design an approach",
	})
}

func latestUserExplicitlyRequestsImmediateExecution(finalPrompt string) bool {
	latest := strings.ToLower(LatestUserTextFromPrompt(finalPrompt))
	return containsAny(latest, []string{
		"继续",
		"推进",
		"执行",
		"运行",
		"完成",
		"改完",
		"落地",
		"一口气",
		"直接动手",
		"直接执行",
		"直接改",
		"直接做",
		"直接修复",
		"直接实现",
		"现在动手",
		"现在执行",
		"现在改",
		"现在修复",
		"现在实现",
		"重启",
		"continue",
		"proceed",
		"execute",
		"run",
		"finish",
		"complete",
		"ship",
		"restart",
		"fix it",
		"implement it",
		"apply it",
		"make the change",
	})
}
