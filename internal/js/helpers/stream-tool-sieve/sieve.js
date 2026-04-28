'use strict';
const {
  resetIncrementalToolState,
  noteText,
  insideCodeFenceWithState,
} = require('./state');
const { trimWrappingJSONFence } = require('./jsonscan');
const {
  findToolMarkupTagOutsideIgnored,
  sanitizeLooseCDATA,
} = require('./parse_payload');
const {
  consumeXMLToolCapture: consumeXMLToolCaptureImpl,
  hasOpenXMLToolTag,
  findPartialXMLToolTagStart,
} = require('./sieve-xml');
const {
  findVisibleJSONToolSegmentStart,
  findPartialVisibleJSONToolSegmentStart,
  consumeVisibleJSONToolCapture,
  visibleJSONToolCaptureMayContinue,
} = require('./visible-json');

const INVALID_TOOL_CALL_CODE = 'upstream_invalid_tool_call';
const INVALID_TOOL_CALL_MESSAGE = 'Upstream model emitted invalid tool call syntax.';
function processToolSieveChunk(state, chunk, toolNames) {
  if (!state) {
    return [];
  }
  if (chunk) {
    state.pending += chunk;
  }
  const events = [];
  while (true) {
    if (Array.isArray(state.pendingToolCalls) && state.pendingToolCalls.length > 0) {
      events.push({ type: 'tool_calls', calls: state.pendingToolCalls });
      state.pendingToolRaw = '';
      state.pendingToolCalls = [];
      continue;
    }
    if (state.capturing) {
      if (state.pending) {
        state.capture += state.pending;
        state.pending = '';
      }
      const consumed = consumeToolCapture(state, toolNames);
      if (!consumed.ready) {
        break;
      }
      const captured = state.capture;
      state.capture = '';
      state.capturing = false;
      resetIncrementalToolState(state);

      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        if (consumed.prefix) {
          noteText(state, consumed.prefix);
          events.push({ type: 'text', text: consumed.prefix });
        }
        state.pendingToolRaw = captured;
        state.pendingToolCalls = consumed.calls;
        if (consumed.suffix) {
          state.pending = consumed.suffix + state.pending;
        }
        continue;
      }
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (consumed.suffix) {
        state.pending += consumed.suffix;
      }
      continue;
    }
    const pending = state.pending || '';
    if (!pending) {
      break;
    }
    let start = findToolSegmentStart(state, pending);
    if (start < 0) {
      start = findVisibleJSONToolSegmentStart(state, pending, insideCodeFenceWithState);
    }
    if (start >= 0) {
      const prefix = pending.slice(0, start);
      if (prefix) {
        noteText(state, prefix);
        events.push({ type: 'text', text: prefix });
      }
      state.pending = '';
      state.capture += pending.slice(start);
      state.capturing = true;
      resetIncrementalToolState(state);
      continue;
    }
    const [safe, hold] = splitSafeContentForToolDetection(state, pending);
    if (!safe) {
      break;
    }
    state.pending = hold;
    noteText(state, safe);
    events.push({ type: 'text', text: safe });
  }
  return events;
}

function flushToolSieve(state, toolNames) {
  if (!state) {
    return [];
  }
  const events = processToolSieveChunk(state, '', toolNames);
  if (Array.isArray(state.pendingToolCalls) && state.pendingToolCalls.length > 0) {
    events.push({ type: 'tool_calls', calls: state.pendingToolCalls });
    state.pendingToolRaw = '';
    state.pendingToolCalls = [];
  }
  if (state.capturing) {
    const consumed = consumeToolCapture(state, toolNames);
    if (consumed.ready) {
      if (consumed.prefix) {
        noteText(state, consumed.prefix);
        events.push({ type: 'text', text: consumed.prefix });
      }
      if (Array.isArray(consumed.calls) && consumed.calls.length > 0) {
        events.push({ type: 'tool_calls', calls: consumed.calls });
      }
      if (consumed.suffix) {
        noteText(state, consumed.suffix);
        events.push({ type: 'text', text: consumed.suffix });
      }
    } else if (state.capture) {
      const content = state.capture;
      const recovered = sanitizeLooseCDATA(content);
      if (recovered !== content) {
        const recoveredResult = consumeXMLToolCaptureImpl(recovered, toolNames, trimWrappingJSONFence);
        if (recoveredResult.ready && Array.isArray(recoveredResult.calls) && recoveredResult.calls.length > 0) {
          if (recoveredResult.prefix) {
            noteText(state, recoveredResult.prefix);
            events.push({ type: 'text', text: recoveredResult.prefix });
          }
          events.push({ type: 'tool_calls', calls: recoveredResult.calls });
          if (recoveredResult.suffix) {
            noteText(state, recoveredResult.suffix);
            events.push({ type: 'text', text: recoveredResult.suffix });
          }
        } else if (incompleteToolTransactionError(content)) {
          events.push({ type: 'error', code: INVALID_TOOL_CALL_CODE, message: INVALID_TOOL_CALL_MESSAGE });
        } else {
          noteText(state, content);
          events.push({ type: 'text', text: content });
        }
      } else if (incompleteToolTransactionError(content)) {
        events.push({ type: 'error', code: INVALID_TOOL_CALL_CODE, message: INVALID_TOOL_CALL_MESSAGE });
      } else {
        noteText(state, content);
        events.push({ type: 'text', text: content });
      }
    }
    state.capture = '';
    state.capturing = false;
    resetIncrementalToolState(state);
  }
  if (state.pending) {
    noteText(state, state.pending);
    events.push({ type: 'text', text: state.pending });
    state.pending = '';
  }
  return events;
}

function incompleteToolTransactionError(captured) {
  const trimmed = String(captured || '').trim();
  if (!trimmed) {
    return false;
  }
  const lower = trimmed.toLowerCase();
  if (hasOpenXMLToolTag(trimmed) ||
      lower.includes('<tool_call') ||
      lower.includes('<tool_calls') ||
      lower.includes('<invoke') ||
      lower.includes('<function_call') ||
      lower.includes('<function_calls') ||
      lower.includes('<tool_use') ||
      lower.includes('<attempt_completion') ||
      lower.includes('<ask_followup_question') ||
      lower.includes('<new_task') ||
      lower.includes('<|dsml') ||
      lower.includes('<｜dsml') ||
      lower.includes('<dsml')) {
    return true;
  }
  return (trimmed.startsWith('{') || trimmed.startsWith('[')) &&
    visibleJSONToolCaptureMayContinue(trimmed) &&
    hasVisibleJSONToolHints(lower);
}

function hasVisibleJSONToolHints(lower) {
  const hasName = lower.includes('"tool"') ||
    lower.includes('"name"') ||
    lower.includes('"tool_name"') ||
    lower.includes('"function"');
  const hasArgs = lower.includes('"arguments"') ||
    lower.includes('"input"') ||
    lower.includes('"params"') ||
    lower.includes('"parameters"');
  return hasName && hasArgs;
}

function splitSafeContentForToolDetection(state, s) {
  const text = s || '';
  if (!text) {
    return ['', ''];
  }
  // Only hold back partial XML tool tags.
  const xmlIdx = findPartialXMLToolTagStart(text);
  if (xmlIdx >= 0) {
    if (insideCodeFenceWithState(state, text.slice(0, xmlIdx))) {
      return [text, ''];
    }
    if (xmlIdx > 0) {
      return [text.slice(0, xmlIdx), text.slice(xmlIdx)];
    }
    return ['', text];
  }
  const jsonIdx = findPartialVisibleJSONToolSegmentStart(state, text, insideCodeFenceWithState);
  if (jsonIdx >= 0) {
    if (jsonIdx > 0) {
      return [text.slice(0, jsonIdx), text.slice(jsonIdx)];
    }
    return ['', text];
  }
  return [text, ''];
}

function findToolSegmentStart(state, s) {
  if (!s) {
    return -1;
  }
  let offset = 0;
  while (true) {
    const tag = findToolMarkupTagOutsideIgnored(s, offset);
    if (!tag) {
      return -1;
    }
    if (!insideCodeFenceWithState(state, s.slice(0, tag.start))) {
      return tag.start;
    }
    offset = tag.end + 1;
  }
}

function consumeToolCapture(state, toolNames) {
  const captured = state.capture || '';
  if (!captured) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }

  // XML-only tool call extraction.
  const xmlResult = consumeXMLToolCaptureImpl(captured, toolNames, trimWrappingJSONFence);
  if (xmlResult.ready) {
    return xmlResult;
  }
  // If XML tags are present but block is incomplete, keep buffering.
  if (hasOpenXMLToolTag(captured)) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const jsonResult = consumeVisibleJSONToolCapture(captured, toolNames);
  if (jsonResult.ready) {
    return jsonResult;
  }
  if (visibleJSONToolCaptureMayContinue(captured)) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }

  // No XML tool tags detected — release captured content as text.
  return {
    ready: true,
    prefix: captured,
    calls: [],
    suffix: '',
  };
}

module.exports = {
  processToolSieveChunk,
  flushToolSieve,
};
