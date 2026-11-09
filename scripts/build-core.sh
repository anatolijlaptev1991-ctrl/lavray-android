#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# Сборка Go-ядра LAVRay в app/libs/lavray-core.aar (gomobile bind).
# Работает в Git Bash на Windows и в Linux CI. Субмодуль third_party/lavray не
# меняется: ядро копируется в build/go/core, туда же кладётся core-overlay
# (пакет mobile) и добавляется зависимость gomobile (go get и -mod=mod нужны
# только в этой копии).
#
# Переменные окружения (все необязательные):
#   ABIS             цели gomobile (по умолчанию android/arm64,android/arm,android/amd64)
#   OUT_AAR          путь результата (по умолчанию app/libs/lavray-core.aar)
#   ANDROID_API      -androidapi (по умолчанию 26)
#   ANDROID_NDK_HOME каталог NDK (по умолчанию <SDK>/ndk/28.2.13676358)
#   EXTRA_TAGS       теги сверх core/BUILD_TAGS (по умолчанию with_low_memory)
#   EXT_LDFLAGS      доп. флаги компоновщика через запятую (по умолчанию --pack-dyn-relocs=android:
#                    -5 МБ на ABI; пустое значение отключает)
#   STAGE            prepare | test | bind | all (по умолчанию all)
#   GOMOBILE_VERSION версия github.com/sagernet/gomobile (по умолчанию v0.1.12)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/third_party/lavray/core"
BUILD="$ROOT/build/go"
COPY="$BUILD/core"
BIN="$BUILD/bin"

ABIS="${ABIS:-android/arm64,android/arm,android/amd64}"
OUT_AAR="${OUT_AAR:-$ROOT/app/libs/lavray-core.aar}"
ANDROID_API="${ANDROID_API:-26}"
EXTRA_TAGS="${EXTRA_TAGS-with_low_memory}"
EXT_LDFLAGS="${EXT_LDFLAGS---pack-dyn-relocs=android}"
STAGE="${STAGE:-all}"
GOMOBILE_VERSION="${GOMOBILE_VERSION:-v0.1.12}"
NDK_VERSION="${NDK_VERSION:-28.2.13676358}"

# Путь в форме, понятной Go и Java: на Windows C:/..., иначе без изменений.
native_path() { if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi; }

find_ndk() {
  if [ -n "${ANDROID_NDK_HOME:-}" ]; then printf '%s' "$ANDROID_NDK_HOME"; return; fi
  local sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  if [ -z "$sdk" ] && [ -n "${LOCALAPPDATA:-}" ]; then sdk="$LOCALAPPDATA/Android/Sdk"; fi
  if [ -z "$sdk" ]; then sdk="$HOME/Android/Sdk"; fi
  printf '%s' "$sdk/ndk/$NDK_VERSION"
}

prepare() {
  [ -f "$SRC/go.mod" ] || { echo "нет $SRC (git submodule update --init)" >&2; exit 1; }
  echo "==> копия ядра в $COPY"
  rm -rf "$COPY"
  mkdir -p "$BUILD"
  cp -r "$SRC" "$COPY"
  mkdir -p "$COPY/mobile"
  cp "$ROOT"/core-overlay/*.go "$COPY/mobile/"
  (
    cd "$COPY"
    export GOFLAGS=-mod=mod
    go get "github.com/sagernet/gomobile@$GOMOBILE_VERSION"
    mkdir -p "$BIN"
    go build -o "$BIN/gomobile$(go env GOEXE)" github.com/sagernet/gomobile/cmd/gomobile
    go build -o "$BIN/gobind$(go env GOEXE)" github.com/sagernet/gomobile/cmd/gobind
  )
}

tags() {
  local t
  t="$(tr -d '\r\n ' < "$SRC/BUILD_TAGS")"
  if [ -n "$EXTRA_TAGS" ]; then t="$t,$EXTRA_TAGS"; fi
  printf '%s' "$t"
}

run_tests() {
  echo "==> go vet и go test ./mobile (теги: $(tags))"
  (
    cd "$COPY"
    export GOFLAGS=-mod=mod
    go vet -tags "$(tags)" ./mobile/
    go test -count=1 -tags "$(tags)" ./mobile/
  )
}

bind() {
  local ndk; ndk="$(find_ndk)"
  [ -d "$ndk" ] || { echo "NDK не найден: $ndk (задайте ANDROID_NDK_HOME)" >&2; exit 1; }
  export ANDROID_NDK_HOME; ANDROID_NDK_HOME="$(native_path "$ndk")"
  export PATH="$BIN:$PATH"
  mkdir -p "$(dirname "$OUT_AAR")"
  echo "==> gomobile bind: $ABIS, api $ANDROID_API, теги $(tags)"
  (
    cd "$COPY"
    export GOFLAGS=-mod=mod
    # 16 KB: max-page-size для Android 15+ (NDK r28 делает это сам, флаг закрепляет).
    gomobile bind -target "$ABIS" -androidapi "$ANDROID_API" -javapkg=lavray.core \
      -tags "$(tags)" -trimpath \
      -ldflags "-s -w -buildid= -extldflags=-Wl,-z,max-page-size=16384${EXT_LDFLAGS:+,$EXT_LDFLAGS}" \
      -o "$(native_path "$OUT_AAR")" ./mobile
  )
  report_sizes
}

report_sizes() {
  local tmp="$BUILD/aar-inspect"
  rm -rf "$tmp"; mkdir -p "$tmp"
  (cd "$tmp" && unzip -q -o "$OUT_AAR")
  echo "==> AAR: $(wc -c < "$OUT_AAR") байт"
  for so in "$tmp"/jni/*/*.so; do
    echo "    ${so#"$tmp"/jni/}: $(wc -c < "$so") байт"
  done
}

case "$STAGE" in
  prepare) prepare ;;
  test) prepare; run_tests ;;
  bind) bind ;;
  all) prepare; run_tests; bind ;;
  *) echo "STAGE: prepare|test|bind|all" >&2; exit 2 ;;
esac
