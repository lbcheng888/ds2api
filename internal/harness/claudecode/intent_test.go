package claudecode

import "testing"

func TestCompileRequestIntentTable(t *testing.T) {
	tests := []struct {
		name string
		in   RequestIntentInput
		want func(t *testing.T, got RequestIntent)
	}{
		{
			name: "继续推进",
			in: RequestIntentInput{
				LatestUserText:     "继续推进",
				AvailableToolNames: []string{"Read", "Edit", "Bash"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.UserAuthorization.Execute || !got.UserAuthorization.Continue {
					t.Fatalf("expected continue execution authorization, got %#v", got.UserAuthorization)
				}
			},
		},
		{
			name: "现在直接动手",
			in: RequestIntentInput{
				FinalText:          "现在直接动手，分三路把代码写到位。",
				AvailableToolNames: []string{"Read", "Edit", "Write"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.TextPromises.Edit || !got.TextPromises.WriteFile || !got.TextPromises.Any {
					t.Fatalf("expected edit/write promise, got %#v", got.TextPromises)
				}
			},
		},
		{
			name: "Let me examine",
			in: RequestIntentInput{
				FinalText:          "Let me examine the relevant files first.",
				AvailableToolNames: []string{"Read", "Grep"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.TextPromises.Inspect || !got.TextPromises.Any {
					t.Fatalf("expected inspect promise, got %#v", got.TextPromises)
				}
			},
		},
		{
			name: "I'll run",
			in: RequestIntentInput{
				FinalText:          "I'll run the unit tests now.",
				AvailableToolNames: []string{"Bash"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.TextPromises.RunCommand || !got.TextPromises.Any {
					t.Fatalf("expected run-command promise, got %#v", got.TextPromises)
				}
			},
		},
		{
			name: "已修改并测试通过",
			in: RequestIntentInput{
				FinalText:          "已修改并测试通过。",
				AvailableToolNames: []string{"Edit", "Bash"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.ClaimsWithoutTools.Edited || !got.ClaimsWithoutTools.Verified || !got.ClaimsWithoutTools.Any {
					t.Fatalf("expected edit/verify claims without tool evidence, got %#v", got.ClaimsWithoutTools)
				}
			},
		},
		{
			name: "纯分析评估",
			in: RequestIntentInput{
				LatestUserText:     "只做纯分析评估，不改代码。",
				FinalText:          "这是纯分析评估，不修改文件。",
				AvailableToolNames: []string{"Read", "Edit"},
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.PureAnalysis {
					t.Fatalf("expected pure analysis intent, got %#v", got)
				}
				if got.TextPromises.Edit || got.TextPromises.WriteFile || got.TextPromises.RunCommand {
					t.Fatalf("pure analysis should not compile write/run promises, got %#v", got.TextPromises)
				}
			},
		},
		{
			name: "同时启动三个代理",
			in: RequestIntentInput{
				LatestUserText:      "继续推进",
				FinalText:           "让我同时启动三个代理来并行实现三个任务。",
				AvailableToolNames:  []string{"Agent", "Read"},
				AllowMetaAgentTools: true,
			},
			want: func(t *testing.T, got RequestIntent) {
				if !got.AgentLaunch.Present || !got.AgentLaunch.CountKnown || got.AgentLaunch.Count != 3 {
					t.Fatalf("expected three-agent launch intent, got %#v", got.AgentLaunch)
				}
				if !got.TextPromises.LaunchAgent || !got.TextPromises.Any {
					t.Fatalf("expected launch-agent promise, got %#v", got.TextPromises)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.want(t, CompileRequestIntent(tt.in))
		})
	}
}

func TestCompileRequestIntentClaimsHonorCurrentTurnToolEvidence(t *testing.T) {
	got := CompileRequestIntent(RequestIntentInput{
		FinalText:            "已修改并测试通过。",
		CurrentTurnToolNames: []string{"Edit", "Bash"},
		AvailableToolNames:   []string{"Edit", "Bash"},
	})
	if got.ClaimsWithoutTools.Any {
		t.Fatalf("expected current-turn Edit/Bash evidence to satisfy claims, got %#v", got.ClaimsWithoutTools)
	}
}
