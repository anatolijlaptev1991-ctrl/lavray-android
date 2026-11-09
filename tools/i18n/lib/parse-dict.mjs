// SPDX-License-Identifier: GPL-3.0-only
import vm from 'node:vm';
import { I18nError } from './errors.mjs';

// `export const ruDict = {`  or  `export const enDict: Record<string, string> = {`
const DECLARATION_RE = /export\s+const\s+[A-Za-z_$][\w$]*\s*(?::[^=]*)?=\s*/;
const EVAL_TIMEOUT_MS = 2000;

/** Cuts the object literal `{ ... }` out of the TS module source (drops prefix, `;`, `as const`). */
export function extractObjectLiteral(source, label) {
  const text = source.replace(/^﻿/, '');
  const match = DECLARATION_RE.exec(text);
  if (!match) {
    throw new I18nError(
      `${label}: no "export const xDict = { ... }" declaration found. ` +
        'Check that the file is a desktop dictionary module (is the third_party/lavray submodule up to date?).',
    );
  }
  const body = text
    .slice(match.index + match[0].length)
    .trim()
    .replace(/;\s*$/, '')
    .replace(/\s+as\s+const\s*$/, '')
    .replace(/;\s*$/, '')
    .trim();
  if (!body.startsWith('{') || !body.endsWith('}')) {
    throw new I18nError(
      `${label}: the dictionary value is not a plain object literal ({ ... }, optionally followed by "as const;"). ` +
        'The generator only understands that shape; adapt tools/i18n/lib/parse-dict.mjs if the format changed.',
    );
  }
  return body;
}

/** Evaluates the literal in an empty vm context (no require, no process). */
export function evaluateObjectLiteral(body, label) {
  const context = vm.createContext(Object.create(null));
  try {
    return vm.runInContext(`(${body}\n)`, context, { timeout: EVAL_TIMEOUT_MS, filename: label });
  } catch (error) {
    throw new I18nError(
      `${label}: cannot evaluate the dictionary object literal (${error.message}). ` +
        'Only plain data (string keys and string values) is supported.',
    );
  }
}

function toStringMap(value, label) {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new I18nError(`${label}: the dictionary did not evaluate to an object. Expected { "key": "text", ... }.`);
  }
  const map = new Map();
  for (const [key, text] of Object.entries(value)) {
    if (typeof text !== 'string') {
      throw new I18nError(`${label}: value of key "${key}" is ${typeof text}, expected a string. Fix the dictionary entry.`);
    }
    map.set(key, text);
  }
  return map;
}

/** Parses a desktop dictionary (TS source text) into Map<key, text>. */
export function parseDictionary(source, label = 'dictionary') {
  const body = extractObjectLiteral(source, label);
  return toStringMap(evaluateObjectLiteral(body, label), label);
}
