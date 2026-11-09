// SPDX-License-Identifier: GPL-3.0-only
import { classifyDictionary } from './classify.mjs';
import { listNames } from './errors.mjs';
import { DEFAULT_LANG, LANGS } from './languages.mjs';
import { isValidResourceName, toResourceName } from './names.mjs';
import { extractPlaceholders, unionPlaceholders } from './placeholders.mjs';

/**
 * Catalog entry shapes:
 *   { kind: 'string',  source, key, name, values: { lang: text },            placeholders: [names] }
 *   { kind: 'plurals', source, key, name, items:  { lang: { cat: text } },   placeholders: [names] }
 * source is 'desktop' or 'android'; key is the original dictionary key (android: equals name).
 */

export function describeEntry(entry) {
  if (entry.source === 'android') return `Android-only string "${entry.name}"`;
  return `desktop ${entry.kind === 'plurals' ? 'plural' : 'key'} "${entry.key}"`;
}

function mapLangs(byLang, fn) {
  return Object.fromEntries(LANGS.map((lang) => [lang, fn(byLang[lang], lang)]));
}

function keySetProblems(kind, setsByLang) {
  const reference = setsByLang[DEFAULT_LANG];
  const problems = [];
  for (const lang of LANGS) {
    if (lang === DEFAULT_LANG) continue;
    const missing = [...reference].filter((key) => !setsByLang[lang].has(key)).sort();
    const extra = [...setsByLang[lang]].filter((key) => !reference.has(key)).sort();
    if (missing.length) problems.push(`${kind}: ${lang} lacks ${missing.length} key(s) that ${DEFAULT_LANG} has: ${listNames(missing)}`);
    if (extra.length) problems.push(`${kind}: ${lang} has ${extra.length} key(s) that ${DEFAULT_LANG} lacks: ${listNames(extra)}`);
  }
  return problems;
}

function commonKeys(setsByLang) {
  return [...setsByLang[DEFAULT_LANG]].filter((key) => LANGS.every((lang) => setsByLang[lang].has(key)));
}

function pick(byLang, key) {
  return Object.fromEntries(LANGS.map((lang) => [lang, byLang[lang].get(key)]));
}

function keysOf(maps) {
  return mapLangs(maps, (map) => new Set(map.keys()));
}

function desktopEntries(classified) {
  const plainKeys = commonKeys(keysOf(mapLangs(classified, (c) => c.plain)));
  const pluralKeys = commonKeys(keysOf(mapLangs(classified, (c) => c.plurals)));
  const plainByLang = mapLangs(classified, (c) => c.plain);
  const pluralsByLang = mapLangs(classified, (c) => c.plurals);
  const strings = plainKeys.map((key) => ({
    kind: 'string', source: 'desktop', key, name: toResourceName(key), values: pick(plainByLang, key),
  }));
  const plurals = pluralKeys.map((base) => ({
    kind: 'plurals',
    source: 'desktop',
    key: base,
    name: toResourceName(base),
    items: mapLangs(pick(pluralsByLang, base), (items) => Object.fromEntries(items)),
  }));
  return [...strings, ...plurals];
}

function androidEntries(android) {
  if (!android) return [];
  return commonKeys(keysOf(android)).map((name) => ({
    kind: 'string', source: 'android', key: name, name, values: pick(android, name),
  }));
}

function nameProblems(entries) {
  const problems = [];
  const seen = { string: new Map(), plurals: new Map() };
  for (const entry of entries) {
    if (!isValidResourceName(entry.name)) {
      problems.push(
        `${describeEntry(entry)} maps to resource name "${entry.name}", which is not a valid Android name ` +
          '(must match ^[A-Za-z][A-Za-z0-9_]*$). Rename the key.',
      );
      continue;
    }
    const first = seen[entry.kind].get(entry.name);
    if (first) {
      problems.push(`name collision: ${describeEntry(first)} and ${describeEntry(entry)} both become resource "${entry.name}". Rename one of them.`);
    } else {
      seen[entry.kind].set(entry.name, entry);
    }
  }
  return problems;
}

function placeholdersOf(entry, lang) {
  if (entry.kind === 'string') return extractPlaceholders(entry.values[lang]);
  return unionPlaceholders(Object.values(entry.items[lang]));
}

function formatSet(names) {
  return `{${names.join(', ')}}`;
}

/** Fills entry.placeholders (taken from the reference language) and reports per-language mismatches. */
function placeholderProblems(entries) {
  const problems = [];
  for (const entry of entries) {
    const reference = placeholdersOf(entry, DEFAULT_LANG);
    entry.placeholders = reference;
    for (const lang of LANGS) {
      const own = placeholdersOf(entry, lang);
      if (own.join('\0') === reference.join('\0')) continue;
      const missing = reference.filter((n) => !own.includes(n));
      const extra = own.filter((n) => !reference.includes(n));
      const diff = [missing.length ? `missing ${formatSet(missing)}` : '', extra.length ? `extra ${formatSet(extra)}` : ''].filter(Boolean).join(', ');
      problems.push(`${describeEntry(entry)}: placeholders in ${lang} ${formatSet(own)} differ from ${DEFAULT_LANG} ${formatSet(reference)} (${diff}). Make the placeholder sets equal.`);
    }
  }
  return problems;
}

const byNameThenKind = (a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : a.kind < b.kind ? 1 : a.kind > b.kind ? -1 : 0);

/**
 * Merges desktop dictionaries (Map per language) and Android-only maps (or null) into sorted entries.
 * Returns { entries, problems, parsed } where parsed = per-language counts of raw keys / plain keys / plural groups.
 */
export function buildCatalog({ desktop, android = null }) {
  const classified = mapLangs(desktop, (map, lang) => classifyDictionary(map, lang));
  const problems = LANGS.flatMap((lang) => classified[lang].problems);
  problems.push(...keySetProblems('desktop dictionaries', keysOf(mapLangs(classified, (c) => c.plain))));
  problems.push(...keySetProblems('desktop plural groups', keysOf(mapLangs(classified, (c) => c.plurals))));
  if (android) problems.push(...keySetProblems('Android-only strings', keysOf(android)));

  const entries = [...desktopEntries(classified), ...androidEntries(android)];
  problems.push(...nameProblems(entries), ...placeholderProblems(entries));
  entries.sort(byNameThenKind);
  const parsed = mapLangs(classified, (c, lang) => ({
    keys: desktop[lang].size, plain: c.plain.size, plurals: c.plurals.size,
  }));
  return { entries, problems, parsed };
}
