package chat

import (
	"net/http/httptest"
	"testing"
)

func TestShouldHoldBufferedToolContentCachesPlanModePromise(t *testing.T) {
	runtime := newChatStreamRuntime(
		httptest.NewRecorder(),
		nil,
		false,
		"chatcmpl-test",
		1,
		"gpt-5.5",
		"<｜User｜>继续推进<｜Assistant｜>",
		false,
		false,
		false,
		[]string{"Read", "Bash", "Edit"},
		nil,
		true,
		false,
	)
	runtime.text.WriteString("In plan mode - before writing any code - I first need to understand primary_object_plan.cheng. I will also consult your lessons file.")

	if !runtime.shouldHoldBufferedToolContent() {
		t.Fatal("expected plan-mode promise to be held")
	}
	if !runtime.holdBufferedToolText {
		t.Fatal("expected hold decision to be cached")
	}
	runtime.text.WriteString(" More streamed text should not trigger another full missing-tool decision.")
	if !runtime.shouldHoldBufferedToolContent() {
		t.Fatal("expected cached hold decision to keep holding")
	}
}

func TestShouldHoldBufferedToolContentIgnoresPlainAnswer(t *testing.T) {
	runtime := newChatStreamRuntime(
		httptest.NewRecorder(),
		nil,
		false,
		"chatcmpl-test",
		1,
		"gpt-5.5",
		"<｜User｜>解释一下<｜Assistant｜>",
		false,
		false,
		false,
		[]string{"Read", "Bash", "Edit"},
		nil,
		true,
		false,
	)
	runtime.text.WriteString("这是一个普通说明，不需要调用工具。")

	if runtime.shouldHoldBufferedToolContent() {
		t.Fatal("plain answer should not be held as a tool promise")
	}
	if runtime.holdBufferedToolText {
		t.Fatal("plain answer should not cache hold state")
	}
}

func TestShouldHoldBufferedToolContentHoldsToolRequiredTurnWithoutCandidateText(t *testing.T) {
	runtime := newChatStreamRuntime(
		httptest.NewRecorder(),
		nil,
		false,
		"chatcmpl-test",
		1,
		"gpt-5.5",
		"<｜User｜>请一口气完成<｜Assistant｜>",
		false,
		false,
		false,
		[]string{"Read", "Bash", "Edit"},
		nil,
		true,
		false,
	)
	runtime.text.WriteString("好的。")

	if !runtime.shouldHoldBufferedToolContent() {
		t.Fatal("tool-required turn should hold even when visible text has no promise phrase")
	}
	if !runtime.holdBufferedToolText {
		t.Fatal("expected tool-required hold decision to be cached")
	}
}
