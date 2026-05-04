package claudecode

import (
	"strings"
	"testing"
)

func TestScheduleAgentLaunchChineseCounts(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{name: "one", text: "启动一个代理评估改动。", want: 1},
		{name: "two", text: "准备两个子代理并行评估。", want: 2},
		{name: "three_digit", text: "启动3个代理处理这些任务。", want: 3},
		{name: "four", text: "启动四个代理并行处理。", want: 4},
		{name: "simultaneous_three", text: "让我同时启动三个代理来并行实现三个任务。", want: 3},
		{name: "prepared_three", text: "准备三个子代理并行推进。", want: 3},
		{name: "route_split", text: "现在直接动手，分三路把代码写到位。", want: 3},
		{name: "clamped", text: "启动9个代理并行处理。", want: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScheduleAgentLaunch(AgentSchedulerInput{
				FinalPrompt:         "<｜User｜>请使用 Agent<｜Assistant｜>",
				Text:                tt.text,
				ToolNames:           []string{"Agent", "Read"},
				AllowMetaAgentTools: true,
			})
			if len(got.Calls) != tt.want {
				t.Fatalf("expected %d calls, got %d: %#v", tt.want, len(got.Calls), got)
			}
			for _, call := range got.Calls {
				if call.Name != "Agent" {
					t.Fatalf("expected Agent call, got %q", call.Name)
				}
				if call.Input["run_in_background"] != true {
					t.Fatalf("expected run_in_background=true, got %#v", call.Input)
				}
			}
		})
	}
}

func TestScheduleAgentLaunchSuppressesExistingCurrentTurnAgents(t *testing.T) {
	tests := []struct {
		name        string
		finalPrompt string
		want        int
		suppressed  bool
	}{
		{
			name:        "launched",
			finalPrompt: "<｜User｜>请启动三个代理<｜Assistant｜>\nAsync agent launched successfully\n",
			want:        0,
			suppressed:  true,
		},
		{
			name:        "running",
			finalPrompt: "<｜User｜>请启动三个代理<｜Assistant｜>\nTask Output runabcdef\nTask is still running.\n",
			want:        0,
			suppressed:  true,
		},
		{
			name:        "additional",
			finalPrompt: "<｜User｜>请再启动两个代理<｜Assistant｜>\nTask Output runabcdef\nTask is still running.\n",
			want:        2,
			suppressed:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScheduleAgentLaunch(AgentSchedulerInput{
				FinalPrompt:         tt.finalPrompt,
				Text:                "准备两个子代理并行推进。",
				ToolNames:           []string{"Agent"},
				AllowMetaAgentTools: true,
			})
			if len(got.Calls) != tt.want {
				t.Fatalf("expected %d calls, got %d: %#v", tt.want, len(got.Calls), got)
			}
			if got.Suppressed != tt.suppressed {
				t.Fatalf("expected suppressed=%v, got %#v", tt.suppressed, got)
			}
		})
	}
}

func TestScheduleAgentLaunchExtractsConcreteTasksAndModes(t *testing.T) {
	got := ScheduleAgentLaunch(AgentSchedulerInput{
		FinalPrompt:         "<｜User｜>调用子代理并行推进<｜Assistant｜>",
		Text:                "准备三个子代理并行推进：一个改 lowering 的函数循环并行化、一个改 parser 层的文件级并行收集、一个研究 emit 的并行方案。三个同时启动。",
		ToolNames:           []string{"Agent", "Read"},
		AllowMetaAgentTools: true,
	})
	if len(got.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %#v", len(got.Calls), got)
	}
	for i, want := range []string{"lowering", "parser", "emit"} {
		if !strings.Contains(inputString(got.Calls[i].Input["description"]), want) {
			t.Fatalf("call %d should preserve task %q: %#v", i, want, got.Calls[i].Input)
		}
	}
	for i := 0; i < 2; i++ {
		prompt := inputString(got.Calls[i].Input["prompt"])
		if !strings.Contains(prompt, "Edit files directly") ||
			!strings.Contains(prompt, "changed files and verification results") {
			t.Fatalf("call %d should be execution-oriented, got %s", i, prompt)
		}
	}
	researchPrompt := inputString(got.Calls[2].Input["prompt"])
	if !strings.Contains(researchPrompt, "Read-only analysis") {
		t.Fatalf("research task should be read-only, got %s", researchPrompt)
	}
	if strings.Contains(researchPrompt, "Edit files directly") {
		t.Fatalf("research task must not request edits, got %s", researchPrompt)
	}
}

func TestScheduleAgentLaunchKeepsPureAssessmentReadOnly(t *testing.T) {
	got := ScheduleAgentLaunch(AgentSchedulerInput{
		FinalPrompt:         "<｜User｜>评估当前实现状态<｜Assistant｜>",
		Text:                "准备三个子代理并行评估：一个分析 lowering 当前状态、一个审查 parser 当前状态、一个研究 emit 当前状态。三个同时启动。",
		ToolNames:           []string{"Agent", "Read"},
		AllowMetaAgentTools: true,
	})
	if len(got.Calls) != 3 {
		t.Fatalf("expected 3 calls, got %d: %#v", len(got.Calls), got)
	}
	for i, call := range got.Calls {
		prompt := inputString(call.Input["prompt"])
		if !strings.Contains(prompt, "Read-only analysis") {
			t.Fatalf("call %d should be read-only, got %s", i, prompt)
		}
		if strings.Contains(prompt, "Edit files directly") {
			t.Fatalf("call %d must not request edits, got %s", i, prompt)
		}
	}
}
