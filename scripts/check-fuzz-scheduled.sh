#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
FUZZ_TIME="${GOPLINT_FUZZ_TIME:-1m}"

targets=(
  FuzzInterprocSupergraphConstruction
  FuzzIFDSTabulation
  FuzzProtocolSummaryFactSerialization
  FuzzSSAConstraintNormalizationEvidence
  FuzzSemanticCatalogDecoding
  FuzzFindingDeterminism
  FuzzSemanticCategoryEvidence
)

cd "${MODULE_DIR}"
for target in "${targets[@]}"; do
  echo "Running bounded fuzz target ${target} for ${FUZZ_TIME}..."
  "${SCRIPT_DIR}/soundness-go-test.sh" ./goplint -run '^$' -fuzz="^${target}$" -fuzztime="${FUZZ_TIME}" -timeout=35m
done
