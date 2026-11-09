#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
#
# Runs ./gradlew with the owner's PC workaround for "Unable to establish loopback
# connection": a short temp directory for the Gradle client, daemon and unix sockets.
# Linux CI does not need this and calls ./gradlew directly.
#
#   LAVRAY_SHORT_TMP=D:/ZCode/.t tools/gradle-local.sh assembleDebug testDebugUnitTest
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="${LAVRAY_SHORT_TMP:-}"
if [ -z "$tmp" ]; then exec "$root/gradlew" "$@"; fi
mkdir -p "$tmp"
export TEMP="$tmp" TMP="$tmp"
exec "$root/gradlew" \
  "-Dorg.gradle.jvmargs=-Xmx2g -Dfile.encoding=UTF-8 -Djdk.net.unixdomain.tmpdir=$tmp -Djava.io.tmpdir=$tmp" \
  "-Pkotlin.daemon.jvm.options=-Djava.io.tmpdir=$tmp,-Djdk.net.unixdomain.tmpdir=$tmp" \
  "$@"
