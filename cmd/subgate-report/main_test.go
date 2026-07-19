// SPDX-License-Identifier: MPL-2.0

package main

import "testing"

func TestParseObservations(t *testing.T) {
	t.Parallel()

	populations, err := parseObservations([]string{"zeta=case-b", "alpha=case-a", "zeta=case-a"})
	if err != nil {
		t.Fatalf("parseObservations() error = %v", err)
	}
	if len(populations) != 2 || populations[0].ID != "alpha" || populations[0].Count != 1 ||
		populations[1].ID != "zeta" || populations[1].Count != 2 {
		t.Fatalf("parseObservations() = %+v, want canonical alpha/zeta observed counts", populations)
	}
}

func TestParseObservationsRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := [][]string{
		nil,
		{"missing-member"},
		{"empty="},
		{"=empty"},
		{"duplicate=member", "duplicate=member"},
	}
	for _, values := range tests {
		if _, err := parseObservations(values); err == nil {
			t.Errorf("parseObservations(%q) unexpectedly succeeded", values)
		}
	}
}
