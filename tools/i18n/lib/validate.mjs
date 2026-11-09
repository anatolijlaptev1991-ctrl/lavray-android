// SPDX-License-Identifier: GPL-3.0-only
import { describeEntry } from './catalog.mjs';
import { hasInvalidXmlChars } from './escape.mjs';
import { DEFAULT_LANG, LANGS } from './languages.mjs';

export const HAPP_RE = /\bhapp\b/i;
export const SING_BOX_RE = /sing[-\s ]?box/i;
/** Desktop keys that may mention sing-box (the licenses screen). */
export const SING_BOX_ALLOWED_KEYS = Object.freeze(['lic.core']);

/** Flattens an entry into [{ lang, category, label, text }] (label: lang, or lang/category for plurals). */
export function entryTexts(entry) {
  if (entry.kind === 'string') {
    return LANGS.map((lang) => ({ lang, category: null, label: lang, text: entry.values[lang] }));
  }
  return LANGS.flatMap((lang) =>
    Object.entries(entry.items[lang]).map(([category, text]) => ({ lang, category, label: `${lang}/${category}`, text })),
  );
}

function excerpt(text) {
  const flat = text.replace(/\s+/g, ' ');
  return flat.length > 60 ? `${flat.slice(0, 57)}...` : flat;
}

function scanWords(entries, regex, word, skip = () => false) {
  const problems = [];
  for (const entry of entries) {
    if (skip(entry)) continue;
    for (const item of entryTexts(entry)) {
      if (regex.test(item.text)) {
        problems.push(`forbidden word "${word}" in ${describeEntry(entry)} [${item.label}]: "${excerpt(item.text)}". Reword the string.`);
      }
    }
  }
  return problems;
}

/** The word Happ must never be user visible. */
export function checkHapp(entries) {
  return scanWords(entries, HAPP_RE, 'Happ');
}

/** sing-box (also singbox / sing box) is forbidden except for allow-listed desktop keys. */
export function checkSingBox(entries, allowKeys = SING_BOX_ALLOWED_KEYS) {
  const allowed = new Set(allowKeys);
  return scanWords(entries, SING_BOX_RE, 'sing-box', (e) => e.source === 'desktop' && allowed.has(e.key));
}

/** Number of entries (not strings) that mention sing-box: used for the warning on non-emitted desktop keys. */
export function countSingBoxMentions(entries) {
  return entries.filter((entry) => entryTexts(entry).some((item) => SING_BOX_RE.test(item.text))).length;
}

function referenceText(entry, item) {
  if (entry.kind === 'string') return entry.values[DEFAULT_LANG];
  const items = entry.items[DEFAULT_LANG];
  return items[item.category] ?? items.other;
}

/** Empty strings are allowed only where the ru source is empty as well. */
export function checkEmpty(entries) {
  const problems = [];
  for (const entry of entries) {
    for (const item of entryTexts(entry)) {
      if (item.lang === DEFAULT_LANG || item.text !== '') continue;
      if (referenceText(entry, item) === '') continue;
      problems.push(`empty string in ${describeEntry(entry)} [${item.label}] while ${DEFAULT_LANG} is not empty. Translate it.`);
    }
  }
  return problems;
}

/** Characters that cannot be written to XML. */
export function checkXmlChars(entries) {
  const problems = [];
  for (const entry of entries) {
    for (const item of entryTexts(entry)) {
      if (hasInvalidXmlChars(item.text)) {
        problems.push(`${describeEntry(entry)} [${item.label}] contains a control character or lone surrogate that XML cannot hold. Remove it.`);
      }
    }
  }
  return problems;
}

/** Desktop entries that mention sing-box although they are not allow-listed. */
export function singBoxDesktopOffenders(entries) {
  const allowed = new Set(SING_BOX_ALLOWED_KEYS);
  return entries.filter((e) => e.source === 'desktop' && !allowed.has(e.key) && countSingBoxMentions([e]) > 0);
}

/**
 * All content checks for the emitted strings plus every Android-only string (even unused ones).
 * lenientDesktopSingBox (the --all mode): desktop sing-box mentions are not fatal, the caller reports them as a warning.
 */
export function checkContent(emitted, androidAll, { lenientDesktopSingBox = false } = {}) {
  const checked = [...emitted, ...androidAll.filter((e) => !emitted.includes(e))];
  const singBoxChecked = lenientDesktopSingBox ? checked.filter((e) => e.source === 'android') : checked;
  return [
    ...checkHapp(checked),
    ...checkSingBox(singBoxChecked),
    ...checkEmpty(emitted),
    ...checkXmlChars(emitted),
  ];
}
