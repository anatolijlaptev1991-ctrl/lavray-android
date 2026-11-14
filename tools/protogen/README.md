<!-- SPDX-License-Identifier: GPL-3.0-only -->
# protogen

Generates the Kotlin API types from `core/proto/lavray.proto` (the API of the core in `core/`).
Plain Node, no dependencies, no Python.

```
node tools/protogen/protogen.mjs          # rewrites app/src/main/java/.../api/gen/
node --test tools/protogen/protogen.test.mjs              # generator tests
```

Output is committed. Regenerate after changing the proto, then run `gradlew testDebugUnitTest`.

| File | Content |
|---|---|
| `Dto01.kt` ... | `@Serializable` data classes and enums, about 650 lines per file |
| `Services.kt` | one `object` per service with `const val` method paths (unary or server stream noted per method) |
| `ApiCalls.kt` | one typed `LavrayClient` extension per RPC; R8 removes the ones the app never calls |

proto3 JSON rules: camelCase names; int64 as string (file-level `UseSerializers`, numbers also accepted on input);
enums by name; bytes as base64; defaults omitted and unknown keys ignored (`ProtoJson` in `api/Proto3Json.kt`);
`optional` fields and `oneof` members are nullable properties, so presence survives a round trip.
Client streaming is rejected at generation time because the contract has none.
