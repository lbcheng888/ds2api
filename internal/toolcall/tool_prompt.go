package toolcall

import "strings"

// BuildToolCallInstructions generates the unified tool-calling instruction block
// used by all adapters (OpenAI, Claude, Gemini). It uses attention-optimized
// structure: rules → negative examples → positive examples → anchor.
//
// The toolNames slice should contain the actual tool names available in the
// current request; the function picks real names for examples.
func BuildToolCallInstructions(toolNames []string) string {
	return `Tool calls use this format:

<|DSML|tool_calls>
  <|DSML|invoke name="TOOL_NAME">
    <|DSML|parameter name="ARG"><![CDATA[value]]></|DSML|parameter>
  </|DSML|invoke>
</|DSML|tool_calls>

Rules:
- All string values use <![CDATA[...]]> (code, paths, text, queries, scripts).
- Numbers, booleans, null stay plain text. Objects use nested XML. Arrays repeat <item>.
- Use only parameter names from the tool schema. Do not invent fields.
- Do NOT wrap in Markdown fences. Output ONLY the tool block, no extra text.
- Bookkeeping/mode tools (TaskCreate, TaskUpdate, TodoWrite, EnterPlanMode) must be paired with a real execution tool when the user asked to execute.
- Do not put multiple Bash/shell commands in one tool_calls block; call one shell command, observe it, then continue.
- For Edit tools, copy old_string exactly from a fresh Read; re-Read after any edit failure.

` + buildCorrectToolExamples(toolNames)
}

func BuildTeamAgentInstructions(toolNames []string) string {
	hasAgent := false
	hasTaskOutput := false
	for _, name := range toolNames {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "agent", "task", "taskcreate":
			hasAgent = true
		case "taskoutput", "task_output":
			hasTaskOutput = true
		}
	}
	if !hasAgent && !hasTaskOutput {
		return ""
	}
	return `TEAM AGENTS:
1) Launch background Agent calls only when the requested work truly benefits from parallel independent analysis or implementation.
2) Put each Agent call in the normal tool-call block; include description, prompt, subagent_type, and run_in_background=true when available.
3) After an Agent is launched, wait for the runtime-provided task_id; never invent task IDs.
4) Use TaskOutput only with a task_id that already appeared in a tool result or runtime status.
5) Never use tool_id or tool-use-id for TaskOutput; the field name must be task_id.
`
}

type promptToolExample struct {
	name   string
	params string
}

func buildCorrectToolExamples(toolNames []string) string {
	names := uniqueToolNames(toolNames)
	examples := make([]string, 0, 2)

	if single, ok := firstBasicExample(names); ok {
		examples = append(examples, renderToolExampleBlock([]promptToolExample{single}))
	}

	if script, ok := firstScriptExample(names); ok {
		examples = append(examples, renderToolExampleBlock([]promptToolExample{script}))
	}

	if len(examples) == 0 {
		return ""
	}
	return "Examples:\n\n" + strings.Join(examples, "\n\n") + "\n\n"
}

func uniqueToolNames(toolNames []string) []string {
	names := make([]string, 0, len(toolNames))
	seen := map[string]bool{}
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func firstBasicExample(names []string) (promptToolExample, bool) {
	for _, name := range names {
		if params, ok := exampleBasicParams(name); ok {
			return promptToolExample{name: name, params: params}, true
		}
	}
	return promptToolExample{}, false
}

func firstScriptExample(names []string) (promptToolExample, bool) {
	for _, name := range names {
		if params, ok := exampleScriptParams(name); ok {
			return promptToolExample{name: name, params: params}, true
		}
	}
	return promptToolExample{}, false
}

func renderToolExampleBlock(calls []promptToolExample) string {
	var b strings.Builder
	b.WriteString("<|DSML|tool_calls>\n")
	for _, call := range calls {
		b.WriteString(`  <|DSML|invoke name="`)
		b.WriteString(call.name)
		b.WriteString(`">` + "\n")
		b.WriteString(indentPromptParameters(call.params, "    "))
		b.WriteString("\n  </|DSML|invoke>\n")
	}
	b.WriteString("</|DSML|tool_calls>")
	return b.String()
}

func indentPromptParameters(body, indent string) string {
	if strings.TrimSpace(body) == "" {
		return indent + `<|DSML|parameter name="content"></|DSML|parameter>`
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			lines[i] = line
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

func wrapParameter(name, inner string) string {
	return `<|DSML|parameter name="` + name + `">` + inner + `</|DSML|parameter>`
}

func exampleBasicParams(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "Read":
		return wrapParameter("file_path", promptCDATA("README.md")), true
	case "Glob":
		return wrapParameter("pattern", promptCDATA("**/*.go")) + "\n" + wrapParameter("path", promptCDATA(".")), true
	case "read_file":
		return wrapParameter("path", promptCDATA("src/main.go")), true
	case "list_files":
		return wrapParameter("path", promptCDATA(".")), true
	case "search_files":
		return wrapParameter("query", promptCDATA("tool call parser")), true
	case "Bash", "execute_command":
		return wrapParameter("command", promptCDATA("pwd")), true
	case "exec_command":
		return wrapParameter("cmd", promptCDATA("pwd")), true
	case "Write":
		return wrapParameter("file_path", promptCDATA("notes.txt")) + "\n" + wrapParameter("content", promptCDATA("Hello world")), true
	case "write_to_file":
		return wrapParameter("path", promptCDATA("notes.txt")) + "\n" + wrapParameter("content", promptCDATA("Hello world")), true
	case "Edit", "Update":
		return wrapParameter("file_path", promptCDATA("README.md")) + "\n" + wrapParameter("old_string", promptCDATA("## Install\n\nRun `make test` before submitting.\n")) + "\n" + wrapParameter("new_string", promptCDATA("## Install\n\nRun `make test && make lint` before submitting.\n")), true
	case "MultiEdit":
		return wrapParameter("file_path", promptCDATA("README.md")) + "\n" + `<|DSML|parameter name="edits"><item><old_string>` + promptCDATA("## Install\n\nRun `make test` before submitting.\n") + `</old_string><new_string>` + promptCDATA("## Install\n\nRun `make test && make lint` before submitting.\n") + `</new_string></item></|DSML|parameter>`, true
	}
	return "", false
}

func exampleScriptParams(name string) (string, bool) {
	scriptCommand := `cat > /tmp/test_escape.sh <<'EOF'
#!/bin/bash
echo 'single "double"'
echo "literal dollar: \$HOME"
EOF
bash /tmp/test_escape.sh`
	scriptContent := `#!/bin/bash
echo 'single "double"'
echo "literal dollar: $HOME"`

	switch strings.TrimSpace(name) {
	case "Bash":
		return wrapParameter("command", promptCDATA(scriptCommand)) + "\n" + wrapParameter("description", promptCDATA("Test shell escaping")), true
	case "execute_command":
		return wrapParameter("command", promptCDATA(scriptCommand)), true
	case "exec_command":
		return wrapParameter("cmd", promptCDATA(scriptCommand)), true
	case "Write":
		return wrapParameter("file_path", promptCDATA("test_escape.sh")) + "\n" + wrapParameter("content", promptCDATA(scriptContent)), true
	case "write_to_file":
		return wrapParameter("path", promptCDATA("test_escape.sh")) + "\n" + wrapParameter("content", promptCDATA(scriptContent)), true
	}
	return "", false
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
