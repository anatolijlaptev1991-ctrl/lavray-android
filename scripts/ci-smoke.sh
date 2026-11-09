#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# Emulator smoke test, run by reactivecircus/android-emulator-runner (adb on PATH, emulator booted).
# Needs the x86_64 DEBUG APK: its debug-only receiver lets the script ask the Go core for its status.
#
#   bash scripts/ci-smoke.sh <debug-apk>
#
# Checks: install; ACTIVATE_VPN granted through appops; MainActivity starts; the :vpn process exists
# (the activity binds LavrayVpnService, which starts the core); the core answers CoreService/GetStatus
# with HTTP 200 over its unix socket; no crash of the app in the logcat crash buffer.
# The logcat is written to $SMOKE_LOG (default smoke-logcat.txt) for the artifact.
set -euo pipefail

APK="${1:?usage: ci-smoke.sh <apk>}"
PKG="io.github.anatolijlaptev1991_ctrl.lavray"
LOG="${SMOKE_LOG:-smoke-logcat.txt}"

dump_log() { adb logcat -d -v threadtime > "$LOG" 2>&1 || true; }
trap dump_log EXIT
fail() { echo "::error::smoke: $*"; exit 1; }

wait_pid() { # process name, seconds
  local i
  for i in $(seq 1 "$2"); do
    if adb shell pidof "$1" | grep -qE '^[0-9]+'; then return 0; fi
    sleep 1
  done
  return 1
}

adb wait-for-device
until [ "$(adb shell getprop sys.boot_completed | tr -d '\r')" = 1 ]; do sleep 2; done

adb install -r -t "$APK"
adb shell appops set "$PKG" ACTIVATE_VPN allow
adb shell pm grant "$PKG" android.permission.POST_NOTIFICATIONS || true
adb logcat -c

started="$(adb shell am start -W -n "$PKG/.ui.MainActivity" | tr -d '\r')"
echo "$started"
grep -q '^Status: ok' <<<"$started" || fail "MainActivity не запустилась"
wait_pid "$PKG" 20 || fail "нет процесса интерфейса $PKG"
wait_pid "$PKG:vpn" 40 || fail "нет процесса $PKG:vpn (служба не поднялась)"

ok=0
for i in $(seq 1 15); do
  adb shell am broadcast -n "$PKG/.debug.DebugApiReceiver" -a lavray.debug.API \
    --es path /lavray.v1.CoreService/GetStatus --es b64 e30= > /dev/null
  sleep 2
  if adb logcat -d -s LVDBG:I | grep -q 'RESULT /lavray.v1.CoreService/GetStatus status=200'; then ok=1; break; fi
  echo "ядро ещё не отвечает (попытка $i)"
done
[ "$ok" -eq 1 ] || fail "ядро в процессе :vpn не ответило на GetStatus"

# Both processes still alive, and nothing of ours in the crash buffer.
wait_pid "$PKG" 1 || fail "процесс интерфейса завершился"
wait_pid "$PKG:vpn" 1 || fail "процесс :vpn завершился"
if adb logcat -d -b crash | grep -q "$PKG"; then fail "в буфере crash есть сбой приложения"; fi
echo "smoke: ok"
