#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Юнит-тесты core-overlay в копии ядра (go vet и go test ./mobile).
set -euo pipefail
STAGE=test exec bash "$(dirname "${BASH_SOURCE[0]}")/build-core.sh"
