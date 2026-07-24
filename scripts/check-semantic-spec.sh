#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"

echo "Running goplint semantic-spec contract tests..."
GOCACHE="${GOCACHE:-/tmp/go-build}" "${SCRIPT_DIR}/soundness-go-test.sh" ./goplint \
	-run '^(TestSemanticSpec|TestValidateSemantic|TestSemanticCoverage|TestSemanticEvidence)'

echo "Emitting deterministic goplint semantic coverage census..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go run ./cmd/catalog-census \
	-catalog spec/semantic-rules.v1.json \
	-registry spec/semantic-evidence.v2.json
