// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestWriteRefinementTraceToSinkWritesTraceRecord(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 64)
	pass := &analysis.Pass{Fset: fset, Report: func(analysis.Diagnostic) {}}
	restore := installDiagnosticReporter(pass, path)
	defer restore()
	result := interprocPathResult{
		WitnessRecord: cfgWitnessRecord{
			Category:    CategoryMissingConstructorValidate,
			FindingID:   "id-trace",
			CFGPath:     []int32{0, 2},
			WitnessHash: "cfgw1_deadbeef",
		},
		Refinement: cfgProtocolRefinementResult{
			Enabled:              true,
			FeasibilityEngine:    cfgSSAConstraintsEngine,
			FeasibilityResult:    cfgFeasibilityResultSAT,
			RefinementStatus:     cfgRefinementStatusViolation,
			RefinementIterations: 1,
			RefinementTrigger:    cfgRefinementTriggerUnsafeCandidate,
			WitnessHash:          "cfgw1_deadbeef",
		},
	}

	writeRefinementTraceToSink(pass, file.Pos(12), result)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read findings stream: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("JSONL records = %d, want 1", len(lines))
	}
	var record FindingStreamRecord
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("decode JSONL record: %v", err)
	}
	if record.Kind != findingStreamKindRefinementTrace ||
		record.Category != CategoryMissingConstructorValidate ||
		record.ID != "id-trace" || record.Message != cfgRefinementStatusViolation {
		t.Fatalf("refinement trace identity = %+v", record)
	}
	if record.Meta["cfg_refinement_status"] != cfgRefinementStatusViolation ||
		record.Meta["cfg_refinement_witness_hash"] != "cfgw1_deadbeef" ||
		record.Meta["cfg_witness_blocks"] != "0,2" {
		t.Fatalf("refinement trace metadata = %v", record.Meta)
	}
}

func TestWriteRefinementTraceToSinkSkipsWhenRefinementDisabled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "findings.jsonl")
	fset := token.NewFileSet()
	file := fset.AddFile("fixture.go", -1, 8)
	pass := &analysis.Pass{Fset: fset, Report: func(analysis.Diagnostic) {}}
	restore := installDiagnosticReporter(pass, path)
	defer restore()

	writeRefinementTraceToSink(pass, file.Pos(1), interprocPathResult{})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("disabled refinement trace stat error = %v, want nonexistent file", err)
	}
}
