//go:build legacy_openai_adapter

package openai

func runtimeStreamMaxDurationSeconds(store ConfigReader) int {
	if store == nil {
		return 900
	}
	return store.RuntimeStreamMaxDurationSeconds()
}

func runtimeReasoningOnlyTimeoutSeconds(store ConfigReader) int {
	if store == nil {
		return 180
	}
	return store.RuntimeReasoningOnlyTimeoutSeconds()
}

func runtimeBufferedToolContentMaxBytes(store ConfigReader) int {
	if store == nil {
		return 262144
	}
	return store.RuntimeBufferedToolContentMaxBytes()
}
