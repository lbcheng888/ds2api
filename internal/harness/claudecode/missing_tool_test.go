package claudecode

import "testing"

func TestDetectMissingToolCallBlocksTraceAndFillPromise(t *testing.T) {
	text := "Looking at the code structure, the pure self-build is blocked by functions with Result patterns. Let me trace the remaining gaps and fill them."
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      text,
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected missing tool call decision, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksNowReadingPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "Now reading the rest of the plan document and key source files in parallel to assess implementation status.",
		FinalPrompt: "<user>请继续</user>",
		ToolNames:   []string{"Read", "Bash", "Grep"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected reading promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksNowLetMeCheckWorkingTree(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "Now let me check the current working tree state and build/test status.",
		FinalPrompt: "<｜User｜>继续推进<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected working-tree check promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksChineseConflictPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "三个文件冲突较多。我先逐个分析，然后处理。每个冲突块都是 HEAD（当前分支）和 7aa650b 之间的差异，策略是保留 HEAD 的内容。",
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected missing tool call decision, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksChineseInProgressParallelWork(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "我正在并行处理三项改动。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected in-progress work promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksChineseDirectCodingPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "从历史来看，三个子代理虽已启动，但没有一个实际把代码写到文件里。现在直接动手，分三路把代码写到位。当前工作树已有修改，基于已有并发基础设施来实现。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected direct coding promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksChineseRunTestsPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "编译通过，现在运行测试验证。</｜Assistant｜>",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected test-run promise without Bash call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksFencedJSONToolCallText(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "Plan approved - implement now\n```json\n{\n  \"tool\": \"TaskCreate\",\n  \"arguments\": {\n    \"subject\": \"Add TokenTracker\",\n    \"description\": \"Create internal/auth/token_tracker.go\"\n  }\n}\n```",
		ToolNames: []string{"TaskCreate", "Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected fenced JSON tool text to be blocked as missing real tool call, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksUnsupportedCompletionClaim(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "缓存已集成到 handler_chat.go 的非流式路径，stop_reason 映射也已更新，这两项改动现在都已完成。",
		FinalPrompt: "<｜User｜>请完善Claude Code专属harness<｜Assistant｜>",
		ToolNames:   []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected completion claim without execution tool evidence to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsCompletionClaimAfterExecutionTool(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text: "handler_chat.go 已更新，测试通过。",
		FinalPrompt: `<｜User｜>请修复 handler_chat.go<｜Assistant｜><|DSML|tool_calls>
  <|DSML|invoke name="Edit">
    <|DSML|parameter name="file_path">handler_chat.go</|DSML|parameter>
  </|DSML|invoke>
  <|DSML|invoke name="Bash">
    <|DSML|parameter name="command">go test ./internal/httpapi/openai/chat</|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls><｜Tool｜>edited and tested<｜end▁of▁toolresults｜><｜Assistant｜>`,
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if got.Blocked {
		t.Fatalf("expected completion summary after execution tool evidence to be allowed, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksTaskTrackingOnlyToolCalls(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text: `<tool_calls>
<invoke name="TodoWrite">
<parameter name="todos"><item><content>评估 cheng 语言实现进度并补齐</content><status>pending</status></item></parameter>
</invoke>
</tool_calls>`,
		ToolNames: []string{"TodoWrite", "Read", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected task-tracking-only call to be treated as missing real work, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsTraceTextWithoutTools(t *testing.T) {
	text := "Let me trace the remaining gaps and fill them."
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      text,
		ToolNames: nil,
	})
	if got.Blocked {
		t.Fatalf("expected no block without callable tools, got %#v", got)
	}
}
func TestDetectMissingBlockNoToolReadVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先阅读现有代码，理解当前实现，然后扩展。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '阅读' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolUnderstandVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先理解当前CFG lowering实现情况。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '理解' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolEvaluateVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先评估当前的实现状态和进度。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '评估' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolLearnAboutVerb(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "先了解项目的整体结构。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '了解' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolScanCurrentState(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "直接切入正题。先整体扫一遍当前状态。",
		ToolNames: []string{"Read", "Grep", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for scan-current-state promise, got %#v", got)
	}
}

func TestBufferedToolHoldCandidateRecognizesScanCurrentState(t *testing.T) {
	if !LooksLikeBufferedToolHoldCandidate("直接切入正题。先整体扫一遍当前状态。") {
		t.Fatal("expected scan-current-state promise to be held while streaming")
	}
}

func TestDetectMissingBlockNoToolINeedToPrefix(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "I need to read the file and understand the structure.",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for 'i need to' future action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolNextStepPrefix(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "下一步开始推进剩余的修复工作。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '下一步' prefix, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolNeedCreateCodingAction(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "需要创建一个新的文件来处理配置。",
		FinalPrompt: "<user>请继续</user>",
		ToolNames:   []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '需要创建' coding action, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolCreateFilePlan(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "创建文件 /tmp/test_config.json 来保存配置。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for '创建文件' file plan, got %#v", got)
	}
}

func TestDetectMissingBlockNoToolWriteToFilePlan(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "write to the config file at /tmp/settings.json",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected block for 'write to ' file plan, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementDetectsChineseTaskListToolCommitment(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text: `任务列表：
- 使用 Read 读取 internal/harness/claudecode/missing_tool.go
- 使用 Edit 集中执行意图判断
- 使用 Bash 运行 go test ./internal/harness/claudecode
现在按这个列表执行。`,
		ToolNames: []string{"TodoWrite", "Read", "Edit", "Bash"},
	})
	if !got.Required || got.Reason != ToolExecutionRequirementToolCommitment {
		t.Fatalf("expected tool commitment requirement, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementDetectsWriteClaimWithoutEvidence(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:      "已修改 internal/harness/claudecode/missing_tool.go。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if !got.Required || got.Reason != ToolExecutionRequirementCompletionClaim {
		t.Fatalf("expected completion claim without write evidence, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementDetectsAgentRunningClaimWithoutEvidence(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:                "已启动 3 个子代理，正在运行中。",
		ToolNames:           []string{"Agent", "Read"},
		AllowMetaAgentTools: true,
	})
	if !got.Required || got.Reason != ToolExecutionRequirementAgentLaunch {
		t.Fatalf("expected agent claim without Agent evidence, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsSubstantiveTextAfterBackgroundAgentLaunch(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		FinalPrompt: `<｜User｜>请分析当前 cheng 语言问题和改进点<｜Assistant｜>
Background agent launched successfully.
<｜Assistant｜>`,
		Text: `结论：当前问题集中在冷启动边界和 seed 职责过重。

- cheng_seed.c 承载了太多不该有的编译能力。
- seed 只应是冷启动外根。
- 后续建议把 parser/lowering/codegen 分层收口。`,
		ToolNames:           []string{"Agent", "Read", "Bash"},
		AllowMetaAgentTools: true,
	})
	if got.Blocked {
		t.Fatalf("expected substantive progress text after Agent launch to be allowed, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsSubstantiveAnalysisReport(t *testing.T) {
	text := `一、编译器自举 - 硬阻断链

1. 纯自举编译的全量闭包差很远：当前 pure self probe 只编译入口模块，完整 manifest 模块集差距很大。
2. DirectObjectEmitWithoutBindInstruction_word_zero 是当前最硬的 blocker：说明 BodyIR 进入 primary object writer 后还没有把通用语句降成 ARM64 机器码。
3. 通用 CFG lowering 未完成：if/elseif/range/while 等控制流还停留在专用切片。
4. 跨函数调用的符号发射有 bug：elif_else_guard_cfg_fixture 的 classify 函数 BodyIR 有正常 WordCount 却无法稳定发射。`
	intent := CompileRequestIntent(RequestIntentInput{
		FinalPrompt:         "<｜User｜>请分析当前 cheng 语言问题和改进点<｜Assistant｜>",
		FinalText:           text,
		AvailableToolNames:  []string{"Agent", "Read", "Bash"},
		AllowMetaAgentTools: true,
	})
	if !intent.PureAnalysis || intent.TextPromises.Any {
		t.Fatalf("unexpected analysis intent: %#v", intent)
	}
	req := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		FinalPrompt:         "<｜User｜>请分析当前 cheng 语言问题和改进点<｜Assistant｜>",
		Text:                text,
		ToolNames:           []string{"Agent", "Read", "Bash"},
		AllowMetaAgentTools: true,
	})
	if req.Required {
		t.Fatalf("expected substantive analysis classifier to allow report, got %#v", req)
	}
	got := DetectMissingToolCall(MissingToolCallInput{
		FinalPrompt:         "<｜User｜>请分析当前 cheng 语言问题和改进点<｜Assistant｜>",
		Text:                text,
		ToolNames:           []string{"Agent", "Read", "Bash"},
		AllowMetaAgentTools: true,
	})
	if got.Blocked {
		t.Fatalf("expected substantive analysis report to be allowed, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksSubstantiveReportWithNextStepPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		FinalPrompt: "<｜User｜>请分析当前 cheng 语言问题和改进点<｜Assistant｜>",
		Text: `结论：当前问题集中在 codegen 链路。

1. parser 已经不是最硬瓶颈。
2. BodyIR 到 primary object writer 的桥还缺关键实现。

下一步：读取 primary_object_plan.cheng 并确认 A64 发射器清单。`,
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected next-step promise to remain blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksTaskListToolCommitmentWithoutRealTool(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text: `Task list:
1. Use Read to inspect internal/harness/claudecode/state.go.
2. Use Edit to update the harness.
3. Use Bash to run the focused Go tests.
I'll execute these steps now.`,
		ToolNames: []string{"TodoWrite", "Read", "Edit", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected task-list tool commitment without a real tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksEnglishExploreReadPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "Let me first understand the current harness by examining the key files.",
		ToolNames: []string{"Read", "Grep", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected exploration/read promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksConversationContextCodebasePromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "Looking at the conversation context, I need to understand the current state of the project before proceeding. Let me examine the codebase first.",
		ToolNames: []string{"Read", "Grep", "Bash"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected conversation-context examine promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksPlanModeCodegenPromise(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "In plan mode - before writing any code - I first need to understand the full codegen pipeline BodyIR ops -> PrimaryObjectPlan -> backend emission, and continue inventory every A64 emitter already available, since the agent noted relevant helper functions exist in primary_object_plan.cheng. I will also consult your lessons file.",
		ToolNames: []string{"Read", "Grep", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected plan-mode codegen promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksRecapNextStepReadPlanFile(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "recap: 正在设计通用 CFG codegen 方案；绕过 BodyKind 枚举硬编码匹配，直接从 TypedExpr IR 节点图生成 arm64 指令。下一步：读完 primary_object_plan.cheng 中 BodyIR→InstructionWords 的核心填充逻辑，确认 A64 发射器清单后写 plan 文件。",
		ToolNames: []string{"Read", "Grep", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected recap next-step read/write promise without tool call to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksToolRequiredTurnWithoutPromiseText(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:        "好的。",
		FinalPrompt: "<｜User｜>请一口气完成<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected execution request without current-turn tool evidence to be blocked, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementRequiresToolForContinueTurn(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:        "好的。",
		FinalPrompt: "<｜User｜>请一口气完成<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if !got.Required || got.Reason != ToolExecutionRequirementRequiredTurn {
		t.Fatalf("expected tool-required-turn requirement, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementAllowsConceptualQuestion(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:        "有，方案是把状态机集中到 harness。",
		FinalPrompt: "<｜User｜>有没有一劳永逸而不是补丁的确定性方案<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if got.Required {
		t.Fatalf("expected conceptual question to remain answerable without tools, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementAllowsOptimizationQuestion(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:        "可以从请求调度、工具闸门和观测闭环三处优化。",
		FinalPrompt: "<｜User｜>还有什么极限优化可以超越DeepSeekV4 Pro/Flash官方API的体验<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if got.Required {
		t.Fatalf("expected optimization question to remain answerable without tools, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementAllowsHowToFixQuestion(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:        "解决办法是把工具必需回合做成状态机。",
		FinalPrompt: "<｜User｜>如何解决ds2api模型只说不调用工具的问题<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if got.Required {
		t.Fatalf("expected how-to-fix question to remain answerable without tools, got %#v", got)
	}
}

func TestClassifyCurrentTurnToolRequirementAllowsEnglishHowToFixQuestion(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text:        "Use a deterministic required-tool gate instead of final-text phrase patches.",
		FinalPrompt: "<｜User｜>How to fix ds2api saying instead of calling tools?<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if got.Required {
		t.Fatalf("expected English how-to-fix question to remain answerable without tools, got %#v", got)
	}
}

func TestIsToolRequiredTurnDetectsLocalDiagnosticQuestion(t *testing.T) {
	got := IsToolRequiredTurn(ToolRequiredTurnInput{
		FinalPrompt: "<｜User｜>CPU使用率为什么这么高，是不是死循环了<｜Assistant｜>",
		ToolNames:   []string{"Read", "Bash", "Edit"},
	})
	if !got {
		t.Fatal("expected local CPU diagnostic request to require tools")
	}
}

func TestClassifyCurrentTurnToolRequirementAllowsExistingExecutionEvidence(t *testing.T) {
	got := ClassifyCurrentTurnToolRequirement(CurrentTurnToolRequirementInput{
		Text: "CPU 状态已检查。",
		FinalPrompt: `<｜User｜>CPU使用率为什么这么高，是不是死循环了<｜Assistant｜><|DSML|tool_calls>
  <|DSML|invoke name="Bash">
    <|DSML|parameter name="command">ps -o pid,pcpu,command -p 123</|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls><｜Tool｜>ok<｜end▁of▁toolresults｜><｜Assistant｜>`,
		ToolNames: []string{"Read", "Bash", "Edit"},
	})
	if got.Required {
		t.Fatalf("expected current-turn Bash evidence to satisfy tool-required turn, got %#v", got)
	}
}

func TestDetectMissingToolCallBlocksAgentLaunchPromiseWithoutToolCall(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:                "I will launch 3 parallel agents to inspect the implementation and verification gaps.",
		FinalPrompt:         "<｜User｜>Use Team Agents and continue<｜Assistant｜>",
		ToolNames:           []string{"Agent", "Read"},
		AllowMetaAgentTools: true,
	})
	if !got.Blocked || got.Code != MissingToolCallCode {
		t.Fatalf("expected agent launch promise without Agent tool_use to be blocked, got %#v", got)
	}
}

func TestDetectMissingToolCallAllowsSimpleChatResponse(t *testing.T) {
	got := DetectMissingToolCall(MissingToolCallInput{
		Text:      "好的，我已经完成了所有的修改，测试也通过了。",
		ToolNames: []string{"Read", "Edit", "Bash"},
	})
	if got.Blocked {
		t.Fatalf("expected simple completion message to be allowed, got %#v", got)
	}
}
