#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(
  CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd
)"
readonly SCRIPT_DIR

# shellcheck source=.buildkite/ci/setup-env.sh
source "${SCRIPT_DIR}/setup-env.sh"

mkdir -p reports/tests coverage

# Capture output via a file redirect rather than `... | tee`. The Buildkite
# agent's srt-shell-wrapper sometimes closes the pipeline's stdout mid-run,
# which makes `tee` exit with SIGPIPE (status 141) and, with `pipefail`,
# masks `go test`'s real status. Writing straight to the report file then
# replaying it for the agent log keeps both visibility and the actual exit
# status.
status=0
go test -race -covermode=atomic -coverprofile=coverage/unit.out ./... \
  >reports/tests/go-test.txt 2>&1 || status=$?
cat reports/tests/go-test.txt
exit "${status}"
