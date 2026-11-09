// SPDX-License-Identifier: GPL-3.0-only

/** Languages of the Android app, default (values/) first, as on desktop. */
export const LANGS = Object.freeze(['ru', 'en', 'zh', 'es', 'de']);
export const DEFAULT_LANG = 'ru';

/** CLDR plural categories in the order they are written to <plurals>. */
export const PLURAL_CATEGORIES = Object.freeze(['zero', 'one', 'two', 'few', 'many', 'other']);

export const DICT_DIR = 'third_party/lavray/ui/src/lib/dictionaries';
export const ANDROID_ONLY_DIR = 'tools/i18n/android';
export const GENERATOR_PATH = 'tools/i18n/generate.mjs';

export function dictPath(lang) {
  return `${DICT_DIR}/${lang}.ts`;
}

export function androidOnlyPath(lang) {
  return `${ANDROID_ONLY_DIR}/${lang}.json`;
}

/** Repo-relative path of the generated strings.xml for a language. */
export function outputPath(lang) {
  const folder = lang === DEFAULT_LANG ? 'values' : `values-${lang}`;
  return `app/src/main/res/${folder}/strings.xml`;
}
