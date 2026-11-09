// SPDX-License-Identifier: GPL-3.0-only
import assert from 'node:assert/strict';
import { after, describe, it } from 'node:test';
import { generate } from '../lib/build.mjs';
import { I18nError } from '../lib/errors.mjs';
import { checkEmpty, checkHapp, checkSingBox } from '../lib/validate.mjs';
import { KOTLIN_PATH, createTrees, kotlinRefs, perLang } from '../testing/fixture-tree.mjs';

const trees = createTrees();
after(() => trees.cleanup());

const OUT = (lang) => (lang === 'ru' ? 'app/src/main/res/values/strings.xml' : `app/src/main/res/values-${lang}/strings.xml`);

function build(spec, extra = {}) {
  const root = trees.make({ dicts: perLang(spec), ...extra });
  return generate({ root, all: true });
}

function failure(spec, extra = {}) {
  try {
    build(spec, extra);
  } catch (error) {
    assert.ok(error instanceof I18nError, `unexpected error type: ${error}`);
    return error.message;
  }
  return assert.fail('expected the generator to fail');
}

const xml = (result, lang) => result.files.get(OUT(lang));

describe('placeholders end to end', () => {
  it('numbers placeholders alphabetically and tolerates reordered translations', () => {
    const result = build({
      'a.pair': { ru: '{host}: {count}', en: '{count} on {host}', zh: '{host}{count}', es: '{count} en {host}', de: '{host} — {count}' },
    });
    assert.match(xml(result, 'ru'), /<string name="a_pair">%2\$s: %1\$s<\/string>/);
    assert.match(xml(result, 'en'), /<string name="a_pair">%1\$s on %2\$s<\/string>/);
  });

  it('fails when the placeholder sets differ between languages, naming key, language and diff', () => {
    const message = failure({
      'a.pair': { ru: '{a} {b}', en: '{a}', zh: '{a} {b}', es: '{a} {b} {c}', de: '{a} {b}' },
    });
    assert.match(message, /desktop key "a\.pair": placeholders in en \{a\} differ from ru \{a, b\} \(missing \{b\}\)/);
    assert.match(message, /placeholders in es .* \(extra \{c\}\)/);
  });

  it('adds formatted="false" only to placeholder-free strings containing %', () => {
    const result = build({ 'p.plain': '100% sure', 'p.fmt': '{n}% done', 'p.none': 'nothing' });
    assert.match(xml(result, 'ru'), /<string name="p_plain" formatted="false">100% sure<\/string>/);
    assert.match(xml(result, 'ru'), /<string name="p_fmt">%1\$s%% done<\/string>/);
    assert.match(xml(result, 'ru'), /<string name="p_none">nothing<\/string>/);
  });
});

describe('plurals', () => {
  const ruWords = { one: '{n} профиль', few: '{n} профиля', many: '{n} профилей', other: '{n} профилей' };

  function pluralSpec() {
    const spec = {};
    for (const [cat, text] of Object.entries(ruWords)) spec[`pl.p.${cat}`] = { ru: text };
    spec['pl.p.one'] = { ...spec['pl.p.one'], en: '{n} profile', es: '{n} perfil', de: '{n} Profil' };
    spec['pl.p.other'] = { ...spec['pl.p.other'], en: '{n} profiles', es: '{n} perfiles', de: '{n} Profile', zh: '{n} 个配置' };
    return spec;
  }

  it('groups ru into four categories in canonical order and en into two', () => {
    const result = build(pluralSpec());
    assert.match(
      xml(result, 'ru'),
      /<plurals name="pl_p">\n {8}<item quantity="one">%1\$s профиль<\/item>\n {8}<item quantity="few">%1\$s профиля<\/item>\n {8}<item quantity="many">%1\$s профилей<\/item>\n {8}<item quantity="other">%1\$s профилей<\/item>\n {4}<\/plurals>/,
    );
    assert.match(xml(result, 'en'), /<plurals name="pl_p">\n {8}<item quantity="one">%1\$s profile<\/item>\n {8}<item quantity="other">%1\$s profiles<\/item>\n {4}<\/plurals>/);
    assert.equal(result.stats.perLang.ru.plurals, 1);
    assert.doesNotMatch(xml(result, 'ru'), /<string name="pl_p/);
  });

  it('allows a category without the placeholder (one -> "one profile") since the union is what counts', () => {
    const spec = pluralSpec();
    spec['pl.p.one'].ru = 'один профиль';
    const result = build(spec);
    assert.match(xml(result, 'ru'), /<item quantity="one">один профиль<\/item>/);
  });

  it('fails when a plural group has no other entry', () => {
    const message = failure({ 'pl.x.one': 'one', 'pl.x.few': 'few' });
    assert.match(message, /plural group "pl\.x" has categories \(one, few\) but no "pl\.x\.other"/);
  });

  it('fails when the group base is also a plain key', () => {
    const message = failure({ 'pl.x': 'plain', 'pl.x.one': 'one', 'pl.x.other': 'other' });
    assert.match(message, /"pl\.x" exists both as a plain key and as a plural group/);
  });

  it('fails when plural bases differ between languages', () => {
    const message = failure({ 'pl.a.other': { ru: 'x', en: 'x', zh: 'x', es: 'x' }, 'pl.b.other': 'y' });
    assert.match(message, /desktop plural groups: de lacks 1 key\(s\) that ru has: pl\.a/);
  });

  it('fails when the placeholder union of a plural differs between languages', () => {
    const spec = {
      'pl.a.one': { ru: '{n} one', en: '{count} one', zh: '{n} one', es: '{n} one', de: '{n} one' },
      'pl.a.other': { ru: '{n} many', en: '{count} many', zh: '{n}', es: '{n}', de: '{n}' },
    };
    assert.match(failure(spec), /desktop plural "pl\.a": placeholders in en \{count\} differ from ru \{n\}/);
  });

  it('keeps the known non-plural look-alike keys as plain strings', () => {
    const result = build({ 'imp.prev.one': 'Profile {proto}', 'sub.err.zero': 'Returned 0 servers', 'cf.warn.other': 'Careful' });
    assert.match(xml(result, 'ru'), /<string name="cf_warn_other">Careful<\/string>/);
    assert.match(xml(result, 'ru'), /<string name="imp_prev_one">Profile %1\$s<\/string>/);
    assert.match(xml(result, 'ru'), /<string name="sub_err_zero">Returned 0 servers<\/string>/);
  });
});

describe('names and key sets', () => {
  it('fails when two different keys map to one resource name', () => {
    const message = failure({ 'a.b': 'x', 'a_b': 'y' });
    assert.match(message, /name collision: desktop key "a\.b" and desktop key "a_b" both become resource "a_b"/);
  });

  it('fails when an Android-only name collides with a desktop name', () => {
    const android = perLang({ nav_home: 'Home' });
    const message = failure({ 'nav.home': 'Home' }, { android });
    assert.match(message, /desktop key "nav\.home" and Android-only string "nav_home" both become resource "nav_home"/);
  });

  it('fails on an invalid Android-only name', () => {
    const message = failure({ k: 'v' }, { android: perLang({ '1bad-name': 'x' }) });
    assert.match(message, /Android-only string "1bad-name" maps to resource name "1bad-name"/);
  });

  it('fails when the desktop key sets differ', () => {
    const message = failure({ a: 'x', b: { ru: 'x', en: 'x', zh: 'x', es: 'x' } });
    assert.match(message, /desktop dictionaries: de lacks 1 key\(s\) that ru has: b/);
  });

  it('fails when the Android-only key sets differ', () => {
    const android = perLang({ only_a: 'x', only_b: { ru: 'x', en: 'x', zh: 'x', es: 'x' } });
    assert.match(failure({ k: 'v' }, { android }), /Android-only strings: de lacks 1 key\(s\) that ru has: only_b/);
  });
});

const refs = (strings) => ({ [KOTLIN_PATH]: kotlinRefs({ strings }) });

describe('forbidden words', () => {
  it('fails on the word Happ in any language, case-insensitive, whole word only', () => {
    const message = failure({ 'x.y': { ru: 'a', en: 'Open HAPP now', zh: 'a', es: 'a', de: 'a' } });
    assert.match(message, /forbidden word "Happ" in desktop key "x\.y" \[en\]/);
    assert.deepEqual(checkHapp([{ kind: 'string', source: 'desktop', key: 'k', name: 'k', values: { ru: 'Happy', en: 'unhappy', zh: '', es: '', de: '' } }]), []);
  });

  it('fails on sing-box, singbox and sing box, but not for the lic.core exception', () => {
    for (const variant of ['sing-box', 'SingBox', 'sing box']) {
      const root = trees.make({ dicts: perLang({ 'x.y': `core ${variant}` }), files: refs(['x_y']) });
      assert.throws(() => generate({ root }), /forbidden word "sing-box" in desktop key "x\.y"/);
    }
    const root = trees.make({ dicts: perLang({ 'lic.core': 'Core based on sing-box', 'x.y': 'fine' }), files: refs(['lic_core', 'x_y']) });
    assert.match(generate({ root }).files.get(OUT('ru')), /name="lic_core"/);
  });

  it('does not fail on sing-box in unused desktop strings (warning only) and tolerates it in --all', () => {
    const root = trees.make({ dicts: perLang({ 'cf.intro': 'about sing-box', 'x.y': 'fine' }), files: refs(['x_y']) });
    const result = generate({ root });
    assert.match(result.warnings[0], /1 unused desktop entry mentions sing-box/);
    const all = generate({ root, all: true });
    assert.match(all.warnings.join(' '), /--all: 1 emitted desktop entries mention sing-box \(cf\.intro\)/);
  });

  it('allows sing-box via lic.core only for desktop strings, never for Android-only ones', () => {
    const android = perLang({ lic_extra: 'about sing-box' });
    assert.match(failure({ k: 'v' }, { android }), /forbidden word "sing-box" in Android-only string "lic_extra"/);
    const entry = { kind: 'string', source: 'android', key: 'lic.core', name: 'lic.core', values: { ru: 'sing-box', en: '', zh: '', es: '', de: '' } };
    assert.equal(checkSingBox([entry]).length, 1);
  });

  it('checks every Android-only string for Happ even when it is not referenced', () => {
    const android = perLang({ unused_name: 'Happ here' });
    const root = trees.make({ dicts: perLang({ k: 'v' }), android });
    assert.throws(() => generate({ root }), /forbidden word "Happ" in Android-only string "unused_name"/);
  });
});

describe('empty strings', () => {
  it('fails on an empty translation when ru is not empty', () => {
    assert.match(failure({ 'x.y': { ru: 'a', en: '', zh: 'a', es: 'a', de: 'a' } }), /empty string in desktop key "x\.y" \[en\]/);
  });

  it('allows empty strings where ru is empty too', () => {
    const result = build({ 'x.y': '' });
    assert.match(xml(result, 'en'), /<string name="x_y"><\/string>/);
    const entry = { kind: 'string', source: 'desktop', key: 'k', name: 'k', values: { ru: '', en: '', zh: '', es: '', de: '' } };
    assert.deepEqual(checkEmpty([entry]), []);
  });
});

describe('structure of the output', () => {
  it('has the header, sorted names, 4-space indent, LF and no BOM', () => {
    const result = build({ 'b.second': 'B', 'a.first': 'A' });
    const text = xml(result, 'ru');
    assert.ok(text.startsWith('<?xml version="1.0" encoding="utf-8"?>\n<!-- SPDX-License-Identifier: GPL-3.0-only -->\n<!-- GENERATED by tools/i18n/generate.mjs'));
    assert.match(text, /DO NOT EDIT\. -->\n<resources>\n {4}<string name="a_first">A<\/string>\n {4}<string name="b_second">B<\/string>\n<\/resources>\n$/);
    assert.ok(!text.includes('\r') && !text.startsWith('﻿'));
    assert.deepEqual([...result.files.keys()].sort(), ['de', 'en', 'es', 'ru', 'zh'].map(OUT).sort());
  });
});
