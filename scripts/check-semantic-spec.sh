#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"

echo "Running goplint semantic-spec contract tests..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go test ./goplint -run '^TestSemanticSpec'
