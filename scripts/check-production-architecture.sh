#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Checking absence of alternate goplint production semantics..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-census \
  -manifest testdata/subgates/architecture.v1.json
