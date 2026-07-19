// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"slices"
	"testing"
)

func TestDecodeFuzzProgramIntegratesFactsAliasesConstraintsAndCalls(t *testing.T) {
	t.Parallel()

	data := []byte{1, 6, 0, 1, 1, 1, 0, 0, 0, 1}
	program, err := DecodeFuzzProgram(data)
	if err != nil {
		t.Fatalf("DecodeFuzzProgram() error: %v", err)
	}
	wantFeatures := []string{
		"alias-copy",
		"call-sites-1",
		"constraint-sat",
		"initial-fact-validated",
		"matched-return",
		"operation-unresolved",
		"procedures-2",
		"unknown-unresolved",
	}
	features := Features(program)
	for _, feature := range wantFeatures {
		if !slices.Contains(features, feature) {
			t.Errorf("integrated fuzz program omitted feature %q: %v", feature, features)
		}
	}
	if got := Interpret(program, 512).ByIdentity[0]; got != OutcomeInconclusive {
		t.Fatalf("integrated outcome = %q, want %q", got, OutcomeInconclusive)
	}

	tests := []struct {
		name string
		data []byte
		want Outcome
	}{
		{name: "entry fact", data: []byte{0, 6, 0, 1, 1, 1, 0, 0, 0, 0}, want: OutcomeViolation},
		{name: "alias kill", data: []byte{1, 6, 0, 2, 1, 1, 0, 0, 0, 1}, want: OutcomeNone},
		{name: "unsatisfiable constraint", data: []byte{1, 6, 0, 1, 1, 2, 0, 0, 0, 1}, want: OutcomeNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			changed, decodeErr := DecodeFuzzProgram(test.data)
			if decodeErr != nil {
				t.Fatalf("DecodeFuzzProgram() error: %v", decodeErr)
			}
			if changed.Fingerprint() == program.Fingerprint() {
				t.Fatal("semantic dimension change left the integrated fingerprint unchanged")
			}
			if got := Interpret(changed, 512).ByIdentity[0]; got != test.want {
				t.Fatalf("Interpret() outcome = %q, want %q", got, test.want)
			}
		})
	}
}
