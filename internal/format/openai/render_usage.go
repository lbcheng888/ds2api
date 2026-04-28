package openai

import (
	"ds2api/internal/promptcache"
	"ds2api/internal/util"
)

func BuildChatUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	return buildChatUsage(finalPrompt, finalThinking, finalText, promptcache.Stats{
		MissTokens: util.EstimateTokens(finalPrompt),
	})
}

func BuildChatUsageWithPromptCache(finalPrompt, finalThinking, finalText string) map[string]any {
	return buildChatUsage(finalPrompt, finalThinking, finalText, promptcache.DefaultLedger.Observe(finalPrompt))
}

func buildChatUsage(finalPrompt, finalThinking, finalText string, cacheStats promptcache.Stats) map[string]any {
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)
	return map[string]any{
		"prompt_tokens":            promptTokens,
		"prompt_cache_hit_tokens":  cacheStats.HitTokens,
		"prompt_cache_miss_tokens": cacheStats.MissTokens,
		"completion_tokens":        reasoningTokens + completionTokens,
		"total_tokens":             promptTokens + reasoningTokens + completionTokens,
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func BuildResponsesUsage(finalPrompt, finalThinking, finalText string) map[string]any {
	return buildResponsesUsage(finalPrompt, finalThinking, finalText, promptcache.Stats{
		MissTokens: util.EstimateTokens(finalPrompt),
	})
}

func BuildResponsesUsageWithPromptCache(finalPrompt, finalThinking, finalText string) map[string]any {
	return buildResponsesUsage(finalPrompt, finalThinking, finalText, promptcache.DefaultLedger.Observe(finalPrompt))
}

func buildResponsesUsage(finalPrompt, finalThinking, finalText string, cacheStats promptcache.Stats) map[string]any {
	promptTokens := util.EstimateTokens(finalPrompt)
	reasoningTokens := util.EstimateTokens(finalThinking)
	completionTokens := util.EstimateTokens(finalText)
	usage := map[string]any{
		"input_tokens":             promptTokens,
		"input_cache_hit_tokens":   cacheStats.HitTokens,
		"input_cache_miss_tokens":  cacheStats.MissTokens,
		"output_tokens":            reasoningTokens + completionTokens,
		"total_tokens":             promptTokens + reasoningTokens + completionTokens,
		"input_tokens_details":     map[string]any{"cache_read_tokens": cacheStats.HitTokens, "cache_creation_tokens": cacheStats.MissTokens},
		"prompt_cache_hit_tokens":  cacheStats.HitTokens,
		"prompt_cache_miss_tokens": cacheStats.MissTokens,
	}
	if reasoningTokens > 0 {
		usage["output_tokens_details"] = map[string]any{"reasoning_tokens": reasoningTokens}
	}
	return usage
}
