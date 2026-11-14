#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-only
# Тесты ядра core/ в копии build/go/core (go vet и go test, см. run_tests в build-core.sh).
set -euo pipefail
STAGE=test exec bash "$(dirname "${BASH_SOURCE[0]}")/build-core.sh"
