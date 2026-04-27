package protocol

import (
	"html"
	"regexp"
	"strings"
)

const (
	TaskStatusUnknown   = "unknown"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusMissing   = "missing"
)

type TaskState struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}

var taskStateNotificationBlockPattern = regexp.MustCompile(`(?is)<task-notification\b[^>]*>(.*?)</task-notification>`)
var taskStateIDTagPattern = regexp.MustCompile(`(?is)<task[-_]?id\b[^>]*>(.*?)</task[-_]?id>`)
var taskStateIDAttrPattern = regexp.MustCompile(`(?is)\btask[-_]?id\s*=\s*["']([^"']+)["']`)
var taskStateStatusTagPattern = regexp.MustCompile(`(?is)<status\b[^>]*>(.*?)</status>`)
var taskStateJSONStatusPattern = regexp.MustCompile(`(?is)["']status["']\s*:\s*["']([^"']+)["']`)
var taskStateOutputLinePattern = regexp.MustCompile(`(?is)\bTask\s+Output(?:\s*\([^)]*\))?\s+([a-z0-9_-]{8,})\b`)

func ExtractTaskStates(text string) []TaskState {
	text = RecentTaskWindow(text)
	states := []TaskState{}
	for _, state := range extractNotificationTaskStates(text) {
		states = upsertTaskState(states, state)
	}
	for _, state := range extractTaskOutputLineStates(text) {
		states = upsertTaskState(states, state)
	}
	return states
}

func TaskIDsWithStatus(states []TaskState, status string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, state := range states {
		if strings.TrimSpace(state.TaskID) == "" || state.Status != status {
			continue
		}
		if _, ok := seen[state.TaskID]; ok {
			continue
		}
		seen[state.TaskID] = struct{}{}
		out = append(out, state.TaskID)
	}
	return out
}

func RecentTaskWindow(text string) string {
	const max = 20000
	if len(text) <= max {
		return text
	}
	return text[len(text)-max:]
}

func extractNotificationTaskStates(text string) []TaskState {
	out := []TaskState{}
	for _, match := range taskStateNotificationBlockPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		block := match[0]
		status := classifyTaskStatus(firstTaskStatus(block))
		for _, id := range extractTaskIDsFromBlock(block) {
			out = append(out, TaskState{TaskID: id, Status: status, Source: "task-notification"})
		}
	}
	return out
}

func extractTaskOutputLineStates(text string) []TaskState {
	matches := taskStateOutputLinePattern.FindAllStringSubmatchIndex(text, -1)
	out := []TaskState{}
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		windowStart := match[1]
		windowEnd := len(text)
		if i+1 < len(matches) {
			windowEnd = matches[i+1][0]
		}
		if windowEnd-windowStart > 800 {
			windowEnd = windowStart + 800
		}
		id := CleanTaskID(text[match[2]:match[3]])
		if id == "" {
			continue
		}
		out = append(out, TaskState{
			TaskID: id,
			Status: classifyTaskStatus(text[windowStart:windowEnd]),
			Source: "task-output",
		})
	}
	return out
}

func extractTaskIDsFromBlock(block string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(raw string) {
		id := CleanTaskID(raw)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, match := range taskStateIDAttrPattern.FindAllStringSubmatch(block, -1) {
		if len(match) >= 2 {
			add(match[1])
		}
	}
	for _, match := range taskStateIDTagPattern.FindAllStringSubmatch(block, -1) {
		if len(match) >= 2 {
			add(match[1])
		}
	}
	return out
}

func firstTaskStatus(block string) string {
	if match := taskStateStatusTagPattern.FindStringSubmatch(block); len(match) >= 2 {
		return match[1]
	}
	if match := taskStateJSONStatusPattern.FindStringSubmatch(block); len(match) >= 2 {
		return match[1]
	}
	return block
}

func classifyTaskStatus(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, phrase := range []string{
		"no task found",
		"task not found",
		"not found with id",
	} {
		if strings.Contains(lower, phrase) {
			return TaskStatusMissing
		}
	}
	for _, phrase := range []string{
		"still running",
		"still in progress",
		"task is running",
		"running",
		"not done",
		"not completed",
	} {
		if strings.Contains(lower, phrase) {
			return TaskStatusRunning
		}
	}
	for _, phrase := range []string{
		"completed",
		"complete",
		"done",
		"finished",
		"succeeded",
	} {
		if strings.Contains(lower, phrase) {
			return TaskStatusCompleted
		}
	}
	if strings.Contains(text, "仍在运行") ||
		strings.Contains(text, "正在运行") ||
		strings.Contains(text, "尚未完成") ||
		strings.Contains(text, "未完成") {
		return TaskStatusRunning
	}
	if strings.Contains(text, "已完成") ||
		strings.Contains(text, "完成") ||
		strings.Contains(text, "结束") {
		return TaskStatusCompleted
	}
	return TaskStatusUnknown
}

func upsertTaskState(states []TaskState, next TaskState) []TaskState {
	next.TaskID = CleanTaskID(next.TaskID)
	if next.TaskID == "" {
		return states
	}
	if strings.TrimSpace(next.Status) == "" {
		next.Status = TaskStatusUnknown
	}
	for i := range states {
		if states[i].TaskID != next.TaskID {
			continue
		}
		states[i] = mergeTaskState(states[i], next)
		return states
	}
	return append(states, next)
}

func mergeTaskState(old TaskState, next TaskState) TaskState {
	if next.Status != TaskStatusUnknown || old.Status == TaskStatusUnknown {
		old.Status = next.Status
	}
	if next.Source != "" {
		old.Source = next.Source
	}
	return old
}

func CleanTaskID(raw string) string {
	id := strings.TrimSpace(html.UnescapeString(raw))
	if id == "" || strings.Contains(id, "<") || strings.Contains(id, ">") {
		return ""
	}
	return id
}
