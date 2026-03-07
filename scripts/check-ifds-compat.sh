#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"

echo "Running goplint IFDS compatibility gate..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go test ./goplint -run '^TestIFDSCompatNoSilentDowngrade$'
