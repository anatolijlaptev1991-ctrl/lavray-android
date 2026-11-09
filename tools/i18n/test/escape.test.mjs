// SPDX-License-Identifier: GPL-3.0-only
import assert from 'node:assert/strict';
import { describe, it } from 'node:test';
import { hasInvalidXmlChars, toAndroidText } from '../lib/escape.mjs';
import { extractPlaceholders, numberingFor } from '../lib/placeholders.mjs';

/** Same pipeline the renderer uses: numbering from the text's own placeholders. */
function convert(text, names = extractPlaceholders(text)) {
  return toAndroidText(text, { numbering: numberingFor(names) });
}

describe('placeholder mapping', () => {
  it('maps {name} to positional %N$s in alphabetical order, not in text order', () => {
    assert.equal(convert('{zeta} then {alpha} then {mid}'), '%3$s then %1$s then %2$s');
  });

  it('uses the same numbering for a reordered translation', () => {
    const names = ['count', 'host'];
    assert.equal(convert('{host}: {count}', names), '%2$s: %1$s');
    assert.equal(convert('{count} on {host}', names), '%1$s on %2$s');
  });

  it('repeats the number for a repeated placeholder', () => {
    assert.equal(convert('{n} of {n}'), '%1$s of %1$s');
  });

  it('keeps {} and { ... } as literal text', () => {
    assert.deepEqual(extractPlaceholders('empty or {} — not used; need { … }'), []);
    assert.equal(convert('Need JSON object: { … } or {}'), 'Need JSON object: { … } or {}');
  });

  it('does not treat names that start with a digit as placeholders', () => {
    assert.deepEqual(extractPlaceholders('{1} {a-b} {ok_1}'), ['ok_1']);
  });

  it('includes unused names in the numbering (union over languages)', () => {
    assert.equal(convert('only {b}', ['a', 'b']), 'only %2$s');
  });
});

describe('percent handling', () => {
  it('doubles % when the key has placeholders', () => {
    assert.equal(convert('{n}% done'), '%1$s%% done');
  });

  it('leaves % alone when there are no placeholders', () => {
    assert.equal(convert('100% sure'), '100% sure');
  });

  it('doubles % in a translation without the placeholder when the key has one', () => {
    assert.equal(convert('50% off', ['n']), '50%% off');
  });
});

describe('Android escaping', () => {
  it('escapes apostrophes and double quotes', () => {
    assert.equal(convert(`it's "ok"`), `it\\'s \\"ok\\"`);
  });

  it('escapes a leading @ and ? only', () => {
    assert.equal(convert('@home'), '\\@home');
    assert.equal(convert('?what'), '\\?what');
    assert.equal(convert('mail@home? yes'), 'mail@home? yes');
  });

  it('does not treat @ after a placeholder as leading', () => {
    assert.equal(convert('{a}@{b}'), '%1$s@%2$s');
  });

  it('writes first and last space as \\u0020', () => {
    assert.equal(convert(' a b '), '\\u0020a b\\u0020');
    assert.equal(convert(' '), '\\u0020');
  });

  it('keeps a trailing space before a placeholder as a normal space but still protects the string ends', () => {
    assert.equal(convert('a {n} b '), 'a %1$s b\\u0020');
    assert.equal(convert('{n} '), '%1$s\\u0020');
  });

  it('protects all but the first space of a run', () => {
    assert.equal(convert('a   b'), 'a \\u0020\\u0020b');
  });

  it('turns real newlines and tabs into \\n and \\t, CRLF into one \\n', () => {
    assert.equal(convert('a\nb\tc\r\nd'), 'a\\nb\\tc\\nd');
  });

  it('doubles backslashes', () => {
    assert.equal(convert('C:\\dir'), 'C:\\\\dir');
  });

  it('keeps NBSP and other Unicode literal', () => {
    assert.equal(convert('a\u00a0b — «ж» 中文 ’'), 'a\u00a0b — «ж» 中文 ’');
  });
});

describe('XML escaping', () => {
  it('escapes & < >', () => {
    assert.equal(convert('a & b <i>c</i>'), 'a &amp; b &lt;i&gt;c&lt;/i&gt;');
  });

  it('detects characters XML cannot hold', () => {
    assert.equal(hasInvalidXmlChars('ok\n\t'), false);
    assert.equal(hasInvalidXmlChars('bad\u0001'), true);
    assert.equal(hasInvalidXmlChars('lone\ud800'), true);
  });
});
