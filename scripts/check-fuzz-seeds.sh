#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CORPUS_DIR="${MODULE_DIR}/goplint/testdata/fuzz"

targets=(
  FuzzInterprocSupergraphConstruction
  FuzzIFDSTabulation
  FuzzProtocolSummaryFactSerialization
  FuzzSSAConstraintNormalizationEvidence
  FuzzSemanticCatalogDecoding
  FuzzFindingDeterminism
  FuzzSemanticCategoryEvidence
)

for target in "${targets[@]}"; do
  if [[ ! -d "${CORPUS_DIR}/${target}" ]]; then
    echo "error: missing committed fuzz corpus for ${target}" >&2
    exit 1
  fi
  if ! find "${CORPUS_DIR}/${target}" -mindepth 1 -maxdepth 1 -type f -print -quit | grep -q .; then
    echo "error: empty committed fuzz corpus for ${target}" >&2
    exit 1
  fi
done

cd "${MODULE_DIR}"
echo "Running deterministic goplint fuzz seed corpora..."
GOCACHE="${GOCACHE:-/tmp/go-build}" "${SCRIPT_DIR}/soundness-go-test.sh" -count=1 ./goplint \
  -run '^(TestFuzzSeedCoverageMatchesAuditMatrix|TestCategoryFuzzSeedEvidence|FuzzInterprocSupergraphConstruction|FuzzIFDSTabulation|FuzzProtocolSummaryFactSerialization|FuzzSSAConstraintNormalizationEvidence|FuzzSemanticCatalogDecoding|FuzzFindingDeterminism|FuzzSemanticCategoryEvidence)$'
