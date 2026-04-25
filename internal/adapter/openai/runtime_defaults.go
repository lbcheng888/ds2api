package openai

func runtimeStreamMaxDurationSeconds(store ConfigReader) int {
	if store == nil {
		return 900
	}
	return store.RuntimeStreamMaxDurationSeconds()
}

func runtimeBufferedToolContentMaxBytes(store ConfigReader) int {
	if store == nil {
		return 262144
	}
	return store.RuntimeBufferedToolContentMaxBytes()
}
