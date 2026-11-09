// SPDX-License-Identifier: GPL-3.0-only

// `{name}` only; `{}` and `{ ... }` are literal text.
export const PLACEHOLDER_SOURCE = '\\{([A-Za-z_][A-Za-z0-9_]*)\\}';

/** Sorted (code-unit order), de-duplicated placeholder names of a text. */
export function extractPlaceholders(text) {
  const names = new Set();
  for (const match of text.matchAll(new RegExp(PLACEHOLDER_SOURCE, 'g'))) names.add(match[1]);
  return [...names].sort();
}

/** Sorted union of the placeholder names of several texts. */
export function unionPlaceholders(texts) {
  const names = new Set();
  for (const text of texts) for (const name of extractPlaceholders(text)) names.add(name);
  return [...names].sort();
}

/** Map name -> 1-based position in alphabetical order. */
export function numberingFor(names) {
  const ordered = [...new Set(names)].sort();
  return new Map(ordered.map((name, index) => [name, index + 1]));
}

/** Splits a text into [literal, name, literal, name, ..., literal]: even indexes literal, odd placeholder names. */
export function splitPlaceholders(text) {
  return text.split(new RegExp(PLACEHOLDER_SOURCE));
}
