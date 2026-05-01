package toolcall

import (
	"regexp"
	"strings"
)

func repairMissingArrayBrackets(colonPos int, s string) string {
	if colonPos < 0 || colonPos >= len(s) {
		return s
	}

	rest := s[colonPos+1:]
	rest = strings.TrimLeft(rest, " \t\r\n")
	if !strings.HasPrefix(rest, "{") {
		return s
	}

	// Find boundaries of all top-level sibling objects
	var objects []struct{ start, end int }
	pos := 0
	for pos < len(rest) {
		rem := strings.TrimLeft(rest[pos:], " \t\r\n")
		if !strings.HasPrefix(rem, "{") {
			break
		}
		objEnd := findObjectEnd(rem)
		if objEnd < 0 {
			break
		}
		objects = append(objects, struct{ start, end int }{pos + (len(rest[pos:]) - len(rem)), pos + (len(rest[pos:]) - len(rem)) + objEnd})
		pos = objects[len(objects)-1].end
		if pos >= len(rest) {
			break
		}
		after := strings.TrimLeft(rest[pos:], " \t\r\n")
		if !strings.HasPrefix(after, ",") {
			break
		}
		pos += len(rest[pos:]) - len(after) + 1 // skip past comma
	}

	if len(objects) < 2 {
		return s
	}

	prefix := s[:colonPos+1] + " ["
	inner := rest[:objects[len(objects)-1].end]
	return prefix + inner + "]"
}

func findObjectEnd(s string) int {
	if !strings.HasPrefix(s, "{") {
		return -1
	}
	depth := 0
	inString := false
	escaped := false
	for i, ch := range s {
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch ch {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func repairInvalidJSONBackslashes(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s) + 10)
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' {
			if i+1 < len(runes) {
				next := runes[i+1]
				switch next {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
					out.WriteRune('\\')
					out.WriteRune(next)
					i++
					continue
				case 'u':
					if i+5 < len(runes) {
						isHex := true
						for j := 1; j <= 4; j++ {
							r := runes[i+1+j]
							if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
								isHex = false
								break
							}
						}
						if isHex {
							out.WriteRune('\\')
							out.WriteRune('u')
							for j := 1; j <= 4; j++ {
								out.WriteRune(runes[i+1+j])
							}
							i += 5
							continue
						}
					}
				}
			}
			// Not a valid escape sequence, double it
			out.WriteString("\\\\")
		} else {
			out.WriteRune(runes[i])
		}
	}
	return out.String()
}

var unquotedKeyPattern = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
func RepairLooseJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// 1. Replace unquoted keys: {key: -> {"key":
	s = unquotedKeyPattern.ReplaceAllString(s, `$1"$2":`)

	// 2. Heuristic: Fix missing array brackets for list of objects.
	// Uses a stack-based bracket counter that handles arbitrary nesting depth,
	// replacing the old depth-limited regex approach.
	// e.g., : {obj1}, {obj2} -> : [{obj1}, {obj2}]
	offset := 0
	for {
		colonPos := strings.Index(s[offset:], ":")
		if colonPos < 0 {
			break
		}
		colonPos += offset
		repaired := repairMissingArrayBrackets(colonPos, s)
		if repaired == s {
			offset = colonPos + 1
			continue
		}
		s = repaired
		offset = 0
	}

	return s
}
