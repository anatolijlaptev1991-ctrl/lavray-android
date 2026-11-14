<!-- SPDX-License-Identifier: GPL-3.0-only -->
# tools/i18n

Node (v24, no npm packages) generator that turns the desktop UI dictionaries into Android string resources.

```
node tools/i18n/generate.mjs            # write the generated files
node tools/i18n/generate.mjs --check    # write nothing, exit 1 if a generated file is stale (CI)
node tools/i18n/generate.mjs --all      # emit every key, not only the used ones (experiments, never commit)
node tools/i18n/generate.mjs --root <dir>   # run against another tree (tests); default is the repo root
node --test "tools/i18n/**/*.test.mjs"  # tests (quote the glob; `node --test tools/i18n/` does not work on Node 24)
```

Input: `tools/i18n/dictionaries/{ru,en,zh,es,de}.ts` (the interface dictionaries, taken over from the desktop LAVRay 0.9.3 and kept here since) and
`tools/i18n/android/{ru,en,zh,es,de}.json` (Android-only strings, see below).
Output (committed, deterministic, do not edit): `app/src/main/res/values/strings.xml` (ru, the default as on desktop) and
`values-en`, `values-zh`, `values-es`, `values-de`. LF, UTF-8 without BOM, 4-space indent, entries sorted by resource name.

## Resource names

Desktop key to name: every character outside `[A-Za-z0-9_]` becomes `_`, case is kept
(`filter.protoAny` -> `filter_protoAny`). Two keys mapping to one name, or a name colliding with an Android-only name, fail the run.
Kotlin uses `R.string.filter_protoAny`.

## Only used keys reach the APK

The generator scans `app/src/main/java/**/*.kt` (and `.java`, `kotlin/`), `app/src/main/res/**/*.xml` and `AndroidManifest.xml`
for `R.string.x`, `R.plurals.x`, `@string/x`, `@plurals/x` (comments, `android.R.*` and `build/` dirs are skipped) and emits only those entries.
A referenced name that exists nowhere is an error listing file and name (typo catcher). Names declared in hand-written
`res/values*/` XML count as existing (e.g. `app_name`); declaring a generated name there too is an error.
Unused entries are only counted in the log.

## Placeholders

`{name}` (matching `{[A-Za-z_][A-Za-z0-9_]*}`) becomes `%N$s`. `{}` and `{ ... }` stay literal text.
N is the 1-based position of the name in the alphabetical (code unit) order of the placeholder names used by that key in all
five languages, so translations can reorder words while the Kotlin call site passes the arguments in one fixed order
(`getString(R.string.x, a, b)` with a, b alphabetical). Always `s`: pass preformatted strings.
Every language must use the same placeholder set for a key. If the key has placeholders a literal `%` becomes `%%`;
otherwise `%` is kept and such a `<string>` gets `formatted="false"`.

## Plurals

Keys `<base>.<cat>` with `cat` in `zero|one|two|few|many|other` form `<plurals name="<base as resource name>">` (items in the
order zero, one, two, few, many, other). The group must have `other`, must not also exist as a plain key, and the plural bases must be
the same in all languages (categories may differ: ru has one/few/many/other, the others one/other). The placeholder rule compares the union
over all categories. Kotlin uses `R.plurals.<name>`.
Look-alike plain keys (`imp.prev.one`, `sub.err.zero`, `cf.warn.other`, `ed.group.other`) are listed in `NOT_PLURAL_KEYS`
(`lib/classify.mjs`) and emitted as plain strings; a new one has to be added there.

## Escaping

XML: `& < >`. Android: `\'`, `\"`, `\\`, leading `@`/`?` as `\@`/`\?`, newline `\n`, tab `\t`. The first and last space of a string,
and every space after another space, is written ` ` (aapt would trim or collapse it). NBSP and other Unicode stay literal.

## Android-only strings

`tools/i18n/android/{ru,en,zh,es,de}.json` are flat `{ "resource_name": "text" }` files written by hand for strings that have no
desktop key. No directory means none; if some of the five files exist all five must exist with identical key sets.
Names must match `^[A-Za-z][A-Za-z0-9_]*$`. Texts follow the same `{name}` placeholder, escaping and used-only rules as desktop strings.

## Checks (exit code 1 with a message)

- Key sets identical in the five dictionaries (modulo plural categories) and in the five Android-only files; placeholder sets equal.
- The word `Happ` (case-insensitive, whole word) must never appear in an emitted string (Android-only strings: always checked).
- `sing-box` / `singbox` / `sing box` must not appear in an emitted string, except the desktop key `lic.core` (licenses screen).
  Unused desktop strings that mention it only produce a warning count. With `--all` desktop mentions are a warning too.
- Empty strings only where ru is empty; no characters XML cannot hold.
