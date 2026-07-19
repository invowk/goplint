// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"strings"
	"testing"
)

func TestCollectDiffCensusRejectsSilentTrackedAndUntrackedOmissions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*testing.T, string)
	}{
		{
			name: "tracked",
			change: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "omitted-tracked.txt", "tracked omission\n")
				runTestGit(t, root, "add", "omitted-tracked.txt")
			},
		},
		{
			name: "untracked",
			change: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "omitted-untracked.txt", "untracked omission\n")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := initializeTestRepository(t)
			tt.change(t, root)
			_, err := collectDiffCensus(
				t.Context(),
				root,
				"HEAD",
				[]string{"tracked.txt"},
				DiffReviewPlan{},
				nil,
			)
			if err == nil || !strings.Contains(err.Error(), "silently omitted") {
				t.Fatalf("collectDiffCensus() error = %v, want silent-omission failure", err)
			}
		})
	}
}

func TestCollectDiffCensusRequiresExactReviewedExclusions(t *testing.T) {
	t.Parallel()

	root := initializeTestRepository(t)
	writeTestFile(t, root, "unrelated.txt", "reviewed unrelated change\n")
	review := DiffReviewPlan{ReviewedExclusions: []ReviewedExclusion{
		{Path: "unrelated.txt", Reason: "Unrelated documentation draft owned by the caller."},
	}}
	census, err := collectDiffCensus(
		t.Context(),
		root,
		"HEAD",
		[]string{"tracked.txt"},
		review,
		[]string{"evidence.json", "evidence.json.tmp"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(census.Changes) != 1 || census.Changes[0].Disposition != diffDispositionExcluded ||
		len(census.ReviewedExclusions) != 1 || len(census.AuthorizedOutputs) != 2 {
		t.Fatalf("collectDiffCensus() = %+v", census)
	}

	tests := []struct {
		name     string
		selected []string
		review   DiffReviewPlan
		want     string
	}{
		{
			name:     "missing reason",
			selected: []string{"tracked.txt"},
			review: DiffReviewPlan{ReviewedExclusions: []ReviewedExclusion{
				{Path: "unrelated.txt"},
			}},
			want: "trimmed nonempty reason",
		},
		{
			name:     "selected overlap",
			selected: []string{"tracked.txt", "unrelated.txt"},
			review:   review,
			want:     "also covered",
		},
		{
			name:     "stale exclusion",
			selected: []string{"tracked.txt"},
			review: DiffReviewPlan{ReviewedExclusions: []ReviewedExclusion{
				{Path: "missing.txt", Reason: "Previously reviewed but no longer changed."},
				{Path: "unrelated.txt", Reason: "Unrelated documentation draft owned by the caller."},
			}},
			want: "is stale",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, collectErr := collectDiffCensus(
				t.Context(),
				root,
				"HEAD",
				tt.selected,
				tt.review,
				nil,
			)
			if collectErr == nil || !strings.Contains(collectErr.Error(), tt.want) {
				t.Fatalf("collectDiffCensus() error = %v, want %q", collectErr, tt.want)
			}
		})
	}
}

func TestPlanRejectsWrongDependencyOrderAndUnexpectedPredecessorPolicy(t *testing.T) {
	t.Parallel()

	fixture := newVerifyFixture(t)
	plan, err := LoadPlan(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	plan.TaskLedgers[0], plan.TaskLedgers[1] = plan.TaskLedgers[1], plan.TaskLedgers[0]
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "dependency order") {
		t.Fatalf("Validate() wrong archive order error = %v", err)
	}
	plan.TaskLedgers[0], plan.TaskLedgers[1] = plan.TaskLedgers[1], plan.TaskLedgers[0]
	plan.TaskLedgers[0].ExpectedPending = []string{}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "dependency order") {
		t.Fatalf("Validate() unexpected predecessor policy error = %v", err)
	}
}
