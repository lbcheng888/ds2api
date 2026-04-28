package claudecode

import (
	"html"
	"sort"
	"strings"

	"ds2api/internal/protocol"
)

type TeamAgentState struct {
	Running                   []string
	Completed                 []string
	Missing                   []string
	NotificationIDs           []string
	AllowedTaskOutputIDs      []string
	HasRecentBackgroundLaunch bool
}

func ExtractTeamAgentState(finalPrompt string) TeamAgentState {
	state := TeamAgentState{
		HasRecentBackgroundLaunch: RecentPromptHasBackgroundAgentLaunch(finalPrompt),
	}
	running := map[string]struct{}{}
	completed := map[string]struct{}{}
	missing := map[string]struct{}{}
	notifications := map[string]struct{}{}
	allowed := map[string]struct{}{}

	for _, task := range protocol.ExtractTaskStates(finalPrompt) {
		id := protocol.CleanTaskID(task.TaskID)
		if id == "" {
			continue
		}
		switch task.Status {
		case protocol.TaskStatusMissing:
			missing[id] = struct{}{}
			delete(running, id)
			delete(completed, id)
			delete(allowed, id)
		case protocol.TaskStatusRunning:
			if _, isMissing := missing[id]; isMissing {
				continue
			}
			running[id] = struct{}{}
			allowed[id] = struct{}{}
		case protocol.TaskStatusCompleted:
			if _, isMissing := missing[id]; isMissing {
				continue
			}
			completed[id] = struct{}{}
			allowed[id] = struct{}{}
		}
	}

	latestUser := html.UnescapeString(LatestUserPromptBlock(finalPrompt))
	if strings.Contains(strings.ToLower(latestUser), "<task-notification") {
		for _, id := range ExtractTaskNotificationIDs(latestUser) {
			if _, isMissing := missing[id]; isMissing {
				continue
			}
			notifications[id] = struct{}{}
			allowed[id] = struct{}{}
		}
	}

	state.Running = sortedKeys(running)
	state.Completed = sortedKeys(completed)
	state.Missing = sortedKeys(missing)
	state.NotificationIDs = sortedKeys(notifications)
	state.AllowedTaskOutputIDs = sortedKeys(allowed)
	return state
}

func (s TeamAgentState) AllowedTaskOutputIDSet() map[string]struct{} {
	allowed := map[string]struct{}{}
	for _, id := range s.AllowedTaskOutputIDs {
		id = protocol.CleanTaskID(id)
		if id != "" {
			allowed[id] = struct{}{}
		}
	}
	return allowed
}

func sortedKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
