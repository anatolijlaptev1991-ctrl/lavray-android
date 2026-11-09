// SPDX-License-Identifier: GPL-3.0-only

/** Error with a human message (what happened, what to do). The CLI prints it and exits with 1. */
export class I18nError extends Error {
  constructor(message, problems = []) {
    super(message);
    this.name = 'I18nError';
    this.problems = problems;
  }
}

const MAX_SHOWN = 40;

/** Throws one I18nError listing every problem, or returns when the list is empty. */
export function failIfAny(title, problems, hint = '') {
  if (problems.length === 0) return;
  const shown = problems.slice(0, MAX_SHOWN).map((p) => `  - ${p}`);
  if (problems.length > MAX_SHOWN) shown.push(`  ... and ${problems.length - MAX_SHOWN} more`);
  const tail = hint ? `\n${hint}` : '';
  throw new I18nError(`${title} (${problems.length}):\n${shown.join('\n')}${tail}`, problems);
}

/** Compact "a, b, c (+N more)" rendering of a list of names for messages. */
export function listNames(names, limit = 8) {
  const head = names.slice(0, limit).join(', ');
  return names.length > limit ? `${head} (+${names.length - limit} more)` : head;
}
