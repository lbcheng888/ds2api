package toolcall

import (
	"encoding/json"
	"encoding/xml"
	"html"
	"regexp"
	"strings"
)

var xmlToolCallPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:tool_call|toolcall)\b[^>]*>\s*(.*?)\s*</(?:[a-z0-9_:-]+:)?(?:tool_call|toolcall)>`)
var xmlToolCallOpenPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:tool_call|toolcall)\b[^>]*>`)
var xmlToolCallClosePattern = regexp.MustCompile(`(?is)</(?:[a-z0-9_:-]+:)?(?:tool_call|toolcall)>`)
var xmlToolCallsClosePattern = regexp.MustCompile(`(?is)</(?:[a-z0-9_:-]+:)?tool_calls>`)
var missingToolCallOpenAnglePattern = regexp.MustCompile(`(?is)(^|[^</])\b(tool_calls|tool_call)\s*>`)
var looseDirectXMLToolElementOpenPattern = regexp.MustCompile(`(?is)^<([a-z0-9_:-]+)\b[^>]*>`)
var looseWrapperParameterValuePattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?parameters\b[^>]*>\s*<([a-z0-9_\-.]+)\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?parameters>`)
var functionCallPattern = regexp.MustCompile(`(?is)<function_call>\s*([^<]+?)\s*</function_call>`)
var functionParamPattern = regexp.MustCompile(`(?is)<function\s+parameter\b([^>]*)>\s*(.*?)\s*</function\s+parameter>`)
var antmlFunctionCallPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?function_call\b([^>]*)>\s*(.*?)\s*</(?:[a-z0-9_]+:)?function_call>`)
var antmlArgumentPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_]+:)?argument\b([^>]*)>\s*(.*?)\s*</(?:[a-z0-9_]+:)?argument>`)
var invokeCallPattern = regexp.MustCompile(`(?is)<invoke\b([^>]*)>(.*?)</invoke>`)
var invokeParamPattern = regexp.MustCompile(`(?is)<parameter\b([^>]*)>\s*(.*?)\s*</parameter>`)
var directToolElementPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?tool>`)
var namedParameterPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:parameter|argument|param)\b([^>]*)>(.*?)</(?:[a-z0-9_:-]+:)?(?:parameter|argument|param)>`)
var selfClosingNamedParameterPattern = regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:parameter|argument|param)\b([^>]*)/>`)
var markupAttrPattern = regexp.MustCompile(`(?is)\b([a-z0-9_:-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'<>/]+))`)
var toolUseFunctionPattern = regexp.MustCompile(`(?is)<tool_use>\s*<function\b([^>]*)>(.*?)</function>\s*</tool_use>`)
var toolUseNameParametersPattern = regexp.MustCompile(`(?is)<tool_use>\s*<tool_name>\s*([^<]+?)\s*</tool_name>\s*<parameters>\s*(.*?)\s*</parameters>\s*</tool_use>`)
var toolUseFunctionNameParametersPattern = regexp.MustCompile(`(?is)<tool_use>\s*<function_name>\s*([^<]+?)\s*</function_name>\s*<parameters>\s*(.*?)\s*</parameters>\s*</tool_use>`)
var toolUseToolNameBodyPattern = regexp.MustCompile(`(?is)<tool_use>\s*<tool_name>\s*([^<]+?)\s*</tool_name>\s*(.*?)\s*</tool_use>`)
var xmlToolNamePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_name\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?tool_name>`),
	regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?function_name\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?function_name>`),
	regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?tool_call_name\b[^>]*>(.*?)</(?:[a-z0-9_:-]+:)?tool_call_name>`),
	regexp.MustCompile(`(?is)<(?:[a-z0-9_:-]+:)?(?:tool_name|function_name|tool_call_name)\s*=\s*["']?([^<>"'\s/]+)\s*</(?:[a-z0-9_:-]+:)?(?:tool_name|function_name|tool_call_name)>`),
}

func parseXMLToolCalls(text string) []ParsedToolCall {
	text = RepairMalformedToolCallXML(text)
	text = repairMissingToolCallClose(text)
	if calls := parseDirectToolElementCalls(text); len(calls) > 0 {
		return calls
	}
	matches := xmlToolCallPattern.FindAllString(text, -1)
	out := make([]ParsedToolCall, 0, len(matches)+1)
	for _, block := range matches {
		call, ok := parseSingleXMLToolCall(block)
		if !ok {
			continue
		}
		out = append(out, call)
	}
	if len(out) > 0 {
		return out
	}
	if call, ok := parseFunctionCallTagStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if calls := parseAntmlFunctionCallStyles(text); len(calls) > 0 {
		return calls
	}
	if call, ok := parseInvokeFunctionCallStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if call, ok := parseToolUseFunctionStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if call, ok := parseToolUseNameParametersStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if call, ok := parseToolUseFunctionNameParametersStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if call, ok := parseToolUseToolNameBodyStyle(text); ok {
		return []ParsedToolCall{call}
	}
	if calls := parseOrphanAgentParameterCalls(text); len(calls) > 0 {
		return calls
	}
	return nil
}

type markupParameterSegment struct {
	Key   string
	Value any
	Start int
	End   int
}

func parseOrphanAgentParameterCalls(text string) []ParsedToolCall {
	segments := markupParameterSegments(text)
	if len(segments) < 2 {
		return nil
	}
	out := []ParsedToolCall{}
	for i := 0; i+1 < len(segments); i++ {
		if !strings.EqualFold(segments[i].Key, "description") || !strings.EqualFold(segments[i+1].Key, "prompt") {
			continue
		}
		description := strings.TrimSpace(asString(segments[i].Value))
		prompt := strings.TrimSpace(asString(segments[i+1].Value))
		if description == "" || prompt == "" {
			continue
		}
		input := map[string]any{
			"description": description,
			"prompt":      prompt,
		}
		consumed := i + 2
		if consumed < len(segments) && strings.EqualFold(segments[consumed].Key, "subagent_type") {
			if subagentType := strings.TrimSpace(asString(segments[consumed].Value)); subagentType != "" {
				input["subagent_type"] = subagentType
			}
			consumed++
		} else if subagentType := orphanAgentPlainSubagentType(text, segments[i+1].End, nextMarkupParameterStart(segments, consumed)); subagentType != "" {
			input["subagent_type"] = subagentType
		}
		out = append(out, ParsedToolCall{Name: "Agent", Input: input})
		i = consumed - 1
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func markupParameterSegments(text string) []markupParameterSegment {
	matches := namedParameterPattern.FindAllStringSubmatchIndex(text, -1)
	out := make([]markupParameterSegment, 0, len(matches))
	for _, m := range matches {
		if len(m) < 6 {
			continue
		}
		attrs := text[m[2]:m[3]]
		key := markupAttrValue(attrs, "name")
		if strings.TrimSpace(key) == "" {
			continue
		}
		value := parseMarkupValue(text[m[4]:m[5]])
		if value == nil {
			continue
		}
		out = append(out, markupParameterSegment{
			Key:   strings.TrimSpace(key),
			Value: value,
			Start: m[0],
			End:   m[1],
		})
	}
	return out
}

func nextMarkupParameterStart(segments []markupParameterSegment, idx int) int {
	if idx >= 0 && idx < len(segments) {
		return segments[idx].Start
	}
	return -1
}

func orphanAgentPlainSubagentType(text string, start, end int) string {
	if start < 0 || start > len(text) {
		return ""
	}
	if end < 0 || end > len(text) {
		end = len(text)
	}
	if end < start {
		return ""
	}
	gap := strings.TrimSpace(html.UnescapeString(text[start:end]))
	if gap == "" || strings.Contains(gap, "<") || len([]rune(gap)) > 120 {
		return ""
	}
	for _, line := range strings.Split(gap, "\n") {
		candidate := strings.TrimSpace(line)
		if isKnownSubagentTypeName(candidate) {
			return candidate
		}
	}
	return ""
}

func isKnownSubagentTypeName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "explore", "code-reviewer", "design", "general-purpose", "task-executor", "cheng-language", "debugger", "plan", "claude-code-guide", "statusline-setup", "steering-architect":
		return true
	default:
		return false
	}
}

func parseDirectToolElementCalls(text string) []ParsedToolCall {
	matches := directToolElementPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		name := markupAttrValue(m[1], "name", "function", "tool")
		if name == "" {
			continue
		}
		inner := strings.TrimSpace(m[2])
		input := extractXMLToolParamsByRegex(inner)
		if len(input) == 0 {
			input = parseMarkupKVObject(inner)
		}
		if input == nil {
			input = map[string]any{}
		}
		out = append(out, ParsedToolCall{Name: name, Input: input})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func repairMissingToolCallClose(text string) string {
	openCount := len(xmlToolCallOpenPattern.FindAllStringIndex(text, -1))
	closeCount := len(xmlToolCallClosePattern.FindAllStringIndex(text, -1))
	missing := openCount - closeCount
	if missing <= 0 {
		return text
	}
	out := text
	for i := 0; i < missing; i++ {
		loc := xmlToolCallsClosePattern.FindStringIndex(out)
		if loc == nil {
			out += "</tool_call>"
			continue
		}
		out = out[:loc[0]] + "</tool_call>" + out[loc[0]:]
	}
	return out
}

func parseSingleXMLToolCall(block string) (ParsedToolCall, bool) {
	inner := strings.TrimSpace(block)
	if m := xmlToolCallPattern.FindStringSubmatch(inner); len(m) >= 2 {
		inner = strings.TrimSpace(m[1])
	} else {
		inner = strings.TrimPrefix(inner, "<tool_call>")
		inner = strings.TrimSuffix(inner, "</tool_call>")
	}
	inner = strings.TrimSpace(inner)
	if strings.HasPrefix(inner, "{") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(inner), &payload); err == nil {
			name, input := parseJSONToolCallPayload(payload)
			if name != "" || len(input) > 0 {
				return ParsedToolCall{Name: name, Input: input}, true
			}
		}
	}
	if call, ok := parseFunctionStyleToolCallBody(inner); ok {
		return call, true
	}

	if call, ok := parseDirectXMLToolElement(inner); ok {
		return call, true
	}
	if call, ok := parseLooseDirectXMLToolElement(inner); ok {
		return call, true
	}

	name := ""
	params := extractXMLToolParamsByRegex(inner)
	dec := xml.NewDecoder(strings.NewReader(block))
	inTool := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			tag := strings.ToLower(t.Name.Local)
			switch tag {
			case "tool_calls":
				continue
			case "tool_call":
				for _, attr := range t.Attr {
					if strings.EqualFold(strings.TrimSpace(attr.Name.Local), "name") && strings.TrimSpace(name) == "" {
						name = strings.TrimSpace(attr.Value)
					}
				}
			case "tool":
				inTool = true
				for _, attr := range t.Attr {
					if strings.EqualFold(strings.TrimSpace(attr.Name.Local), "name") && strings.TrimSpace(name) == "" {
						name = strings.TrimSpace(attr.Value)
					}
				}
			case "parameters":
				var node struct {
					Inner string `xml:",innerxml"`
				}
				if err := dec.DecodeElement(&node, &t); err == nil {
					inner := strings.TrimSpace(node.Inner)
					if inner != "" {
						extracted := extractRawTagValue(inner)
						if parsed := parseStructuredToolCallInput(extracted); len(parsed) > 0 {
							for k, vv := range parsed {
								params[k] = vv
							}
						}
					}
				}
			case "tool_name", "function_name", "tool_call_name", "name":
				var v string
				if err := dec.DecodeElement(&v, &t); err == nil && strings.TrimSpace(v) != "" {
					name = strings.TrimSpace(v)
				}
			case "input", "arguments", "argument", "args", "params":
				var v string
				if err := dec.DecodeElement(&v, &t); err == nil && strings.TrimSpace(v) != "" {
					if parsed := parseStructuredToolCallInput(strings.TrimSpace(v)); len(parsed) > 0 {
						for k, vv := range parsed {
							params[k] = vv
						}
					}
				}
			default:
				if inTool {
					var v string
					if err := dec.DecodeElement(&v, &t); err == nil {
						params[t.Name.Local] = strings.TrimSpace(html.UnescapeString(v))
					}
				} else if !isXMLToolMetadataTag(tag) {
					var node struct {
						Inner string `xml:",innerxml"`
					}
					if err := dec.DecodeElement(&node, &t); err == nil {
						value := parseMarkupValue(node.Inner)
						if value != nil {
							appendMarkupValue(params, t.Name.Local, value)
						}
					}
				}
			}
		case xml.EndElement:
			tag := strings.ToLower(t.Name.Local)
			if tag == "tool" {
				inTool = false
			}
		}
	}
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSpace(html.UnescapeString(extractXMLToolNameByRegex(stripTopLevelXMLParameters(inner))))
	}
	if strings.TrimSpace(name) == "" {
		if len(params) > 0 {
			return ParsedToolCall{Name: "", Input: params}, true
		}
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{Name: strings.TrimSpace(html.UnescapeString(name)), Input: params}, true
}

func parseJSONToolCallPayload(payload map[string]any) (string, map[string]any) {
	name := strings.TrimSpace(asString(payload["tool"]))
	if name == "" {
		name = strings.TrimSpace(asString(payload["name"]))
	}
	if name == "" {
		name = strings.TrimSpace(asString(payload["tool_name"]))
	}
	if fn, ok := payload["function"].(map[string]any); ok {
		if name == "" {
			name = strings.TrimSpace(asString(fn["name"]))
		}
		if input := firstJSONToolCallInput(fn); len(input) > 0 {
			return name, input
		}
	}
	if input := firstJSONToolCallInput(payload); len(input) > 0 {
		return name, input
	}
	input := map[string]any{}
	for key, value := range payload {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "tool", "name", "tool_name", "function":
			continue
		default:
			input[key] = value
		}
	}
	return name, input
}

func parseFunctionStyleToolCallBody(inner string) (ParsedToolCall, bool) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" || !strings.HasSuffix(trimmed, ")") {
		return ParsedToolCall{}, false
	}
	open := strings.Index(trimmed, "(")
	if open <= 0 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(trimmed[:open])
	if !isFunctionStyleToolName(name) {
		return ParsedToolCall{}, false
	}
	raw := strings.TrimSpace(trimmed[open+1 : len(trimmed)-1])
	if !strings.HasPrefix(raw, "{") {
		return ParsedToolCall{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{Name: name, Input: parseToolCallInput(payload)}, true
}

func isFunctionStyleToolName(name string) bool {
	for i, r := range name {
		isAlpha := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if i == 0 {
			if isAlpha || r == '_' {
				continue
			}
			return false
		}
		if isAlpha || isDigit || r == '_' || r == '.' || r == '-' {
			continue
		}
		return false
	}
	return name != ""
}

func firstJSONToolCallInput(obj map[string]any) map[string]any {
	for _, key := range []string{"arguments", "input", "params", "parameters"} {
		if value, ok := obj[key]; ok {
			return parseToolCallInput(value)
		}
	}
	return nil
}

func RepairMalformedToolCallXML(text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "tool_call") && !strings.Contains(lower, "toolcall") {
		return text
	}
	if !strings.Contains(lower, "<tool_call") &&
		!strings.Contains(lower, "<toolcall") &&
		!strings.Contains(lower, "</tool_call") &&
		!strings.Contains(lower, "</toolcall") &&
		!strings.Contains(lower, "<tool ") &&
		!strings.Contains(lower, "<invoke") &&
		!strings.Contains(lower, "<function_call") {
		return text
	}
	return missingToolCallOpenAnglePattern.ReplaceAllString(text, `${1}<${2}>`)
}

func parseLooseDirectXMLToolElement(inner string) (ParsedToolCall, bool) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" || !strings.HasPrefix(trimmed, "<") {
		return ParsedToolCall{}, false
	}
	start := looseDirectXMLToolElementOpenPattern.FindStringSubmatchIndex(trimmed)
	if len(start) < 4 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(trimmed[start[2]:start[3]])
	if name == "" || isXMLToolMetadataTag(name) {
		return ParsedToolCall{}, false
	}
	if idx := strings.LastIndex(name, ":"); idx >= 0 && idx+1 < len(name) {
		name = name[idx+1:]
	}
	body := strings.TrimSpace(trimmed[start[1]:])
	closeTag := "</" + strings.ToLower(name) + ">"
	if closeIdx := strings.LastIndex(strings.ToLower(body), closeTag); closeIdx >= 0 {
		body = strings.TrimSpace(body[:closeIdx])
	}
	input := extractXMLToolParamsByRegex(body)
	if len(input) == 0 || isOnlyRawValue(input, body) {
		input = parseMarkupKVObject(body)
	}
	if len(input) == 0 {
		input = parseStructuredToolCallInput(body)
	}
	if input == nil {
		input = map[string]any{}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func stripTopLevelXMLParameters(inner string) string {
	out := strings.TrimSpace(inner)
	for {
		idx := strings.Index(strings.ToLower(out), "<parameters")
		if idx < 0 {
			return out
		}
		segment := out[idx:]
		segmentLower := strings.ToLower(segment)
		openEnd := strings.Index(segmentLower, ">")
		if openEnd < 0 {
			return out
		}
		closeIdx := strings.Index(segmentLower, "</parameters>")
		if closeIdx < 0 {
			return out[:idx]
		}
		end := idx + closeIdx + len("</parameters>")
		out = out[:idx] + out[end:]
	}
}

func extractXMLToolNameByRegex(inner string) string {
	for _, pattern := range xmlToolNamePatterns {
		if m := pattern.FindStringSubmatch(inner); len(m) >= 2 {
			if v := strings.TrimSpace(stripTagText(m[1])); v != "" {
				return v
			}
		}
	}
	return ""
}

func extractXMLToolParamsByRegex(inner string) map[string]any {
	if named := parseNamedMarkupParameters(inner); len(named) > 0 {
		return named
	}
	raw := findMarkupTagValue(inner, toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag)
	if raw == "" {
		raw = extractLooseMarkupArguments(inner)
	}
	if raw == "" {
		return map[string]any{}
	}
	if named := parseNamedMarkupParameters(raw); len(named) > 0 {
		return named
	}
	parsed := parseMarkupInput(raw)
	if len(parsed) == 0 || isOnlyRawValue(parsed, raw) {
		loose := parseLooseWrapperParameterValues(inner)
		if len(loose) > 0 {
			for k, v := range parseMarkupKVObject(stripTopLevelXMLParameters(inner)) {
				loose[k] = v
			}
			return loose
		}
	}
	if parsed == nil {
		return map[string]any{}
	}
	return parsed
}

func parseLooseWrapperParameterValues(inner string) map[string]any {
	matches := looseWrapperParameterValuePattern.FindAllStringSubmatch(strings.TrimSpace(inner), -1)
	if len(matches) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		key := strings.TrimSpace(m[1])
		if key == "" || isXMLToolMetadataTag(key) {
			continue
		}
		value := parseMarkupValue(m[2])
		if value == nil {
			continue
		}
		appendMarkupValue(out, key, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func extractLooseMarkupArguments(inner string) string {
	lower := strings.ToLower(inner)
	for _, tag := range toolCallMarkupArgsTagNames {
		openNeedle := "<" + tag
		openIdx := strings.Index(lower, openNeedle)
		if openIdx < 0 {
			continue
		}
		openEnd := strings.Index(lower[openIdx:], ">")
		if openEnd < 0 {
			continue
		}
		bodyStart := openIdx + openEnd + 1
		closeNeedle := "</" + tag + ">"
		bodyEnd := strings.Index(lower[bodyStart:], closeNeedle)
		if bodyEnd >= 0 {
			return strings.TrimSpace(inner[bodyStart : bodyStart+bodyEnd])
		}
		return strings.TrimSpace(inner[bodyStart:])
	}
	return ""
}

func parseDirectXMLToolElement(inner string) (ParsedToolCall, bool) {
	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return ParsedToolCall{}, false
	}
	dec := xml.NewDecoder(strings.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return ParsedToolCall{}, false
	}
	start, ok := tok.(xml.StartElement)
	if !ok {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(start.Name.Local)
	if name == "" || isXMLToolMetadataTag(name) {
		return ParsedToolCall{}, false
	}
	if strings.EqualFold(name, "tool") {
		name = xmlToolNameAttribute(start)
		if name == "" {
			return ParsedToolCall{}, false
		}
	}
	var node struct {
		Inner string `xml:",innerxml"`
	}
	if err := dec.DecodeElement(&node, &start); err != nil {
		return ParsedToolCall{}, false
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if ch, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(ch)) == "" {
			continue
		}
		return ParsedToolCall{}, false
	}
	rawInput := strings.TrimSpace(node.Inner)
	input := extractXMLToolParamsByRegex(rawInput)
	if len(input) == 0 {
		input = parseStructuredToolCallInput(rawInput)
	}
	if strings.EqualFold(start.Name.Local, "tool") {
		if params, ok := input["parameters"].(map[string]any); ok {
			input = params
		}
	} else if len(input) == 0 || isOnlyRawValue(input, rawInput) {
		return ParsedToolCall{}, false
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func isXMLToolMetadataTag(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "tool_call", "tool_calls", "tool_use", "function_call", "function_calls", "tool_name", "function_name", "tool_call_name", "name", "parameters", "parameter", "param", "input", "arguments", "argument", "args", "params":
		return true
	default:
		return false
	}
}

func xmlToolNameAttribute(start xml.StartElement) string {
	for _, attr := range start.Attr {
		switch strings.ToLower(strings.TrimSpace(attr.Name.Local)) {
		case "name", "function", "tool":
			if strings.TrimSpace(attr.Value) != "" {
				return strings.TrimSpace(html.UnescapeString(attr.Value))
			}
		}
	}
	return ""
}

func parseNamedMarkupParameters(raw string) map[string]any {
	out := map[string]any{}
	matches := namedParameterPattern.FindAllStringSubmatch(strings.TrimSpace(raw), -1)
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		key := markupAttrValue(m[1], "name")
		if key == "" {
			continue
		}
		value := parseMarkupValue(m[2])
		if value == nil {
			continue
		}
		appendMarkupValue(out, key, value)
	}
	for _, m := range selfClosingNamedParameterPattern.FindAllStringSubmatch(strings.TrimSpace(raw), -1) {
		if len(m) < 2 {
			continue
		}
		key := markupAttrValue(m[1], "name")
		if key == "" {
			continue
		}
		value := markupAttrValue(m[1], "value")
		if value == "" {
			continue
		}
		appendMarkupValue(out, key, html.UnescapeString(value))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseFunctionCallTagStyle(text string) (ParsedToolCall, bool) {
	m := functionCallPattern.FindStringSubmatch(text)
	if len(m) < 2 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(html.UnescapeString(m[1]))
	if name == "" {
		return ParsedToolCall{}, false
	}
	input := map[string]any{}
	for _, pm := range functionParamPattern.FindAllStringSubmatch(text, -1) {
		if len(pm) < 3 {
			continue
		}
		key := markupAttrValue(pm[1], "name")
		val := extractRawTagValue(pm[2])
		if key != "" {
			if parsed := parseStructuredToolCallInput(val); len(parsed) > 0 {
				if isOnlyRawValue(parsed, val) {
					input[key] = val
				} else {
					input[key] = parsed
				}
			}
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseAntmlFunctionCallStyles(text string) []ParsedToolCall {
	matches := antmlFunctionCallPattern.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]ParsedToolCall, 0, len(matches))
	for _, m := range matches {
		if call, ok := parseSingleAntmlFunctionCallMatch(m); ok {
			out = append(out, call)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseSingleAntmlFunctionCallMatch(m []string) (ParsedToolCall, bool) {
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := markupAttrValue(m[1], "name", "function")
	if name == "" {
		return ParsedToolCall{}, false
	}
	body := strings.TrimSpace(m[2])
	input := map[string]any{}
	if strings.HasPrefix(body, "{") {
		if err := json.Unmarshal([]byte(body), &input); err == nil {
			return ParsedToolCall{Name: name, Input: input}, true
		}
	}
	for _, am := range antmlArgumentPattern.FindAllStringSubmatch(body, -1) {
		if len(am) < 3 {
			continue
		}
		k := markupAttrValue(am[1], "name")
		v := extractRawTagValue(am[2])
		if k != "" {
			input[k] = v
		}
	}
	if len(input) > 0 {
		return ParsedToolCall{Name: name, Input: input}, true
	}
	if paramsRaw := findMarkupTagValue(body, toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag); paramsRaw != "" {
		if parsed := parseMarkupInput(paramsRaw); len(parsed) > 0 {
			return ParsedToolCall{Name: name, Input: parsed}, true
		}
	}
	if strings.Contains(body, "<") {
		if parsed := parseStructuredToolCallInput(body); len(parsed) > 0 && !isOnlyRawValue(parsed, body) {
			return ParsedToolCall{Name: name, Input: parsed}, true
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseInvokeFunctionCallStyle(text string) (ParsedToolCall, bool) {
	m := invokeCallPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := markupAttrValue(m[1], "name", "function", "tool")
	if name == "" {
		return ParsedToolCall{}, false
	}
	input := map[string]any{}
	for _, pm := range invokeParamPattern.FindAllStringSubmatch(m[2], -1) {
		if len(pm) < 3 {
			continue
		}
		k := markupAttrValue(pm[1], "name")
		v := extractRawTagValue(pm[2])
		if k != "" {
			if parsed := parseStructuredToolCallInput(v); len(parsed) > 0 {
				if isOnlyRawValue(parsed, v) {
					input[k] = v
				} else {
					input[k] = parsed
				}
			}
		}
	}
	if len(input) == 0 {
		if argsRaw := findMarkupTagValue(m[2], toolCallMarkupArgsTagNames, toolCallMarkupArgsPatternByTag); argsRaw != "" {
			input = parseMarkupInput(argsRaw)
		} else if kv := parseMarkupKVObject(m[2]); len(kv) > 0 {
			input = kv
		} else if parsed := parseStructuredToolCallInput(m[2]); len(parsed) > 0 && !isOnlyRawValue(parsed, strings.TrimSpace(html.UnescapeString(m[2]))) {
			input = parsed
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseToolUseFunctionStyle(text string) (ParsedToolCall, bool) {
	m := toolUseFunctionPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := markupAttrValue(m[1], "name", "function", "tool")
	if name == "" {
		return ParsedToolCall{}, false
	}
	body := m[2]
	input := map[string]any{}
	for _, pm := range invokeParamPattern.FindAllStringSubmatch(body, -1) {
		if len(pm) < 3 {
			continue
		}
		k := markupAttrValue(pm[1], "name")
		v := extractRawTagValue(pm[2])
		if k != "" {
			if parsed := parseStructuredToolCallInput(v); len(parsed) > 0 {
				if isOnlyRawValue(parsed, v) {
					input[k] = v
				} else {
					input[k] = parsed
				}
			}
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseToolUseNameParametersStyle(text string) (ParsedToolCall, bool) {
	m := toolUseNameParametersPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(html.UnescapeString(m[1]))
	if name == "" {
		return ParsedToolCall{}, false
	}
	raw := strings.TrimSpace(m[2])
	input := map[string]any{}
	if raw != "" {
		if parsed := parseStructuredToolCallInput(raw); len(parsed) > 0 {
			input = parsed
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseToolUseFunctionNameParametersStyle(text string) (ParsedToolCall, bool) {
	m := toolUseFunctionNameParametersPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(html.UnescapeString(m[1]))
	if name == "" {
		return ParsedToolCall{}, false
	}
	raw := strings.TrimSpace(m[2])
	input := map[string]any{}
	if raw != "" {
		if parsed := parseStructuredToolCallInput(raw); len(parsed) > 0 {
			input = parsed
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func parseToolUseToolNameBodyStyle(text string) (ParsedToolCall, bool) {
	m := toolUseToolNameBodyPattern.FindStringSubmatch(text)
	if len(m) < 3 {
		return ParsedToolCall{}, false
	}
	name := strings.TrimSpace(html.UnescapeString(m[1]))
	if name == "" {
		return ParsedToolCall{}, false
	}
	body := strings.TrimSpace(m[2])
	input := map[string]any{}
	if body != "" {
		if kv := parseXMLChildKV(body); len(kv) > 0 {
			input = kv
		} else if kv := parseMarkupKVObject(body); len(kv) > 0 {
			input = kv
		} else if parsed := parseStructuredToolCallInput(body); len(parsed) > 0 {
			input = parsed
		}
	}
	return ParsedToolCall{Name: name, Input: input}, true
}

func markupAttrValue(attrs string, names ...string) string {
	if strings.TrimSpace(attrs) == "" {
		return ""
	}
	for _, m := range markupAttrPattern.FindAllStringSubmatch(attrs, -1) {
		if len(m) < 5 {
			continue
		}
		key := strings.TrimSpace(m[1])
		if key == "" {
			continue
		}
		for _, want := range names {
			if !strings.EqualFold(key, strings.TrimSpace(want)) {
				continue
			}
			for i := 2; i <= 4; i++ {
				if m[i] != "" {
					return strings.TrimSpace(html.UnescapeString(m[i]))
				}
			}
			return ""
		}
	}
	return ""
}

func parseXMLChildKV(body string) map[string]any {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil
	}
	parsed := parseStructuredToolCallInput(trimmed)
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
