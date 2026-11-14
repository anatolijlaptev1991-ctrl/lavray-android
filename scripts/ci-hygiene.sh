#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# Repository hygiene gates for CI (ci.yml and release.yml). Pure bash and git, no Android tooling,
# so it also runs locally:  bash scripts/ci-hygiene.sh strings|secrets|authors|all
#
#   strings   the word "Happ" must not appear in generated strings or tools/i18n/android/*.json;
#             "sing-box" is allowed only in the licenses string (resource name lic_core)
#   secrets   no *.jks, *.keystore, *.p12, *.pfx tracked; no private keys or tokens in tracked files
#   authors   every commit in BASE..HEAD has the owner's noreply author email; without a usable
#             BASE (new branch, force push, release tag) the whole history of HEAD is checked
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OWNER_EMAIL="${OWNER_EMAIL:-262499256+anatolijlaptev1991-ctrl@users.noreply.github.com}"
LICENSE_RES_NAME="lic_core"

fail() { echo "::error::$*"; exit 1; }

gate_strings() {
  local files=() f bad
  for f in app/src/main/res/values*/strings.xml tools/i18n/android/*.json; do
    [ -f "$f" ] && files+=("$f")
  done
  [ "${#files[@]}" -gt 0 ] || fail "нет ни одного файла строк (app/src/main/res/values*/strings.xml, tools/i18n/android/*.json)"

  bad="$(grep -nIiw -- 'happ' "${files[@]}" || true)"
  if [ -n "$bad" ]; then
    echo "$bad"
    fail "слово Happ найдено в строках интерфейса"
  fi

  # sing-box: the licenses string is the single permitted place. In strings.xml that is the
  # <string name="lic_core"> line; tools/i18n/android/*.json has no exception.
  bad="$(grep -nIiE -- 'sing[- ]?box' "${files[@]}" | grep -v -- "name=\"$LICENSE_RES_NAME\"" || true)"
  if [ -n "$bad" ]; then
    echo "$bad"
    fail "sing-box найден в строках вне ключа лицензий ($LICENSE_RES_NAME)"
  fi
  echo "strings: ok (${#files[@]} files)"
}

gate_secrets() {
  local bad
  bad="$(git ls-files | grep -Ei '\.(jks|keystore|p12|pfx)$' || true)"
  if [ -n "$bad" ]; then
    echo "$bad"
    fail "в репозитории отслеживается файл ключа (jks/keystore/p12/pfx)"
  fi

  # Simple, deliberately conservative patterns. The tracked tree of this repository only
  # (the whole tracked tree, the Go core in core/ included). This script is excluded from its own scan.
  local pattern
  pattern='-----BEGIN [A-Z ]*PRIVATE KEY-----'
  pattern+='|ghp_[A-Za-z0-9]{36}|gho_[A-Za-z0-9]{36}|ghs_[A-Za-z0-9]{36}|ghu_[A-Za-z0-9]{36}|ghr_[A-Za-z0-9]{36}'
  pattern+='|github_pat_[A-Za-z0-9_]{22,}'
  pattern+='|glpat-[A-Za-z0-9_-]{20,}'
  pattern+='|xox[abprs]-[A-Za-z0-9-]{10,}'
  pattern+='|AKIA[0-9A-Z]{16}'
  pattern+='|AIza[0-9A-Za-z_-]{35}'
  pattern+='|sk-ant-[A-Za-z0-9_-]{20,}'
  pattern+='|(storePassword|keyPassword)[[:space:]]*[=:][[:space:]]*["'"'"'][^"'"'"'$]+["'"'"']'
  bad="$(git grep -nIE -e "$pattern" -- . ':!scripts/ci-hygiene.sh' | cut -c1-120 || true)"
  if [ -n "$bad" ]; then
    # Print file and line only, never the matched secret.
    echo "$bad" | cut -d: -f1,2
    fail "в отслеживаемых файлах найдены закрытый ключ или токен (места выше)"
  fi
  echo "secrets: ok"
}

gate_authors() {
  local base="${BASE:-}" head="${HEAD:-HEAD}" range bad
  if [ -n "$base" ] && ! [[ "$base" =~ ^0+$ ]] && git cat-file -e "${base}^{commit}" 2>/dev/null; then
    range="$base..$head"
  else
    range="$head"
  fi
  bad="$(git log --format='%H %ae' "$range" | awk -v want="$OWNER_EMAIL" '$2 != want { print }' || true)"
  if [ -n "$bad" ]; then
    echo "$bad"
    fail "коммиты с чужой почтой автора (нужна $OWNER_EMAIL)"
  fi
  echo "authors: ok ($(git rev-list --count "$range") commits in $range)"
}

case "${1:-all}" in
  strings) gate_strings ;;
  secrets) gate_secrets ;;
  authors) gate_authors ;;
  all) gate_strings; gate_secrets; gate_authors ;;
  *) echo "usage: ci-hygiene.sh strings|secrets|authors|all" >&2; exit 2 ;;
esac
