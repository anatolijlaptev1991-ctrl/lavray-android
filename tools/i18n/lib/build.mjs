// SPDX-License-Identifier: GPL-3.0-only
import fs from 'node:fs';
import path from 'node:path';
import { buildCatalog } from './catalog.mjs';
import { I18nError, failIfAny } from './errors.mjs';
import { ANDROID_ONLY_DIR, LANGS, androidOnlyPath, dictPath, outputPath } from './languages.mjs';
import { parseDictionary } from './parse-dict.mjs';
import { renderResources } from './render.mjs';
import { scanUsage } from './scan-usage.mjs';
import { checkContent, countSingBoxMentions, singBoxDesktopOffenders } from './validate.mjs';

/** Reads and parses the five desktop dictionaries into { lang: Map<key, text> }. */
export function readDictionaries(root) {
  const result = {};
  for (const lang of LANGS) {
    const rel = dictPath(lang);
    const file = path.join(root, rel);
    if (!fs.existsSync(file)) {
      throw new I18nError(`Dictionary ${rel} not found (the five interface dictionaries live in tools/i18n/dictionaries).`);
    }
    result[lang] = parseDictionary(fs.readFileSync(file, 'utf8'), rel);
  }
  return result;
}

function readAndroidFile(root, lang) {
  const rel = androidOnlyPath(lang);
  let data;
  try {
    data = JSON.parse(fs.readFileSync(path.join(root, rel), 'utf8').replace(/^﻿/, ''));
  } catch (error) {
    throw new I18nError(`${rel}: cannot read JSON (${error.message}). Fix the file; it must be a flat { "resource_name": "text" } object.`);
  }
  const flat = typeof data === 'object' && data !== null && !Array.isArray(data);
  if (!flat || Object.values(data).some((value) => typeof value !== 'string')) {
    throw new I18nError(`${rel}: expected a flat object whose values are all strings: { "resource_name": "text" }.`);
  }
  return new Map(Object.entries(data));
}

/**
 * Reads tools/i18n/android/*.json into { lang: Map<name, text> }.
 * A missing directory (or one with none of the five files) is empty -> null; a partial set is an error.
 */
export function readAndroidOnly(root) {
  const dir = path.join(root, ANDROID_ONLY_DIR);
  if (!fs.existsSync(dir)) return null;
  const present = LANGS.filter((lang) => fs.existsSync(path.join(root, androidOnlyPath(lang))));
  if (present.length === 0) return null;
  const absent = LANGS.filter((lang) => !present.includes(lang));
  if (absent.length > 0) {
    throw new I18nError(
      `${ANDROID_ONLY_DIR} has ${present.join(', ')}.json but lacks ${absent.join(', ')}.json. ` +
        'Provide all five files (ru, en, zh, es, de) with identical key sets, or remove the directory.',
    );
  }
  return Object.fromEntries(LANGS.map((lang) => [lang, readAndroidFile(root, lang)]));
}

function referenceProblems(entries, usage) {
  const problems = [];
  const other = { string: 'plurals', plurals: 'string' };
  for (const type of ['string', 'plurals']) {
    for (const [name, files] of usage.refs[type]) {
      const known = entries.some((e) => e.kind === type && e.name === name) || usage.declared[type].has(name);
      if (known) continue;
      const wrongKind = entries.some((e) => e.kind === other[type] && e.name === name);
      const hint = wrongKind ? ` (a ${other[type]} resource with that name exists; use R.${other[type]}.${name})` : '';
      problems.push(`${files.join(', ')}: R.${type}.${name} exists nowhere${hint}. Check the spelling or add the key to the dictionaries / tools/i18n/android/*.json.`);
    }
  }
  return problems;
}

function duplicateProblems(emitted, usage) {
  return emitted
    .filter((e) => usage.declared[e.kind].has(e.name))
    .map((e) => `resource "${e.name}" is generated and also declared in a hand-written res/values XML. Remove one of them.`);
}

function languageStats(emitted) {
  const stats = {};
  for (const lang of LANGS) {
    stats[lang] = {
      strings: emitted.filter((e) => e.kind === 'string').length,
      plurals: emitted.filter((e) => e.kind === 'plurals').length,
    };
  }
  return stats;
}

function singBoxWarnings(unused, emittedOffenders) {
  const warnings = [];
  const ignored = countSingBoxMentions(unused.filter((e) => e.source === 'desktop'));
  if (ignored) warnings.push(`${ignored} unused desktop entr${ignored === 1 ? 'y mentions' : 'ies mention'} sing-box; not emitted, so ignored.`);
  if (emittedOffenders.length) {
    warnings.push(`--all: ${emittedOffenders.length} emitted desktop entries mention sing-box (${emittedOffenders.map((e) => e.key).join(', ')}); do not ship this output.`);
  }
  return warnings;
}

/**
 * Runs the whole pipeline against a repo root.
 * Returns { files: Map<relPath, content>, stats, warnings }. Throws I18nError with all problems found.
 */
export function generate({ root, all = false }) {
  const desktop = readDictionaries(root);
  const android = readAndroidOnly(root);
  const catalog = buildCatalog({ desktop, android });
  const problems = [...catalog.problems];

  const usage = all ? null : scanUsage(root, LANGS.map(outputPath));
  const emitted = usage ? catalog.entries.filter((e) => usage.refs[e.kind].has(e.name)) : catalog.entries;
  const unused = catalog.entries.filter((e) => !emitted.includes(e));
  if (usage) problems.push(...referenceProblems(catalog.entries, usage), ...duplicateProblems(emitted, usage));
  problems.push(...checkContent(emitted, catalog.entries.filter((e) => e.source === 'android'), { lenientDesktopSingBox: all }));
  failIfAny('i18n generation failed', problems);

  const files = new Map(LANGS.map((lang) => [outputPath(lang), renderResources(lang, emitted)]));
  const warnings = singBoxWarnings(unused, all ? singBoxDesktopOffenders(emitted) : []);
  const stats = {
    perLang: languageStats(emitted),
    parsed: catalog.parsed,
    androidOnly: catalog.entries.filter((e) => e.source === 'android').length,
    unused: unused.length,
  };
  return { files, stats, warnings };
}
