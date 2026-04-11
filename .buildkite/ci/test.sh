#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SCRIPT_DIR

# shellcheck source=.buildkite/ci/setup-env.sh
source "${SCRIPT_DIR}/setup-env.sh"

mkdir -p reports/tests coverage

go test -race -covermode=atomic -coverprofile=coverage/unit.out ./... \
  2>&1 | tee reports/tests/go-test.txt
