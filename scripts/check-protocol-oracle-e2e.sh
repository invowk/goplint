#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Running blocking generated-Go analyzer oracle..."
GOCACHE="${GOCACHE:-/tmp/go-build}" "${SCRIPT_DIR}/soundness-go-test.sh" -count=1 ./goplint \
  -run '^(TestProtocolOracleGeneratedGoEndToEnd|TestCategoryGeneratedEvidence)$'
