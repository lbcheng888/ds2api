package toolcall

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
)

var toolCallElementPattern = regexp.MustCompile(`(?is)<tool_call\b([^>]*)>(.*?)</tool_call>`)

func RepairMalformedToolCallXML(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	originalLower := strings.ToLower(text)
	repaired := repairMissingToolCallsOpenAngle(text)
	repaired = toolCallElementPattern.ReplaceAllStringFunc(repaired, repairToolCallElement)
	if strings.Contains(originalLower, "<tool_call") &&
		!strings.Contains(originalLower, "<tool_calls") &&
		!strings.Contains(originalLower, "<tools") &&
		strings.Contains(strings.ToLower(repaired), "<invoke") {
		repaired = "<tool_calls>" + repaired + "</tool_calls>"
	}
	return repaired
}

func repairMissingToolCallsOpenAngle(text string) string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	leadingLen := len(text) - len(trimmed)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "tool_calls>"):
		return text[:leadingLen] + "<" + trimmed
	case strings.HasPrefix(lower, "tool_calls\n"), strings.HasPrefix(lower, "tool_calls\r\n"):
		lineEnd := strings.IndexAny(trimmed, "\r\n")
		return text[:leadingLen] + "<tool_calls>" + trimmed[lineEnd:]
	default:
		return text
	}
}

func repairToolCallElement(raw string) string {
	m := toolCallElementPattern.FindStringSubmatch(raw)
	if len(m) < 3 {
		return raw
	}
	attrs := parseXMLTagAttributes(m[1])
	body := strings.TrimSpace(m[2])
	name := strings.TrimSpace(html.UnescapeString(attrs["name"]))
	if name == "" {
		name = firstXMLTextElement(body, "tool_name")
	}
	if name == "" {
		if fnName, _ := functionStyleJSONCall(body); fnName != "" {
			name = fnName
		}
	}
	if name == "" {
		return raw
	}
	body = removeXMLTextElement(body, "tool_name")
	body = removeXMLTextElement(body, "tool_call_id")
	body = strings.TrimSpace(body)

	if paramsBody, ok := firstXMLElementBody(body, "parameters"); ok {
		body = parametersBodyToInvokeParameters(paramsBody)
	} else if jsonBody := functionStyleJSONBody(body, name); jsonBody != "" {
		body = jsonBody
	} else if _, jsonBody := functionStyleJSONCall(body); jsonBody != "" {
		body = jsonBody
	} else {
		body = normalizeLegacyParamTags(body)
	}
	return `<invoke name="` + html.EscapeString(name) + `">` + body + `</invoke>`
}

func firstXMLTextElement(text, tag string) string {
	body, ok := firstXMLElementBody(text, tag)
	if !ok {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(body))
}

func firstXMLElementBody(text, tag string) (string, bool) {
	blocks := findXMLElementBlocks(text, tag)
	if len(blocks) == 0 {
		return "", false
	}
	return blocks[0].Body, true
}

func removeXMLTextElement(text, tag string) string {
	pattern := regexp.MustCompile(`(?is)<` + regexp.QuoteMeta(tag) + `\b[^>]*>.*?</` + regexp.QuoteMeta(tag) + `>`)
	return pattern.ReplaceAllString(text, "")
}

func parametersBodyToInvokeParameters(body string) string {
	kv := parseMarkupKVObject(body)
	if len(kv) == 0 {
		return strings.TrimSpace(body)
	}
	var b strings.Builder
	for _, match := range toolCallMarkupKVPattern.FindAllStringSubmatch(strings.TrimSpace(body), -1) {
		if len(match) < 4 || !strings.EqualFold(match[1], match[3]) {
			continue
		}
		key := strings.TrimSpace(match[1])
		if key == "" {
			continue
		}
		value := parameterBodyString(parseMarkupValue(match[2]))
		b.WriteString(`<parameter name="`)
		b.WriteString(html.EscapeString(key))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(value))
		b.WriteString(`</parameter>`)
	}
	if b.Len() == 0 {
		return strings.TrimSpace(body)
	}
	return b.String()
}

func parameterBodyString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	}
}

func normalizeLegacyParamTags(body string) string {
	out := strings.TrimSpace(body)
	if out == "" {
		return ""
	}
	out = regexp.MustCompile(`(?is)<\s*param\b`).ReplaceAllString(out, "<parameter")
	out = regexp.MustCompile(`(?is)</\s*param\s*>`).ReplaceAllString(out, "</parameter>")
	out = regexp.MustCompile(`(?is)<\s*argument\b`).ReplaceAllString(out, "<parameter")
	out = regexp.MustCompile(`(?is)</\s*argument\s*>`).ReplaceAllString(out, "</parameter>")
	return out
}

func functionStyleJSONBody(body, name string) string {
	trimmed := strings.TrimSpace(body)
	prefix := name + "("
	if !strings.HasPrefix(trimmed, prefix) || !strings.HasSuffix(trimmed, ")") {
		return ""
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, prefix), ")"))
	if !strings.HasPrefix(inner, "{") {
		return ""
	}
	return inner
}

func functionStyleJSONCall(body string) (string, string) {
	trimmed := strings.TrimSpace(body)
	open := strings.Index(trimmed, "(")
	if open <= 0 || !strings.HasSuffix(trimmed, ")") {
		return "", ""
	}
	name := strings.TrimSpace(trimmed[:open])
	if name == "" || !isSimpleToolFunctionName(name) {
		return "", ""
	}
	inner := strings.TrimSpace(strings.TrimSuffix(trimmed[open+1:], ")"))
	if !strings.HasPrefix(inner, "{") {
		return "", ""
	}
	return name, inner
}

func isSimpleToolFunctionName(name string) bool {
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' || r == ':' || ('0' <= r && r <= '9') || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z') {
			continue
		}
		return false
	}
	return true
}
