package config

import "testing"

type mockModelAliasReader map[string]string

func (m mockModelAliasReader) ModelAliases() map[string]string { return m }

func TestResolveModelDirectDeepSeek(t *testing.T) {
	got, ok := ResolveModel(nil, "deepseek-chat")
	if !ok || got != "deepseek-chat" {
		t.Fatalf("expected deepseek-chat, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelDirectDeepSeekV4(t *testing.T) {
	for _, model := range []string{"deepseek-v4-pro[1m]", "deepseek-v4-pro", "deepseek-v4-flash"} {
		got, ok := ResolveModel(nil, model)
		if !ok || got != model {
			t.Fatalf("expected %s, got ok=%v model=%q", model, ok, got)
		}
	}
}

func TestResolveModelAlias(t *testing.T) {
	got, ok := ResolveModel(nil, "gpt-4.1")
	if !ok || got != "deepseek-chat" {
		t.Fatalf("expected alias gpt-4.1 -> deepseek-chat, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelHeuristicReasoner(t *testing.T) {
	got, ok := ResolveModel(nil, "o3-super")
	if !ok || got != "deepseek-reasoner" {
		t.Fatalf("expected heuristic reasoner, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelUnknown(t *testing.T) {
	_, ok := ResolveModel(nil, "totally-custom-model")
	if ok {
		t.Fatal("expected unknown model to fail resolve")
	}
}

func TestResolveModelDirectDeepSeekExpert(t *testing.T) {
	got, ok := ResolveModel(nil, "deepseek-expert-chat")
	if !ok || got != "deepseek-expert-chat" {
		t.Fatalf("expected deepseek-expert-chat, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelCustomAliasToExpert(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"my-expert-model": "deepseek-expert-reasoner-search",
	}, "my-expert-model")
	if !ok || got != "deepseek-expert-reasoner-search" {
		t.Fatalf("expected alias -> deepseek-expert-reasoner-search, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelCustomAliasToVision(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"my-vision-model": "deepseek-vision-chat-search",
	}, "my-vision-model")
	if !ok || got != "deepseek-vision-chat-search" {
		t.Fatalf("expected alias -> deepseek-vision-chat-search, got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelCustomAliasToDeepSeekV4(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"coding-main": "deepseek-v4-pro[1m]",
	}, "coding-main")
	if !ok || got != "deepseek-v4-pro[1m]" {
		t.Fatalf("expected alias -> deepseek-v4-pro[1m], got ok=%v model=%q", ok, got)
	}
}

func TestResolveModelCustomAliasOverridesDirectDeepSeek(t *testing.T) {
	got, ok := ResolveModel(mockModelAliasReader{
		"deepseek-v4-pro": "deepseek-v4-pro[1m]",
	}, "deepseek-v4-pro")
	if !ok || got != "deepseek-v4-pro[1m]" {
		t.Fatalf("expected direct alias override -> deepseek-v4-pro[1m], got ok=%v model=%q", ok, got)
	}
}

func TestClaudeModelsResponsePaginationFields(t *testing.T) {
	resp := ClaudeModelsResponse()
	if _, ok := resp["first_id"]; !ok {
		t.Fatalf("expected first_id in response: %#v", resp)
	}
	if _, ok := resp["last_id"]; !ok {
		t.Fatalf("expected last_id in response: %#v", resp)
	}
	if _, ok := resp["has_more"]; !ok {
		t.Fatalf("expected has_more in response: %#v", resp)
	}
}
