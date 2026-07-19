#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Running historical goplint soundness counterexamples..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/subgate-census \
  -manifest testdata/subgates/counterexamples.v1.json
