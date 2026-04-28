package toolcall

import (
	"strings"
	"testing"
)

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
		{Name: "Read", Input: map[string]any{}},
		{Name: "read_file", Input: map[string]any{}},
		{Name: "TaskCreate", Input: map[string]any{"subject": "Review"}},
	}
	if got := NormalizeCallsForSchemasWithMeta(calls, nil, true); len(got) != 0 {
		t.Fatalf("expected known empty required-field calls to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasDropsKnownEmptyRequiredFieldsEvenWhenSchemaIsLoose(t *testing.T) {
	schemas := ParameterSchemas{
		"Read": {
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"limit":     map[string]any{"type": "integer"},
			},
		},
	}
	calls := []ParsedToolCall{{Name: "Read", Input: map[string]any{}}}
	if got := NormalizeCallsForSchemas(calls, schemas); len(got) != 0 {
		t.Fatalf("expected loose schema Read without file_path to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasKeepsKnownRequiredFieldsWithoutSchema(t *testing.T) {
	calls := []ParsedToolCall{
		{Name: "Agent", Input: map[string]any{"description": "Explore", "prompt": "Inspect files"}},
		{Name: "TaskOutput", Input: map[string]any{"task_id": "task_123"}},
		{Name: "Bash", Input: map[string]any{"command": "pwd"}},
		{Name: "Read", Input: map[string]any{"file_path": "README.md"}},
		{Name: "read_file", Input: map[string]any{"path": "README.md"}},
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

func TestNormalizeCallsForSchemasMapsTaskOutputToolIDAlias(t *testing.T) {
	schemas := ParameterSchemas{
		"TaskOutput": {
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string"},
				"block":   map[string]any{"type": "boolean"},
				"timeout": map[string]any{"type": "integer"},
			},
			"required": []any{"task_id"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "TaskOutput",
		Input: map[string]any{
			"tool_id": "task_123",
			"block":   "true",
			"timeout": "30000",
		},
	}}

	got := NormalizeCallsForSchemasWithMeta(calls, schemas, true)
	if len(got) != 1 {
		t.Fatalf("expected normalized TaskOutput call, got %#v", got)
	}
	if got[0].Input["task_id"] != "task_123" {
		t.Fatalf("expected tool_id alias mapped to task_id, got %#v", got[0].Input)
	}
	if got[0].Input["block"] != true || got[0].Input["timeout"] != int64(30000) {
		t.Fatalf("expected scalar coercion after alias mapping, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasLimitsBackgroundAgentConcurrency(t *testing.T) {
	calls := make([]ParsedToolCall, 0, 6)
	for i := 0; i < 6; i++ {
		calls = append(calls, ParsedToolCall{
			Name:  "Agent",
			Input: map[string]any{"description": "Explore", "prompt": "Inspect files"},
		})
	}
	calls = append(calls, ParsedToolCall{
		Name:  "TaskOutput",
		Input: map[string]any{"task_id": "task_123"},
	})

	got := NormalizeCallsForSchemasWithMeta(calls, nil, true)
	agentCount := 0
	taskOutputCount := 0
	for _, call := range got {
		switch call.Name {
		case "Agent":
			agentCount++
		case "TaskOutput":
			taskOutputCount++
		}
	}
	if agentCount != 4 {
		t.Fatalf("expected 4 Agent calls after concurrency gate, got %d in %#v", agentCount, got)
	}
	if taskOutputCount != 1 {
		t.Fatalf("expected TaskOutput not to be counted as background Agent, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasStripsSubagentTypeNamePrefix(t *testing.T) {
	schemas := ParameterSchemas{
		"Agent": {
			"type": "object",
			"properties": map[string]any{
				"description":       map[string]any{"type": "string"},
				"prompt":            map[string]any{"type": "string"},
				"subagent_type":     map[string]any{"type": "string"},
				"run_in_background": map[string]any{"type": "boolean"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "Agent",
		Input: map[string]any{
			"description":       "Agent team: evaluate plan coverage",
			"prompt":            "Review plan coverage.",
			"subagent_type":     "name=general-purpose",
			"run_in_background": "true",
		},
	}}

	got := NormalizeCallsForSchemasWithMeta(calls, schemas, true)
	if len(got) != 1 {
		t.Fatalf("expected normalized Agent call, got %#v", got)
	}
	if got[0].Input["subagent_type"] != "general-purpose" {
		t.Fatalf("expected subagent_type prefix stripped, got %#v", got[0].Input)
	}
	if got[0].Input["run_in_background"] != true {
		t.Fatalf("expected boolean coercion, got %#v", got[0].Input)
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

func TestNormalizeCallsForSchemasMapsNamelessParameterToSingleMissingRequiredField(t *testing.T) {
	schemas := ParameterSchemas{
		"Grep": {
			"type": "object",
			"properties": map[string]any{
				"pattern":     map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"output_mode": map[string]any{"type": "string"},
			},
			"required": []any{"pattern"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "Grep",
		Input: map[string]any{
			"parameter":   "MachOTextObjectWrite|ElfTextObjectWrite",
			"path":        "⁄Users/lbcheng/cheng-lang/src/core/backend",
			"output_mode": "content",
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected repaired Grep call, got %#v", got)
	}
	if got[0].Input["pattern"] != "MachOTextObjectWrite|ElfTextObjectWrite" {
		t.Fatalf("expected nameless parameter to become pattern, got %#v", got[0].Input)
	}
	if got[0].Input["path"] != "/Users/lbcheng/cheng-lang/src/core/backend" {
		t.Fatalf("expected path slashes normalized, got %#v", got[0].Input)
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

func TestNormalizeCallsForSchemasStripsCDATAWrappersFromStringFields(t *testing.T) {
	schemas := ParameterSchemas{
		"Bash": {
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
			"required": []any{"command"},
		},
	}
	calls := []ParsedToolCall{{
		Name:  "Bash",
		Input: map[string]any{"command": `<![CDATA[grep -rn "uir_egraph" /tmp | head -20]]`},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected normalized Bash call, got %#v", got)
	}
	if got[0].Input["command"] != `grep -rn "uir_egraph" /tmp | head -20` {
		t.Fatalf("expected CDATA wrapper stripped from command, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasStripsCDATAWrappersWithoutSchema(t *testing.T) {
	calls := []ParsedToolCall{{
		Name:  "Bash",
		Input: map[string]any{"command": `<![CDATA[grep -rn "uir_vec_cost" /tmp | head -20]]>`},
	}}
	got := NormalizeCallsForSchemas(calls, nil)
	if len(got) != 1 {
		t.Fatalf("expected normalized Bash call, got %#v", got)
	}
	if got[0].Input["command"] != `grep -rn "uir_vec_cost" /tmp | head -20` {
		t.Fatalf("expected CDATA wrapper stripped without schema, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasSplitsReadFilesArray(t *testing.T) {
	schemas := ParameterSchemas{
		"Read": {
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"offset":    map[string]any{"type": "integer"},
				"limit":     map[string]any{"type": "integer"},
			},
			"required": []any{"file_path"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "Read",
		Input: map[string]any{
			"files": []any{
				map[string]any{"path": "/tmp/a.cheng", "offset": "10"},
				map[string]any{"file_path": "/tmp/b.cheng", "offset": "20"},
			},
			"limit": "200",
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 2 {
		t.Fatalf("expected files array to split into 2 Read calls, got %#v", got)
	}
	if got[0].Input["file_path"] != "/tmp/a.cheng" || got[1].Input["file_path"] != "/tmp/b.cheng" {
		t.Fatalf("expected scalar file_path values, got %#v", got)
	}
	if got[0].Input["offset"] != int64(10) || got[1].Input["offset"] != int64(20) {
		t.Fatalf("expected per-file offsets to normalize, got %#v", got)
	}
	if got[0].Input["limit"] != int64(200) || got[1].Input["limit"] != int64(200) {
		t.Fatalf("expected shared limit to normalize, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasDropsDegenerateBashCommand(t *testing.T) {
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
	calls := []ParsedToolCall{{Name: "Bash", Input: map[string]any{"command": `"`}}}
	if got := NormalizeCallsForSchemas(calls, schemas); len(got) != 0 {
		t.Fatalf("expected degenerate Bash command to be dropped, got %#v", got)
	}
}

func TestNormalizeCallsForSchemasStripsEmbeddedNamedParameterWrapper(t *testing.T) {
	schemas := ParameterSchemas{
		"Glob": {
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"pattern": map[string]any{"type": "string"},
			},
			"required": []any{"pattern"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "Glob",
		Input: map[string]any{
			"path":    "/Users/lbcheng/cheng-lang",
			"pattern": `<![CDATA[<parameter name="pattern">src/core/ir/function_task.cheng]]>`,
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected normalized Glob call, got %#v", got)
	}
	if got[0].Input["pattern"] != "src/core/ir/function_task.cheng" {
		t.Fatalf("expected embedded parameter wrapper stripped, got %#v", got[0].Input)
	}
}

func TestNormalizeCallsForSchemasRewritesLocalResourceRead(t *testing.T) {
	schemas := ParameterSchemas{
		"read_mcp_resource": {
			"type": "object",
			"properties": map[string]any{
				"server": map[string]any{"type": "string"},
				"uri":    map[string]any{"type": "string"},
			},
			"required": []any{"server", "uri"},
		},
		"exec_command": {
			"type": "object",
			"properties": map[string]any{
				"cmd": map[string]any{"type": "string"},
			},
			"required": []any{"cmd"},
		},
	}
	calls := []ParsedToolCall{{
		Name: "read_mcp_resource",
		Input: map[string]any{
			"server": "skill-creator",
			"uri":    "/Users/lbcheng/.codex/skills/cheng语言/SKILL.md",
			"tool_parameters": map[string]any{
				"server": "skill-creator",
				"uri":    "/Users/lbcheng/.codex/skills/cheng语言/SKILL.md",
			},
		},
	}}
	got := NormalizeCallsForSchemas(calls, schemas)
	if len(got) != 1 {
		t.Fatalf("expected one rewritten call, got %#v", got)
	}
	if got[0].Name != "exec_command" {
		t.Fatalf("expected exec_command, got %#v", got[0])
	}
	cmd, _ := got[0].Input["cmd"].(string)
	if !strings.Contains(cmd, `sed -n '1,200p'`) || !strings.Contains(cmd, `/Users/lbcheng/.codex/skills/cheng语言/SKILL.md`) {
		t.Fatalf("expected bounded local read command, got %#v", got[0].Input)
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
