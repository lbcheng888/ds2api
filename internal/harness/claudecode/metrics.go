package claudecode

import "sync"

var metricsState = struct {
	mu       sync.Mutex
	repairs  map[string]int64
	streams  map[string]int64
	failures map[string]int64
}{
	repairs:  map[string]int64{},
	streams:  map[string]int64{},
	failures: map[string]int64{},
}

func recordRepair(reason string) {
	recordMetric(metricsState.repairs, reason)
}

func recordStreamOutcome(reason string) {
	recordMetric(metricsState.streams, reason)
}

func recordFailureDecision(code string) {
	recordMetric(metricsState.failures, code)
}

func recordMetric(bucket map[string]int64, key string) {
	if key == "" {
		return
	}
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	bucket[key]++
}

func SnapshotMetrics() map[string]any {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	return map[string]any{
		"repairs":  cloneMetricMap(metricsState.repairs),
		"streams":  cloneMetricMap(metricsState.streams),
		"failures": cloneMetricMap(metricsState.failures),
	}
}

func cloneMetricMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
