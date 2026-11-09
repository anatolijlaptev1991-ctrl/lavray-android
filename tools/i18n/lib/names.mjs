// SPDX-License-Identifier: GPL-3.0-only

export const ANDROID_NAME_RE = /^[A-Za-z][A-Za-z0-9_]*$/;

/** Desktop key -> Android resource name: every char outside [A-Za-z0-9_] becomes "_". Case is kept. */
export function toResourceName(key) {
  return key.replace(/[^A-Za-z0-9_]/gu, '_');
}

export function isValidResourceName(name) {
  return ANDROID_NAME_RE.test(name);
}
