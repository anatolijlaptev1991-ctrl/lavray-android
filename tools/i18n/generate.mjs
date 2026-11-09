// SPDX-License-Identifier: GPL-3.0-only
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { generate } from './lib/build.mjs';
import { I18nError } from './lib/errors.mjs';
import { LANGS } from './lib/languages.mjs';

const DEFAULT_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const USAGE = 'usage: node tools/i18n/generate.mjs [--check] [--all] [--root <dir>]';

/** Parses argv (without node and script) into { check, all, root }. */
export function parseArgs(argv) {
  const options = { check: false, all: false, root: DEFAULT_ROOT };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '--check') options.check = true;
    else if (arg === '--all') options.all = true;
    else if (arg === '--root') {
      const value = argv[(i += 1)];
      if (!value) throw new I18nError(`--root needs a directory. ${USAGE}`);
      options.root = path.resolve(value);
    } else {
      throw new I18nError(`Unknown argument "${arg}". ${USAGE}`);
    }
  }
  return options;
}

function readIfExists(file) {
  return fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : null;
}

function staleFiles(root, files) {
  return [...files].filter(([rel, content]) => readIfExists(path.join(root, rel)) !== content).map(([rel]) => rel);
}

function writeFiles(root, files) {
  for (const [rel, content] of files) {
    const target = path.join(root, rel);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content, 'utf8');
  }
}

function summaryLines(result) {
  const { perLang, parsed, unused, androidOnly } = result.stats;
  const lines = LANGS.map((lang) => {
    const emitted = `${perLang[lang].strings} strings, ${perLang[lang].plurals} plurals`;
    return `  ${lang}: ${emitted} emitted (desktop dictionary: ${parsed[lang].keys} keys = ${parsed[lang].plain} plain + ${parsed[lang].plurals} plural groups)`;
  });
  lines.push(`  Android-only strings: ${androidOnly}; unused entries not emitted: ${unused}`);
  return [...lines, ...result.warnings.map((w) => `  warning: ${w}`)];
}

/** Runs the generator; returns the process exit code. `out` receives log lines. */
export function run(argv, out = console.log) {
  const { check, all, root } = parseArgs(argv);
  const result = generate({ root, all });
  if (check) {
    const stale = staleFiles(root, result.files);
    if (stale.length > 0) {
      out(`i18n check failed: ${stale.length} generated file(s) are stale or missing:\n${stale.map((f) => `  - ${f}`).join('\n')}`);
      out('Run: node tools/i18n/generate.mjs   and commit the result.');
      return 1;
    }
    out(`i18n check OK: ${result.files.size} generated files are up to date.`);
    return 0;
  }
  writeFiles(root, result.files);
  out(`i18n: wrote ${result.files.size} files${all ? ' (--all)' : ''}`);
  summaryLines(result).forEach((line) => out(line));
  return 0;
}

function main() {
  try {
    process.exitCode = run(process.argv.slice(2));
  } catch (error) {
    if (!(error instanceof I18nError)) throw error;
    console.error(`i18n error: ${error.message}`);
    process.exitCode = 1;
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main();
