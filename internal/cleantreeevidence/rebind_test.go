// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadRecordRejectsRetiredAndUnknownFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version int
		want    []string
		absent  []string
	}{
		{
			name:    "retired single-digest format",
			version: 3,
			want:    []string{"retired single-digest format", "make generate-goplint-clean-tree-evidence"},
		},
		{
			name:    "unknown future format",
			version: 99,
			want:    []string{"unsupported clean-tree record format 99"},
			absent:  []string{"retired single-digest format"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "record.json")
			if err := os.WriteFile(path, fmt.Appendf(nil, `{"format_version": %d}`, tt.version), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadRecord(path)
			if err == nil {
				t.Fatalf("LoadRecord() accepted format %d", tt.version)
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("LoadRecord() error = %v, want %q", err, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(err.Error(), absent) {
					t.Fatalf("LoadRecord() error = %v, must not mention %q", err, absent)
				}
			}
		})
	}
}

func TestRebindRefreshesProseIdentityAndCarriesReport(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	options := fixture.captureOptions()
	captured, err := Capture(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, fixture.root, "docs/notes.md", "revised prose notes\n")
	rebound, err := Rebind(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	if rebound.Status != "passed" {
		t.Fatalf("Rebind() status = %q, want passed", rebound.Status)
	}
	if rebound.Provenance.Kind != ProvenanceRebound {
		t.Fatalf("Rebind() provenance kind = %q, want %q", rebound.Provenance.Kind, ProvenanceRebound)
	}
	if rebound.Provenance.PreviousProseDigest != captured.Repository.ProseTreeDigest {
		t.Fatalf(
			"Rebind() previous prose digest = %s, want pre-drift %s",
			rebound.Provenance.PreviousProseDigest,
			captured.Repository.ProseTreeDigest,
		)
	}
	if rebound.Repository.ProseTreeDigest == captured.Repository.ProseTreeDigest {
		t.Fatal("Rebind() did not refresh the prose tree digest for the drifted documentation")
	}
	if rebound.Repository.SemanticTreeDigest != captured.Repository.SemanticTreeDigest {
		t.Fatalf(
			"Rebind() semantic digest = %s, want retained %s",
			rebound.Repository.SemanticTreeDigest,
			captured.Repository.SemanticTreeDigest,
		)
	}
	if !reflect.DeepEqual(rebound.AggregateReport, captured.AggregateReport) {
		t.Fatalf(
			"Rebind() aggregate report differs from the retained report: got %+v, want %+v",
			rebound.AggregateReport,
			captured.AggregateReport,
		)
	}
	published, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(published, rebound) {
		t.Fatalf("published record %+v differs from Rebind() result %+v", published, rebound)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatalf("Verify() rejected the re-bound record: %v", err)
	}
}

func TestVerifyRejectsProseStaleRecordWithoutRebind(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	writeTestFile(t, fixture.root, "docs/notes.md", "revised prose notes\n")
	refreshFixtureCensusOnly(t, fixture)
	err := Verify(t.Context(), fixture.options)
	if err == nil || !strings.Contains(err.Error(), "prose-stale") {
		t.Fatalf("Verify() error = %v, want prose-stale rejection", err)
	}
}

func TestRebindFailsClosedOnSemanticDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		drift  func(*testing.T, verifyFixture)
		absent string
	}{
		{
			name: "semantic drift",
			drift: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(t, fixture.root, "input.txt", "semantic drift\n")
			},
		},
		{
			name: "mixed prose and semantic drift names only the semantic path",
			drift: func(t *testing.T, fixture verifyFixture) {
				t.Helper()
				writeTestFile(t, fixture.root, "docs/notes.md", "revised prose notes\n")
				writeTestFile(t, fixture.root, "input.txt", "semantic drift\n")
			},
			absent: "docs/notes.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			tt.drift(t, fixture)
			evidencePath := resolveFromRoot(fixture.root, fixture.options.EvidencePath)
			before, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			_, rebindErr := Rebind(t.Context(), fixture.captureOptions())
			if rebindErr == nil {
				t.Fatal("Rebind() reblessed semantic drift")
			}
			for _, want := range []string{"drifted semantic paths:", "input.txt"} {
				if !strings.Contains(rebindErr.Error(), want) {
					t.Fatalf("Rebind() error = %v, want %q", rebindErr, want)
				}
			}
			if tt.absent != "" && strings.Contains(rebindErr.Error(), tt.absent) {
				t.Fatalf("Rebind() error = %v, must not name documentation path %q", rebindErr, tt.absent)
			}
			after, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("failed Rebind() modified the retained record")
			}
		})
	}
}

func TestRebindRejectsTamperedRetainedAggregateReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tamper func(*testing.T, *Record)
		want   string
	}{
		{
			name: "report bytes diverge from recorded digest",
			tamper: func(t *testing.T, record *Record) {
				t.Helper()
				record.AggregateReport.Report.Subgates[0].Populations[0].Count++
			},
			want: "recorded digest",
		},
		{
			name: "consistently re-digested tampered report fails identity validation",
			tamper: func(t *testing.T, record *Record) {
				t.Helper()
				record.AggregateReport.Report.Subgates[0].Populations[0].Count = 0
				digest, err := digestJSON(record.AggregateReport.Report)
				if err != nil {
					t.Fatal(err)
				}
				record.AggregateReport.SHA256 = digest
			},
			want: "validate retained aggregate report",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			evidencePath := resolveFromRoot(fixture.root, fixture.options.EvidencePath)
			record, err := LoadRecord(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			tt.tamper(t, &record)
			if err := WriteRecord(evidencePath, record); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			_, rebindErr := Rebind(t.Context(), fixture.captureOptions())
			if rebindErr == nil || !strings.Contains(rebindErr.Error(), tt.want) {
				t.Fatalf("Rebind() error = %v, want %q", rebindErr, tt.want)
			}
			after, err := os.ReadFile(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("failed Rebind() modified the retained record")
			}
		})
	}
}

func TestVerifyRejectsForgedProvenanceDigests(t *testing.T) {
	t.Parallel()

	forged := digestBytes([]byte("forged"))
	tests := []struct {
		name   string
		mutate func(*Record)
		want   string
	}{
		{
			name:   "aggregate semantic tree digest",
			mutate: func(record *Record) { record.Provenance.AggregateSemanticTreeDigest = forged },
			want:   "bound to semantic content",
		},
		{
			name:   "aggregate workspace digest",
			mutate: func(record *Record) { record.Provenance.AggregateWorkspaceDigest = forged },
			want:   "workspace digest does not match",
		},
		{
			name:   "carried report digest",
			mutate: func(record *Record) { record.Provenance.CarriedReportSHA256 = forged },
			want:   "report digest does not match",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newVerifyFixture(t)
			evidencePath := resolveFromRoot(fixture.root, fixture.options.EvidencePath)
			record, err := LoadRecord(evidencePath)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(&record)
			if err := WriteRecord(evidencePath, record); err != nil {
				t.Fatal(err)
			}
			err = Verify(t.Context(), fixture.options)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Verify() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// refreshFixtureCensusOnly rewrites the retained record's complete-diff census
// to the current caller state while keeping the retained repository identity,
// so Verify reaches the prose-staleness comparison instead of failing on an
// intentionally stale census.
func refreshFixtureCensusOnly(t *testing.T, fixture verifyFixture) {
	t.Helper()
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	paths, err := LoadPathSelection(resolveFromRoot(fixture.root, fixture.options.PathsPath))
	if err != nil {
		t.Fatal(err)
	}
	record, err := LoadRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath))
	if err != nil {
		t.Fatal(err)
	}
	record.DiffCensus, err = collectDiffCensus(
		t.Context(),
		fixture.root,
		record.Repository.BaseCommit,
		paths,
		plan.DiffReview,
		[]string{fixture.options.EvidencePath, fixture.options.EvidencePath + ".tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRecord(resolveFromRoot(fixture.root, fixture.options.EvidencePath), record); err != nil {
		t.Fatal(err)
	}
}
