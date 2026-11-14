// SPDX-License-Identifier: GPL-3.0-only
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { after, describe, it } from 'node:test';
import { fileURLToPath } from 'node:url';
import { parseArgs, run } from '../generate.mjs';
import { generate } from '../lib/build.mjs';
import { I18nError } from '../lib/errors.mjs';
import { LANGS, outputPath } from '../lib/languages.mjs';
import { KOTLIN_PATH, createTrees, kotlinRefs, perLang } from '../testing/fixture-tree.mjs';

const GENERATE = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'generate.mjs');
const trees = createTrees();
after(() => trees.cleanup());

const SPEC = { 'nav.home': { ru: 'Главная', en: 'Home', zh: '主页', es: 'Inicio', de: 'Start' }, 'x.y': "it's" };

function tree(extra = {}) {
  return trees.make({ dicts: perLang(SPEC), files: { [KOTLIN_PATH]: kotlinRefs({ strings: ['nav_home', 'x_y'] }) }, ...extra });
}

function quiet(argv) {
  const lines = [];
  const code = run(argv, (line) => lines.push(line));
  return { code, text: lines.join('\n') };
}

describe('generator run', () => {
  it('writes five files with UTF-8, LF and no BOM', () => {
    const root = tree();
    const { code, text } = quiet(['--root', root]);
    assert.equal(code, 0);
    assert.match(text, /wrote 5 files/);
    for (const lang of LANGS) {
      const buffer = fs.readFileSync(path.join(root, outputPath(lang)));
      assert.notDeepEqual([...buffer.subarray(0, 3)], [0xef, 0xbb, 0xbf]);
      assert.ok(!buffer.includes(0x0d));
    }
    assert.match(fs.readFileSync(path.join(root, outputPath('ru')), 'utf8'), /Главная/);
    assert.match(fs.readFileSync(path.join(root, outputPath('en')), 'utf8'), /it\\'s/);
  });

  it('is deterministic: two runs give identical bytes', () => {
    const root = tree();
    quiet(['--root', root]);
    const first = LANGS.map((lang) => fs.readFileSync(path.join(root, outputPath(lang))));
    quiet(['--root', root]);
    const second = LANGS.map((lang) => fs.readFileSync(path.join(root, outputPath(lang))));
    assert.deepEqual(second, first);
    assert.deepEqual([...generate({ root }).files.values()], [...generate({ root }).files.values()]);
  });
});

describe('--check', () => {
  it('reports missing files, passes after generation, and detects a stale file', () => {
    const root = tree();
    assert.equal(quiet(['--check', '--root', root]).code, 1);
    assert.ok(!fs.existsSync(path.join(root, outputPath('ru'))), '--check must not write');

    quiet(['--root', root]);
    assert.equal(quiet(['--check', '--root', root]).code, 0);

    const target = path.join(root, outputPath('de'));
    fs.writeFileSync(target, `${fs.readFileSync(target, 'utf8')}<!-- edited -->\n`);
    const stale = quiet(['--check', '--root', root]);
    assert.equal(stale.code, 1);
    assert.match(stale.text, /values-de\/strings\.xml/);
    assert.doesNotMatch(stale.text, /values-en/);
  });

  it('detects a changed dictionary as stale output', () => {
    const root = tree();
    quiet(['--root', root]);
    const ruDict = path.join(root, 'tools/i18n/dictionaries/ru.ts');
    fs.writeFileSync(ruDict, fs.readFileSync(ruDict, 'utf8').replace('Главная', 'Домой'));
    assert.equal(quiet(['--check', '--root', root]).code, 1);
  });
});

describe('Android-only files', () => {
  it('treats a missing directory as empty', () => {
    assert.equal(generate({ root: tree() }).stats.androidOnly, 0);
  });

  it('requires all five files when some exist', () => {
    const root = tree();
    trees.write(root, 'tools/i18n/android/ru.json', '{"a": "b"}');
    assert.throws(() => generate({ root }), /has ru\.json but lacks en, zh, es, de\.json/);
  });

  it('rejects non-flat JSON with a readable message', () => {
    const android = perLang({ a: 'x' });
    const root = tree({ android });
    trees.write(root, 'tools/i18n/android/en.json', '{"a": {"nested": 1}}');
    assert.throws(() => generate({ root }), /en\.json: expected a flat object/);
  });
});

describe('command line', () => {
  it('parses flags and rejects unknown ones', () => {
    assert.deepEqual({ ...parseArgs(['--all', '--check', '--root', '.']), root: 'x' }, { check: true, all: true, root: 'x' });
    assert.throws(() => parseArgs(['--bogus']), I18nError);
    assert.throws(() => parseArgs(['--root']), /--root needs a directory/);
  });

  it('exits with 1 and a human message on a validation failure', () => {
    const root = trees.make({ dicts: perLang({ k: 'open Happ' }), files: { [KOTLIN_PATH]: kotlinRefs({ strings: ['k'] }) } });
    const result = spawnSync(process.execPath, [GENERATE, '--root', root], { encoding: 'utf8' });
    assert.equal(result.status, 1);
    assert.match(result.stderr, /i18n error: i18n generation failed/);
    assert.match(result.stderr, /forbidden word "Happ"/);
  });

  it('exits with 0 and prints a summary on success', () => {
    const root = tree();
    const result = spawnSync(process.execPath, [GENERATE, '--root', root], { encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /ru: 2 strings, 0 plurals emitted/);
  });
});
