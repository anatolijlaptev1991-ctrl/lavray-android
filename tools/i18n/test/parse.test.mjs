// SPDX-License-Identifier: GPL-3.0-only
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import { I18nError } from '../lib/errors.mjs';
import { LANGS, dictPath } from '../lib/languages.mjs';
import { toResourceName } from '../lib/names.mjs';
import { parseDictionary } from '../lib/parse-dict.mjs';
import { classifyDictionary } from '../lib/classify.mjs';

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..');

describe('parseDictionary', () => {
  it('parses `as const;` with comments and trailing commas', () => {
    const source = `// SPDX\n// c\nexport const ruDict = {\n  // inline\n  "a.b": "x", /* more */\n  "c": 'it\\'s',\n} as const;\n`;
    assert.deepEqual([...parseDictionary(source, 'ru')], [['a.b', 'x'], ['c', "it's"]]);
  });

  it('parses a typed declaration without as const', () => {
    const map = parseDictionary('export const enDict: Record<string, string> = { "k": "v" }\n', 'en');
    assert.equal(map.get('k'), 'v');
  });

  it('decodes escapes of the JS string (a \\n sequence becomes a real newline)', () => {
    const map = parseDictionary('export const xDict = { "k": "a\\nb\\u00e9" };', 'x');
    assert.equal(map.get('k'), 'a\nbé');
  });

  it('evaluates in an empty context: no process, no require', () => {
    const attempt = (expr) => () => parseDictionary(`export const xDict = { "k": ${expr} };`, 'x');
    assert.throws(attempt('process.env.PATH'), I18nError);
    assert.throws(attempt('require("fs").readFileSync("x")'), I18nError);
    assert.throws(attempt('typeof process === "undefined" ? 1 : "x"'), /expected a string/);
  });

  it('fails with a human message when there is no declaration', () => {
    assert.throws(() => parseDictionary('const x = {};', 'de.ts'), /de\.ts: no "export const xDict/);
  });

  it('fails when a value is not a string', () => {
    assert.throws(() => parseDictionary('export const xDict = { "k": 5 };', 'x'), /value of key "k" is number/);
  });

  it('fails on a non-object body', () => {
    assert.throws(() => parseDictionary('export const xDict = [1, 2];', 'x'), /not a plain object literal/);
  });
});

describe('resource names', () => {
  it('replaces every char outside [A-Za-z0-9_] with _ and keeps case', () => {
    assert.equal(toResourceName('filter.protoAny'), 'filter_protoAny');
    assert.equal(toResourceName('nav.connection'), 'nav_connection');
    assert.equal(toResourceName('a-b c.d'), 'a_b_c_d');
  });
});

describe('real desktop dictionaries (smoke)', () => {
  const dicts = Object.fromEntries(
    LANGS.map((lang) => [lang, parseDictionary(fs.readFileSync(path.join(REPO_ROOT, dictPath(lang)), 'utf8'), dictPath(lang))]),
  );

  it('parses all five', () => {
    for (const lang of LANGS) assert.ok(dicts[lang].size > 1000, `${lang} has ${dicts[lang].size} keys`);
  });

  it('has identical plain key sets and identical plural bases', () => {
    const parts = Object.fromEntries(LANGS.map((lang) => [lang, classifyDictionary(dicts[lang], lang)]));
    for (const lang of LANGS) {
      assert.deepEqual(parts[lang].problems, []);
      assert.deepEqual([...parts[lang].plain.keys()].sort(), [...parts.ru.plain.keys()].sort(), `${lang} plain keys`);
      assert.deepEqual([...parts[lang].plurals.keys()].sort(), [...parts.ru.plurals.keys()].sort(), `${lang} plural bases`);
    }
  });

  it('has ru plural pl.p with the few category', () => {
    assert.ok(dicts.ru.has('pl.p.few'));
    assert.ok(classifyDictionary(dicts.ru, 'ru').plurals.get('pl.p').has('few'));
  });
});
