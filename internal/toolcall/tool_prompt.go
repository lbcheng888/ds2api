package toolcall

import "strings"

// BuildTeamAgentInstructions adds focused rules for Claude Code Team Agents.
// It is separated from BuildToolCallInstructions so adapters can place the
// short agent block near the concrete tool schemas while sharing one source.
func BuildTeamAgentInstructions(toolNames []string) string {
	hasAgent := false
	hasTaskOutput := false
	for _, name := range toolNames {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "agent", "task":
			hasAgent = true
		case "taskoutput", "task_output":
			hasTaskOutput = true
		}
	}
	if !hasAgent && !hasTaskOutput {
		return ""
	}
	taskOutputRule := `4) When the client shows running or completed task IDs, call TaskOutput only for those concrete task_id values. Never use tool_id or tool-use-id for TaskOutput, never invent task IDs, and never retry an ID after "No task found".`
	if !hasTaskOutput {
		taskOutputRule = `4) If no background-result collection tool is listed, do not invent one or fabricate task IDs; keep working with direct tools and summarize only visible completed results.`
	}
	return `TEAM AGENTS:
1) If the user explicitly asks for Team Agents, multiple agents, parallel review, or subagents, launch Agent/task tool calls in this response; do not merely say agents will be launched.
2) Each Agent/task call must have concrete description, prompt, and subagent_type fields. The prompt must name the exact file/path scope or the exact question it owns.
3) Keep one lead in the main thread. Use at most 4 parallel Agent/task calls, avoid nested agents, and split write work into non-overlapping files.
` + taskOutputRule + `
5) If a background agent fails or stalls, report that concrete failure and finish the work with direct Read/Grep/Glob/Bash/Edit tools where possible.`
}

// BuildToolCallInstructions generates the unified tool-calling instruction block
// used by all adapters (OpenAI, Claude, Gemini). It uses attention-optimized
// structure: rules → negative examples → positive examples → anchor.
//
// The toolNames slice should contain the actual tool names available in the
// current request; the function picks real names for examples.
func BuildToolCallInstructions(toolNames []string) string {
	// Pick real tool names for examples; fall back to generic names.
	ex1 := "read_file"
	ex2 := "write_to_file"
	ex3 := "ask_followup_question"
	used := map[string]bool{}
	for _, n := range toolNames {
		switch {
		// Read/query-type tools
		case !used["ex1"] && matchAny(n, "read_file", "list_files", "search_files", "Read", "read", "Glob", "glob", "Grep", "grep"):
			ex1 = n
			used["ex1"] = true
		// Write/execute-type tools
		case !used["ex2"] && matchAny(n, "write_to_file", "apply_diff", "execute_command", "exec_command", "Write", "write", "Edit", "edit", "MultiEdit", "Bash", "bash"):
			ex2 = n
			used["ex2"] = true
		// Interactive/meta tools
		case !used["ex3"] && !IsTaskTrackingToolName(n) && matchAny(n, "ask_followup_question", "attempt_completion", "update_todo_list", "Task", "task", "Agent", "agent", "TaskOutput", "taskoutput"):
			ex3 = n
			used["ex3"] = true
		}
	}
	if !used["ex1"] {
		if used["ex2"] {
			ex1 = ex2
		} else if name := firstPromptToolName(toolNames); name != "" {
			ex1 = name
		}
	}
	if !used["ex2"] {
		if name := firstPromptToolName(toolNames); name != "" {
			ex2 = name
		}
	}
	if !used["ex3"] {
		if name := firstPromptToolName(toolNames); name != "" {
			ex3 = name
		}
	}
	ex1Params := exampleReadParams(ex1)
	ex2Params := exampleWriteOrExecParams(ex2)
	ex3Params := exampleInteractiveParams(ex3)
	ex4Params := exampleLongTextParams(ex2)
	hasTaskOutput := hasPromptToolName(toolNames, "TaskOutput", "taskoutput", "task_output")

	instructions := `TOOL CALL FORMAT — FOLLOW EXACTLY:

<tool_calls>
  <tool_call>
    <tool_name>TOOL_NAME_HERE</tool_name>
    <parameters>
      <PARAMETER_NAME>PARAMETER_VALUE</PARAMETER_NAME>
    </parameters>
  </tool_call>
</tool_calls>

RULES:
1) Use the <tool_calls> XML format only. Never emit JSON or function-call syntax.
2) Put one or more <tool_call> entries under a single <tool_calls> root.
3) Parameters must be XML, not JSON.
4) Simple string values should be plain XML text. Use <![CDATA[...]]> only for multiline text, code, scripts, file contents, or strings containing XML-sensitive characters such as <, >, or &.
5) Objects use nested XML elements. Arrays may repeat the same tag or use <item> children.
6) Numbers, booleans, and null stay plain text.
7) Use only the parameter names in the tool schema. Do not invent fields.
8) Include every field marked required in the tool schema.
9) Do NOT emit a stray ]]> token. If CDATA is used, it must be a complete <![CDATA[...]]> section inside one parameter.
10) Do NOT wrap XML in markdown fences. Do NOT output explanations, role markers, or internal monologue.
11) Use task/subagent tools only for genuinely independent large subtasks or when the user explicitly asks to launch agents/subagents. Launch at most 4 Agent/task calls in one response; use direct read/glob/grep/bash-style tools for the rest.
12) If a task/subagent tool was already used and returned enough information, answer from that result immediately in visible text instead of launching another task/subagent.
13) Do not call TaskCreate, TaskUpdate, TodoWrite, or TodoRead. They only update the client's task UI and do not inspect, edit, run, or verify code.
14) A response whose only tool calls are task-tracking tools is invalid. If you need a plan, write it briefly in reasoning, then call real work tools such as Read, Grep, Glob, Bash, Edit, MultiEdit, Agent, or TaskOutput.
15) Never use placeholder argument values such as file_path, path, TODO, or /path/to/file. Tool arguments must contain concrete values from the current request or tool results.
16) If you need to inspect, read, search, edit, run, implement, patch, or verify anything, emit the next <tool_calls> block in this same response. Do not end with future-tense or setup text such as "I'll read", "I'll implement", "let me run", "next I will", "只需要补", "需要改", or "还要新增".
17) With tools available, a response that only promises future action is invalid. Either call the needed tool now, or provide the final answer if the work is actually complete.
18) For Edit/MultiEdit-style tools, old_string must be copied exactly from the latest file content you read, including whitespace and newlines. It must be unique in that file. If an edit fails, read that file again before retrying; do not retry with the same old_string.
19) Prefer small, targeted Edit/MultiEdit replacements over replacing long stale blocks. Never build old_string from a diff hunk or from memory; use the current file text.
20) Do not use Write/write_to_file to rewrite an existing source file or a large file. For existing files, use Edit/MultiEdit/apply_diff-style tools with exact old_string replacements. Use Write only for new small files.
21) If the user asks to optimize, improve, fix, continue, proceed, "请优化", "继续", "按建议推进", or "直接改", choose the highest-priority actionable change from prior findings and call the needed tools now.
22) Do not use question/ask_followup_question to ask the user to pick among your own recommended directions after they asked to optimize or proceed. Use question only for a true blocker such as missing credentials, destructive approval, or mutually exclusive product requirements.
23) If you receive <task-notification> or need to wait for background agents, call the available TaskOutput-style tool with concrete task_id values now. Do not use tool_id or tool-use-id as the TaskOutput task_id. Do not answer only with reasoning or future-tense waiting text.
24) Do not use read_mcp_resource for file:// URLs, skill:// URIs, local disk paths, or skill files. Use Read/read/Grep/Glob/Bash/exec_command-style tools for local files. Only use read_mcp_resource for MCP resources from a real listed MCP server, and its resource parameter is uri, not url.
25) Search budget: do not repeat semantically identical Search/Grep/Glob/Bash rg calls. Changing escaping, whitespace, grouping, or directory spelling does not make it a new search.
26) Once Search/Grep/Bash rg returns a useful file path or file:line result, the next tool call must read a small bounded window around that location, edit that location, or run a targeted verification. Do not keep broadening the same regex.
27) If two searches produce no new file path or file:line result, stop searching and proceed from the existing candidate files.
28) Do not invent absolute repository paths. Use the current working directory, paths supplied by the user, or paths returned by tools. If a path contains a generated suffix such as repo-<hash> or project-<hash>, verify it exists with Bash before Read/Edit; otherwise use a cwd-relative path.
29) Read/read file path parameters must be one concrete string path per tool call. Do not pass an array of files to one Read call; emit multiple Read calls instead.
30) Bash/execute_command command must be a complete shell command. Never call Bash with only shell punctuation or quotes such as >, <, |, &, ;, ", ', or ` + "`" + `.

PARAMETER SHAPES:
- string => <name>value</name>
- multiline/code string => <name><![CDATA[value]]></name>
- object => nested XML elements
- array => repeated tags or <item> children
- number/bool/null => plain text

【WRONG — Do NOT do these】:

Wrong 1 — mixed text after XML:
  <tool_calls>...</tool_calls> I hope this helps.
Wrong 2 — function-call syntax:
  Grep({"pattern": "token"})
Wrong 3 — JSON parameters:
  <tool_call><tool_name>` + ex1 + `</tool_name><parameters>{"path":"x"}</parameters></tool_call>
Wrong 4 — Markdown code fences:
  ` + "```xml" + `
  <tool_calls>...</tool_calls>
  ` + "```" + `

Remember: The ONLY valid way to use tools is the <tool_calls> XML block at the end of your response.

【CORRECT EXAMPLES】:

Example A — Single tool:
<tool_calls>
  <tool_call>
    <tool_name>` + ex1 + `</tool_name>
    <parameters>` + ex1Params + `</parameters>
  </tool_call>
</tool_calls>

Example B — Two tools in parallel:
<tool_calls>
  <tool_call>
    <tool_name>` + ex1 + `</tool_name>
    <parameters>` + ex1Params + `</parameters>
  </tool_call>
  <tool_call>
    <tool_name>` + ex2 + `</tool_name>
    <parameters>` + ex2Params + `</parameters>
  </tool_call>
</tool_calls>

Example C — Tool with nested XML parameters:
<tool_calls>
  <tool_call>
    <tool_name>` + ex3 + `</tool_name>
    <parameters>` + ex3Params + `</parameters>
  </tool_call>
</tool_calls>
 
Example D — Tool with long script using CDATA (RELIABLE FOR CODE/SCRIPTS):
<tool_calls>
  <tool_call>
    <tool_name>` + ex2 + `</tool_name>
    <parameters>` + ex4Params + `</parameters>
  </tool_call>
</tool_calls>

`
	if !hasTaskOutput {
		instructions = strings.Replace(instructions,
			"14) A response whose only tool calls are task-tracking tools is invalid. If you need a plan, write it briefly in reasoning, then call real work tools such as Read, Grep, Glob, Bash, Edit, MultiEdit, Agent, or TaskOutput.",
			"14) A response whose only tool calls are task-tracking tools is invalid. If you need a plan, write it briefly in reasoning, then call real work tools such as Read, Grep, Glob, Bash, Edit, MultiEdit, or Agent.",
			1,
		)
		instructions = strings.Replace(instructions,
			"23) If you receive <task-notification> or need to wait for background agents, call the available TaskOutput-style tool with concrete task_id values now. Do not use tool_id or tool-use-id as the TaskOutput task_id. Do not answer only with reasoning or future-tense waiting text.",
			"23) If background agents exist but no background-result collection tool is listed, do not invent one; use available direct tools or provide visible status from completed results.",
			1,
		)
	}
	return instructions
}

func matchAny(name string, candidates ...string) bool {
	for _, c := range candidates {
		if name == c {
			return true
		}
	}
	return false
}

func hasPromptToolName(toolNames []string, candidates ...string) bool {
	for _, name := range toolNames {
		if matchAny(strings.TrimSpace(name), candidates...) {
			return true
		}
	}
	return false
}

func firstPromptToolName(toolNames []string) string {
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" || IsTaskTrackingToolName(name) {
			continue
		}
		return name
	}
	return ""
}

func exampleReadParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Read":
		return `<file_path>README.md</file_path>`
	case "read":
		return `<filePath>README.md</filePath>`
	case "Glob", "glob":
		return `<pattern>**/*.go</pattern><path>.</path>`
	case "Grep", "grep":
		return `<pattern>TODO</pattern><path>.</path>`
	default:
		return `<path>src/main.go</path>`
	}
}

func exampleWriteOrExecParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Bash", "bash":
		return `<command>pwd</command><description>Show current directory</description>`
	case "execute_command":
		return `<command>pwd</command>`
	case "exec_command":
		return `<cmd>pwd</cmd>`
	case "write":
		return `<filePath>/tmp/example.txt</filePath><content>Hello world</content>`
	case "edit":
		return `<filePath>README.md</filePath><oldString>foo</oldString><newString>bar</newString>`
	case "Edit":
		return `<file_path>README.md</file_path><old_string>foo</old_string><new_string>bar</new_string>`
	case "MultiEdit":
		return `<file_path>README.md</file_path><edits><old_string>foo</old_string><new_string>bar</new_string></edits>`
	default:
		return `<path>output.txt</path><content>Hello world</content>`
	}
}

func exampleLongTextParams(name string) string {
	script := promptCDATA(`#!/bin/bash
if [ "$1" = "test" ]; then
  echo "Success!"
fi`)
	switch strings.TrimSpace(name) {
	case "Bash", "bash":
		return `<command>` + script + `</command><description>Run test shell script</description>`
	case "execute_command":
		return `<command>` + script + `</command>`
	case "exec_command":
		return `<cmd>` + script + `</cmd>`
	case "write":
		return `<filePath>` + promptCDATA("/tmp/script.sh") + `</filePath><content>` + script + `</content>`
	default:
		return `<path>` + promptCDATA("script.sh") + `</path><content>` + script + `</content>`
	}
}

func exampleInteractiveParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Task", "task":
		return `<description>Investigate flaky tests</description><prompt>Run targeted tests and summarize failures</prompt><subagent_type>general</subagent_type>`
	case "Agent", "agent":
		return `<description>Explore Cheng codebase</description><prompt>Inspect the repository structure and report concise actionable findings.</prompt><subagent_type>Explore</subagent_type>`
	case "TaskOutput", "taskoutput":
		return `<task_id>task_123</task_id><block>false</block><timeout>5000</timeout>`
	default:
		return `<question>Which approach do you prefer?</question><follow_up><text>Option A</text></follow_up><follow_up><text>Option B</text></follow_up>`
	}
}

func promptCDATA(text string) string {
	if text == "" {
		return ""
	}
	if strings.Contains(text, "]]>") {
		return "<![CDATA[" + strings.ReplaceAll(text, "]]>", "]]]]><![CDATA[>") + "]]>"
	}
	return "<![CDATA[" + text + "]]>"
}
