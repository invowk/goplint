// SPDX-License-Identifier: MPL-2.0

package mutationguard

import (
	"strings"
	"testing"
)

func TestAssertionEventRoundTrip(t *testing.T) {
	t.Parallel()

	event := AssertionEvent{
		FormatVersion: EventFormatVersion,
		AssertionID:   "diagnostic-category",
		Expected:      Observation{Subject: "diagnostic-category", State: "unvalidated-cast"},
		Actual:        Observation{Subject: "diagnostic-category", State: "primitive"},
	}
	encoded, err := EncodeEvent(event)
	if err != nil {
		t.Fatalf("EncodeEvent() error = %v", err)
	}
	decoded, found, err := DecodeOutputLine("    guard_test.go:42: " + encoded + "\n")
	if err != nil {
		t.Fatalf("DecodeOutputLine() error = %v", err)
	}
	if !found || decoded != event {
		t.Fatalf("DecodeOutputLine() = (%+v, %t), want (%+v, true)", decoded, found, event)
	}
}

func TestAssertionEventAllowsCanonicalSemanticDelimiters(t *testing.T) {
	t.Parallel()

	event := AssertionEvent{
		FormatVersion: EventFormatVersion,
		AssertionID:   "diagnostic-counts",
		Expected:      Observation{Subject: "diagnostic-counts", State: "violations=5,inconclusives=1"},
		Actual:        Observation{Subject: "diagnostic-counts", State: "violations=0,inconclusives=0"},
	}
	if _, err := EncodeEvent(event); err != nil {
		t.Fatalf("EncodeEvent() error = %v", err)
	}
}

func TestDecodeOutputLineRejectsMalformedEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		line string
	}{
		{name: "malformed JSON", line: EventPrefix + "{not-json}"},
		{
			name: "unknown field",
			line: EventPrefix +
				`{"format_version":1,"assertion_id":"a","expected":{"subject":"s","state":"clean"},` +
				`"actual":{"subject":"s","state":"mutated"},"extra":true}`,
		},
		{
			name: "trailing value",
			line: EventPrefix +
				`{"format_version":1,"assertion_id":"a","expected":{"subject":"s","state":"clean"},` +
				`"actual":{"subject":"s","state":"mutated"}} {}`,
		},
		{
			name: "identical observations",
			line: EventPrefix +
				`{"format_version":1,"assertion_id":"a","expected":{"subject":"s","state":"same"},` +
				`"actual":{"subject":"s","state":"same"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, found, err := DecodeOutputLine(tt.line); !found || err == nil {
				t.Fatalf("DecodeOutputLine() = (found=%t, err=%v), want found error", found, err)
			}
		})
	}
}

func TestDecodeOutputLineIgnoresOrdinaryOutput(t *testing.T) {
	t.Parallel()

	event, found, err := DecodeOutputLine("ordinary assertion failure")
	if err != nil || found || event != (AssertionEvent{}) {
		t.Fatalf("DecodeOutputLine() = (%+v, %t, %v), want zero, false, nil", event, found, err)
	}
}

func TestObservationRejectsNonCanonicalTokens(t *testing.T) {
	t.Parallel()

	for _, observation := range []Observation{
		{},
		{Subject: " subject", State: "state"},
		{Subject: "subject", State: "state\nnext"},
		{Subject: "Subject", State: "state"},
	} {
		if err := observation.Validate(); err == nil || !strings.Contains(err.Error(), "observation") {
			t.Fatalf("Observation.Validate() error = %v, want observation validation failure", err)
		}
	}
}
