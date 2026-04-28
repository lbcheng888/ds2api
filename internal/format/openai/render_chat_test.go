package openai

import (
	"ds2api/internal/toolcall"
	"strings"
	"testing"
)

func TestBuildChatCompletionBlocksStandaloneMetaAgentToolCall(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>Agent</tool_name>
    <parameters>
      <description>Explore</description>
      <prompt>Explore the repository</prompt>
      <subagent_type>general</subagent_type>
    </parameters>
  </tool_call>
</tool_calls>`
	resp := BuildChatCompletion("cid", "deepseek", "prompt", "", text, []string{"Agent", "read"}, toolcall.ParameterSchemas{
		"Agent": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}, false)
	choices, _ := resp["choices"].([]map[string]any)
	msg, _ := choices[0]["message"].(map[string]any)
	if _, ok := msg["tool_calls"]; ok {
		t.Fatalf("expected no executable tool calls, got %#v", msg)
	}
	content, _ := msg["content"].(string)
	if !strings.Contains(content, "Agent/subagent tools are disabled") {
		t.Fatalf("expected blocked-tool content, got %#v", msg)
	}
	if choices[0]["finish_reason"] != "stop" {
		t.Fatalf("expected stop finish reason, got %#v", choices[0])
	}
}

func TestBuildChatCompletionAllowsStandaloneMetaAgentToolCallWhenConfigured(t *testing.T) {
	text := `<tool_calls>
  <tool_call>
    <tool_name>Agent</tool_name>
    <parameters>
      <description>Explore</description>
      <prompt>Explore the repository</prompt>
      <subagent_type>general</subagent_type>
    </parameters>
  </tool_call>
</tool_calls>`
	resp := BuildChatCompletion("cid", "deepseek", "prompt", "", text, []string{"Agent", "read"}, toolcall.ParameterSchemas{
		"Agent": {
			"type": "object",
			"properties": map[string]any{
				"description":   map[string]any{"type": "string"},
				"prompt":        map[string]any{"type": "string"},
				"subagent_type": map[string]any{"type": "string"},
			},
			"required": []any{"description", "prompt", "subagent_type"},
		},
	}, true)
	choices, _ := resp["choices"].([]map[string]any)
	msg, _ := choices[0]["message"].(map[string]any)
	if _, ok := msg["tool_calls"]; !ok {
		t.Fatalf("expected executable Agent tool call, got %#v", msg)
	}
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", choices[0])
	}
}

func TestBuildChatCompletionPromotesToolCallsFromThinkingWhenVisibleTextEmpty(t *testing.T) {
	thinking := `<tool_calls>
  <tool_call>
    <tool_name>Read</tool_name>
    <parameters>
      <file_path>/tmp/a.txt</file_path>
    </parameters>
  </tool_call>
</tool_calls>`
	resp := BuildChatCompletion("cid", "deepseek", "prompt", thinking, "", []string{"Read"}, toolcall.ParameterSchemas{
		"Read": {
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
			},
			"required": []any{"file_path"},
		},
	}, false)
	choices, _ := resp["choices"].([]map[string]any)
	msg, _ := choices[0]["message"].(map[string]any)
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", choices[0])
	}
	if _, ok := msg["tool_calls"]; !ok {
		t.Fatalf("expected tool calls promoted from thinking, got %#v", msg)
	}
	if msg["content"] != nil {
		t.Fatalf("expected nil content for tool call response, got %#v", msg["content"])
	}
}

func TestBuildChatCompletionPromotesVisibleJSONToolSequenceWithLeadingProse(t *testing.T) {
	text := `Let me read the plan first.
{
  "tool": "Read",
  "arguments": {
    "file_path": "/tmp/plan.md",
    "offset": 200,
    "limit": 200
  }
}
{
  "tool": "TaskCreate",
  "arguments": {
    "description": "track",
    "prompt": "track"
  }
}`
	resp := BuildChatCompletion("cid", "deepseek", "prompt", "", text, []string{"Read", "TaskCreate"}, toolcall.ParameterSchemas{
		"Read": {
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string"},
				"offset":    map[string]any{"type": "integer"},
				"limit":     map[string]any{"type": "integer"},
			},
			"required": []any{"file_path"},
		},
		"TaskCreate": {
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string"},
				"prompt":      map[string]any{"type": "string"},
			},
		},
	}, false)
	choices, _ := resp["choices"].([]map[string]any)
	msg, _ := choices[0]["message"].(map[string]any)
	if choices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %#v", choices[0])
	}
	toolCalls, _ := msg["tool_calls"].([]map[string]any)
	if len(toolCalls) != 1 {
		t.Fatalf("expected only Read tool call, got %#v", msg)
	}
	fn, _ := toolCalls[0]["function"].(map[string]any)
	if fn["name"] != "Read" {
		t.Fatalf("expected Read function, got %#v", toolCalls[0])
	}
	if msg["content"] != nil {
		t.Fatalf("expected nil content for tool call response, got %#v", msg["content"])
	}
}

func TestBuildChatCompletionDeepSeekOfficialEnvelope(t *testing.T) {
	resp := BuildChatCompletion("cid", "deepseek-v4-pro", "prompt", "think", "answer", nil, nil, false)
	if resp["object"] != "chat.completion" {
		t.Fatalf("unexpected object: %#v", resp["object"])
	}
	if resp["system_fingerprint"] != DeepSeekSystemFingerprint() {
		t.Fatalf("expected system_fingerprint, got %#v", resp)
	}
	usage, _ := resp["usage"].(map[string]any)
	if usage["prompt_cache_hit_tokens"] == nil || usage["prompt_cache_miss_tokens"] == nil {
		t.Fatalf("expected DeepSeek cache token usage fields, got %#v", usage)
	}
}

func TestBuildChatStreamChunkDeepSeekOfficialEnvelope(t *testing.T) {
	chunk := BuildChatStreamChunk("cid", 123, "deepseek-v4-pro", []map[string]any{
		BuildChatStreamDeltaChoice(0, map[string]any{"role": "assistant", "content": "hi"}),
	}, nil)
	if chunk["object"] != "chat.completion.chunk" {
		t.Fatalf("unexpected object: %#v", chunk["object"])
	}
	if chunk["system_fingerprint"] != DeepSeekSystemFingerprint() {
		t.Fatalf("expected system_fingerprint, got %#v", chunk)
	}
	choices := chunk["choices"].([]map[string]any)
	if choices[0]["finish_reason"] != nil || choices[0]["logprobs"] != nil {
		t.Fatalf("expected streaming delta choice finish_reason/logprobs null, got %#v", choices[0])
	}

	usageChunk := BuildChatStreamUsageChunk("cid", 123, "deepseek-v4-pro", map[string]any{"total_tokens": 3})
	if got := usageChunk["choices"].([]map[string]any); len(got) != 0 {
		t.Fatalf("expected empty choices in usage chunk, got %#v", usageChunk)
	}
	if _, ok := usageChunk["usage"].(map[string]any); !ok {
		t.Fatalf("expected usage object, got %#v", usageChunk)
	}
}
