package claudecode

import (
	"strings"
	"sync"

	"ds2api/internal/toolcall"
)

type profileMetrics struct {
	repairs  map[string]int64
	streams  map[string]int64
	failures map[string]int64
	dedupes  map[string]int64
}

var metricsState = struct {
	mu       sync.Mutex
	profiles map[string]*profileMetrics
}{
	profiles: map[string]*profileMetrics{},
}

func profileKey(profile string) string {
	if profile == "" {
		return "unknown"
	}
	return profile
}

func getOrCreateProfile(profile string) *profileMetrics {
	pk := profileKey(profile)
	if pm, ok := metricsState.profiles[pk]; ok {
		return pm
	}
	pm := &profileMetrics{
		repairs:  map[string]int64{},
		streams:  map[string]int64{},
		failures: map[string]int64{},
		dedupes:  map[string]int64{},
	}
	metricsState.profiles[pk] = pm
	return pm
}

func recordRepair(profile, reason string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).repairs[metricKey(reason)]++
}

func recordStreamOutcome(profile, reason string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).streams[metricKey(reason)]++
}

func recordFailureDecision(profile, code string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).failures[metricKey(code)]++
}

// RecordDeduplication adds the number of dropped duplicate items for Admin diagnostics.
func RecordDeduplication(profile, reason string, dropped int) {
	if dropped <= 0 {
		return
	}
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).dedupes[metricKey(reason)] += int64(dropped)
}

func recordDedupeReport(profile string, report toolcall.DedupeReport) {
	RecordDeduplication(profile, "tool_calls", report.ToolCallsDropped)
	RecordDeduplication(profile, "todo_items", report.TodoItemsDropped)
}

func SnapshotMetrics() map[string]any {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	out := make(map[string]any, len(metricsState.profiles))
	for profile, pm := range metricsState.profiles {
		failureRate := computeFailureRate(pm)
		repairHitRate := computeRepairHitRate(pm)
		out[profile] = map[string]any{
			"repairs":         cloneMetricMap(pm.repairs),
			"streams":         cloneMetricMap(pm.streams),
			"failures":        cloneMetricMap(pm.failures),
			"dedupes":         cloneMetricMap(pm.dedupes),
			"failure_rate":    failureRate,
			"repair_hit_rate": repairHitRate,
		}
	}
	return out
}

func computeFailureRate(pm *profileMetrics) float64 {
	var totalFailures, totalOutcomes int64
	for _, count := range pm.failures {
		totalFailures += count
	}
	for _, count := range pm.streams {
		totalOutcomes += count
	}
	if totalOutcomes == 0 {
		return 0.0
	}
	return float64(totalFailures) / float64(totalOutcomes)
}

func computeRepairHitRate(pm *profileMetrics) float64 {
	var totalRepairs, totalOpportunities int64
	for _, count := range pm.repairs {
		totalRepairs += count
	}
	// 修复机会 = streams总数 (因为每次stream结果都有机会修复)
	for _, count := range pm.streams {
		totalOpportunities += count
	}
	if totalOpportunities == 0 {
		return 0.0
	}
	return float64(totalRepairs) / float64(totalOpportunities)
}

// SuggestGoldenTestCategory 根据失败模式建议golden test类别
func SuggestGoldenTestCategory(failurePattern string) string {
	pattern := strings.ToLower(strings.TrimSpace(failurePattern))

	// 工具调用相关
	if strings.Contains(pattern, "tool") || strings.Contains(pattern, "call") {
		if strings.Contains(pattern, "missing") {
			return "tool_call_missing"
		}
		if strings.Contains(pattern, "invalid") {
			return "tool_call_invalid_syntax"
		}
		if strings.Contains(pattern, "duplicate") {
			return "tool_call_duplicate"
		}
		return "tool_call_error"
	}

	// 探索重复
	if strings.Contains(pattern, "exploration") || strings.Contains(pattern, "repeated") {
		return "repeated_exploration"
	}

	// 流处理
	if strings.Contains(pattern, "stream") || strings.Contains(pattern, "incomplete") {
		return "incomplete_stream_transaction"
	}

	// 输出修复
	if strings.Contains(pattern, "repair") || strings.Contains(pattern, "fix") {
		if strings.Contains(pattern, "read") {
			return "read_intent_repair"
		}
		if strings.Contains(pattern, "agent") {
			return "agent_launch_repair"
		}
		return "output_repair"
	}

	// 默认分类
	return "other_failure"
}

func metricKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "unknown"
	}
	return key
}

func cloneMetricMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
