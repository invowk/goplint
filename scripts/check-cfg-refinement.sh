#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MODULE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${MODULE_DIR}"

echo "Running goplint Phase C refinement gate..."
GOCACHE="${GOCACHE:-/tmp/go-build}" go test ./goplint -run '^(TestCFGSMTFeasibilityBackend|TestCFGWitnessHashDeterministic|TestWriteRefinementTraceToSinkWritesTraceRecord|TestWriteRefinementTraceToSinkSkipsWhenPhaseCDisabled|TestParseFindingsJSONLIgnoresNonFindingKinds|TestPhaseCRefinementGate)$'
