'use strict';

const {
  toStringSafe,
} = require('./state');
const {
  isLikelyStandaloneJSONToolStart,
  nextVisibleJSONCandidateIndex,
  jsonLikeStandaloneToolJSONEnd,
  jsonLikeValueEnd,
} = require('./visible-json-scan');

function parseVisibleJSONToolCalls(text) {
  const raw = toStringSafe(text).trim();
  if (raw.startsWith('{')) {
    return parseVisibleJSONToolObjectSequence(raw);
  }
  if (!raw.startsWith('[')) {
    return [];
  }
  let items;
  try {
    items = JSON.parse(raw);
  } catch (_err) {
    return [];
  }
  if (!Array.isArray(items) || items.length === 0) {
    return [];
  }
  const out = [];
  for (const item of items) {
    const call = parseVisibleJSONToolCallObject(item);
    if (!call) {
      return [];
    }
    out.push(call);
  }
  return out;
}

function parseVisibleJSONToolObjectSequence(raw) {
  const out = [];
  let pos = 0;
  while (pos < raw.length) {
    while (pos < raw.length && /\s/.test(raw[pos])) {
      pos += 1;
    }
    if (pos >= raw.length) {
      break;
    }
    if (raw[pos] !== '{') {
      return [];
    }
    const end = jsonLikeValueEnd(raw.slice(pos));
    if (end < 0) {
      return [];
    }
    let obj;
    try {
      obj = JSON.parse(raw.slice(pos, pos + end));
    } catch (_err) {
      return [];
    }
    const call = parseVisibleJSONToolCallObject(obj);
    if (!call) {
      return [];
    }
    out.push(call);
    pos += end;
  }
  return out;
}

function parseVisibleJSONToolCallObject(obj) {
  if (!obj || typeof obj !== 'object' || Array.isArray(obj)) {
    return null;
  }
  let name = toStringSafe(obj.tool || obj.name || obj.tool_name).trim();
  let input = {};
  if (obj.function && typeof obj.function === 'object' && !Array.isArray(obj.function)) {
    if (!name) {
      name = toStringSafe(obj.function.name).trim();
    }
    input = firstVisibleJSONInput(obj.function);
  }
  if (Object.keys(input).length === 0) {
    input = firstVisibleJSONInput(obj);
  }
  if (!name) {
    return null;
  }
  return { name, input };
}

function firstVisibleJSONInput(obj) {
  for (const key of ['arguments', 'input', 'params', 'parameters']) {
    if (Object.prototype.hasOwnProperty.call(obj, key)) {
      return parseVisibleJSONInput(obj[key]);
    }
  }
  return {};
}

function parseVisibleJSONInput(value) {
  if (!value) {
    return {};
  }
  if (typeof value === 'object' && !Array.isArray(value)) {
    return value;
  }
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed;
      }
    } catch (_err) {
      return { _raw: value.trim() };
    }
  }
  return {};
}

function looksLikeVisibleJSONToolCallSyntax(text) {
  const trimmed = toStringSafe(text).trim();
  if (!trimmed.startsWith('[') && !trimmed.startsWith('{')) {
    return false;
  }
  return hasVisibleJSONToolHints(trimmed.toLowerCase()) &&
    parseVisibleJSONToolCalls(trimmed).length > 0;
}

function findVisibleJSONToolSegmentStart(state, text, insideCodeFenceWithState) {
  let offset = 0;
  while (offset < text.length) {
    const idx = nextVisibleJSONCandidateIndex(text, offset);
    if (idx < 0) {
      return -1;
    }
    const candidate = text.slice(idx);
    if (isLikelyStandaloneJSONToolStart(text, idx) &&
        hasVisibleJSONToolHints(candidate.toLowerCase()) &&
        !insideCodeFenceWithState(state, text.slice(0, idx))) {
      return idx;
    }
    offset = idx + 1;
  }
  return -1;
}

function findPartialVisibleJSONToolSegmentStart(state, text, insideCodeFenceWithState) {
  let offset = 0;
  while (offset < text.length) {
    const idx = nextVisibleJSONCandidateIndex(text, offset);
    if (idx < 0) {
      return -1;
    }
    if (isLikelyStandaloneJSONToolStart(text, idx) &&
        jsonLikeStandaloneToolJSONEnd(text.slice(idx)) < 0 &&
        !insideCodeFenceWithState(state, text.slice(0, idx))) {
      return idx;
    }
    offset = idx + 1;
  }
  return -1;
}

function consumeVisibleJSONToolCapture(captured, toolNames) {
  const raw = toStringSafe(captured);
  const trimmedStart = raw.replace(/^[ \t\r\n]+/g, '');
  const leadingLen = raw.length - trimmedStart.length;
  const end = jsonLikeStandaloneToolJSONEnd(trimmedStart);
  if (end < 0) {
    return { ready: false, prefix: '', calls: [], suffix: '' };
  }
  const block = trimmedStart.slice(0, end);
  const suffix = trimmedStart.slice(end);
  const calls = parseVisibleJSONToolCalls(block, toolNames);
  if (calls.length === 0) {
    return { ready: true, prefix: raw.slice(0, leadingLen + end), calls: [], suffix };
  }
  return { ready: true, prefix: '', calls, suffix };
}

function visibleJSONToolCaptureMayContinue(captured) {
  const trimmed = toStringSafe(captured).trim();
  if (!trimmed.startsWith('[') && !trimmed.startsWith('{')) {
    return false;
  }
  if (jsonLikeStandaloneToolJSONEnd(trimmed) >= 0) {
    return false;
  }
  return trimmed.startsWith('{') || hasVisibleJSONToolHints(trimmed.toLowerCase());
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

module.exports = {
  parseVisibleJSONToolCalls,
  looksLikeVisibleJSONToolCallSyntax,
  findVisibleJSONToolSegmentStart,
  findPartialVisibleJSONToolSegmentStart,
  consumeVisibleJSONToolCapture,
  visibleJSONToolCaptureMayContinue,
};
