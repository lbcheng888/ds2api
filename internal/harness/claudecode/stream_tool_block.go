package claudecode

import "ds2api/internal/toolcall"

type StreamToolBlockResult struct {
	Calls  []toolcall.ParsedToolCall
	Text   string
	Parsed bool
}

func ParseStreamToolBlock(block string, toolNames []string, allowMetaAgentTools bool) StreamToolBlockResult {
	repaired := toolcall.RepairMalformedToolCallXML(block)
	calls := toolcall.ParseToolCalls(repaired, toolNames)
	if len(calls) == 0 {
		return StreamToolBlockResult{Text: block}
	}
	if !allowMetaAgentTools && toolcall.AllCallsAreMetaAgentTools(calls) {
		recordStreamOutcome("meta_agent_blocked")
		return StreamToolBlockResult{
			Text:   toolcall.MetaAgentToolBlockedMessage(),
			Parsed: true,
		}
	}
	recordStreamOutcome("tool_call")
	return StreamToolBlockResult{
		Calls:  calls,
		Parsed: true,
	}
}
