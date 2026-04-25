package toolcall

import "testing"

func TestNormalizeCallsForSchemasDropsMissingRequiredTaskFields(t *testing.T) {
	schemas := ParameterSchemas{
		"task": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}
	calls := []ParsedToolCall{{Name: "task", Input: map[string]any{}}}
	if got := NormalizeCallsForSchemas(calls, schemas); len(got) != 0 {
		t.Fatalf("expected invalid task call to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasDropsMetaAgentTools(t *testing.T) {
	schemas := ParameterSchemas{
		"Agent": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
		"TaskOutput": {
			"type":       "object",
			"properties": map[string]any{"task_id": map[string]any{"type": "string"}},
			"required":   []any{"task_id"},
		},
	}
	calls := []ParsedToolCall{
		{
			Name: "Agent",
			Input: map[string]any{
				"description":   "Explore",
				"prompt":        "Explore the repository",
				"subagent_type": "general",
			},
		},
		{
			Name:  "TaskOutput",
			Input: map[string]any{"task_id": "task_a"},
		},
	}
	if got := NormalizeCallsForSchemas(calls, schemas); len(got) != 0 {
		t.Fatalf("expected meta agent call to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasDropsKnownEmptyRequiredFieldsWithoutSchema(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Agent", Input: map[string]any{}},
		{Name: "TaskOutput", Input: map[string]any{}},
		{Name: "Bash", Input: map[string]any{}},
		{Name: "TaskCreate", Input: map[string]any{"subject": "Review"}},
	}
	if got := NormalizeCallsForSchemasWithMeta(calls, nil, true); len(got) != 0 {
		t.Fatalf("expected known empty required-field calls to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasKeepsKnownRequiredFieldsWithoutSchema(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Agent", Input: map[string]any{"description": "Explore", "prompt": "Inspect files"}},
		{Name: "TaskOutput", Input: map[string]any{"task_id": "task_123"}},
		{Name: "Bash", Input: map[string]any{"command": "pwd"}},
		{Name: "TaskCreate", Input: map[string]any{"subject": "Review", "description": "Find issues"}},
	}
	got := NormalizeCallsForSchemasWithMeta(calls, nil, true)
	if len(got) != len(calls)-1 {
		t.Fatalf("expected valid non-task-tracking calls to survive without schema, got %#v", got)
	}
	for _, call := range got {
		if call.Name == "TaskCreate" {
			t.Fatalf("expected TaskCreate to be dropped as task tracking, got %#v", got)
		}
	}
}

func TestNormalizeCallsForSchemasKeepsUnknownNoArgToolWithoutSchema(t *testing.T) {
	calls := []ParsedToolCall{{Name: "get_time", Input: map[string]any{}}}
	got := NormalizeCallsForSchemas(calls, nil)
	if len(got) != 1 {
		t.Fatalf("expected unknown no-arg tool to survive without schema, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasKeepsOnlyDeclaredFields(t *testing.T) {
	schemas := ParameterSchemas{
		"Bash": {
			"type": "object",
			"properties": map[string]any{
				"command":     map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
			},
			"required": []any{"command"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "Bash",
		Input: map[string]any{
			"command": "pwd",
			"extra":   "drop me",
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected one valid call, got %#v", got)
	}
	if got[0].Input["command"] != "pwd" {
		t.Fatalf("expected command to survive, got %#v", got[0].Input)
	}
	if _, ok := got[0].Input["extra"]; ok {
		t.Fatalf("expected undeclared field to be removed, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasCoercesScalarTypes(t *testing.T) {
	schemas := ParameterSchemas{
		"set": {
			"type": "object",
			"properties": map[string]any{
				"enabled": map[string]any{"type": "boolean"},
				"count":   map[string]any{"type": "integer"},
			},
			"required": []any{"enabled", "count"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "set",
		Input: map[string]any{
			"enabled": "true",
			"count":   "3",
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected one valid call, got %#v", got)
	}
	if got[0].Input["enabled"] != true {
		t.Fatalf("expected boolean coercion, got %#v", got[0].Input)
	}
	if got[0].Input["count"] != int64(3) {
		t.Fatalf("expected integer coercion, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasResolvesKnownToolAliases(t *testing.T) {
	schemas := ParameterSchemas{
		"glob": {
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{"type": "string"},
			},
			"required": []any{"pattern"},
		},
	}
	calls := []ParsedToolCall{{Name: "globe", Input: map[string]any{"pattern": "*.go"}}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected normalized call, got %#v", got)
	}
	if got[0].Name != "glob" {
		t.Fatalf("expected glob tool name, got %#v", got[0])
	}
}

func TestNormalizeCallsForSchemasMapsEquivalentPropertyNamesAndPathSlashes(t *testing.T) {
	schemas := ParameterSchemas{
		"Read": {
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"limit":     map[string]any{"type": "integer"},
			},
			"required": []any{"file_path"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "read",
		Input: map[string]any{
			"filePath": "⁄Users/lbcheng/cheng-lang/README.md",
			"limit":    "10",
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected normalized call, got %#v", got)
	}
	if got[0].Name != "Read" {
		t.Fatalf("expected resolved Read tool name, got %#v", got[0])
	}
	if got[0].Input["file_path"] != "/Users/lbcheng/cheng-lang/README.md" {
		t.Fatalf("expected normalized file_path, got %#v", got[0].Input)
	}
	if got[0].Input["limit"] != int64(10) {
		t.Fatalf("expected integer limit, got %#v", got[0].Input)
	}
}

func TestExtractParameterSchemasSupportsOpenAITools(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "task",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
	}
	got := ExtractParameterSchemas(tools)
	if _, ok := got["task"]; !ok {
		t.Fatalf("expected task schema, got %#v", got)
	}
}

func TestValidateStrictFunctionToolsAcceptsDeepSeekStrictSchema(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "search_docs",
				"strict": true,
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"query": map[string]any{
							"type": "string",
						},
						"limit": map[string]any{
							"type": "integer",
						},
					},
					"required": []any{"query", "limit"},
				},
			},
		},
	}
	if err := ValidateStrictFunctionTools(tools); err != nil {
		t.Fatalf("expected strict schema to pass, got %v", err)
	}
}

func TestValidateStrictFunctionToolsRejectsMissingAdditionalPropertiesFalse(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "search_docs",
				"strict": true,
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
					},
					"required": []any{"query"},
				},
			},
		},
	}
	if err := ValidateStrictFunctionTools(tools); err == nil {
		t.Fatal("expected strict schema without additionalProperties=false to fail")
	}
}

func TestValidateStrictFunctionToolsRequiresAllPropertiesRequired(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "search_docs",
				"strict": true,
				"parameters": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"query": map[string]any{"type": "string"},
						"limit": map[string]any{"type": "integer"},
					},
					"required": []any{"query"},
				},
			},
		},
	}
	if err := ValidateStrictFunctionTools(tools); err == nil {
		t.Fatal("expected strict schema with optional property to fail")
	}
}

func TestValidateStrictFunctionToolsRejectsMixedStrictFlags(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":   "a",
				"strict": true,
			},
		},
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "b",
			},
		},
	}
	if err := ValidateStrictFunctionTools(tools); err == nil {
		t.Fatal("expected mixed strict tools to fail")
	}
}
