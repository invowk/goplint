// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/mutationguard"
)

type mutationGuardObservation = mutationguard.Observation

func requireMutationGuardObservation(
	t *testing.T,
	assertionID string,
	expected,
	actual mutationGuardObservation,
) {
	t.Helper()

	if expected == actual {
		return
	}
	marker, err := mutationguard.EncodeEvent(mutationguard.AssertionEvent{
		FormatVersion: mutationguard.EventFormatVersion,
		AssertionID:   assertionID,
		Expected:      expected,
		Actual:        actual,
	})
	if err != nil {
		t.Fatalf("encode mutation guard mismatch: %v", err)
	}
	t.Fatal(marker)
}

func mutationGuardState(subject, state string) mutationGuardObservation {
	return mutationGuardObservation{Subject: subject, State: state}
}

func TestMutationGuardMismatchEncodingIsStable(t *testing.T) {
	t.Parallel()

	marker, err := mutationguard.EncodeEvent(mutationguard.AssertionEvent{
		FormatVersion: mutationguard.EventFormatVersion,
		AssertionID:   "guard/assertion",
		Expected:      mutationGuardState("semantic-subject", "expected-state"),
		Actual:        mutationGuardState("semantic-subject", "actual-state"),
	})
	if err != nil {
		t.Fatalf("mutationguard.EncodeEvent() error: %v", err)
	}
	want := `GOPLINT_MUTATION_GUARD_MISMATCH_V1 {"format_version":1,"assertion_id":"guard/assertion","expected":{"subject":"semantic-subject","state":"expected-state"},"actual":{"subject":"semantic-subject","state":"actual-state"}}`
	if marker != want {
		t.Fatalf("mutation guard marker = %s, want %s", marker, want)
	}
}
