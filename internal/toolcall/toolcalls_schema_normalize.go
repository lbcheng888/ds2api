package toolcall

import (
	"encoding/json"
	"fmt"
	"strings"
)

func NormalizeParsedToolCallsForSchemas(calls []ParsedToolCall, toolsRaw any) []ParsedToolCall {
	if len(calls) == 0 {
		return calls
	}
	out, _ := NormalizeParsedToolCallsForSchemasWithReport(calls, toolsRaw)
	return out
}

func NormalizeParsedToolCallsForSchemasWithReport(calls []ParsedToolCall, toolsRaw any) ([]ParsedToolCall, DedupeReport) {
	if len(calls) == 0 {
		return calls, DedupeReport{}
	}
	calls, report := NormalizeKnownToolCallInputsWithReport(calls)
	schemas := buildToolSchemaIndex(toolsRaw)
	if len(schemas) == 0 {
		out, dedupeReport := DedupeParsedToolCallsWithReport(calls)
		report.add(dedupeReport)
		return out, report
	}

	var changedAny bool
	out := make([]ParsedToolCall, len(calls))
	for i, call := range calls {
		out[i] = call
		schema, ok := schemas[strings.ToLower(strings.TrimSpace(call.Name))]
		if !ok || out[i].Input == nil {
			continue
		}
		normalized, changed := normalizeToolValueWithSchema(out[i].Input, schema)
		if !changed {
			continue
		}
		changedAny = true
		if input, ok := normalized.(map[string]any); ok {
			var inputReport DedupeReport
			out[i].Input, inputReport = NormalizeKnownToolCallInputValuesWithReport(call.Name, input)
			report.add(inputReport)
		}
	}
	if !changedAny {
		deduped, dedupeReport := DedupeParsedToolCallsWithReport(calls)
		report.add(dedupeReport)
		return deduped, report
	}
	deduped, dedupeReport := DedupeParsedToolCallsWithReport(out)
	report.add(dedupeReport)
	return deduped, report
}

func buildToolSchemaIndex(toolsRaw any) map[string]any {
	tools, ok := toolsRaw.([]any)
	if !ok || len(tools) == 0 {
		return nil
	}
	out := make(map[string]any, len(tools))
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _, schema := ExtractToolMeta(tool)
		if name == "" || schema == nil {
			continue
		}
		out[strings.ToLower(name)] = schema
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ExtractToolMeta(tool map[string]any) (string, string, any) {
	name := strings.TrimSpace(asStringValue(tool["name"]))
	desc := strings.TrimSpace(asStringValue(tool["description"]))
	schema := firstNonNil(
		tool["parameters"],
		tool["input_schema"],
		tool["inputSchema"],
		tool["schema"],
	)
	if fn, ok := tool["function"].(map[string]any); ok {
		if name == "" {
			name = strings.TrimSpace(asStringValue(fn["name"]))
		}
		if desc == "" {
			desc = strings.TrimSpace(asStringValue(fn["description"]))
		}
		schema = firstNonNil(
			schema,
			fn["parameters"],
			fn["input_schema"],
			fn["inputSchema"],
			fn["schema"],
		)
	}
	return name, desc, schema
}

func normalizeToolValueWithSchema(value any, schema any) (any, bool) {
	return normalizeToolValueWithSchemaField(value, schema, "")
}

func normalizeToolValueWithSchemaField(value any, schema any, fieldName string) (any, bool) {
	if value == nil || schema == nil {
		return value, false
	}
	schemaMap, ok := schema.(map[string]any)
	if !ok || len(schemaMap) == 0 {
		return value, false
	}
	if shouldCoerceSchemaToString(schemaMap) {
		return stringifySchemaValue(value)
	}
	if looksLikeObjectSchema(schemaMap) {
		obj, ok := value.(map[string]any)
		if !ok || len(obj) == 0 {
			return value, false
		}
		properties, _ := schemaMap["properties"].(map[string]any)
		additional := schemaMap["additionalProperties"]
		changed := false
		out := make(map[string]any, len(obj))
		for key, current := range obj {
			next := current
			var fieldChanged bool
			if propSchema, ok := properties[key]; ok {
				next, fieldChanged = normalizeToolValueWithSchemaField(current, propSchema, key)
			} else if disallowsAdditionalSchemaValues(additional) {
				changed = true
				continue
			} else if additional != nil {
				next, fieldChanged = normalizeToolValueWithSchemaField(current, additional, key)
			}
			out[key] = next
			changed = changed || fieldChanged
		}
		if !changed {
			return value, false
		}
		return out, true
	}
	if looksLikeArraySchema(schemaMap) {
		arr, converted := normalizeSchemaArrayCandidate(value, schemaMap, fieldName)
		if arr == nil {
			return value, false
		}
		itemsSchema := schemaMap["items"]
		if itemsSchema == nil {
			if converted {
				return arr, true
			}
			return value, false
		}
		changed := converted
		out := make([]any, len(arr))
		switch itemSchemas := itemsSchema.(type) {
		case []any:
			for i, item := range arr {
				if i >= len(itemSchemas) {
					out[i] = item
					continue
				}
				next, itemChanged := normalizeToolValueWithSchemaField(item, itemSchemas[i], fieldName)
				out[i] = next
				changed = changed || itemChanged
			}
		default:
			for i, item := range arr {
				next, itemChanged := normalizeToolValueWithSchemaField(item, itemsSchema, fieldName)
				out[i] = next
				changed = changed || itemChanged
			}
		}
		if !changed {
			return value, false
		}
		return out, true
	}
	if is, ok := isBooleanSchema(schemaMap); ok && is {
		if b, coerced := coerceBoolValue(value); coerced {
			return b, true
		}
		return value, false
	}
	if is, ok := isNumberOrIntegerSchema(schemaMap); ok && is {
		if n, coerced := coerceNumberValue(value); coerced {
			return n, true
		}
		return value, false
	}
	return value, false
}

func normalizeSchemaArrayCandidate(value any, schema map[string]any, fieldName string) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, false
	case string:
		if arr, ok := parseLooseJSONArrayValue(v, fieldName); ok {
			return arr, true
		}
		parsed, ok := parseLooseArrayElementValue(v)
		if !ok {
			return nil, false
		}
		if canWrapSingleArrayItem(parsed, schema["items"]) {
			return []any{parsed}, true
		}
		return nil, false
	case map[string]any:
		if arr, ok := coerceArrayValue(v, fieldName); ok {
			return arr, true
		}
		if canWrapSingleArrayItem(v, schema["items"]) {
			return []any{v}, true
		}
		return nil, false
	default:
		return nil, false
	}
}

func canWrapSingleArrayItem(value any, itemSchema any) bool {
	schemaMap, ok := itemSchema.(map[string]any)
	if !ok || len(schemaMap) == 0 {
		return false
	}
	if looksLikeObjectSchema(schemaMap) {
		_, ok := value.(map[string]any)
		return ok
	}
	if shouldCoerceSchemaToString(schemaMap) {
		_, ok := value.(string)
		return ok
	}
	if is, known := isBooleanSchema(schemaMap); known && is {
		_, ok := coerceBoolValue(value)
		return ok
	}
	if is, known := isNumberOrIntegerSchema(schemaMap); known && is {
		_, ok := coerceNumberValue(value)
		return ok
	}
	return false
}

func disallowsAdditionalSchemaValues(additional any) bool {
	b, ok := additional.(bool)
	return ok && !b
}

func isBooleanSchema(schema map[string]any) (bool, bool) {
	typ, ok := schema["type"].(string)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "boolean":
		return true, true
	default:
		return false, true
	}
}

func coerceBoolValue(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off", "":
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}

func isNumberOrIntegerSchema(schema map[string]any) (bool, bool) {
	typ, ok := schema["type"].(string)
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "number", "integer":
		return true, true
	default:
		return false, true
	}
}

func coerceNumberValue(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, false
	case float32:
		return float64(x), true
	case int:
		return float64(x), false
	case int64:
		return float64(x), false
	case json.Number:
		n, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return n, false
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		var f float64
		if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

func shouldCoerceSchemaToString(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if isStringConst(schema["const"]) {
		return true
	}
	if isStringEnum(schema["enum"]) {
		return true
	}
	switch v := schema["type"].(type) {
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "string")
	case []any:
		return isOnlyStringLikeTypes(v)
	case []string:
		items := make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
		return isOnlyStringLikeTypes(items)
	default:
		return false
	}
}

func looksLikeObjectSchema(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if typ, ok := schema["type"].(string); ok && strings.EqualFold(strings.TrimSpace(typ), "object") {
		return true
	}
	if _, ok := schema["properties"].(map[string]any); ok {
		return true
	}
	_, hasAdditional := schema["additionalProperties"]
	return hasAdditional
}

func looksLikeArraySchema(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	if typ, ok := schema["type"].(string); ok && strings.EqualFold(strings.TrimSpace(typ), "array") {
		return true
	}
	_, hasItems := schema["items"]
	return hasItems
}

func isOnlyStringLikeTypes(values []any) bool {
	if len(values) == 0 {
		return false
	}
	hasString := false
	for _, item := range values {
		typ, ok := item.(string)
		if !ok {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "string":
			hasString = true
		case "null":
			continue
		default:
			return false
		}
	}
	return hasString
}

func isStringConst(v any) bool {
	_, ok := v.(string)
	return ok
}

func isStringEnum(v any) bool {
	values, ok := v.([]any)
	if !ok || len(values) == 0 {
		return false
	}
	for _, item := range values {
		if _, ok := item.(string); !ok {
			return false
		}
	}
	return true
}

func stringifySchemaValue(value any) (any, bool) {
	if value == nil {
		return value, false
	}
	if s, ok := value.(string); ok {
		return s, false
	}
	b, err := json.Marshal(value)
	if err != nil {
		return value, false
	}
	return string(b), true
}

func asStringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
