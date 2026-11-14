// SPDX-License-Identifier: GPL-3.0-only
//
// protogen: lavray.proto -> Kotlin DTOs and service method constants.
//
//   node tools/protogen/protogen.mjs [--proto <file>] [--out <dir>]
//
// Output (committed): app/src/main/java/io/github/anatolijlaptev1991_ctrl/lavray/api/gen/
//   Dto01.kt, Dto02.kt, ...  @Serializable data classes and enums
//   Services.kt              object per service with const val method paths
//   ApiCalls.kt              typed LavrayClient extension functions (unary / server stream)
//
// proto3 JSON rules honoured by the generated code (together with the Json
// configuration in api/Proto3Json.kt): camelCase names, int64 as string,
// enums by name, bytes as base64, defaults omitted, unknown keys ignored,
// oneof members are plain nullable properties (JSON has no wrapper for them).

import { readFileSync, writeFileSync, mkdirSync, readdirSync, unlinkSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SPDX = '// SPDX-License-Identifier: GPL-3.0-only';
const PKG = 'io.github.anatolijlaptev1991_ctrl.lavray.api.gen';
const API_PKG = 'io.github.anatolijlaptev1991_ctrl.lavray.api';
const CHUNK_LINES = 650;

// ---------------------------------------------------------------- tokenizer

export function tokenize(src) {
  const tokens = [];
  let i = 0;
  while (i < src.length) {
    const c = src[i];
    if (/\s/.test(c)) { i++; continue; }
    if (c === '/' && src[i + 1] === '/') {
      while (i < src.length && src[i] !== '\n') i++;
      continue;
    }
    if (c === '/' && src[i + 1] === '*') {
      const end = src.indexOf('*/', i + 2);
      if (end < 0) throw new Error('unterminated block comment');
      i = end + 2;
      continue;
    }
    if (c === '"' || c === "'") {
      let j = i + 1;
      while (j < src.length && src[j] !== c) j += src[j] === '\\' ? 2 : 1;
      tokens.push({ t: 'str', v: src.slice(i + 1, j) });
      i = j + 1;
      continue;
    }
    if (/[A-Za-z_]/.test(c)) {
      let j = i + 1;
      while (j < src.length && /[A-Za-z0-9_.]/.test(src[j])) j++;
      tokens.push({ t: 'id', v: src.slice(i, j) });
      i = j;
      continue;
    }
    if (/[0-9-]/.test(c)) {
      let j = i + 1;
      while (j < src.length && /[0-9a-fA-FxX.]/.test(src[j])) j++;
      tokens.push({ t: 'num', v: src.slice(i, j) });
      i = j;
      continue;
    }
    tokens.push({ t: 'p', v: c });
    i++;
  }
  return tokens;
}

// ------------------------------------------------------------------- parser

class Cursor {
  constructor(tokens) { this.tokens = tokens; this.pos = 0; }
  peek() { return this.tokens[this.pos]; }
  next() { return this.tokens[this.pos++]; }
  done() { return this.pos >= this.tokens.length; }
  is(v) { return this.peek()?.v === v && this.peek().t !== 'str'; }
  expect(v) {
    const tok = this.next();
    if (!tok || tok.v !== v) throw new Error(`expected "${v}" but got "${tok?.v}" (token #${this.pos})`);
    return tok;
  }
  ident() {
    const tok = this.next();
    if (!tok || tok.t !== 'id') throw new Error(`expected identifier, got "${tok?.v}"`);
    return tok.v;
  }
  skipStatement() { while (!this.is(';')) this.next(); this.expect(';'); }
  skipBrackets() { while (!this.is(']')) this.next(); this.expect(']'); }
}

function parseEnum(cur) {
  const name = cur.ident();
  cur.expect('{');
  const values = [];
  while (!cur.is('}')) {
    if (cur.is('option') || cur.is('reserved')) { cur.skipStatement(); continue; }
    const vName = cur.ident();
    cur.expect('=');
    cur.next();
    if (cur.is('[')) cur.skipBrackets();
    cur.expect(';');
    values.push(vName);
  }
  cur.expect('}');
  return { kind: 'enum', name, values };
}

function parseField(cur, oneof) {
  let label = '';
  if (cur.is('optional') || cur.is('repeated')) label = cur.next().v;
  if (cur.is('map')) {
    cur.next(); cur.expect('<');
    const key = cur.ident(); cur.expect(',');
    const value = cur.ident(); cur.expect('>');
    const name = cur.ident();
    cur.expect('='); cur.next();
    if (cur.is('[')) cur.skipBrackets();
    cur.expect(';');
    return { name, map: { key, value }, label: 'map', oneof };
  }
  const type = cur.ident();
  const name = cur.ident();
  cur.expect('='); cur.next();
  if (cur.is('[')) cur.skipBrackets();
  cur.expect(';');
  return { name, type, label, oneof };
}

function parseMessage(cur) {
  const name = cur.ident();
  cur.expect('{');
  const msg = { kind: 'message', name, fields: [], enums: [], messages: [] };
  while (!cur.is('}')) {
    if (cur.is('reserved') || cur.is('option')) { cur.skipStatement(); continue; }
    if (cur.is('enum')) { cur.next(); msg.enums.push(parseEnum(cur)); continue; }
    if (cur.is('message')) { cur.next(); msg.messages.push(parseMessage(cur)); continue; }
    if (cur.is('oneof')) {
      cur.next();
      const oneofName = cur.ident();
      cur.expect('{');
      while (!cur.is('}')) {
        if (cur.is('option')) { cur.skipStatement(); continue; }
        msg.fields.push(parseField(cur, oneofName));
      }
      cur.expect('}');
      continue;
    }
    msg.fields.push(parseField(cur, null));
  }
  cur.expect('}');
  return msg;
}

function parseService(cur) {
  const name = cur.ident();
  cur.expect('{');
  const rpcs = [];
  while (!cur.is('}')) {
    if (cur.is('option')) { cur.skipStatement(); continue; }
    cur.expect('rpc');
    const rpcName = cur.ident();
    cur.expect('(');
    const clientStream = cur.is('stream') ? (cur.next(), true) : false;
    const req = cur.ident();
    cur.expect(')');
    cur.expect('returns');
    cur.expect('(');
    const serverStream = cur.is('stream') ? (cur.next(), true) : false;
    const res = cur.ident();
    cur.expect(')');
    if (cur.is('{')) { while (!cur.is('}')) cur.next(); cur.expect('}'); } else cur.expect(';');
    if (clientStream) throw new Error(`${name}.${rpcName}: client streaming is not supported`);
    rpcs.push({ name: rpcName, req, res, serverStream });
  }
  cur.expect('}');
  return { name, rpcs };
}

export function parseProto(src) {
  const cur = new Cursor(tokenize(src));
  const file = { package: '', messages: [], enums: [], services: [] };
  while (!cur.done()) {
    const word = cur.ident();
    if (word === 'syntax' || word === 'import' || word === 'option') cur.skipStatement();
    else if (word === 'package') { file.package = cur.ident(); cur.expect(';'); }
    else if (word === 'message') file.messages.push(parseMessage(cur));
    else if (word === 'enum') file.enums.push(parseEnum(cur));
    else if (word === 'service') file.services.push(parseService(cur));
    else throw new Error(`unexpected top-level token "${word}"`);
  }
  return file;
}

// ----------------------------------------------------------------- resolver

const SCALARS = {
  bool: 'Boolean', string: 'String', bytes: 'ByteArray', double: 'Double', float: 'Float',
  int32: 'Int', sint32: 'Int', sfixed32: 'Int',
  uint32: 'UInt', fixed32: 'UInt',
  int64: 'Long', sint64: 'Long', sfixed64: 'Long',
  uint64: 'ULong', fixed64: 'ULong',
};
const SCALAR_DEFAULT = {
  Boolean: 'false', String: '""', ByteArray: 'ByteArray(0)', Double: '0.0', Float: '0.0f',
  Int: '0', UInt: '0u', Long: '0L', ULong: '0uL',
};
const KOTLIN_HARD = new Set(['as', 'break', 'class', 'continue', 'do', 'else', 'false', 'for', 'fun', 'if', 'in',
  'interface', 'is', 'null', 'object', 'package', 'return', 'super', 'this', 'throw', 'true', 'try', 'typealias',
  'typeof', 'val', 'var', 'when', 'while']);
const DATA_CLASS_CLASH = new Set(['copy', 'hashCode', 'toString', 'equals']);

export function camel(snake) {
  return snake.replace(/_+([A-Za-z0-9])/g, (_, ch) => ch.toUpperCase());
}

export function upperSnake(pascal) {
  return pascal.replace(/([a-z0-9])([A-Z])/g, '$1_$2').replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2').toUpperCase();
}

function lowerFirst(s) { return s.charAt(0).toLowerCase() + s.slice(1); }

function kName(name) {
  const n = camel(name);
  if (DATA_CLASS_CLASH.has(n) || /^component\d+$/.test(n)) throw new Error(`field name "${name}" clashes with a data class member`);
  return KOTLIN_HARD.has(n) ? `\`${n}\`` : n;
}

/** Collect "Qualified.Name" -> {kind} for every message and enum. */
function buildIndex(file) {
  const index = new Map();
  const add = (prefix, items, kind) => items.forEach((it) => index.set(prefix + it.name, kind));
  const walk = (prefix, msg) => {
    add(`${prefix}${msg.name}.`, msg.enums, 'enum');
    add(`${prefix}${msg.name}.`, msg.messages, 'message');
    msg.messages.forEach((m) => walk(`${prefix}${msg.name}.`, m));
  };
  add('', file.enums, 'enum');
  add('', file.messages, 'message');
  file.messages.forEach((m) => walk('', m));
  return index;
}

function resolveType(index, scope, name) {
  if (SCALARS[name]) return { scalar: SCALARS[name] };
  const parts = scope.split('.').filter(Boolean);
  for (let n = parts.length; n >= 0; n--) {
    const candidate = [...parts.slice(0, n), name].join('.');
    if (index.has(candidate)) return { ref: candidate, kind: index.get(candidate) };
  }
  throw new Error(`unresolved type "${name}" in ${scope}`);
}

// ------------------------------------------------------------------ emitter

function typeOf(index, scope, field) {
  const one = (t) => {
    const r = resolveType(index, scope, t);
    return r.scalar ?? r.ref;
  };
  if (field.label === 'map') {
    const keyType = one(field.map.key);
    const valType = one(field.map.value);
    return { type: `Map<${keyType}, ${valType}>`, def: 'emptyMap()' };
  }
  const base = one(field.type);
  if (field.label === 'repeated') return { type: `List<${base}>`, def: 'emptyList()' };
  const r = resolveType(index, scope, field.type);
  if (r.kind === 'message') return { type: `${base}?`, def: 'null' };
  // optional and oneof members carry presence in proto3 JSON: absent is not the same as default.
  if (field.label === 'optional' || field.oneof) return { type: `${base}?`, def: 'null' };
  if (r.kind === 'enum') return { type: base, def: null, enumRef: r.ref };
  return { type: base, def: SCALAR_DEFAULT[base] };
}

function emitEnum(en, indent) {
  const pad = ' '.repeat(indent);
  const body = en.values.map((v) => `${pad}    ${v},`).join('\n');
  return `${pad}@Serializable\n${pad}enum class ${en.name} {\n${body}\n${pad}}\n`;
}

function emitMessage(index, enumsByRef, msg, scope, indent = 0) {
  const pad = ' '.repeat(indent);
  const here = scope ? `${scope}.${msg.name}` : msg.name;
  const nested = [...msg.enums.map((e) => emitEnum(e, indent + 4)),
    ...msg.messages.map((m) => emitMessage(index, enumsByRef, m, here, indent + 4))];
  if (msg.fields.length === 0 && nested.length === 0) {
    return `${pad}@Serializable\n${pad}data object ${msg.name}\n`;
  }
  if (msg.fields.length === 0) throw new Error(`message ${here} has nested types but no fields`);
  const props = msg.fields.map((f) => {
    const t = typeOf(index, here, f);
    const def = t.def ?? `${t.enumRef}.${enumsByRef.get(t.enumRef)[0]}`;
    const oneofNote = f.oneof ? ` // oneof ${f.oneof}` : '';
    return `${pad}    val ${kName(f.name)}: ${t.type} = ${def},${oneofNote}`;
  });
  const ctor = `${pad}@Serializable\n${pad}data class ${msg.name}(\n${props.join('\n')}\n${pad})`;
  return nested.length === 0 ? `${ctor}\n` : `${ctor} {\n${nested.join('\n')}${pad}}\n`;
}

function enumFirstValues(file) {
  const map = new Map();
  const walkMsg = (prefix, msg) => {
    msg.enums.forEach((e) => map.set(`${prefix}${msg.name}.${e.name}`, e.values));
    msg.messages.forEach((m) => walkMsg(`${prefix}${msg.name}.`, m));
  };
  file.enums.forEach((e) => map.set(e.name, e.values));
  file.messages.forEach((m) => walkMsg('', m));
  return map;
}

function fileHeader(extraImports = []) {
  const imports = ['kotlinx.serialization.Serializable', ...extraImports].sort();
  return `${SPDX}
// GENERATED by tools/protogen/protogen.mjs from core/proto/lavray.proto. DO NOT EDIT.
@file:UseSerializers(Int64StringSerializer::class, UInt64StringSerializer::class, Base64BytesSerializer::class)

package ${PKG}

import ${API_PKG}.Base64BytesSerializer
import ${API_PKG}.Int64StringSerializer
import ${API_PKG}.UInt64StringSerializer
${imports.map((i) => `import ${i}`).join('\n')}
import kotlinx.serialization.UseSerializers

`;
}

export function generateDtoFiles(file) {
  const index = buildIndex(file);
  const enumsByRef = enumFirstValues(file);
  const blocks = [
    ...file.enums.map((e) => emitEnum(e, 0)),
    ...file.messages.map((m) => emitMessage(index, enumsByRef, m, '')),
  ];
  const chunks = [];
  let cur = [];
  let lines = 0;
  for (const b of blocks) {
    const n = b.split('\n').length;
    if (lines + n > CHUNK_LINES && cur.length) { chunks.push(cur); cur = []; lines = 0; }
    cur.push(b);
    lines += n;
  }
  if (cur.length) chunks.push(cur);
  return chunks.map((c, i) => ({
    name: `Dto${String(i + 1).padStart(2, '0')}.kt`,
    text: fileHeader() + c.join('\n'),
  }));
}

export function generateServices(file) {
  const out = [`${SPDX}\n// GENERATED by tools/protogen/protogen.mjs. DO NOT EDIT.\n\npackage ${PKG}\n`];
  for (const s of file.services) {
    out.push(`/** ${file.package}.${s.name}. Unary methods use application/json, server streams application/connect+json. */`);
    out.push(`object ${s.name} {`);
    out.push(`    const val NAME = "${file.package}.${s.name}"`);
    for (const r of s.rpcs) {
      out.push(`    const val ${upperSnake(r.name)} = "/${file.package}.${s.name}/${r.name}" // ${r.serverStream ? 'server stream' : 'unary'}`);
    }
    out.push('}\n');
  }
  return { name: 'Services.kt', text: out.join('\n') };
}

export function generateCalls(file) {
  const out = [`${SPDX}
// GENERATED by tools/protogen/protogen.mjs. DO NOT EDIT.
// One extension per RPC. R8 drops every function the app never calls.

package ${PKG}

import ${API_PKG}.LavrayClient
import kotlinx.coroutines.flow.Flow
`];
  for (const s of file.services) {
    const prefix = lowerFirst(s.name.replace(/Service$/, ''));
    for (const r of s.rpcs) {
      const fn = `${prefix}${r.name}`;
      const dflt = (name) => (file.messages.find((m) => m.name === name)?.fields.length === 0 ? name : `${name}()`);
      const sig = `fun LavrayClient.${fn}(request: ${r.req} = ${dflt(r.req)})`;
      const args = `${s.name}.${upperSnake(r.name)}, request, ${r.req}.serializer(), ${r.res}.serializer()`;
      out.push(r.serverStream
        ? `${sig}: Flow<${r.res}> =\n    serverStream(${args})\n`
        : `suspend ${sig}: ${r.res} =\n    unary(${args})\n`);
    }
  }
  return { name: 'ApiCalls.kt', text: out.join('\n') };
}

export function generateAll(src) {
  const file = parseProto(src);
  return [...generateDtoFiles(file), generateServices(file), generateCalls(file)];
}

// --------------------------------------------------------------------- main

function main(argv) {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
  const args = new Map();
  for (let i = 0; i < argv.length; i += 2) args.set(argv[i], argv[i + 1]);
  const protoPath = args.get('--proto') ?? join(root, 'core/proto/lavray.proto');
  const outDir = args.get('--out') ?? join(root, 'app/src/main/java', ...PKG.split('.'));
  const files = generateAll(readFileSync(protoPath, 'utf8'));
  mkdirSync(outDir, { recursive: true });
  for (const f of readdirSync(outDir)) {
    if (/^(Dto\d+|Services|ApiCalls)\.kt$/.test(f)) unlinkSync(join(outDir, f));
  }
  for (const f of files) writeFileSync(join(outDir, f.name), f.text, 'utf8');
  console.log(`protogen: wrote ${files.length} files to ${outDir}`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) main(process.argv.slice(2));
