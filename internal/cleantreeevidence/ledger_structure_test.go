// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invowk/goplint/internal/soundnessgate"
)

func TestValidateTaskLedgerStructure(t *testing.T) {
	t.Parallel()

	archived := func(date, name string) TaskLedgerPlan {
		return TaskLedgerPlan{Name: name, Path: "openspec/changes/archive/" + date + "-" + name + "/tasks.md"}
	}
	active := func(name string) TaskLedgerPlan {
		return TaskLedgerPlan{Name: name, Path: "openspec/changes/" + name + "/tasks.md"}
	}
	tests := []struct {
		name    string
		ledgers []TaskLedgerPlan
		want    string
	}{
		{
			name: "archived predecessors then active accepted",
			ledgers: []TaskLedgerPlan{
				archived("2026-01-01", "base"),
				archived("2026-01-01", "sibling"),
				archived("2026-02-01", "follow-up"),
				active("current"),
			},
		},
		{
			name:    "active-only accepted",
			ledgers: []TaskLedgerPlan{active("current")},
		},
		{
			name:    "archived-only accepted",
			ledgers: []TaskLedgerPlan{archived("2026-01-01", "base"), archived("2026-02-01", "follow-up")},
		},
		{
			name:    "archived ledger after an active ledger",
			ledgers: []TaskLedgerPlan{active("current"), archived("2026-01-01", "base")},
			want:    "follows an active change ledger",
		},
		{
			name:    "regressing archive dates",
			ledgers: []TaskLedgerPlan{archived("2026-02-01", "later"), archived("2026-01-01", "earlier")},
			want:    "regresses the archive date order",
		},
		{
			name: "archived ledger name does not match its change name",
			ledgers: []TaskLedgerPlan{
				{Name: "other", Path: "openspec/changes/archive/2026-01-01-base/tasks.md"},
			},
			want: "does not match its archived change name",
		},
		{
			name: "active ledger name does not match its change name",
			ledgers: []TaskLedgerPlan{
				{Name: "other", Path: "openspec/changes/current/tasks.md"},
			},
			want: "does not match its change name",
		},
		{
			name: "path outside openspec changes",
			ledgers: []TaskLedgerPlan{
				{Name: "docs", Path: "docs/tasks.md"},
			},
			want: "is not an OpenSpec change tasks ledger",
		},
		{
			name: "change artifact that is not the tasks ledger",
			ledgers: []TaskLedgerPlan{
				{Name: "current", Path: "openspec/changes/current/proposal.md"},
			},
			want: "is not an OpenSpec change tasks ledger",
		},
		{
			name: "malformed archive date",
			ledgers: []TaskLedgerPlan{
				{Name: "base", Path: "openspec/changes/archive/2026-1-1-base/tasks.md"},
			},
			want: "is not an OpenSpec change tasks ledger",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTaskLedgerStructure(tt.ledgers)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("validateTaskLedgerStructure() error = %v, want acceptance", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateTaskLedgerStructure() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPlanValidatePendingIDPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending []string
		want    string
	}{
		{name: "well-formed sorted pending IDs accepted", pending: []string{"1.1", "10.2"}},
		{name: "empty pending set accepted", pending: []string{}},
		{name: "three-part identifier", pending: []string{"1.2.3"}, want: "is not a task number"},
		{name: "alphabetic identifier", pending: []string{"7a"}, want: "is not a task number"},
		{name: "unsorted identifiers", pending: []string{"2", "10"}, want: "must be sorted"},
		{name: "duplicate identifiers", pending: []string{"3", "3"}, want: "duplicate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plan := newMinimalValidPlan()
			plan.TaskLedgers[0].ExpectedPending = tt.pending
			err := plan.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want acceptance", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestArchivingChangeRequiresOnlyReviewedPlanEdit proves the single-source
// ledger contract: transitioning a change from active to archived (the
// archive-path rename) requires editing only the reviewed plan. No Go
// constant, schema value, or other proof artifact names the ledger, so a
// fresh Capture succeeds after the directory rename plus one plan edit.
func TestArchivingChangeRequiresOnlyReviewedPlanEdit(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	options := CaptureOptions(fixture.options)
	if _, err := Capture(t.Context(), options); err != nil {
		t.Fatalf("Capture() with the active change ledger: %v", err)
	}
	oldDir := filepath.Join(fixture.root, "openspec", "changes", "fixture-active")
	newDir := filepath.Join(fixture.root, "openspec", "changes", "archive", "2026-03-01-fixture-active")
	if err := os.Rename(oldDir, newDir); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	plan.TaskLedgers[2].Path = "openspec/changes/archive/2026-03-01-fixture-active/tasks.md"
	writeTestJSON(t, fixture.root, fixture.options.PlanPath, plan)
	record, err := Capture(t.Context(), options)
	if err != nil {
		t.Fatalf("Capture() after the plan-only archive edit: %v", err)
	}
	if record.Status != "passed" {
		t.Fatalf("Capture() status = %q, want passed", record.Status)
	}
	if err := Verify(t.Context(), fixture.options); err != nil {
		t.Fatalf("Verify() after the plan-only archive edit: %v", err)
	}
}

// newMinimalValidPlan builds the smallest plan accepted by Plan.Validate so
// single-field mutations isolate exactly one structural rejection.
func newMinimalValidPlan() Plan {
	return Plan{
		FormatVersion:         FormatVersion,
		OwnershipManifestPath: testOwnershipManifestPath,
		Inputs:                []InputPlan{{Name: "reviewed-input", Path: "input.txt"}},
		Toolchain: []ToolPlan{
			{Name: "git", Command: []string{"git", "--version"}, RequiredVersionRE: "^git version "},
		},
		TaskLedgers: []TaskLedgerPlan{
			{
				Name:            "fixture-base",
				Path:            "openspec/changes/archive/2026-01-01-fixture-base/tasks.md",
				ExpectedPending: []string{},
			},
			{
				Name:            "fixture-active",
				Path:            "openspec/changes/fixture-active/tasks.md",
				ExpectedPending: []string{},
			},
		},
		DiffReview: DiffReviewPlan{ReviewedExclusions: []ReviewedExclusion{}},
		Counterexamples: CounterexamplePlan{
			Path: "counterexamples.json",
			Required: []CounterexampleObservationPlan{
				{ID: "CE-1", Observation: "must report"},
			},
		},
		Commands: []CommandPlan{
			{Name: "proof", Args: []string{"true"}, TimeoutMinutes: 1},
		},
		AggregateReport: AggregateReportPlan{
			CommandName:  "proof",
			OutputFile:   "aggregate-report.json",
			ManifestPath: "manifest.json",
			RegistryPath: "registry.json",
			Profile:      soundnessgate.ProfileSemantic,
		},
		MutationProofs: []MutationProofPlan{
			{Name: "test-mutant", Observation: "test-mutation"},
		},
	}
}
