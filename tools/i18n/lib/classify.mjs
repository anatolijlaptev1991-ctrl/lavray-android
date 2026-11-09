// SPDX-License-Identifier: GPL-3.0-only
import { PLURAL_CATEGORIES } from './languages.mjs';

/**
 * Desktop keys that merely look like `<base>.<category>` but are plain strings, e.g. "imp.prev.one"
 * ("Profile {proto}") or "cf.warn.other". They are emitted as plain strings (resource imp_prev_one).
 * Any other group without `.other` is an error; a new look-alike key must be added here.
 */
export const NOT_PLURAL_KEYS = Object.freeze(['cf.warn.other', 'ed.group.other', 'imp.prev.one', 'sub.err.zero']);

const PLURAL_KEY_RE = new RegExp(`^(.+)\\.(${PLURAL_CATEGORIES.join('|')})$`);

/**
 * Splits a dictionary into plain keys and plural groups (`<base>.<cat>`).
 * Returns { plain: Map<key,text>, plurals: Map<base, Map<cat,text>>, problems: string[] }.
 */
export function classifyDictionary(map, lang) {
  const plain = new Map();
  const plurals = new Map();
  for (const [key, text] of map) {
    const match = NOT_PLURAL_KEYS.includes(key) ? null : PLURAL_KEY_RE.exec(key);
    if (!match) {
      plain.set(key, text);
      continue;
    }
    const [, base, category] = match;
    if (!plurals.has(base)) plurals.set(base, new Map());
    plurals.get(base).set(category, text);
  }
  return { plain, plurals, problems: groupProblems(plain, plurals, lang) };
}

function groupProblems(plain, plurals, lang) {
  const problems = [];
  for (const [base, items] of plurals) {
    if (!items.has('other')) {
      problems.push(
        `[${lang}] plural group "${base}" has categories (${[...items.keys()].join(', ')}) but no "${base}.other" entry. ` +
          `Add "${base}.other" (or rename the key if it is not a plural).`,
      );
    }
    if (plain.has(base)) {
      problems.push(`[${lang}] "${base}" exists both as a plain key and as a plural group ("${base}.<category>"). Keep only one.`);
    }
  }
  return problems;
}

/** Plural items of a group sorted in the canonical category order. */
export function sortedCategories(items) {
  return PLURAL_CATEGORIES.filter((category) => items.has(category));
}
