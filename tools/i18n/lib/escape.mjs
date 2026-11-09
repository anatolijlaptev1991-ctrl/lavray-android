// SPDX-License-Identifier: GPL-3.0-only
import { splitPlaceholders } from './placeholders.mjs';

const SIMPLE_ESCAPES = new Map([
  ['&', '&amp;'],
  ['<', '&lt;'],
  ['>', '&gt;'],
  ["'", "\\'"],
  ['"', '\\"'],
  ['\\', '\\\\'],
  ['\n', '\\n'],
  ['\t', '\\t'],
]);

// Characters that are not allowed in XML 1.0 at all (tab, LF and CR are allowed).
const INVALID_XML_CHAR_RE = /[\u0000-\u0008\u000B\u000C\u000E-\u001F￾￿]/u;

export function hasInvalidXmlChars(text) {
  return INVALID_XML_CHAR_RE.test(text) || !text.isWellFormed();
}

function escapeSpace({ first, last, prevSpace }) {
  // aapt trims the ends and collapses runs of spaces, so those spaces are written as  .
  return first || last || prevSpace ? '\\u0020' : ' ';
}

function escapeChar(char, ctx) {
  if (char === ' ') return escapeSpace(ctx);
  if (char === '%') return ctx.formatMode ? '%%' : '%';
  if ((char === '@' || char === '?') && ctx.first) return `\\${char}`;
  return SIMPLE_ESCAPES.get(char) ?? char;
}

/** Escapes one literal (placeholder-free) piece of text. atStart/atEnd: piece begins/ends the whole string. */
export function escapeLiteral(piece, { atStart = true, atEnd = true, formatMode = false } = {}) {
  const chars = [...piece];
  let out = '';
  let prevSpace = false;
  chars.forEach((char, index) => {
    const ctx = {
      first: atStart && index === 0,
      last: atEnd && index === chars.length - 1,
      prevSpace,
      formatMode,
    };
    out += escapeChar(char, ctx);
    prevSpace = char === ' ';
  });
  return out;
}

function placeholderToken(name, numbering, text) {
  const position = numbering.get(name);
  if (position === undefined) {
    throw new Error(`internal: placeholder {${name}} has no number (text: "${text}")`);
  }
  return `%${position}$s`;
}

/**
 * Converts a dictionary text to the body of an Android string resource:
 * `{name}` -> `%N$s` (numbering: Map name -> N), Android escaping elsewhere.
 * formatMode (the key has placeholders) doubles literal `%`.
 */
export function toAndroidText(rawText, { numbering = new Map(), formatMode = numbering.size > 0 } = {}) {
  const text = rawText.replace(/\r\n?/g, '\n');
  const parts = splitPlaceholders(text);
  const lastIndex = parts.length - 1;
  return parts
    .map((part, index) => {
      if (index % 2 === 1) return placeholderToken(part, numbering, text);
      return escapeLiteral(part, { atStart: index === 0, atEnd: index === lastIndex, formatMode });
    })
    .join('');
}
