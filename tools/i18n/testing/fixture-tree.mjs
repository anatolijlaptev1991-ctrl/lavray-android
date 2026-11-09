// SPDX-License-Identifier: GPL-3.0-only
// Test helpers: build throw-away repo trees under os.tmpdir().
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { ANDROID_ONLY_DIR, DICT_DIR, LANGS } from '../lib/languages.mjs';

/**
 * Expands { key: 'same text' | { ru, en, zh, es, de } } into { lang: { key: text } }.
 * A per-language object may omit languages: the key is then absent there.
 */
export function perLang(spec) {
  const result = Object.fromEntries(LANGS.map((lang) => [lang, {}]));
  for (const [key, value] of Object.entries(spec)) {
    for (const lang of LANGS) {
      if (typeof value === 'string') result[lang][key] = value;
      else if (lang in value) result[lang][key] = value[lang];
    }
  }
  return result;
}

/** TypeScript source of a dictionary module in one of the shapes seen on desktop. */
export function dictSource(lang, entries, style = 'const') {
  const lines = Object.entries(entries).map(([k, v]) => `  ${JSON.stringify(k)}: ${JSON.stringify(v)},`);
  const body = `{\n  // a comment\n${lines.join('\n')}\n}`;
  const name = `${lang}Dict`;
  if (style === 'typed') return `// SPDX-License-Identifier: GPL-3.0-only\nexport const ${name}: Record<string, string> = ${body}\n`;
  return `// SPDX-License-Identifier: GPL-3.0-only\n\nexport const ${name} = ${body} as const;\n`;
}

/** Creates and tracks temp trees; call cleanup() from an `after` hook. */
export function createTrees() {
  const created = [];

  function write(root, rel, content) {
    const target = path.join(root, rel);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content, 'utf8');
  }

  /**
   * make({ dicts, android, files }):
   *   dicts   { lang: { key: text } } (see perLang)
   *   android { lang: { name: text } } or null/undefined for "no tools/i18n/android directory"
   *   files   { 'repo/relative/path': 'content' } (Kotlin sources, XML, ...)
   */
  function make({ dicts, android = null, files = {} }) {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'lavray-i18n-'));
    created.push(root);
    for (const lang of LANGS) {
      const style = lang === 'en' ? 'typed' : 'const';
      write(root, `${DICT_DIR}/${lang}.ts`, dictSource(lang, dicts[lang], style));
      if (android && android[lang]) write(root, `${ANDROID_ONLY_DIR}/${lang}.json`, JSON.stringify(android[lang], null, 2));
    }
    for (const [rel, content] of Object.entries(files)) write(root, rel, content);
    return root;
  }

  function cleanup() {
    for (const root of created.splice(0)) fs.rmSync(root, { recursive: true, force: true });
  }

  return { make, write, cleanup };
}

/** Kotlin file referencing the given resource names (R.string.x / R.plurals.y). */
export function kotlinRefs({ strings = [], plurals = [] }) {
  const lines = [
    ...strings.map((n) => `    val s_${n} = R.string.${n}`),
    ...plurals.map((n) => `    val p_${n} = R.plurals.${n}`),
  ];
  return `package app\n\nobject Refs {\n${lines.join('\n')}\n}\n`;
}

export const KOTLIN_PATH = 'app/src/main/java/app/Refs.kt';
