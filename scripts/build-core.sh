#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# Сборка Go-ядра LAVRay в app/libs/lavray-core.aar (gomobile bind).
# Работает в Git Bash на Windows и в Linux CI. Ядро лежит в core/ этого репозитория
# (пакет mobile — мост с Android). Сборка идёт в копии build/go/core: туда добавляется
# зависимость gomobile и подменяется modernc.org/libc (go get, -mod=mod и replace
# меняют go.mod только копии, core/ не трогается).
#
# Переменные окружения (все необязательные):
#   ABIS             цели gomobile (по умолчанию android/arm64,android/arm,android/amd64)
#   OUT_AAR          путь результата (по умолчанию app/libs/lavray-core.aar)
#   ANDROID_API      -androidapi (по умолчанию 26)
#   ANDROID_NDK_HOME каталог NDK (по умолчанию <SDK>/ndk/28.2.13676358)
#   EXTRA_TAGS       теги сверх core/BUILD_TAGS (по умолчанию пусто; with_low_memory снят: он ставит
#                    UDP-буфер 8 КиБ при MTU tun 9000, см. docs/MEASUREMENTS.md, раздел 10)
#   SINGBOX_DROP     протоколы sing-box, убираемые из реестра сверх обязательного набора
#                    (по умолчанию tor; для замеров: "tor ssh snell anytls shadowtls")
#   GOMOBILE_EXTRA   доп. флаги gomobile bind для замеров (например -v: вывод компоновщика)
#   GO_LDFLAGS_EXTRA доп. флаги go link для замеров (например -dumpdep), по умолчанию пусто
#   EXT_LDFLAGS      доп. флаги компоновщика через запятую (по умолчанию --pack-dyn-relocs=android:
#                    -5 МБ на ABI; пустое значение отключает)
#   STAGE            prepare | test | bind | all (по умолчанию all)
#   GOMOBILE_VERSION версия github.com/sagernet/gomobile (по умолчанию v0.1.12)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/core"
BUILD="$ROOT/build/go"
COPY="$BUILD/core"
LIBC="$BUILD/libc"
SINGBOX="$BUILD/sing-box"
BIN="$BUILD/bin"

ABIS="${ABIS:-android/arm64,android/arm,android/amd64}"
OUT_AAR="${OUT_AAR:-$ROOT/app/libs/lavray-core.aar}"
ANDROID_API="${ANDROID_API:-26}"
EXTRA_TAGS="${EXTRA_TAGS-}"
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

# modernc.org/libc (SQLite) issues legacy x86-64 system calls (open, stat, lstat, ...) that
# Android's seccomp filter kills with SIGSYS. The module is copied to build/go/libc, every
# raw system call in its musl glue goes through scripts/libc-android (which rewrites
# them into the *at calls), and the core module is pointed at the copy. Only the build
# copy changes; arm64 and arm never issued these calls, so the rewrite is a no-op there.
patch_libc() {
  local src
  # `go list -m` leaves .Dir empty for a module that is not in the cache yet (a clean CI runner),
  # so the module is downloaded first; with a warm cache this is a no-op and needs no network.
  (cd "$COPY" && GOFLAGS=-mod=mod go mod download modernc.org/libc)
  src="$(cd "$COPY" && GOFLAGS=-mod=mod go list -m -f '{{.Dir}}' modernc.org/libc)"
  [ -f "$src/syscall_musl.go" ] || { echo "нет syscall_musl.go в $src (libc обновлён?)" >&2; exit 1; }
  rm -rf "$LIBC"
  cp -r "$src" "$LIBC"
  chmod -R u+w "$LIBC"
  cp -r "$ROOT/scripts/libc-android/androidcompat" "$LIBC/"
  cp "$ROOT"/scripts/libc-android/rawsys_*.go "$LIBC/"
  sed -i -e 's/unix.Syscall6(/rawSyscall6(/g' -e 's/unix.Syscall(/rawSyscall(/g' "$LIBC/syscall_musl.go"
  if grep -q 'unix.Syscall' "$LIBC/syscall_musl.go"; then
    echo "syscall_musl.go: остались прямые unix.Syscall" >&2; exit 1
  fi
  (cd "$COPY" && GOFLAGS=-mod=mod go mod edit -replace "modernc.org/libc=$(native_path "$LIBC")")
}

# Реестр sing-box (include/registry.go) регистрирует ВСЕ протоколы и службы без условий сборки,
# поэтому линкуется то, чем приложение не пользуется: gRPC-сервер service/api (grpc, daemon,
# x/net/trace -> text/template и html/template), ssmapi, origin_ca (certmagic, zap, acmez),
# resolved (godbus), bridge (nftables/netlink), Tor (нужен внешний бинарь tor, на Android
# невозможен). Модуль sing-box копируется из кэша в build/go/sing-box, регистрации удаляются,
# ядро указывает на копию через replace (как libc). Нулевой допуск: если ожидаемой строки нет
# (пин sing-box обновлён), сборка падает, а не пропускает патч молча.
#
# SINGBOX_DROP — дополнительные протоколы (имена пакетов protocol/<имя>), для замеров размера:
# SINGBOX_DROP="ssh snell anytls shadowtls". По умолчанию только tor; остальные доступны через
# пользовательский JSON, их удаление решает владелец (docs/MEASUREMENTS.md).
SINGBOX_DROP="${SINGBOX_DROP-tor}"
SB_PKG='github.com/sagernet/sing-box'

# drop_exact <файл> <строка>: удаляет строку, равную <строке> целиком; ровно одно совпадение.
drop_exact() {
  local f="$1" line="$2" n
  n="$(grep -cxF -- "$line" "$f" || true)"
  [ "$n" = 1 ] || {
    echo "patch_singbox: в $(basename "$f") ожидалась ровно одна строка «${line#"${line%%[![:space:]]*}"}», найдено $n (пин sing-box обновлён? обновите patch_singbox)" >&2
    exit 1
  }
  grep -vxF -- "$line" "$f" > "$f.new"
  mv "$f.new" "$f"
}

# drop_register <файл> <пакет>: удаляет строки <пакет>.Register*(registry) (одну и более).
drop_register() {
  local f="$1" pkg="$2" n
  n="$(grep -cE "^[[:space:]]*${pkg}\\.Register[A-Za-z]*\\(registry\\)\$" "$f" || true)"
  [ "$n" -ge 1 ] || { echo "patch_singbox: в $(basename "$f") нет регистраций ${pkg}.Register*" >&2; exit 1; }
  sed -i -E "/^[[:space:]]*${pkg}\\.Register[A-Za-z]*\\(registry\\)\$/d" "$f"
}

patch_singbox() {
  local src reg tab=$'\t' p
  (cd "$COPY" && GOFLAGS=-mod=mod go mod download "$SB_PKG")
  src="$(cd "$COPY" && GOFLAGS=-mod=mod go list -m -f '{{.Dir}}' "$SB_PKG")"
  [ -f "$src/include/registry.go" ] || { echo "нет include/registry.go в $src (sing-box обновлён?)" >&2; exit 1; }
  rm -rf "$SINGBOX"
  cp -r "$src" "$SINGBOX"
  chmod -R u+w "$SINGBOX"
  reg="$SINGBOX/include/registry.go"
  sed -i 's/\r$//' "$reg"

  drop_exact "$reg" "${tab}\"$SB_PKG/service/api\""
  drop_exact "$reg" "${tab}originca \"$SB_PKG/service/origin_ca\""
  drop_exact "$reg" "${tab}\"$SB_PKG/service/resolved\""
  drop_exact "$reg" "${tab}\"$SB_PKG/service/ssmapi\""
  drop_exact "$reg" "${tab}\"$SB_PKG/protocol/bridge\""
  drop_exact "$reg" "${tab}api.RegisterService(registry)"
  drop_exact "$reg" "${tab}resolved.RegisterService(registry)"
  drop_exact "$reg" "${tab}resolved.RegisterTransport(registry)"
  drop_exact "$reg" "${tab}ssmapi.RegisterService(registry)"
  drop_exact "$reg" "${tab}bridge.RegisterOutbound(registry)"
  drop_exact "$reg" "${tab}originca.RegisterCertificateProvider(registry)"
  for p in $SINGBOX_DROP; do
    drop_exact "$reg" "${tab}\"$SB_PKG/protocol/$p\""
    drop_register "$reg" "$p"
  done

  if grep -nE '\b(api|resolved|ssmapi|originca|bridge)\.Register' "$reg" >/dev/null; then
    echo "patch_singbox: в registry.go остались регистрации удалённых служб" >&2; exit 1
  fi
  (cd "$COPY" && GOFLAGS=-mod=mod go mod edit -replace "$SB_PKG=$(native_path "$SINGBOX")")
  echo "==> sing-box: из реестра убраны api, ssmapi, origin_ca, resolved, bridge${SINGBOX_DROP:+, $SINGBOX_DROP}"
}

prepare() {
  [ -f "$SRC/go.mod" ] || { echo "нет $SRC/go.mod" >&2; exit 1; }
  echo "==> копия ядра в $COPY"
  rm -rf "$COPY"
  mkdir -p "$BUILD"
  cp -r "$SRC" "$COPY"
  (
    cd "$COPY"
    export GOFLAGS=-mod=mod
    go get "github.com/sagernet/gomobile@$GOMOBILE_VERSION"
    mkdir -p "$BIN"
    go build -o "$BIN/gomobile$(go env GOEXE)" github.com/sagernet/gomobile/cmd/gomobile
    go build -o "$BIN/gobind$(go env GOEXE)" github.com/sagernet/gomobile/cmd/gobind
  )
  patch_libc
  patch_singbox
}

tags() {
  local t
  t="$(tr -d '\r\n ' < "$SRC/BUILD_TAGS")"
  if [ -n "$EXTRA_TAGS" ]; then t="$t,$EXTRA_TAGS"; fi
  printf '%s' "$t"
}

run_tests() {
  # Ядро теперь своё (core/), поэтому проверяется целиком, а не только мост.
  echo "==> go vet и go test всего ядра (теги: $(tags))"
  (
    cd "$COPY"
    export GOFLAGS=-mod=mod
    go vet -tags "$(tags)" ./...
    go test -count=1 -tags "$(tags)" ./...
  )
  (cd "$LIBC" && GOFLAGS=-mod=mod go test -count=1 ./androidcompat/)
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
    gomobile bind ${GOMOBILE_EXTRA:-} -target "$ABIS" -androidapi "$ANDROID_API" -javapkg=lavray.core \
      -tags "$(tags)" -trimpath \
      -ldflags "-s -w -buildid= ${GO_LDFLAGS_EXTRA:+$GO_LDFLAGS_EXTRA }-extldflags=-Wl,-z,max-page-size=16384${EXT_LDFLAGS:+,$EXT_LDFLAGS}" \
      -o "$(native_path "$OUT_AAR")" ./mobile
  )
  report_sizes
}

# Охрана от возврата: если в линковку попадает функция, вызывающая reflect Method/MethodByName с
# непостоянным именем (text/template, godbus, ...), компоновщик Go перестаёт вырезать неиспользуемые
# методы, и библиотека растёт на 6,4 МБ (arm64, замер docs/MEASUREMENTS.md, раздел 10.2). Имена
# функций остаются в .gopclntab и после -s -w; пробы ниже — экспортируемые методы стандартной
# библиотеки, которые ядро не вызывает: они есть в библиотеке, только когда вырезание выключено.
# Падаем, если присутствуют минимум три из четырёх. Отключение: SKIP_DCE_CHECK=1.
DCE_PROBES=('net/http.(*Cookie).Valid' 'log.(*Logger).Panicln' 'log.(*Logger).Fatalln' 'bufio.(*Writer).AvailableBuffer')

check_method_dce() { # каталог распакованного AAR
  local so p n
  [ "${SKIP_DCE_CHECK:-0}" = 1 ] && return 0
  for so in "$1"/jni/*/libgojni.so; do
    n=0
    for p in "${DCE_PROBES[@]}"; do
      if grep -a -q -F -- "$p" "$so"; then n=$((n + 1)); fi
    done
    if [ "$n" -ge 3 ]; then
      echo "ВЫРЕЗАНИЕ МЕТОДОВ ВЫКЛЮЧЕНО в ${so#"$1"/jni/}: найдено $n из ${#DCE_PROBES[@]} неиспользуемых методов std." >&2
      echo "Что-то в линковке вызывает reflect Method/MethodByName с непостоянным именем; найдите его:" >&2
      echo "  GOMOBILE_EXTRA=-v GO_LDFLAGS_EXTRA=-dumpdep ABIS=android/arm64 STAGE=bind bash scripts/build-core.sh 2> dump.txt; grep -a '<ReflectMethod>' dump.txt" >&2
      exit 1
    fi
  done
  echo "==> вырезание методов Go включено"
}

report_sizes() {
  local tmp="$BUILD/aar-inspect"
  rm -rf "$tmp"; mkdir -p "$tmp"
  (cd "$tmp" && unzip -q -o "$OUT_AAR")
  echo "==> AAR: $(wc -c < "$OUT_AAR") байт"
  for so in "$tmp"/jni/*/*.so; do
    echo "    ${so#"$tmp"/jni/}: $(wc -c < "$so") байт"
  done
  check_method_dce "$tmp"
}

case "$STAGE" in
  prepare) prepare ;;
  test) prepare; run_tests ;;
  bind) bind ;;
  all) prepare; run_tests; bind ;;
  *) echo "STAGE: prepare|test|bind|all" >&2; exit 2 ;;
esac
