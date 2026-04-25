package toolcall

import "strings"

const metaAgentToolBlockedMessage = "Agent/subagent tools are disabled by ds2api for DeepSeek coding-agent compatibility. Continue with direct file tools such as read, grep, glob, and bash."

func IsMetaAgentToolName(name string) bool {
	key := canonicalToolPolicyName(name)
	switch key {
	case "agent", "task", "subagent", "newtask", "todowrite", "todoread", "taskcreate", "taskupdate", "taskoutput":
		return true
	default:
		return false
	}
}

func IsTaskTrackingToolName(name string) bool {
	key := canonicalToolPolicyName(name)
	switch key {
	case "taskcreate", "taskupdate", "todowrite", "todoread":
		return true
	default:
		return false
	}
}

func MetaAgentToolBlockedMessage() string {
	return metaAgentToolBlockedMessage
}

func AllCallsAreMetaAgentTools(calls []ParsedToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !IsMetaAgentToolName(call.Name) {
			return false
		}
	}
	return true
}

func AllCallsAreTaskTrackingTools(calls []ParsedToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if !IsTaskTrackingToolName(call.Name) {
			return false
		}
	}
	return true
}

func canonicalToolPolicyName(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch r {
		case '_', '-', ' ', '.':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}
