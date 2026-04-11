#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SCRIPT_DIR

# shellcheck source=.buildkite/ci/setup-env.sh
source "${SCRIPT_DIR}/setup-env.sh"

mkdir -p dist reports/build

go build -buildvcs=true -o dist/buildkite . \
  2>&1 | tee reports/build/go-build.txt
