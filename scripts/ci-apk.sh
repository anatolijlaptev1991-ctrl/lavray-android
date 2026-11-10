#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# APK helpers for CI (ci.yml and release.yml). Linux only (Android SDK/NDK from the runner).
#
#   version                          print versionName from app/build.gradle.kts
#   find <dir> <abi>                 print the single APK for an ABI in <dir>
#   verify <dir>                     exactly one APK per ABI and no others; size budget per ABI;
#                                    versionCode = base*10 + ABI index (app/build.gradle.kts);
#                                    16 KB alignment (zipalign -P 16 and LOAD segments) for the 64-bit ABIs
#   stage <srcdir> <outdir> [suffix] copy to <outdir>/LAVRay-<version>-<abi><suffix>.apk
#   recompress <dir> [abi...]        better deflate for libgojni.so and classes.dex, then zipalign and re-sign
#   signer <apk> release             fail when the APK is signed with the Android debug key
#
# Environment:
#   APK_BUDGET_MB       "arm64-v8a=58 armeabi-v7a=59 x86_64=62" (1 MB = 1000000 bytes), required by verify
#   ANDROID_HOME        SDK root; ANDROID_NDK_HOME NDK root (set by .github/actions/setup-build)
#   BUILD_TOOLS_VERSION build-tools directory for zipalign and apksigner (default 36.0.0)
#   ZIPALIGN, APKSIGNER, AAPT2, LLVM_READELF   override tool paths
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ALL_ABIS=(arm64-v8a armeabi-v7a x86_64)
ALIGNED_ABIS=(arm64-v8a x86_64)
PAGE=16384

fail() { echo "::error::$*"; exit 1; }
summary() { if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then printf '%s\n' "$*" >> "$GITHUB_STEP_SUMMARY"; fi; }

version() {
  local v
  v="$(sed -n 's/^[[:space:]]*versionName[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$ROOT/app/build.gradle.kts" | head -n1)"
  [ -n "$v" ] || fail "versionName не найден в app/build.gradle.kts"
  printf '%s' "$v"
}

find_apk() {
  local dir="$1" abi="$2" found
  found="$(find "$dir" -maxdepth 1 -type f \( -name "*-$abi-*.apk" -o -name "*-$abi.apk" \) | sort)"
  [ -n "$found" ] || fail "нет APK для $abi в $dir"
  [ "$(printf '%s\n' "$found" | wc -l)" -eq 1 ] || fail "несколько APK для $abi в $dir: $found"
  printf '%s' "$found"
}

sdk_tool() { # name override-var
  local name="$1" override="${2:-}" sdk bt path
  if [ -n "$override" ]; then printf '%s' "$override"; return; fi
  sdk="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  bt="${BUILD_TOOLS_VERSION:-36.0.0}"
  path="$sdk/build-tools/$bt/$name"
  [ -x "$path" ] || fail "нет $path (установите build-tools;$bt)"
  printf '%s' "$path"
}

llvm_readelf() {
  local p="${LLVM_READELF:-${ANDROID_NDK_HOME:-}/toolchains/llvm/prebuilt/linux-x86_64/bin/llvm-readelf}"
  [ -x "$p" ] || fail "нет llvm-readelf: $p (проверьте ANDROID_NDK_HOME)"
  printf '%s' "$p"
}

# versionCode of the APK of an ABI: baseVersionCode * 10 + index (armeabi-v7a 1, arm64-v8a 2, x86_64 3),
# the same rule as androidComponents.onVariants in app/build.gradle.kts.
expected_version_code() { # abi
  local base idx
  base="$(sed -n 's/^val baseVersionCode[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$ROOT/app/build.gradle.kts" | head -n1)"
  [ -n "$base" ] || fail "baseVersionCode не найден в app/build.gradle.kts"
  case "$1" in
    armeabi-v7a) idx=1 ;;
    arm64-v8a) idx=2 ;;
    x86_64) idx=3 ;;
    *) fail "неизвестный ABI $1" ;;
  esac
  printf '%s' $(( base * 10 + idx ))
}

check_version_code() { # apk abi
  local aapt2 got want
  aapt2="$(sdk_tool aapt2 "${AAPT2:-}")"
  got="$("$aapt2" dump badging "$1" | sed -n "s/^package:.* versionCode='\([0-9]*\)'.*/\1/p" | head -n1)"
  want="$(expected_version_code "$2")"
  [ -n "$got" ] || fail "$2: не удалось прочитать versionCode из $1"
  [ "$got" = "$want" ] || fail "$2: versionCode $got, ожидалось $want (base*10+индекс ABI)"
  echo "versionCode $2: $got"
}

budget_bytes() { # abi
  local kv
  for kv in ${APK_BUDGET_MB:-}; do
    if [ "${kv%%=*}" = "$1" ]; then printf '%s' $(( ${kv#*=} * 1000000 )); return; fi
  done
  fail "в APK_BUDGET_MB нет бюджета для $1"
}

check_size() { # apk abi
  local size budget
  size="$(stat -c %s "$1")"; budget="$(budget_bytes "$2")"
  summary "| $2 | $size | $budget |"
  if [ "$size" -gt "$budget" ]; then
    fail "$2: APK $size байт больше бюджета $budget байт (APK_BUDGET_MB)"
  fi
  echo "size $2: $size <= $budget"
}

check_alignment() { # apk abi
  local apk="$1" abi="$2" zipalign readelf tmp so n=0 off
  zipalign="$(sdk_tool zipalign "${ZIPALIGN:-}")"
  readelf="$(llvm_readelf)"
  "$zipalign" -c -P 16 -v 4 "$apk" > /dev/null || fail "$abi: zipalign -c -P 16 не прошёл (APK не выровнен по 16 КБ)"

  tmp="$(mktemp -d)"
  unzip -q -o "$apk" "lib/$abi/*.so" -d "$tmp" || { rm -rf "$tmp"; fail "$abi: в APK нет lib/$abi/*.so"; }
  [ -f "$tmp/lib/$abi/libgojni.so" ] || { rm -rf "$tmp"; fail "$abi: нет lib/$abi/libgojni.so (ядро Go не попало в APK)"; }
  for so in "$tmp/lib/$abi/"*.so; do
    n=$((n + 1))
    while read -r off; do
      if [ $((off)) -lt "$PAGE" ] || [ $((off % PAGE)) -ne 0 ]; then
        rm -rf "$tmp"
        fail "$abi: $(basename "$so"): выравнивание LOAD $off, нужно кратное 0x4000"
      fi
    done < <("$readelf" -lW "$so" | awk '$1 == "LOAD" { print $NF }')
  done
  rm -rf "$tmp"
  echo "alignment $abi: zipalign -P 16 ok, $n libraries with LOAD align >= 0x4000"
}

verify() {
  local dir="$1" abi apk count
  count="$(find "$dir" -maxdepth 1 -type f -name '*.apk' | wc -l)"
  [ "$count" -eq "${#ALL_ABIS[@]}" ] || fail "в $dir $count APK, ожидалось ${#ALL_ABIS[@]} (по одному на ABI, без универсального)"
  summary "### Размер APK"
  summary "| ABI | байт | бюджет |"
  summary "|---|---|---|"
  for abi in "${ALL_ABIS[@]}"; do
    apk="$(find_apk "$dir" "$abi")"
    check_size "$apk" "$abi"
    check_version_code "$apk" "$abi"
  done
  for abi in "${ALIGNED_ABIS[@]}"; do
    apk="$(find_apk "$dir" "$abi")"
    check_alignment "$apk" "$abi"
  done
}

stage() {
  local src="$1" out="$2" suffix="${3:-}" ver abi
  ver="$(version)"
  mkdir -p "$out"
  for abi in "${ALL_ABIS[@]}"; do
    cp "$(find_apk "$src" "$abi")" "$out/LAVRay-$ver-$abi$suffix.apk"
  done
  ls -l "$out"
}

# Better deflate for the two big entries (libgojni.so, classes.dex) with 7-Zip's encoder: -4.0 % on
# arm64-v8a (20 645 226 -> 19 828 938 bytes, docs/MEASUREMENTS.md section 10.6), plain deflate that
# every Android installs. 7-Zip rewrites the central directory and drops the signing block and the
# alignment padding, so the pipeline is: 7z update -> zipalign -P 16 -> apksigner sign (same key) ->
# checks. The original APK is kept unless every check passes and the result is smaller.
# Signing key: the four LAVRAY_KEYSTORE_* variables when all are set, otherwise the Android debug key
# (CI of pull requests; the release job refuses debug-signed APKs in `signer`).
find_7z() {
  local c
  for c in "${SEVENZIP:-}" 7zz 7z 7za; do
    if [ -n "$c" ] && command -v "$c" >/dev/null 2>&1; then command -v "$c"; return; fi
  done
  fail "нет 7-Zip (7zz, 7z или 7za; apt-get install p7zip-full)"
}

# The Android debug keystore of this machine. Where the Android Gradle plugin creates it depends on the environment
# (ANDROID_USER_HOME, ANDROID_PREFS_ROOT, ANDROID_SDK_HOME, the home directory), so it is looked for, not assumed.
# Prints the path, or nothing and returns 1 when there is none. DEBUG_KEYSTORE overrides the search.
find_debug_keystore() {
  local c
  for c in "${DEBUG_KEYSTORE:-}" "${ANDROID_USER_HOME:-}/debug.keystore" "${ANDROID_PREFS_ROOT:-}/.android/debug.keystore"            "${ANDROID_SDK_HOME:-}/.android/debug.keystore" "${HOME:-}/.android/debug.keystore"; do
    if [ -n "$c" ] && [ -f "$c" ]; then printf '%s' "$c"; return 0; fi
  done
  for c in "${HOME:-}" "${ANDROID_HOME:-}" "${GRADLE_USER_HOME:-}" "${RUNNER_TEMP:-}" "${GITHUB_WORKSPACE:-}" /tmp; do
    [ -n "$c" ] && [ -d "$c" ] || continue
    c="$(find "$c" -maxdepth 5 -type f -name debug.keystore 2>/dev/null | head -n1 || true)"
    if [ -n "$c" ]; then printf '%s' "$c"; return 0; fi
  done
  return 1
}

recompress_one() { # apk
  local apk="$1" sz tmp entries zipalign apksigner before after cert_before cert_after
  local -a sign files
  sz="$(find_7z)"; zipalign="$(sdk_tool zipalign "${ZIPALIGN:-}")"; apksigner="$(sdk_tool apksigner "${APKSIGNER:-}")"
  if [ -n "${LAVRAY_KEYSTORE_PATH:-}" ] && [ -n "${LAVRAY_KEYSTORE_PASSWORD:-}" ] && [ -n "${LAVRAY_KEY_ALIAS:-}" ] && [ -n "${LAVRAY_KEY_PASSWORD:-}" ]; then
    sign=(--ks "$LAVRAY_KEYSTORE_PATH" --ks-pass env:LAVRAY_KEYSTORE_PASSWORD --ks-key-alias "$LAVRAY_KEY_ALIAS" --key-pass env:LAVRAY_KEY_PASSWORD)
  else
    local debug_ks
    debug_ks="$(find_debug_keystore)" || fail "нет отладочного хранилища ключей debug.keystore (искал в ANDROID_USER_HOME, ANDROID_PREFS_ROOT, ANDROID_SDK_HOME, HOME, ANDROID_HOME, GRADLE_USER_HOME, RUNNER_TEMP, GITHUB_WORKSPACE, /tmp); задайте DEBUG_KEYSTORE"
    echo "recompress: отладочный ключ из $debug_ks"
    sign=(--ks "$debug_ks" --ks-pass pass:android --ks-key-alias androiddebugkey --key-pass pass:android)
  fi
  tmp="$(mktemp -d)"
  before="$(stat -c %s "$apk")"
  cert_before="$("$apksigner" verify --print-certs "$apk" | grep 'certificate SHA-256 digest')" || { rm -rf "$tmp"; fail "apksigner verify не прошёл до пережатия: $apk"; }
  mkdir -p "$tmp/x"
  unzip -q "$apk" 'lib/*/libgojni.so' 'classes*.dex' -d "$tmp/x" || { rm -rf "$tmp"; fail "$(basename "$apk"): нет libgojni.so или classes.dex"; }
  entries="$(cd "$tmp/x" && find . -type f | sed 's|^\./||' | sort)"
  cp "$apk" "$tmp/out.apk"
  readarray -t files <<<"$entries"
  (cd "$tmp/x" && "$sz" a -tzip -mx=9 -mm=Deflate -mfb=258 -mpass=15 -mtc=off ../out.apk "${files[@]}" > "$tmp/7z.log") \
    || { cat "$tmp/7z.log"; rm -rf "$tmp"; fail "7-Zip не обновил $(basename "$apk")"; }
  "$zipalign" -f -P 16 4 "$tmp/out.apk" "$tmp/aligned.apk" || { rm -rf "$tmp"; fail "zipalign не выровнял $(basename "$apk")"; }
  "$apksigner" sign "${sign[@]}" --v1-signing-enabled false --v4-signing-enabled false "$tmp/aligned.apk" \
    || { rm -rf "$tmp"; fail "apksigner sign не подписал $(basename "$apk")"; }
  "$apksigner" verify "$tmp/aligned.apk" || { rm -rf "$tmp"; fail "apksigner verify не прошёл после пережатия: $(basename "$apk")"; }
  "$zipalign" -c -P 16 4 "$tmp/aligned.apk" || { rm -rf "$tmp"; fail "zipalign -c -P 16 не прошёл после пережатия: $(basename "$apk")"; }
  unzip -v "$tmp/aligned.apk" | awk '$NF == "resources.arsc" && $2 == "Stored" { ok = 1 } END { exit !ok }' \
    || { rm -rf "$tmp"; fail "$(basename "$apk"): resources.arsc больше не хранится без сжатия"; }
  [ "$(unzip -Z1 "$tmp/aligned.apk" | sort | uniq -d | wc -l)" -eq 0 ] || { rm -rf "$tmp"; fail "$(basename "$apk"): дубликаты записей после пережатия"; }
  cert_after="$("$apksigner" verify --print-certs "$tmp/aligned.apk" | grep 'certificate SHA-256 digest')"
  [ "$cert_before" = "$cert_after" ] || { rm -rf "$tmp"; fail "$(basename "$apk"): сертификат подписи изменился при пережатии"; }
  after="$(stat -c %s "$tmp/aligned.apk")"
  if [ "$after" -lt "$before" ]; then
    mv "$tmp/aligned.apk" "$apk"
    echo "recompress $(basename "$apk"): $before -> $after байт (-$((before - after)))"
  else
    echo "recompress $(basename "$apk"): $before -> $after, не меньше, оставлен исходный"
  fi
  rm -rf "$tmp"
}

recompress() { # dir [abi...]
  local dir="$1" abi; shift
  local -a abis=("$@")
  [ "${#abis[@]}" -gt 0 ] || abis=("${ALL_ABIS[@]}")
  for abi in "${abis[@]}"; do recompress_one "$(find_apk "$dir" "$abi")"; done
}

signer() {
  local apk="$1" mode="${2:-release}" apksigner certs
  apksigner="$(sdk_tool apksigner "${APKSIGNER:-}")"
  certs="$("$apksigner" verify --print-certs "$apk")" || fail "apksigner verify не прошёл: $apk"
  if [ "$mode" = release ] && grep -q 'CN=Android Debug' <<<"$certs"; then
    fail "$(basename "$apk") подписан отладочным ключом Android, публикация отменена"
  fi
  grep -E 'certificate (DN|SHA-256 digest)' <<<"$certs" | sed "s/^/  /"
}

case "${1:-}" in
  version) version ;;
  find) find_apk "${2:?dir}" "${3:?abi}" ;;
  verify) verify "${2:?dir}" ;;
  stage) stage "${2:?srcdir}" "${3:?outdir}" "${4:-}" ;;
  recompress) recompress "${2:?dir}" "${@:3}" ;;
  signer) signer "${2:?apk}" "${3:-release}" ;;
  *) echo "usage: ci-apk.sh version|find|verify|stage|recompress|signer" >&2; exit 2 ;;
esac
