package toolcall

import (
	"fmt"
	"strings"
)

func coalesceParallelShellCalls(calls []ParsedToolCall) []ParsedToolCall {
	if len(calls) < 2 {
		return calls
	}
	shellCount := 0
	for _, call := range calls {
		if _, ok := shellCommandFromCall(call); ok {
			shellCount++
		}
	}
	if shellCount < 2 {
		return calls
	}

	commands := make([]string, 0, shellCount)
	firstShell := -1
	for i, call := range calls {
		if command, ok := shellCommandFromCall(call); ok {
			if firstShell < 0 {
				firstShell = i
			}
			commands = append(commands, command)
		}
	}
	if firstShell < 0 || len(commands) < 2 {
		return calls
	}

	out := make([]ParsedToolCall, 0, len(calls)-shellCount+1)
	for i, call := range calls {
		if _, ok := shellCommandFromCall(call); !ok {
			out = append(out, call)
			continue
		}
		if i != firstShell {
			continue
		}
		merged := call
		merged.Input = cloneSchemaMap(call.Input)
		field := shellCommandFieldName(call.Name)
		merged.Input[field] = mergedShellCommand(commands)
		if _, ok := merged.Input["description"]; ok {
			merged.Input["description"] = fmt.Sprintf("Run %d shell commands sequentially", len(commands))
		}
		out = append(out, merged)
	}
	return out
}

func shellCommandFromCall(call ParsedToolCall) (string, bool) {
	field := shellCommandFieldName(call.Name)
	if field == "" || call.Input == nil {
		return "", false
	}
	value, ok := inputValueForKnownRequiredField(call.Input, field)
	if !ok {
		return "", false
	}
	command := strings.TrimSpace(normalizeToolStringValue(fmt.Sprint(value)))
	return command, command != ""
}

func shellCommandFieldName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "bash", "execute_command":
		return "command"
	case "exec_command":
		return "cmd"
	default:
		return ""
	}
}

func mergedShellCommand(commands []string) string {
	var b strings.Builder
	b.WriteString("set +e\n")
	b.WriteString("__ds2_status=0\n")
	for i, command := range commands {
		fmt.Fprintf(&b, "printf '\\n[ds2api] command %d/%d\\n'\n", i+1, len(commands))
		fmt.Fprintf(&b, "cat <<'__DS2API_COMMAND_%d__'\n", i+1)
		b.WriteString(command)
		if !strings.HasSuffix(command, "\n") {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "__DS2API_COMMAND_%d__\n", i+1)
		fmt.Fprintf(&b, "printf '[ds2api] output %d/%d\\n'\n", i+1, len(commands))
		b.WriteString("(\n")
		b.WriteString(command)
		if !strings.HasSuffix(command, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString(")\n")
		b.WriteString("__ds2_code=$?\n")
		fmt.Fprintf(&b, "if [ \"$__ds2_code\" -ne 0 ]; then printf '[ds2api] command %d exited %%s\\n' \"$__ds2_code\" >&2; __ds2_status=\"$__ds2_code\"; fi\n", i+1)
	}
	b.WriteString("exit \"$__ds2_status\"")
	return b.String()
}
