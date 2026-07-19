#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Validating the manifest-derived scheduled protocol oracle corpus..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go test -count=1 ./internal/protocoloracle \
  -run '^TestScheduledProfileIsStrictBlockingSuperset$'

echo "Comparing the scheduled protocol oracle corpus with the production analyzer..."
GOPLINT_PROTOCOL_ORACLE_PROFILE=scheduled \
  GOCACHE="${GOCACHE:-/tmp/go-build}" \
  go test -count=1 ./goplint -run '^TestProtocolOracleScheduledGeneratedGo$'
