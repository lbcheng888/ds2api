'use strict';

function isLikelyStandaloneJSONArrayStart(text, idx) {
  if (idx < 0 || idx >= text.length || text[idx] !== '[') {
    return false;
  }
  const lineStart = text.lastIndexOf('\n', idx - 1) + 1;
  if (text.slice(lineStart, idx).trim() !== '') {
    return false;
  }
  const after = text.slice(idx + 1).replace(/^[ \t\r\n]+/g, '');
  return after === '' || after.startsWith('{');
}

function isLikelyStandaloneJSONObjectStart(text, idx) {
  if (idx < 0 || idx >= text.length || text[idx] !== '{') {
    return false;
  }
  const lineStart = text.lastIndexOf('\n', idx - 1) + 1;
  if (text.slice(lineStart, idx).trim() !== '') {
    return false;
  }
  const after = text.slice(idx + 1).replace(/^[ \t\r\n]+/g, '');
  return after === '' || after.startsWith('"') || after.startsWith('}');
}

function isLikelyStandaloneJSONToolStart(text, idx) {
  if (idx < 0 || idx >= text.length) {
    return false;
  }
  if (text[idx] === '[') {
    return isLikelyStandaloneJSONArrayStart(text, idx);
  }
  return isLikelyStandaloneJSONObjectStart(text, idx);
}

function nextVisibleJSONCandidateIndex(text, offset) {
  const start = Math.max(0, offset || 0);
  const arrayIdx = text.indexOf('[', start);
  const objectIdx = text.indexOf('{', start);
  if (arrayIdx < 0 && objectIdx < 0) {
    return -1;
  }
  if (arrayIdx < 0) {
    return objectIdx;
  }
  if (objectIdx < 0) {
    return arrayIdx;
  }
  return Math.min(arrayIdx, objectIdx);
}

function jsonLikeStandaloneToolJSONEnd(text) {
  if (text.startsWith('[')) {
    return jsonLikeValueEnd(text);
  }
  if (text.startsWith('{')) {
    return jsonLikeObjectSequenceEnd(text);
  }
  return -1;
}

function jsonLikeObjectSequenceEnd(text) {
  let pos = 0;
  let end = -1;
  let count = 0;
  while (true) {
    while (pos < text.length && /\s/.test(text[pos])) {
      pos += 1;
    }
    if (pos >= text.length) {
      return count > 0 ? end : -1;
    }
    if (text[pos] !== '{') {
      return count > 0 ? end : -1;
    }
    const objectEnd = jsonLikeValueEnd(text.slice(pos));
    if (objectEnd < 0) {
      return -1;
    }
    pos += objectEnd;
    end = pos;
    count += 1;
  }
}

function jsonLikeValueEnd(text) {
  if (!text || (text[0] !== '[' && text[0] !== '{')) {
    return -1;
  }
  const stack = [];
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (inString) {
      if (escaped) {
        escaped = false;
        continue;
      }
      if (ch === '\\') {
        escaped = true;
        continue;
      }
      if (ch === '"') {
        inString = false;
      }
      continue;
    }
    if (ch === '"') {
      inString = true;
      continue;
    }
    if (ch === '[') {
      stack.push(']');
      depth += 1;
      continue;
    }
    if (ch === '{') {
      stack.push('}');
      depth += 1;
      continue;
    }
    if (ch === ']' || ch === '}') {
      if (depth === 0 || stack.length === 0 || stack[stack.length - 1] !== ch) {
        return -1;
      }
      stack.pop();
      depth -= 1;
      if (depth === 0) {
        return i + 1;
      }
    }
  }
  return -1;
}

module.exports = {
  isLikelyStandaloneJSONToolStart,
  nextVisibleJSONCandidateIndex,
  jsonLikeStandaloneToolJSONEnd,
  jsonLikeValueEnd,
};
