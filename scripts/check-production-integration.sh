#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Running real-analyzer protocol production integration..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-census \
  -manifest testdata/subgates/production-integration.v1.json
