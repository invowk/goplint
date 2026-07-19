#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Checking mixed-category inconclusive suppression surfaces..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go test -count=1 . ./goplint \
  -run '^(TestInconclusiveSuppressionOrchestration|TestMixedCategoryInconclusiveBlocksAllSuppressionSurfaces)$'
