#!/usr/bin/env bash
set -euo pipefail

parallel_args=()
if [[ -n "${GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM:-}" ]]; then
  if [[ ! "${GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM}" =~ ^[1-9][0-9]*$ ]]; then
    echo "invalid GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM=${GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM@Q}: want a positive integer" >&2
    exit 2
  fi
  parallel_args+=("-parallel=${GOPLINT_SOUNDNESS_GO_TEST_PARALLELISM}")
fi

exec go test "${parallel_args[@]}" "$@"
