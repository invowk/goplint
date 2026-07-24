#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"
echo "Running independent protocol oracle and solver-core component corpus..."
GOCACHE="${GOCACHE:-/tmp/go-build}" "${SCRIPT_DIR}/soundness-go-test.sh" -count=1 ./internal/protocoloracle ./goplint \
  -run '^(TestBoundedCorpusCardinalityCensusAndReferenceOutcomes|TestReferenceInterpreterReviewedScenarios|TestManifestDimensionsAreCorpusSensitive|TestCorpusProfilesPartitionDeterministically|TestGeneratedGoCorpusParsesAndTypeChecks|TestGeneratedGoProjectionTraceCoversDeclaredIntegratedDimensions|TestProtocolOracleSolverCoreComponent|TestProtocolOracleIndependence|TestProtocolMetamorphicRelations|TestProtocolOracleEvidence|TestCategoryMetamorphicEvidence)$'
