package claudecode

import "sync"

type profileMetrics struct {
	repairs  map[string]int64
	streams  map[string]int64
	failures map[string]int64
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
	}
	metricsState.profiles[pk] = pm
	return pm
}

func recordRepair(profile, reason string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).repairs[reason]++
}

func recordStreamOutcome(profile, reason string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).streams[reason]++
}

func recordFailureDecision(profile, code string) {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	getOrCreateProfile(profile).failures[code]++
}

func SnapshotMetrics() map[string]any {
	metricsState.mu.Lock()
	defer metricsState.mu.Unlock()
	out := make(map[string]any, len(metricsState.profiles))
	for profile, pm := range metricsState.profiles {
		out[profile] = map[string]any{
			"repairs":  cloneMetricMap(pm.repairs),
			"streams":  cloneMetricMap(pm.streams),
			"failures": cloneMetricMap(pm.failures),
		}
	}
	return out
}

func cloneMetricMap(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
