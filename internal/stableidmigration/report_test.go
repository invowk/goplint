// SPDX-License-Identifier: MPL-2.0

package stableidmigration

import (
	"bytes"
	"testing"
)

func TestBuildAcceptsDeterministicOneToOneMigration(t *testing.T) {
	t.Parallel()

	oldScan := scan(
		`{"package":"example.com/a","category":"primitive","id":"old-a","message":"a","posn":"/old/a.go:1:1"}`,
		`{"package":"example.com/b","category":"primitive","id":"same-b","message":"b","posn":"/old/b.go:2:1"}`,
	)
	newScan := scan(
		`{"package":"example.com/b","category":"primitive","id":"same-b","message":"b","posn":"/new/b.go:2:1"}`,
		`{"package":"example.com/a","category":"primitive","id":"new-a","message":"a","posn":"/new/a.go:1:1"}`,
	)
	repeatScan := scan(
		`{"package":"example.com/a","category":"primitive","id":"new-a","message":"a","posn":"/repeat/a.go:1:1"}`,
		`{"package":"example.com/b","category":"primitive","id":"same-b","message":"b","posn":"/repeat/b.go:2:1"}`,
	)

	report, err := Build(oldScan, newScan, repeatScan)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !report.Accepted || !report.Deterministic {
		t.Fatalf("Build() report = %+v, want accepted deterministic migration", report)
	}
	if report.Counts.Changed != 1 || report.Counts.Retained != 1 || report.Counts.Added != 0 || report.Counts.Removed != 0 {
		t.Fatalf("Build() counts = %+v, want one changed and one retained", report.Counts)
	}
}

func TestBuildRejectsNondeterminismCollisionDuplicateAndUnmappedChurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		oldScan   []byte
		newScan   []byte
		repeat    []byte
		failure   string
		collision bool
		duplicate bool
	}{
		{
			name:    "nondeterministic repeat",
			oldScan: scan(`{"package":"p","category":"c","id":"old","message":"m","posn":"a.go:1:1"}`),
			newScan: scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			repeat:  scan(`{"package":"p","category":"c","id":"other","message":"m","posn":"a.go:1:1"}`),
			failure: "repeated new scans",
		},
		{
			name:    "nondeterministic finding metadata",
			oldScan: scan(`{"package":"p","category":"c","id":"old","message":"m","posn":"a.go:1:1","meta":{"cfg_outcome_status":"violation"}}`),
			newScan: scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1","meta":{"cfg_outcome_status":"violation"}}`),
			repeat:  scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1","meta":{"cfg_outcome_status":"inconclusive"}}`),
			failure: "repeated new scans",
		},
		{
			name:      "stable ID collision",
			oldScan:   scan(`{"package":"p","category":"c","id":"old-a","message":"a","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"old-b","message":"b","posn":"a.go:2:1"}`),
			newScan:   scan(`{"package":"p","category":"c","id":"collision","message":"a","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"collision","message":"b","posn":"a.go:2:1"}`),
			repeat:    scan(`{"package":"p","category":"c","id":"collision","message":"a","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"collision","message":"b","posn":"a.go:2:1"}`),
			failure:   "multiple semantic findings",
			collision: true,
		},
		{
			name:      "duplicate emission",
			oldScan:   scan(`{"package":"p","category":"c","id":"old","message":"m","posn":"a.go:1:1"}`),
			newScan:   scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			repeat:    scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			failure:   "emitted more than once",
			duplicate: true,
		},
		{
			name:      "duplicate emission with root-aliased positions",
			oldScan:   scan(`{"package":"p","category":"c","id":"old","message":"m","posn":"a.go:1:1"}`),
			newScan:   scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"/checkout/a.go:1:1"}`, `{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			repeat:    scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`, `{"package":"p","category":"c","id":"new","message":"m","posn":"/repeat/a.go:1:1"}`),
			failure:   "emitted more than once",
			duplicate: true,
		},
		{
			name:    "unmapped addition",
			oldScan: nil,
			newScan: scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			repeat:  scan(`{"package":"p","category":"c","id":"new","message":"m","posn":"a.go:1:1"}`),
			failure: "unexplained added population",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			report, err := Build(test.oldScan, test.newScan, test.repeat)
			if err != nil {
				t.Fatalf("Build() error = %v", err)
			}
			if report.Accepted {
				t.Fatal("Build() accepted invalid migration")
			}
			if !containsFailure(report.Failures, test.failure) {
				t.Fatalf("Build() failures = %q, want substring %q", report.Failures, test.failure)
			}
			if test.collision && len(report.Collisions) == 0 {
				t.Fatal("Build() omitted collision details")
			}
			if test.duplicate && len(report.Duplicates) == 0 {
				t.Fatal("Build() omitted duplicate details")
			}
		})
	}
}

func TestBuildRejectsMalformedFindingAndMarshalIsDeterministic(t *testing.T) {
	t.Parallel()

	if _, err := Build(scan(`{"category":"c","id":"id","message":"m"}`), nil, nil); err == nil {
		t.Fatal("Build() accepted finding without package")
	}

	input := scan(`{"package":"p","category":"c","id":"id","message":"m","posn":"a.go:1:1"}`)
	report, err := Build(input, input, input)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	first, err := Marshal(report)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := Marshal(report)
	if err != nil {
		t.Fatalf("Marshal() repeat error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Marshal() output changed across identical calls")
	}
}

func TestBuildReviewedAcceptsOnlyExactExplainedPopulationChange(t *testing.T) {
	t.Parallel()

	newScan := scan(`{"package":"p","category":"nonzero-value-field","id":"new","message":"m","posn":"a.go:1:1"}`)
	unreviewed, err := Build(nil, newScan, newScan)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if unreviewed.Accepted || len(unreviewed.PopulationChanges) != 1 {
		t.Fatalf("Build() report = %+v, want one rejected population change", unreviewed)
	}
	change := unreviewed.PopulationChanges[0]
	review := PopulationChangeReview{
		Status:          change.Status,
		Category:        change.Category,
		Population:      change.Population,
		CanonicalSHA256: change.CanonicalSHA256,
		Reason:          "legacy scan omitted this explicitly enabled category",
		Evidence:        "evidence/legacy-scan-scope.md",
	}
	reviewed, err := BuildReviewed(nil, newScan, newScan, []PopulationChangeReview{review})
	if err != nil {
		t.Fatalf("BuildReviewed() error = %v", err)
	}
	if !reviewed.Accepted || !reviewed.PopulationChanges[0].Reviewed {
		t.Fatalf("BuildReviewed() report = %+v, want accepted reviewed change", reviewed)
	}

	review.CanonicalSHA256 = "stale"
	stale, err := BuildReviewed(nil, newScan, newScan, []PopulationChangeReview{review})
	if err != nil {
		t.Fatalf("BuildReviewed(stale) error = %v", err)
	}
	if stale.Accepted || !containsFailure(stale.Failures, "does not match current population digest") {
		t.Fatalf("BuildReviewed(stale) failures = %q, want digest rejection", stale.Failures)
	}
}

func scan(lines ...string) []byte {
	var out bytes.Buffer
	for _, line := range lines {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func containsFailure(failures []string, substring string) bool {
	for _, failure := range failures {
		if bytes.Contains([]byte(failure), []byte(substring)) {
			return true
		}
	}
	return false
}
