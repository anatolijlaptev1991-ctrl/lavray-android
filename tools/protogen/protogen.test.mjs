// SPDX-License-Identifier: GPL-3.0-only
// Run: node --test tools/protogen/protogen.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { camel, upperSnake, parseProto, generateAll } from './protogen.mjs';

const SAMPLE = `
syntax = "proto3";
package demo.v1;
option go_package = "x";
/* block
   comment */
message Empty {}
enum Mode { MODE_UNSPECIFIED = 0; MODE_TUN = 2; reserved 1; }
message Req {
  enum Kind { KIND_A = 0; KIND_B = 1; }
  optional int64 profile_id = 1; // trailing
  Mode mode = 2;
  repeated string tags = 3;
  map<int64, Inner> by_id = 4;
  bytes blob = 5;
  Kind kind = 6;
  oneof pick { string s = 7; Inner inner_msg = 8; int32 n = 9; }
  reserved 10;
  reserved "gone";
}
message Inner { bool in = 1; }
service Demo {
  rpc Do(Req) returns (Empty);
  rpc Watch(Empty) returns (stream Req);
}
`;

test('case helpers', () => {
  assert.equal(camel('ws_early_data_max'), 'wsEarlyDataMax');
  assert.equal(upperSnake('GetPublicIp'), 'GET_PUBLIC_IP');
  assert.equal(upperSnake('RestartProfile'), 'RESTART_PROFILE');
});

test('parser keeps messages, enums, oneofs, maps and streaming flags', () => {
  const f = parseProto(SAMPLE);
  assert.equal(f.package, 'demo.v1');
  assert.deepEqual(f.messages.map((m) => m.name), ['Empty', 'Req', 'Inner']);
  assert.deepEqual(f.messages[1].enums[0].values, ['KIND_A', 'KIND_B']);
  assert.equal(f.messages[1].fields.find((x) => x.name === 'by_id').label, 'map');
  assert.equal(f.messages[1].fields.find((x) => x.name === 'n').oneof, 'pick');
  assert.deepEqual(f.services[0].rpcs.map((r) => r.serverStream), [false, true]);
});

test('generated Kotlin follows proto3 JSON conventions', () => {
  const files = generateAll(SAMPLE);
  const dto = files.find((x) => x.name === 'Dto01.kt').text;
  assert.match(dto, /@file:UseSerializers\(Int64StringSerializer::class/);
  assert.match(dto, /data object Empty/);
  assert.match(dto, /val profileId: Long\? = null/);
  assert.match(dto, /val mode: Mode = Mode\.MODE_UNSPECIFIED/);
  assert.match(dto, /val tags: List<String> = emptyList\(\)/);
  assert.match(dto, /val byId: Map<Long, Inner> = emptyMap\(\)/);
  assert.match(dto, /val blob: ByteArray = ByteArray\(0\)/);
  assert.match(dto, /val kind: Req\.Kind = Req\.Kind\.KIND_A/);
  assert.match(dto, /val s: String\? = null, \/\/ oneof pick/);
  assert.match(dto, /val n: Int\? = null/);
  assert.match(dto, /val `in`: Boolean = false/);
  const services = files.find((x) => x.name === 'Services.kt').text;
  assert.match(services, /const val DO = "\/demo\.v1\.Demo\/Do"/);
  const calls = files.find((x) => x.name === 'ApiCalls.kt').text;
  assert.match(calls, /suspend fun LavrayClient\.demoDo\(request: Req = Req\(\)\): Empty/);
  assert.match(calls, /fun LavrayClient\.demoWatch\(request: Empty = Empty\): Flow<Req>/);
});

test('client streaming is rejected', () => {
  assert.throws(() => parseProto('syntax="proto3"; message A {} service S { rpc R(stream A) returns (A); }'), /client streaming/);
});
