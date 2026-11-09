// SPDX-License-Identifier: GPL-3.0-only
import fs from 'node:fs';
import path from 'node:path';

const SKIP_DIRS = new Set(['build', '.gradle', '.git', 'node_modules', 'generated']);
const SOURCE_ROOTS = ['app/src/main/java', 'app/src/main/kotlin'];
const RES_ROOT = 'app/src/main/res';
const MANIFEST = 'app/src/main/AndroidManifest.xml';

// `android.R.string.ok` belongs to the platform, not to the app.
const CODE_REF_RE = /(?<![A-Za-z0-9_$])(?<!\bandroid\.)R\.(string|plurals)\.([A-Za-z_][A-Za-z0-9_]*)/g;
const XML_REF_RE = /@(string|plurals)\/([A-Za-z_][A-Za-z0-9_]*)/g;
const XML_DECL_RE = /<(string|plurals)\s[^>]*?\bname\s*=\s*"([^"]+)"/g;

function walk(dir, accept, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(entry.name)) walk(path.join(dir, entry.name), accept, out);
    } else if (accept(entry.name)) {
      out.push(path.join(dir, entry.name));
    }
  }
  return out;
}

// String literals are matched first and kept, so a comment opener inside a string ("image/*", "*/*", "http://") does not
// start a comment and swallow the code after it.
const CODE_TOKEN_RE = /"""[\s\S]*?"""|"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|\/\*[\s\S]*?\*\/|\/\/[^\n]*/g;

export function stripCodeComments(text) {
  return text.replace(CODE_TOKEN_RE, (token) => (token.startsWith("/*") || token.startsWith("//") ? "" : token));
}

export function stripXmlComments(text) {
  return text.replace(/<!--[\s\S]*?-->/g, '');
}

function collect(regex, text) {
  return [...text.matchAll(regex)].map((m) => ({ type: m[1], name: m[2] }));
}

function emptyUsage() {
  return {
    refs: { string: new Map(), plurals: new Map() },
    declared: { string: new Set(), plurals: new Set() },
  };
}

function addRef(usage, { type, name }, file) {
  const files = usage.refs[type].get(name) ?? [];
  if (!files.includes(file)) files.push(file);
  usage.refs[type].set(name, files);
}

function relative(root, file) {
  return path.relative(root, file).split(path.sep).join('/');
}

function scanCode(root, usage) {
  for (const sourceRoot of SOURCE_ROOTS) {
    const files = walk(path.join(root, sourceRoot), (n) => /\.(kt|java)$/.test(n));
    for (const file of files) {
      const text = stripCodeComments(fs.readFileSync(file, 'utf8'));
      for (const ref of collect(CODE_REF_RE, text)) addRef(usage, ref, relative(root, file));
    }
  }
}

function scanXml(root, usage, excluded) {
  const xmlFiles = walk(path.join(root, RES_ROOT), (n) => n.endsWith('.xml'));
  const manifest = path.join(root, MANIFEST);
  if (fs.existsSync(manifest)) xmlFiles.push(manifest);
  for (const file of xmlFiles) {
    const rel = relative(root, file);
    if (excluded.has(rel)) continue;
    const text = stripXmlComments(fs.readFileSync(file, 'utf8'));
    for (const ref of collect(XML_REF_RE, text)) addRef(usage, ref, rel);
    for (const decl of collect(XML_DECL_RE, text)) usage.declared[decl.type].add(decl.name);
  }
}

/**
 * Scans the Android app for resource references.
 * Returns { refs: {string, plurals: Map<name, files[]>}, declared: {string, plurals: Set<name>} }.
 * `declared` holds names declared in hand-written values XML (everything except `excludeFiles`,
 * the generated outputs). A missing app directory counts as empty.
 */
export function scanUsage(root, excludeFiles = []) {
  const usage = emptyUsage();
  scanCode(root, usage);
  scanXml(root, usage, new Set(excludeFiles));
  return usage;
}
