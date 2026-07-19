// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"path/filepath"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
)

func TestProtocolOracleUNSATWitnessIsDischargedEndToEnd(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(
		filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"),
	)
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	wanted := map[string]bool{
		"dimension/constraint/unsat":                                      false,
		"scheduled/integrated/noop/none/copy/none/unsat/needs-validation": false,
		"scheduled/integrated/mutate/none/copy/none/unsat/validated":      false,
	}
	if err := protocoloracle.EnumerateProfile(manifest, "scheduled", func(program protocoloracle.Program) error {
		if _, ok := wanted[program.CaseID]; !ok {
			return nil
		}
		wanted[program.CaseID] = true
		_, compareErr := compareGeneratedGoProgram(t, program, manifest.Scheduled.MaxStates, "")
		return compareErr
	}); err != nil {
		t.Fatal(err)
	}
	for caseID, found := range wanted {
		if !found {
			t.Errorf("generated UNSAT regression case %q is missing", caseID)
		}
	}
}

func TestProtocolOracleUNSATNodeDoesNotPruneSiblingBranch(t *testing.T) {
	t.Parallel()

	program, err := protocoloracle.DecodeFuzzProgram([]byte("2"))
	if err != nil {
		t.Fatalf("DecodeFuzzProgram() error: %v", err)
	}
	const maxStates = 512
	reference := protocoloracle.Interpret(program, maxStates)
	if got := reference.Evidence[0].Outcome; got != protocoloracle.OutcomeViolation {
		t.Fatalf("reference identity 0 outcome = %q, want %q", got, protocoloracle.OutcomeViolation)
	}
	if _, err := compareGeneratedGoProgram(t, program, maxStates, ""); err != nil {
		t.Fatalf("compareGeneratedGoProgram() error: %v", err)
	}
}
