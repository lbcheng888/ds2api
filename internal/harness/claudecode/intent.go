package claudecode

import (
	"html"
	"regexp"
	"strings"
)

type RequestIntentInput struct {
	LatestUserText       string
	FinalText            string
	FinalPrompt          string
	AvailableToolNames   []string
	CurrentTurnToolNames []string
	AllowMetaAgentTools  bool
}

type RequestIntent struct {
	LatestUserText      string
	UserAuthorization   IntentUserAuthorization
	TextPromises        IntentTextPromises
	ClaimsWithoutTools  IntentClaimsWithoutTools
	ToolEvidence        IntentToolEvidence
	PureAnalysis        bool
	ToolRequiredTurn    bool
	AgentLaunch         IntentAgentLaunch
	HasAvailableTools   bool
	HasAvailableAgent   bool
	AllowMetaAgentTools bool
}

type IntentUserAuthorization struct {
	Execute               bool
	Continue              bool
	ProceedWithSuggestion bool
	DirectAction          bool
}

type IntentTextPromises struct {
	Read        bool
	Inspect     bool
	Search      bool
	Edit        bool
	WriteFile   bool
	RunCommand  bool
	LaunchAgent bool
	Any         bool
}

type IntentClaimsWithoutTools struct {
	Edited   bool
	Ran      bool
	Verified bool
	Any      bool
}

type IntentToolEvidence struct {
	Read        bool
	Search      bool
	Edit        bool
	WriteFile   bool
	RunCommand  bool
	LaunchAgent bool
	Any         bool
}

type IntentAgentLaunch struct {
	Present    bool
	Count      int
	CountKnown bool
}

func CompileRequestIntent(in RequestIntentInput) RequestIntent {
	latestUser := normalizeIntentText(in.LatestUserText)
	if latestUser == "" && in.FinalPrompt != "" {
		latestUser = normalizeIntentText(html.UnescapeString(LatestUserTextFromPrompt(in.FinalPrompt)))
		latestUser = normalizeIntentText(systemReminderBlockPattern.ReplaceAllString(latestUser, " "))
	}
	finalText := normalizeIntentText(in.FinalText)
	evidence := compileToolEvidence(in.CurrentTurnToolNames, in.FinalPrompt)
	agentLaunch := compileAgentLaunchIntent(latestUser, finalText)
	textPromises := compileTextPromises(finalText, agentLaunch)
	claims := compileClaimsWithoutTools(finalText, evidence)

	intent := RequestIntent{
		LatestUserText:      latestUser,
		UserAuthorization:   compileUserAuthorization(latestUser),
		TextPromises:        textPromises,
		ClaimsWithoutTools:  claims,
		ToolEvidence:        evidence,
		PureAnalysis:        compilePureAnalysis(latestUser, finalText, textPromises),
		AgentLaunch:         agentLaunch,
		HasAvailableTools:   HasCallableTools(in.AvailableToolNames),
		AllowMetaAgentTools: in.AllowMetaAgentTools,
	}
	_, intent.HasAvailableAgent = FindBackgroundAgentToolName(in.AvailableToolNames)
	intent.ToolRequiredTurn = compileToolRequiredTurn(latestUser, intent, in.AvailableToolNames)
	return intent
}

func compileUserAuthorization(text string) IntentUserAuthorization {
	lower := strings.ToLower(text)
	auth := IntentUserAuthorization{
		Continue: containsAny(lower, []string{
			"继续",
			"继续推进",
			"接着",
			"继续做",
			"continue",
			"keep going",
			"carry on",
		}),
		ProceedWithSuggestion: containsAny(lower, []string{
			"按建议",
			"按你的建议",
			"按这个方案",
			"按上面",
			"照此推进",
			"proceed with",
			"as suggested",
			"your suggestion",
			"your recommendation",
			"go with that",
		}),
		DirectAction: containsAny(lower, []string{
			"现在直接动手",
			"直接动手",
			"直接改",
			"直接做",
			"直接执行",
			"现在开始",
			"go ahead",
			"do it now",
			"start now",
		}),
	}
	auth.Execute = auth.Continue || auth.ProceedWithSuggestion || auth.DirectAction || containsAny(lower, []string{
		"执行",
		"运行",
		"推进",
		"实现",
		"修复",
		"修改",
		"改完",
		"写入",
		"落地",
		"完成",
		"开干",
		"一口气",
		"execute",
		"run it",
		"implement",
		"fix",
		"modify",
		"finish",
		"ship it",
	})
	return auth
}

func compileToolRequiredTurn(latestUser string, intent RequestIntent, availableToolNames []string) bool {
	if !hasExecutableWorkTools(availableToolNames) || intent.ToolEvidence.Any || intent.PureAnalysis {
		return false
	}
	latestUserLower := strings.ToLower(normalizeIntentText(latestUser))
	if latestUserLooksConceptualOnly(latestUserLower) &&
		!latestUserHasLocalDiagnosticCue(latestUserLower) &&
		!latestUserHasDirectExecutionCue(latestUserLower) {
		return false
	}
	if intent.UserAuthorization.Execute {
		return true
	}
	if intent.AgentLaunch.Present && intent.HasAvailableAgent && intent.AllowMetaAgentTools {
		return true
	}
	return latestUserRequestsLocalToolWork(latestUser)
}

func hasExecutableWorkTools(toolNames []string) bool {
	for _, name := range toolNames {
		if isExecutableWorkToolName(name) {
			return true
		}
	}
	return false
}

func isExecutableWorkToolName(name string) bool {
	if isTaskTrackingToolName(name) {
		return false
	}
	switch CanonicalTaskOutputToolName(name) {
	case "read", "view", "open",
		"grep", "glob", "search", "find",
		"edit", "multiedit", "multieditfile", "update", "applypatch", "applydiff", "apply",
		"write", "createfile",
		"bash", "shell", "exec", "runcommand", "terminal",
		"agent", "task":
		return true
	default:
		return false
	}
}

func latestUserRequestsLocalToolWork(text string) bool {
	lower := strings.ToLower(normalizeIntentText(text))
	if lower == "" || latestUserExplicitlyDisablesTools(lower) {
		return false
	}
	if latestUserLooksConceptualOnly(lower) &&
		!latestUserHasLocalDiagnosticCue(lower) &&
		!latestUserHasDirectExecutionCue(lower) {
		return false
	}
	return latestUserHasLocalDiagnosticCue(lower) || latestUserHasDirectExecutionCue(lower) || containsAny(lower, []string{
		"排查",
		"查一下",
		"查明",
		"定位",
		"诊断",
		"根因",
		"自检",
		"复查",
		"验证",
		"测试",
		"构建",
		"编译",
		"重启",
		"启动",
		"服务",
		"进程",
		"队列",
		"状态",
		"日志",
		"端口",
		"cpu",
		"内存",
		"死循环",
		"一口气",
		"完成",
		"推进",
		"修好",
		"修复",
		"解决",
		"改",
		"实现",
		"优化",
		"落地",
		"执行",
		"运行",
		"debug",
		"diagnose",
		"investigate",
		"root cause",
		"verify",
		"test",
		"build",
		"compile",
		"restart",
		"service",
		"process",
		"queue",
		"status",
		"log",
		"port",
		"cpu",
		"memory",
		"dead loop",
		"infinite loop",
		"finish",
		"complete",
		"continue",
		"proceed",
		"fix",
		"implement",
		"optimize",
		"run",
	})
}

func latestUserLooksConceptualOnly(lower string) bool {
	return containsAny(lower, []string{
		"如何",
		"怎么",
		"什么",
		"有没有",
		"是否",
		"是不是",
		"为什么",
		"吗",
		"？",
		"?",
		"how to",
		"what ",
		"why ",
		"whether",
		"is there",
		"are there",
	})
}

func latestUserHasLocalDiagnosticCue(lower string) bool {
	return containsAny(lower, []string{
		"cpu",
		"内存",
		"死循环",
		"队列",
		"进程",
		"服务",
		"端口",
		"日志",
		"状态",
		"编译",
		"测试",
		"构建",
		"重启",
		"error editing file",
		"memory",
		"infinite loop",
		"dead loop",
		"queue",
		"process",
		"service",
		"port",
		"log",
		"status",
		"build",
		"test",
		"restart",
	})
}

func latestUserHasDirectExecutionCue(lower string) bool {
	return containsAny(lower, []string{
		"请一口气",
		"一口气完成",
		"请完成",
		"请继续",
		"继续推进",
		"继续做",
		"请修复",
		"请修改",
		"请实现",
		"请优化",
		"请执行",
		"请运行",
		"直接动手",
		"直接改",
		"直接做",
		"直接执行",
		"现在开始",
		"现在直接",
		"落地",
		"开干",
		"go ahead",
		"do it",
		"do it now",
		"start now",
		"fix it",
		"implement it",
		"run it",
		"execute it",
		"ship it",
	})
}

func latestUserExplicitlyDisablesTools(lower string) bool {
	return containsAny(lower, []string{
		"纯分析",
		"只分析",
		"仅分析",
		"只解释",
		"仅解释",
		"只回答",
		"仅回答",
		"不用工具",
		"不要调用工具",
		"不需要工具",
		"不要用工具",
		"不改代码",
		"不修改",
		"不要改",
		"不写文件",
		"analysis only",
		"explain only",
		"answer only",
		"no tools",
		"do not use tools",
		"don't use tools",
		"no edits",
		"do not edit",
		"read-only",
		"read only",
	})
}

func compileTextPromises(text string, agentLaunch IntentAgentLaunch) IntentTextPromises {
	lower := strings.ToLower(text)
	promises := IntentTextPromises{
		Read:        hasPromiseFor(lower, readPromiseVerbs(), readPromisePhrases()),
		Inspect:     hasPromiseFor(lower, inspectPromiseVerbs(), inspectPromisePhrases()),
		Search:      hasPromiseFor(lower, searchPromiseVerbs(), searchPromisePhrases()),
		Edit:        hasPromiseFor(lower, editPromiseVerbs(), editPromisePhrases()),
		WriteFile:   hasPromiseFor(lower, writeFilePromiseVerbs(), writeFilePromisePhrases()),
		RunCommand:  hasPromiseFor(lower, runCommandPromiseVerbs(), runCommandPromisePhrases()),
		LaunchAgent: agentLaunch.Present,
	}
	if containsAny(lower, []string{"现在直接动手", "直接动手", "把代码写到位", "写到位"}) {
		promises.Edit = true
	}
	promises.Any = promises.Read || promises.Inspect || promises.Search || promises.Edit ||
		promises.WriteFile || promises.RunCommand || promises.LaunchAgent
	return promises
}

func compileClaimsWithoutTools(text string, evidence IntentToolEvidence) IntentClaimsWithoutTools {
	lower := strings.ToLower(text)
	edited := containsAny(lower, []string{
		"已修改",
		"已编辑",
		"已更新",
		"已写入",
		"已修复",
		"已实现",
		"已经修改",
		"已经编辑",
		"已经更新",
		"修改完成",
		"修复完成",
		"implemented",
		"updated",
		"edited",
		"patched",
		"fixed",
	})
	ran := containsAny(lower, []string{
		"已运行",
		"已执行",
		"已经运行",
		"已经执行",
		"跑了",
		"ran ",
		"executed ",
		"command completed",
	})
	verified := containsAny(lower, []string{
		"已验证",
		"验证通过",
		"测试通过",
		"已测试",
		"已经验证",
		"已经测试",
		"tests pass",
		"tests passed",
		"verified",
		"validated",
		"tested",
	})
	claims := IntentClaimsWithoutTools{
		Edited:   edited && !evidence.Edit && !evidence.WriteFile,
		Ran:      ran && !evidence.RunCommand,
		Verified: verified && !evidence.RunCommand,
	}
	claims.Any = claims.Edited || claims.Ran || claims.Verified
	return claims
}

func compileToolEvidence(toolNames []string, finalPrompt string) IntentToolEvidence {
	evidence := IntentToolEvidence{}
	for _, name := range toolNames {
		evidence.addToolName(name)
	}
	if strings.TrimSpace(finalPrompt) != "" {
		turn := strings.ToLower(latestConversationTurnBlock(finalPrompt))
		for _, name := range promptToolEvidenceNames(turn) {
			evidence.addToolName(name)
		}
	}
	evidence.Any = evidence.Read || evidence.Search || evidence.Edit ||
		evidence.WriteFile || evidence.RunCommand || evidence.LaunchAgent
	return evidence
}

func (e *IntentToolEvidence) addToolName(name string) {
	canonical := CanonicalTaskOutputToolName(name)
	switch canonical {
	case "read", "view", "open":
		e.Read = true
	case "grep", "glob", "search", "find":
		e.Search = true
	case "edit", "multiedit", "multieditfile", "update", "applypatch", "applydiff", "apply":
		e.Edit = true
	case "write", "createfile":
		e.WriteFile = true
	case "bash", "shell", "exec", "runcommand", "terminal":
		e.RunCommand = true
	case "agent", "task":
		e.LaunchAgent = true
	}
}

func promptToolEvidenceNames(turn string) []string {
	candidates := []string{
		"Read", "Grep", "Glob", "Search", "Find",
		"Edit", "MultiEdit", "Update", "ApplyPatch", "ApplyDiff",
		"Write", "Bash", "Shell", "Exec", "RunCommand", "Agent", "Task",
	}
	out := []string{}
	for _, name := range candidates {
		canonical := strings.ToLower(name)
		for _, marker := range []string{
			`invoke name="` + canonical + `"`,
			`"name":"` + canonical + `"`,
			`<tool>` + canonical,
			`<tool_name>` + canonical + `</tool_name>`,
		} {
			if strings.Contains(turn, marker) {
				out = append(out, name)
				break
			}
		}
	}
	return out
}

func compilePureAnalysis(latestUser, finalText string, promises IntentTextPromises) bool {
	if promises.Edit || promises.WriteFile || promises.RunCommand || promises.LaunchAgent {
		return false
	}
	lower := strings.ToLower(latestUser + " " + finalText)
	if strings.TrimSpace(lower) == "" {
		return false
	}
	if containsAny(lower, []string{
		"纯分析",
		"只分析",
		"仅分析",
		"只评估",
		"仅评估",
		"只审查",
		"仅审查",
		"只 review",
		"仅 review",
		"不改代码",
		"不修改",
		"不要改",
		"不写文件",
		"analysis only",
		"review only",
		"assessment only",
		"evaluate only",
		"read-only",
		"read only",
		"no edits",
		"do not edit",
	}) {
		return true
	}
	if !containsAny(lower, []string{"分析", "评估", "审查", "review", "assess", "evaluate", "analysis"}) {
		return false
	}
	if containsAny(lower, []string{
		"实现",
		"修复",
		"修改",
		"写入",
		"执行",
		"运行",
		"启动",
	}) {
		return false
	}
	return !executionProofContainsAnyEnglishWord(lower, []string{
		"implement",
		"fix",
		"modify",
		"edit",
		"run",
		"execute",
		"launch",
	})
}

func compileAgentLaunchIntent(latestUser, finalText string) IntentAgentLaunch {
	text := strings.TrimSpace(finalText + " " + latestUser)
	lower := strings.ToLower(text)
	if !containsAny(lower, []string{"agent", "代理", "子代理", "team agents"}) {
		return IntentAgentLaunch{}
	}
	present := containAnyAgentLaunchPatterns(lower)
	if !present {
		present = containsAny(lower, []string{
			"启动",
			"创建",
			"运行",
			"生成",
			"发起",
			"调用",
			"准备",
			"派",
			"开",
			"launch",
			"start",
			"create",
			"spawn",
			"run",
		})
	}
	if !present {
		return IntentAgentLaunch{}
	}
	count := agentSchedulerExtractExplicitCount(text)
	return IntentAgentLaunch{
		Present:    true,
		Count:      count,
		CountKnown: count > 0,
	}
}

func hasPromiseFor(lower string, verbs []string, phrases []string) bool {
	if containsAny(lower, phrases) {
		return true
	}
	for _, prefix := range intentFuturePrefixes() {
		idx := strings.Index(lower, prefix)
		if idx < 0 {
			continue
		}
		tail := lower[idx+len(prefix):]
		if containsAnyIntentToken(tail, verbs) {
			return true
		}
	}
	return false
}

func containsAnyIntentToken(text string, tokens []string) bool {
	for _, token := range tokens {
		if containsIntentToken(text, token) {
			return true
		}
	}
	return false
}

func containsIntentToken(text, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if intentASCIIWordPattern.MatchString(token) {
		return containsToolNameToken(text, token)
	}
	return strings.Contains(text, token)
}

func normalizeIntentText(text string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

func intentFuturePrefixes() []string {
	return []string{
		"i'll",
		"i’ll",
		"i will",
		"i'm going to",
		"i am going to",
		"i need to",
		"let me",
		"now i'll",
		"now i will",
		"next i'll",
		"next i will",
		"我将",
		"我会",
		"我先",
		"让我",
		"先",
		"继续",
		"接下来",
		"现在",
		"现在开始",
		"开始",
		"马上",
		"下一步",
		"我准备",
		"我打算",
		"我要",
	}
}

func readPromiseVerbs() []string {
	return []string{"read", "open", "view", "读取", "阅读", "读", "查看", "看"}
}

func readPromisePhrases() []string {
	return []string{"now reading", "start reading", "先读取", "先读", "继续读取", "阅读现有代码"}
}

func inspectPromiseVerbs() []string {
	return []string{"examine", "inspect", "check", "analyze", "assess", "evaluate", "understand", "trace", "检查", "分析", "评估", "了解", "确认", "查"}
}

func inspectPromisePhrases() []string {
	return []string{"let me examine", "start examining", "先检查", "先确认", "先分析", "先评估", "先了解", "查一下"}
}

func searchPromiseVerbs() []string {
	return []string{"search", "grep", "glob", "find", "locate", "搜索", "查找", "定位", "检索"}
}

func searchPromisePhrases() []string {
	return []string{"let me search", "先搜索", "先查找", "先定位", "grep for", "search for"}
}

func editPromiseVerbs() []string {
	return []string{"edit", "modify", "patch", "fix", "implement", "update", "refactor", "change", "编辑", "修改", "修复", "实现", "更新", "改", "补"}
}

func editPromisePhrases() []string {
	return []string{"现在直接动手", "直接动手", "现在实现", "现在修改", "开始实现", "继续推进", "把代码写到位", "写到位"}
}

func writeFilePromiseVerbs() []string {
	return []string{"write", "create", "overwrite", "save", "生成", "创建", "写入", "写", "保存"}
}

func writeFilePromisePhrases() []string {
	return []string{"write file", "write the file", "create file", "create the file", "写文件", "写入文件", "保存到", "批量写入", "写到位"}
}

func runCommandPromiseVerbs() []string {
	return []string{"run", "execute", "test", "verify", "build", "运行", "执行", "测试", "验证", "构建", "跑"}
}

func runCommandPromisePhrases() []string {
	return []string{"i'll run", "i will run", "now running", "run tests", "running tests", "运行测试", "跑测试", "测试验证", "现在运行"}
}

var intentASCIIWordPattern = regexp.MustCompile(`^[a-z0-9_]+$`)
