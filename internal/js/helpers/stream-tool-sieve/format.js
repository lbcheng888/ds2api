'use strict';

const crypto = require('crypto');

function formatOpenAIStreamToolCalls(calls, idStore, toolsRaw) {
  if (!Array.isArray(calls) || calls.length === 0) {
    return [];
  }
  const normalized = normalizeParsedToolCallsForSchemas(calls, toolsRaw);
  return normalized.map((c, idx) => ({
    index: idx,
    id: ensureStreamToolCallID(idStore, idx),
    type: 'function',
    function: {
      name: c.name,
      arguments: JSON.stringify(c.input || {}),
    },
  }));
}

function normalizeParsedToolCallsForSchemas(calls, toolsRaw) {
  if (!Array.isArray(calls) || calls.length === 0) {
    return calls;
  }
  const schemas = buildToolSchemaIndex(toolsRaw);
  if (!schemas) {
    return calls;
  }
  let changedAny = false;
  const out = calls.map((call) => {
    const name = String(call && call.name || '').trim().toLowerCase();
    const schema = schemas[name];
    if (!schema || !call || !call.input || typeof call.input !== 'object' || Array.isArray(call.input)) {
      return call;
    }
    const [normalized, changed] = normalizeToolValueWithSchema(call.input, schema);
    if (!changed || !normalized || typeof normalized !== 'object' || Array.isArray(normalized)) {
      return call;
    }
    changedAny = true;
    return { ...call, input: normalized };
  });
  return changedAny ? out : calls;
}

function buildToolSchemaIndex(toolsRaw) {
  if (!Array.isArray(toolsRaw) || toolsRaw.length === 0) {
    return null;
  }
  const out = {};
  for (const item of toolsRaw) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      continue;
    }
    const [name, schema] = extractToolNameAndSchema(item);
    if (!name || !schema || typeof schema !== 'object' || Array.isArray(schema)) {
      continue;
    }
    out[name.toLowerCase()] = schema;
  }
  return Object.keys(out).length > 0 ? out : null;
}

function extractToolNameAndSchema(tool) {
  const fn = tool && typeof tool.function === 'object' && !Array.isArray(tool.function) ? tool.function : null;
  const name = firstNonEmptyString(tool.name, fn && fn.name);
  const schema = firstNonNil(
    tool.parameters,
    tool.input_schema,
    tool.inputSchema,
    tool.schema,
    fn && fn.parameters,
    fn && fn.input_schema,
    fn && fn.inputSchema,
    fn && fn.schema,
  );
  return [name, schema];
}

function normalizeToolValueWithSchema(value, schema, fieldName = '') {
  if (value == null || !schema || typeof schema !== 'object' || Array.isArray(schema)) {
    return [value, false];
  }
  if (shouldCoerceSchemaToString(schema)) {
    return stringifySchemaValue(value);
  }
  if (looksLikeObjectSchema(schema)) {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
      return [value, false];
    }
    const properties = schema.properties && typeof schema.properties === 'object' && !Array.isArray(schema.properties) ? schema.properties : null;
    const additional = schema.additionalProperties;
    let changed = false;
    const out = {};
    for (const [key, current] of Object.entries(value)) {
      let next = current;
      let fieldChanged = false;
      if (properties && Object.prototype.hasOwnProperty.call(properties, key)) {
        [next, fieldChanged] = normalizeToolValueWithSchema(current, properties[key], key);
      } else if (additional != null) {
        [next, fieldChanged] = normalizeToolValueWithSchema(current, additional, key);
      }
      out[key] = next;
      changed = changed || fieldChanged;
    }
    return changed ? [out, true] : [value, false];
  }
  if (looksLikeArraySchema(schema)) {
    const [items, converted] = normalizeSchemaArrayCandidate(value, schema, fieldName);
    if (!items || items.length === 0 || schema.items == null) {
      return [value, false];
    }
    let changed = converted;
    const out = items.map((item, idx) => {
      const itemSchema = Array.isArray(schema.items) ? schema.items[idx] : schema.items;
      if (itemSchema == null) {
        return item;
      }
      const [next, itemChanged] = normalizeToolValueWithSchema(item, itemSchema, fieldName);
      changed = changed || itemChanged;
      return next;
    });
    return changed ? [out, true] : [value, false];
  }
  return [value, false];
}

function normalizeSchemaArrayCandidate(value, schema, fieldName) {
  if (Array.isArray(value)) {
    return [value, false];
  }
  if (value && typeof value === 'object') {
    const coerced = coerceArrayValue(value, fieldName);
    if (coerced) {
      return [coerced, true];
    }
    if (canWrapSingleArrayItem(value, schema.items)) {
      return [[value], true];
    }
    return [null, false];
  }
  if (typeof value === 'string') {
    const parsed = parseLooseArrayValue(value, fieldName);
    if (parsed) {
      return [parsed, true];
    }
    const single = parseLooseElementValue(value);
    if (single != null && canWrapSingleArrayItem(single, schema.items)) {
      return [[single], true];
    }
  }
  return [null, false];
}

function coerceArrayValue(value, fieldName) {
  if (Array.isArray(value)) {
    return value;
  }
  if (!value || typeof value !== 'object') {
    return null;
  }
  if (Object.prototype.hasOwnProperty.call(value, 'item')) {
    const item = value.item;
    const nested = coerceArrayValue(item, '');
    return nested || [item];
  }
  if (fieldName && Object.prototype.hasOwnProperty.call(value, fieldName)) {
    return coerceArrayValue(value[fieldName], '');
  }
  return null;
}

function parseLooseArrayValue(raw, fieldName) {
  const parsed = parseLooseElementValue(raw);
  const coerced = coerceArrayValue(parsed, fieldName);
  if (coerced) {
    return coerced;
  }
  const segments = splitTopLevelJSONValues(String(raw).trim());
  if (!segments || segments.length < 2) {
    return null;
  }
  const out = [];
  for (const segment of segments) {
    const item = parseLooseElementValue(segment);
    if (item == null) {
      return null;
    }
    out.push(item);
  }
  return out;
}

function parseLooseElementValue(raw) {
  const trimmed = String(raw || '').trim();
  if (!trimmed) {
    return null;
  }
  try {
    return JSON.parse(trimmed);
  } catch {}
  const repaired = repairLooseJSONObject(trimmed);
  if (repaired !== trimmed) {
    try {
      return JSON.parse(repaired);
    } catch {}
  }
  return null;
}

function repairLooseJSONObject(text) {
  const trimmed = String(text || '').trim();
  if (!trimmed || (trimmed[0] !== '{' && trimmed[0] !== '[')) {
    return trimmed;
  }
  return trimmed
    .replace(/([{,]\s*)([A-Za-z_][A-Za-z0-9_-]*)(\s*:)/g, '$1"$2"$3')
    .replace(/,\s*([}\]])/g, '$1');
}

function splitTopLevelJSONValues(raw) {
  const text = String(raw || '').trim();
  if (!text) {
    return null;
  }
  const values = [];
  let start = 0;
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let i = 0; i < text.length; i += 1) {
    const ch = text[i];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (ch === '\\') {
        escaped = true;
      } else if (ch === '"') {
        inString = false;
      }
      continue;
    }
    if (ch === '"') {
      inString = true;
    } else if (ch === '{' || ch === '[') {
      depth += 1;
    } else if (ch === '}' || ch === ']') {
      depth = Math.max(0, depth - 1);
    } else if (ch === ',' && depth === 0) {
      const segment = text.slice(start, i).trim();
      if (!segment) {
        return null;
      }
      values.push(segment);
      start = i + 1;
    }
  }
  const last = text.slice(start).trim();
  if (!last) {
    return null;
  }
  values.push(last);
  return values.length >= 2 ? values : null;
}

function canWrapSingleArrayItem(value, itemSchema) {
  if (!itemSchema || typeof itemSchema !== 'object' || Array.isArray(itemSchema)) {
    return false;
  }
  if (looksLikeObjectSchema(itemSchema)) {
    return !!value && typeof value === 'object' && !Array.isArray(value);
  }
  if (shouldCoerceSchemaToString(itemSchema)) {
    return typeof value === 'string';
  }
  if (isBooleanSchema(itemSchema)) {
    return typeof value === 'boolean';
  }
  if (isNumberSchema(itemSchema)) {
    return typeof value === 'number';
  }
  return false;
}

function shouldCoerceSchemaToString(schema) {
  if (!schema || typeof schema !== 'object' || Array.isArray(schema)) {
    return false;
  }
  if (typeof schema.const === 'string') {
    return true;
  }
  if (Array.isArray(schema.enum) && schema.enum.length > 0 && schema.enum.every((item) => typeof item === 'string')) {
    return true;
  }
  if (typeof schema.type === 'string') {
    return schema.type.trim().toLowerCase() === 'string';
  }
  if (Array.isArray(schema.type) && schema.type.length > 0) {
    let hasString = false;
    for (const item of schema.type) {
      if (typeof item !== 'string') {
        return false;
      }
      const typ = item.trim().toLowerCase();
      if (typ === 'string') {
        hasString = true;
      } else if (typ !== 'null') {
        return false;
      }
    }
    return hasString;
  }
  return false;
}

function looksLikeObjectSchema(schema) {
  return !!schema && typeof schema === 'object' && !Array.isArray(schema) && (
    (typeof schema.type === 'string' && schema.type.trim().toLowerCase() === 'object') ||
    (schema.properties && typeof schema.properties === 'object' && !Array.isArray(schema.properties)) ||
    schema.additionalProperties != null
  );
}

function looksLikeArraySchema(schema) {
  return !!schema && typeof schema === 'object' && !Array.isArray(schema) && (
    (typeof schema.type === 'string' && schema.type.trim().toLowerCase() === 'array') ||
    schema.items != null
  );
}

function isBooleanSchema(schema) {
  return !!schema && typeof schema === 'object' && !Array.isArray(schema) &&
    typeof schema.type === 'string' && schema.type.trim().toLowerCase() === 'boolean';
}

function isNumberSchema(schema) {
  return !!schema && typeof schema === 'object' && !Array.isArray(schema) &&
    typeof schema.type === 'string' && ['number', 'integer'].includes(schema.type.trim().toLowerCase());
}

function stringifySchemaValue(value) {
  if (value == null) {
    return [value, false];
  }
  if (typeof value === 'string') {
    return [value, false];
  }
  try {
    return [JSON.stringify(value), true];
  } catch {
    return [value, false];
  }
}

function firstNonNil(...values) {
  for (const value of values) {
    if (value != null) {
      return value;
    }
  }
  return null;
}

function firstNonEmptyString(...values) {
  for (const value of values) {
    if (typeof value !== 'string') {
      continue;
    }
    const trimmed = value.trim();
    if (trimmed) {
      return trimmed;
    }
  }
  return '';
}

function ensureStreamToolCallID(idStore, index) {
  if (!(idStore instanceof Map)) {
    return `call_${newCallID()}`;
  }
  const key = Number.isInteger(index) ? index : 0;
  const existing = idStore.get(key);
  if (existing) {
    return existing;
  }
  const next = `call_${newCallID()}`;
  idStore.set(key, next);
  return next;
}

function newCallID() {
  if (typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID().replace(/-/g, '');
  }
  return `${Date.now()}${Math.floor(Math.random() * 1e9)}`;
}

module.exports = {
  formatOpenAIStreamToolCalls,
};
