// SPDX-License-Identifier: GPL-3.0-only
import assert from 'node:assert/strict';
import { after, describe, it } from 'node:test';
import { generate } from '../lib/build.mjs';
import { scanUsage, stripCodeComments } from '../lib/scan-usage.mjs';
import { KOTLIN_PATH, createTrees, kotlinRefs, perLang } from '../testing/fixture-tree.mjs';

const trees = createTrees();
after(() => trees.cleanup());

const SPEC = {
  'nav.home': 'Home',
  'nav.more': 'Other',
  'pl.p.one': { ru: '{n} профиль', en: '{n} profile', zh: '{n}', es: '{n} perfil', de: '{n} Profil' },
  'pl.p.other': { ru: '{n} профилей', en: '{n} profiles', zh: '{n}', es: '{n} perfiles', de: '{n} Profile' },
};
const RU_XML = 'app/src/main/res/values/strings.xml';

function tree(files, extra = {}) {
  return trees.make({ dicts: perLang(SPEC), files, ...extra });
}

describe('used key scanning', () => {
  it('emits only referenced strings and plurals, counting the rest as unused', () => {
    const root = tree({ [KOTLIN_PATH]: kotlinRefs({ strings: ['nav_home'], plurals: ['pl_p'] }) });
    const result = generate({ root });
    const ru = result.files.get(RU_XML);
    assert.match(ru, /name="nav_home"/);
    assert.match(ru, /<plurals name="pl_p">/);
    assert.doesNotMatch(ru, /nav_more/);
    assert.equal(result.stats.unused, 1);
    assert.deepEqual(result.stats.perLang.en, { strings: 1, plurals: 1 });
  });

  it('finds @string/ and @plurals/ in layouts and the manifest', () => {
    const root = tree({
      'app/src/main/res/layout/main.xml': '<TextView android:text="@string/nav_home"/>',
      'app/src/main/AndroidManifest.xml': '<application android:label="@string/nav_more"/>',
      'app/src/main/res/xml/p.xml': '<x a="@plurals/pl_p"/>',
    });
    const result = generate({ root });
    assert.equal(result.stats.unused, 0);
    assert.match(result.files.get(RU_XML), /name="nav_more"/);
  });

  it('ignores android.R.string, comments and the generated strings.xml itself', () => {
    const code = [
      'val a = android.R.string.ok',
      '// R.string.typo_in_comment',
      '/* R.string.typo_in_block */',
      'val url = "https://x"; val b = R.string.nav_home',
    ].join('\n');
    const root = tree({ [KOTLIN_PATH]: code, [RU_XML]: '<resources><string name="x">@string/ghost</string></resources>' });
    const result = generate({ root });
    assert.equal(result.stats.perLang.ru.strings, 1);
  });

  it('fails on a reference that exists nowhere, naming file and name', () => {
    const root = tree({ [KOTLIN_PATH]: 'val a = R.string.nav_hom', 'app/src/main/res/layout/m.xml': '<a t="@plurals/pl_q"/>' });
    assert.throws(() => generate({ root }), (error) => {
      assert.match(error.message, /app\/src\/main\/java\/app\/Refs\.kt: R\.string\.nav_hom exists nowhere/);
      assert.match(error.message, /app\/src\/main\/res\/layout\/m\.xml: R\.plurals\.pl_q exists nowhere/);
      return true;
    });
  });

  it('hints when the name exists with the other resource type', () => {
    const root = tree({ [KOTLIN_PATH]: 'val a = R.string.pl_p' });
    assert.throws(() => generate({ root }), /use R\.plurals\.pl_p/);
  });

  it('accepts names declared in hand-written values XML and rejects duplicates of generated ones', () => {
    const declared = '<resources><string name="app_name">App</string><string name="nav_home">dup</string></resources>';
    const refsOnly = tree({ [KOTLIN_PATH]: 'val a = R.string.app_name', 'app/src/main/res/values/app.xml': declared });
    assert.equal(generate({ root: refsOnly }).stats.perLang.ru.strings, 0);
    const dup = tree({ [KOTLIN_PATH]: 'val a = R.string.nav_home', 'app/src/main/res/values/app.xml': declared });
    assert.throws(() => generate({ root: dup }), /"nav_home" is generated and also declared in a hand-written/);
  });

  it('skips build directories', () => {
    const root = tree({ 'app/src/main/java/build/Gen.kt': 'val a = R.string.nothing', [KOTLIN_PATH]: 'val b = R.string.nav_home' });
    assert.equal(generate({ root }).stats.perLang.ru.strings, 1);
  });

  it('treats a missing app directory as empty', () => {
    const root = tree({});
    assert.equal(generate({ root }).stats.perLang.ru.strings, 0);
    assert.equal(scanUsage(root).refs.string.size, 0);
  });

  it('--all emits everything and does not scan', () => {
    const root = tree({ [KOTLIN_PATH]: 'val a = R.string.not_defined_anywhere' });
    const result = generate({ root, all: true });
    assert.deepEqual(result.stats.perLang.ru, { strings: 2, plurals: 1 });
    assert.equal(result.stats.unused, 0);
  });

  it('uses Android-only strings when referenced', () => {
    const android = perLang({ app_extra: 'Extra', app_unused: 'Unused' });
    const root = tree({ [KOTLIN_PATH]: 'val a = R.string.app_extra' }, { android });
    const result = generate({ root });
    assert.match(result.files.get(RU_XML), /name="app_extra"/);
    assert.doesNotMatch(result.files.get(RU_XML), /app_unused/);
  });

  it('strips Kotlin comments without touching URLs', () => {
    assert.equal(stripCodeComments('a // x\nb "http://h" /* y */ c'), 'a \nb "http://h"  c');
  });

  it('does not let a comment opener inside a string swallow the code after it', () => {
    const code = 'launch("image/*")\nval t = R.string.after_one\nlaunch("*/*")\nval u = R.string.after_two';
    assert.equal(stripCodeComments(code), code);
    assert.equal(stripCodeComments('x = "a" // R.string.gone\ny = 1'), 'x = "a" \ny = 1');
  });
});
