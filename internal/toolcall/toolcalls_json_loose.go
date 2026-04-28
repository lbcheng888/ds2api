package toolcall

import "strings"

func visibleJSONLooseBlockEnd(text string) int {
	if text == "" || (text[0] != '{' && text[0] != '[') {
		return -1
	}
	open, close := byte('{'), byte('}')
	if text[0] == '[' {
		open, close = '[', ']'
	}
	depth := 0
	lastEnd := -1
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				lastEnd = i + 1
				rest := strings.TrimLeft(text[lastEnd:], " \t\r\n")
				if rest == "" {
					return lastEnd
				}
				if text[0] == '{' && strings.HasPrefix(rest, "{") {
					i = len(text[:lastEnd]) + len(text[lastEnd:]) - len(rest) - 1
					continue
				}
				return lastEnd
			}
			if depth < 0 {
				return -1
			}
		}
	}
	return lastEnd
}

func repairVisibleJSONLooseCommandStrings(text string) string {
	lines := strings.Split(text, "\n")
	changed := false
	for i, line := range lines {
		repaired := repairVisibleJSONLooseCommandStringLine(line)
		if repaired != line {
			lines[i] = repaired
			changed = true
		}
	}
	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
}

func repairVisibleJSONLooseCommandStringLine(line string) string {
	for _, key := range []string{"command", "cmd"} {
		needle := `"` + key + `": "`
		idx := strings.Index(line, needle)
		if idx < 0 {
			continue
		}
		valueStart := idx + len(needle)
		tail := line[valueStart:]
		rightTrimmed := strings.TrimRight(tail, " \t\r")
		spacing := tail[len(rightTrimmed):]
		comma := ""
		if strings.HasSuffix(rightTrimmed, ",") {
			comma = ","
			rightTrimmed = strings.TrimRight(strings.TrimSuffix(rightTrimmed, ","), " \t\r")
		}
		if !strings.HasSuffix(rightTrimmed, `"`) {
			continue
		}
		value := strings.TrimSuffix(rightTrimmed, `"`)
		return line[:valueStart] + escapeLooseJSONStringInnerQuotes(value) + `"` + comma + spacing
	}
	return line
}

func escapeLooseJSONStringInnerQuotes(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '"' && !isEscapedAt(value, i) {
			out.WriteByte('\\')
		}
		out.WriteByte(value[i])
	}
	return out.String()
}

func isEscapedAt(s string, idx int) bool {
	backslashes := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}
