package toolcall

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

type ParameterSchemas = map[string]map[string]any

func ExtractParameterSchemas(toolsRaw any) ParameterSchemas {
	tools := normalizeToolList(toolsRaw)
	if len(tools) == 0 {
		return nil
	}
	out := ParameterSchemas{}
	for _, tool := range tools {
		name, schema := extractOneParameterSchema(tool)
		if name == "" || len(schema) == 0 {
			continue
		}
		out[name] = schema
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeCallsForSchemas(calls []ParsedToolCall, schemas ParameterSchemas) []ParsedToolCall {
	return NormalizeCallsForSchemasWithMeta(calls, schemas, false)
}

func NormalizeCallsForSchemasWithMeta(calls []ParsedToolCall, schemas ParameterSchemas, allowMetaAgentTools bool) []ParsedToolCall {
	if len(calls) == 0 {
		return nil
	}
	availableToolNames := toolNamesFromSchemas(schemas)
	if len(availableToolNames) > 0 {
		calls = RewriteCallsForAvailableTools(calls, availableToolNames)
	}
	out := make([]ParsedToolCall, 0, len(calls))
	backgroundAgentCalls := 0
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		if name == "" {
			continue
		}
		name = resolveToolNameForSchemas(name, schemas)
		if IsTaskTrackingToolName(name) {
			continue
		}
		if !allowMetaAgentTools && IsMetaAgentToolName(name) {
			continue
		}
		if allowMetaAgentTools && IsBackgroundAgentToolName(name) {
			backgroundAgentCalls++
			if backgroundAgentCalls > 4 {
				continue
			}
		}
		input := call.Input
		if input == nil {
			input = map[string]any{}
		}
		input = normalizeToolInputStrings(input)
		input = normalizeKnownToolInputAliases(name, input)
		for _, expanded := range expandKnownClientToolCalls(ParsedToolCall{Name: name, Input: input}) {
			expandedInput := expanded.Input
			if expandedInput == nil {
				expandedInput = map[string]any{}
			}
			if schema, ok := schemas[name]; ok && len(schema) > 0 {
				normalized, valid := normalizeObjectForSchema(expandedInput, schema)
				if !valid {
					continue
				}
				expandedInput = normalized
			}
			if !knownToolCallHasRequiredFields(name, expandedInput) {
				continue
			}
			if isInvalidKnownClientToolCall(name, expandedInput) {
				continue
			}
			out = append(out, ParsedToolCall{Name: name, Input: expandedInput})
		}
	}
	return coalesceParallelShellCalls(out)
}

func normalizeKnownToolInputAliases(name string, input map[string]any) map[string]any {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "taskoutput":
		return normalizeTaskOutputInputAliases(input)
	default:
		return input
	}
}

func normalizeTaskOutputInputAliases(input map[string]any) map[string]any {
	if len(input) == 0 {
		return input
	}
	if value, ok := inputValueForKnownRequiredField(input, "taskid"); ok && !isEmptyKnownRequiredValue(value) {
		return input
	}
	for _, alias := range []string{"tool_id", "toolId", "toolID"} {
		if value, ok := input[alias]; ok && !isEmptyKnownRequiredValue(value) {
			out := cloneSchemaMap(input)
			out["task_id"] = value
			return out
		}
	}
	return input
}

func toolNamesFromSchemas(schemas ParameterSchemas) []string {
	if len(schemas) == 0 {
		return nil
	}
	out := make([]string, 0, len(schemas))
	for name := range schemas {
		if strings.TrimSpace(name) != "" {
			out = append(out, strings.TrimSpace(name))
		}
	}
	return out
}

func knownToolCallHasRequiredFields(name string, input map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read_file", "readfile":
		return strings.TrimSpace(localPathFromReadFileInput(input)) != ""
	}
	required := knownRequiredToolFields(name)
	if len(required) == 0 {
		return true
	}
	for _, field := range required {
		value, ok := inputValueForKnownRequiredField(input, field)
		if !ok || isEmptyKnownRequiredValue(value) {
			return false
		}
	}
	return true
}

func knownRequiredToolFields(name string) []string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "agent", "task":
		return []string{"description", "prompt"}
	case "taskcreate":
		return []string{"subject", "description"}
	case "taskupdate":
		return []string{"taskid", "status"}
	case "taskoutput":
		return []string{"taskid"}
	case "todowrite", "todo_write":
		return []string{"todos"}
	case "bash":
		return []string{"command"}
	case "exec_command":
		return []string{"cmd"}
	case "read_mcp_resource":
		return []string{"server", "uri"}
	case "read":
		return []string{"filepath"}
	case "grep":
		return []string{"pattern"}
	case "glob", "globe":
		return []string{"pattern"}
	case "edit", "update":
		return []string{"filepath", "oldstring", "newstring"}
	case "multiedit":
		return []string{"filepath", "edits"}
	default:
		return nil
	}
}

func inputValueForKnownRequiredField(input map[string]any, field string) (any, bool) {
	want := canonicalSchemaPropertyName(field)
	for candidate, value := range input {
		if canonicalSchemaPropertyName(candidate) == want {
			return value, true
		}
	}
	return nil, false
}

func isEmptyKnownRequiredValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

func resolveToolNameForSchemas(name string, schemas ParameterSchemas) string {
	name = strings.TrimSpace(name)
	if name == "" || len(schemas) == 0 {
		return name
	}
	if _, ok := schemas[name]; ok {
		return name
	}
	for candidate := range schemas {
		if strings.EqualFold(candidate, name) {
			return candidate
		}
	}
	for _, alias := range knownToolNameAliases(strings.ToLower(name)) {
		if _, ok := schemas[alias]; ok {
			return alias
		}
		for candidate := range schemas {
			if strings.EqualFold(candidate, alias) {
				return candidate
			}
		}
	}
	return name
}

func knownToolNameAliases(lower string) []string {
	switch strings.TrimSpace(lower) {
	case "globe":
		return []string{"glob", "Glob"}
	default:
		return nil
	}
}

func normalizeToolList(raw any) []map[string]any {
	switch v := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case []map[string]any:
		return v
	default:
		return nil
	}
}

// SchemaPropertyDefaults extracts default values from a tool's parameter schema.
// Returns a map of property name -> default value for properties that have a "default" key.
func SchemaPropertyDefaults(schemas ParameterSchemas, toolName string) map[string]any {
	schema, ok := schemas[toolName]
	if !ok || len(schema) == 0 {
		return nil
	}
	properties := schemaProperties(schema)
	if len(properties) == 0 {
		return nil
	}
	defaults := make(map[string]any)
	for name, propSchema := range properties {
		if def, ok := propSchema["default"]; ok {
			defaults[name] = def
		}
	}
	if len(defaults) == 0 {
		return nil
	}
	return defaults
}

func extractOneParameterSchema(tool map[string]any) (string, map[string]any) {
	fn, _ := tool["function"].(map[string]any)
	name := strings.TrimSpace(asSchemaString(tool["name"]))
	if name == "" && len(fn) > 0 {
		name = strings.TrimSpace(asSchemaString(fn["name"]))
	}
	if name == "" {
		return "", nil
	}
	for _, candidate := range []any{
		fn["parameters"],
		tool["parameters"],
		tool["input_schema"],
		fn["input_schema"],
	} {
		if schema, ok := candidate.(map[string]any); ok {
			return name, schema
		}
	}
	return name, nil
}

func normalizeObjectForSchema(input map[string]any, schema map[string]any) (map[string]any, bool) {
	properties := schemaProperties(schema)
	required := schemaRequired(schema)
	if len(properties) == 0 {
		for name := range required {
			if _, ok := input[name]; !ok {
				return nil, false
			}
		}
		return cloneSchemaMap(input), true
	}
	input = repairGenericParameterForSchema(input, properties, required)

	out := map[string]any{}
	for name, propSchema := range properties {
		raw, ok := inputValueForSchemaProperty(input, name, properties)
		if !ok {
			continue
		}
		normalized, valid := normalizeValueForSchema(raw, propSchema)
		if !valid {
			continue
		}
		if s, ok := normalized.(string); ok {
			normalized = normalizeEmbeddedNamedParameterString(name, s)
		}
		if s, ok := normalized.(string); ok && isPathLikeSchemaProperty(name) {
			normalized = normalizeToolPathString(s)
		}
		if s, ok := normalized.(string); ok && isSubagentTypeSchemaProperty(name) {
			normalized = normalizeSubagentTypeString(s)
		}
		out[name] = normalized
	}
	for name := range required {
		if _, ok := out[name]; !ok {
			return nil, false
		}
	}
	return out, true
}

func repairGenericParameterForSchema(input map[string]any, properties map[string]map[string]any, required map[string]struct{}) map[string]any {
	if len(input) == 0 || len(properties) == 0 || len(required) == 0 {
		return input
	}
	genericKey := ""
	var genericValue any
	for _, key := range []string{"parameter", "argument", "param"} {
		if _, declared := properties[key]; declared {
			continue
		}
		value, ok := input[key]
		if !ok || isEmptyKnownRequiredValue(value) {
			continue
		}
		if genericKey != "" {
			return input
		}
		genericKey = key
		genericValue = value
	}
	if genericKey == "" {
		return input
	}
	missing := ""
	for name := range required {
		if _, ok := inputValueForSchemaProperty(input, name, properties); ok {
			continue
		}
		if missing != "" {
			return input
		}
		missing = name
	}
	if missing == "" {
		return input
	}
	if _, ok := properties[missing]; !ok {
		return input
	}
	out := cloneSchemaMap(input)
	out[missing] = genericValue
	return out
}

func inputValueForSchemaProperty(input map[string]any, name string, properties map[string]map[string]any) (any, bool) {
	if raw, ok := input[name]; ok {
		return raw, true
	}
	want := canonicalSchemaPropertyName(name)
	if want == "" {
		return nil, false
	}
	var (
		found any
		ok    bool
	)
	for candidate, raw := range input {
		if _, isDeclared := properties[candidate]; isDeclared {
			continue
		}
		if canonicalSchemaPropertyName(candidate) != want {
			continue
		}
		if ok {
			return nil, false
		}
		found = raw
		ok = true
	}
	return found, ok
}

func canonicalSchemaPropertyName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch r {
		case '_', '-', ' ':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

func isPathLikeSchemaProperty(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "path", "filepath", "file_path", "cwd", "workdir", "working_directory":
		return true
	default:
		return false
	}
}

func isSubagentTypeSchemaProperty(name string) bool {
	switch canonicalSchemaPropertyName(name) {
	case "subagenttype", "agenttype":
		return true
	default:
		return false
	}
}

func normalizeSubagentTypeString(value string) string {
	trimmed := strings.TrimSpace(value)
	for _, prefix := range []string{"name=", "type=", "agent=", "subagent_type="} {
		if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}
	return value
}

func normalizeToolPathString(path string) string {
	replacer := strings.NewReplacer(
		"⁄", "/",
		"∕", "/",
		"／", "/",
	)
	return replacer.Replace(path)
}

func normalizeValueForSchema(value any, schema map[string]any) (any, bool) {
	if normalized, ok, valid := normalizeValueForUnionSchema(value, schema); ok {
		return normalized, valid
	}
	types := schemaTypes(schema)
	if len(types) == 0 {
		switch {
		case len(schemaProperties(schema)) > 0:
			m, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			return normalizeObjectForSchema(m, schema)
		case schema["items"] != nil:
			return normalizeArrayForSchema(value, schema)
		default:
			if !valueMatchesEnum(value, schema) {
				return nil, false
			}
			return value, true
		}
	}

	for _, typ := range orderSchemaTypes(types, value) {
		normalized, valid := normalizeValueForSchemaType(value, schema, typ)
		if !valid {
			continue
		}
		if !valueMatchesEnum(normalized, schema) {
			continue
		}
		return normalized, true
	}
	return nil, false
}

func normalizeValueForUnionSchema(value any, schema map[string]any) (any, bool, bool) {
	for _, key := range []string{"oneOf", "anyOf"} {
		options, ok := schema[key].([]any)
		if !ok || len(options) == 0 {
			continue
		}
		for _, option := range options {
			optionSchema, ok := option.(map[string]any)
			if !ok {
				continue
			}
			normalized, valid := normalizeValueForSchema(value, optionSchema)
			if valid {
				return normalized, true, true
			}
		}
		return nil, true, false
	}
	return nil, false, false
}

func normalizeValueForSchemaType(value any, schema map[string]any, typ string) (any, bool) {
	switch typ {
	case "null":
		if value == nil {
			return nil, true
		}
		if s, ok := value.(string); ok && strings.EqualFold(strings.TrimSpace(s), "null") {
			return nil, true
		}
		return nil, false
	case "string":
		switch v := value.(type) {
		case string:
			return normalizeToolStringValue(v), true
		case bool:
			return strconv.FormatBool(v), true
		case json.Number:
			return v.String(), true
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return fmt.Sprint(v), true
		default:
			return nil, false
		}
	case "boolean":
		switch v := value.(type) {
		case bool:
			return v, true
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return nil, false
			}
			return parsed, true
		default:
			return nil, false
		}
	case "integer":
		return normalizeInteger(value)
	case "number":
		return normalizeNumber(value)
	case "array":
		return normalizeArrayForSchema(value, schema)
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		return normalizeObjectForSchema(m, schema)
	default:
		return nil, false
	}
}

func normalizeArrayForSchema(value any, schema map[string]any) (any, bool) {
	itemsSchema, _ := schema["items"].(map[string]any)
	items, ok := normalizeSchemaArrayValue(value)
	if !ok {
		return nil, false
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		normalized := item
		if len(itemsSchema) > 0 {
			var valid bool
			normalized, valid = normalizeValueForSchema(item, itemsSchema)
			if !valid {
				return nil, false
			}
		}
		out = append(out, normalized)
	}
	return out, true
}

func normalizeSchemaArrayValue(value any) ([]any, bool) {
	switch v := value.(type) {
	case []any:
		return v, true
	case map[string]any:
		if len(v) != 1 {
			return nil, false
		}
		item, ok := v["item"]
		if !ok {
			return nil, false
		}
		if list, ok := item.([]any); ok {
			return list, true
		}
		return []any{item}, true
	default:
		return nil, false
	}
}

func normalizeInteger(value any) (any, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint:
		return int64(v), true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		if v > math.MaxInt64 {
			return nil, false
		}
		return int64(v), true
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil || math.Trunc(f) != f {
			return nil, false
		}
		return int64(f), true
	case float32:
		f := float64(v)
		if math.Trunc(f) != f {
			return nil, false
		}
		return int64(f), true
	case float64:
		if math.Trunc(v) != v {
			return nil, false
		}
		return int64(v), true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, false
		}
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.Trunc(f) != f {
			return nil, false
		}
		return int64(f), true
	default:
		return nil, false
	}
}

func normalizeNumber(value any) (any, bool) {
	switch v := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return v, true
	case json.Number:
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil {
			return nil, false
		}
		return f, true
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, false
		}
		return f, true
	default:
		return nil, false
	}
}

func schemaProperties(schema map[string]any) map[string]map[string]any {
	raw, _ := schema["properties"].(map[string]any)
	if len(raw) == 0 {
		return nil
	}
	out := map[string]map[string]any{}
	for name, prop := range raw {
		if propSchema, ok := prop.(map[string]any); ok {
			out[name] = propSchema
		}
	}
	return out
}

func schemaRequired(schema map[string]any) map[string]struct{} {
	raw, _ := schema["required"].([]any)
	if len(raw) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, item := range raw {
		name := strings.TrimSpace(asSchemaString(item))
		if name == "" {
			continue
		}
		out[name] = struct{}{}
	}
	return out
}

func schemaTypes(schema map[string]any) []string {
	raw := schema["type"]
	switch v := raw.(type) {
	case string:
		if typ := strings.TrimSpace(v); typ != "" {
			return []string{typ}
		}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if typ := strings.TrimSpace(asSchemaString(item)); typ != "" {
				out = append(out, typ)
			}
		}
		return out
	}
	return nil
}

func orderSchemaTypes(types []string, value any) []string {
	if len(types) < 2 {
		return types
	}
	preferred := inferredSchemaType(value)
	if preferred == "" {
		return types
	}
	out := make([]string, 0, len(types))
	for _, typ := range types {
		if typ == preferred {
			out = append(out, typ)
		}
	}
	for _, typ := range types {
		if typ != preferred {
			out = append(out, typ)
		}
	}
	return out
}

func inferredSchemaType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "integer"
	case float32, float64:
		return "number"
	default:
		return ""
	}
}

func valueMatchesEnum(value any, schema map[string]any) bool {
	raw, _ := schema["enum"].([]any)
	if len(raw) == 0 {
		return true
	}
	for _, candidate := range raw {
		if reflect.DeepEqual(value, candidate) {
			return true
		}
		if valueString, ok := value.(string); ok {
			if candidateString, ok := candidate.(string); ok && valueString == candidateString {
				return true
			}
		}
	}
	return false
}

func normalizeToolInputStrings(input map[string]any) map[string]any {
	if len(input) == 0 {
		return input
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = normalizeToolInputValueStrings(value)
	}
	return out
}

func normalizeToolInputValueStrings(value any) any {
	switch v := value.(type) {
	case string:
		return normalizeToolStringValue(v)
	case map[string]any:
		return normalizeToolInputStrings(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeToolInputValueStrings(item)
		}
		return out
	default:
		return value
	}
}

func normalizeToolStringValue(value string) string {
	trimmed := strings.TrimSpace(value)
	const cdataOpen = "<![CDATA["
	if !strings.HasPrefix(trimmed, cdataOpen) {
		return value
	}
	body := strings.TrimPrefix(trimmed, cdataOpen)
	if idx := strings.Index(body, "]]>"); idx >= 0 {
		return body[:idx]
	}
	return strings.TrimSuffix(strings.TrimSpace(body), "]]")
}

func normalizeEmbeddedNamedParameterString(propertyName, value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasPrefix(trimmed, "<") {
		return value
	}
	for _, tag := range []string{"parameter", "argument", "param"} {
		prefix := "<" + tag
		if !strings.HasPrefix(strings.ToLower(trimmed), prefix) {
			continue
		}
		close := strings.Index(trimmed, ">")
		if close < 0 {
			return value
		}
		attrs := trimmed[len(prefix):close]
		attrName := markupAttrValue(attrs, "name")
		if attrName == "" || canonicalSchemaPropertyName(attrName) != canonicalSchemaPropertyName(propertyName) {
			return value
		}
		body := strings.TrimSpace(trimmed[close+1:])
		if body == "" {
			return ""
		}
		closeTag := "</" + tag + ">"
		if idx := strings.LastIndex(strings.ToLower(body), closeTag); idx >= 0 {
			body = strings.TrimSpace(body[:idx])
		}
		return htmlUnescapeSchemaString(body)
	}
	return value
}

func htmlUnescapeSchemaString(value string) string {
	replacer := strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&amp;", "&",
		"&quot;", `"`,
		"&#39;", "'",
		"&#x27;", "'",
	)
	return replacer.Replace(value)
}

func cloneSchemaMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func asSchemaString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
