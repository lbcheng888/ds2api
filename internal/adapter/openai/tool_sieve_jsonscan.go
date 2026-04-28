package openai

import claudecodeharness "ds2api/internal/harness/claudecode"

//nolint:unused // package tests keep legacy helper names while implementation lives in claudecode harness.
func trimWrappingJSONFence(prefix, suffix string) (string, string) {
	return claudecodeharness.TrimWrappingJSONFence(prefix, suffix)
}
