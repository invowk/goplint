// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/invowk/goplint/internal/soundnessgate"
)

// reviewedGateManifestPath locates the real aggregate manifest from this
// package directory.
const reviewedGateManifestPath = "../../spec/soundness-gate.v1.json"

// cleanTreeFreshnessSubgateID is the completion-only subgate that verifies the
// retained exact-tree record.
const cleanTreeFreshnessSubgateID = "clean-tree-freshness"

// reviewedFreshnessCommand pins the exact reviewed command vector of the
// completion freshness subgate. Reuse containment rests on this vector never
// gaining the reused-aggregate opt-in, so the whole vector is golden: any edit
// to it must be a deliberate, reviewed change.
var reviewedFreshnessCommand = []string{
	"go", "run", "./cmd/check-clean-tree-evidence",
	"-root", ".",
	"-paths", "testdata/gates/clean-tree-v5.paths",
	"-plan", "testdata/gates/clean-tree-v5.json",
	"-evidence", "testdata/gates/clean-tree-run.v5.json",
}

func TestReviewedFreshnessSubgateNeverOptsIntoReusedAggregate(t *testing.T) {
	t.Parallel()

	manifest := loadReviewedGateManifest(t)
	index := slices.IndexFunc(manifest.Subgates, func(subgate soundnessgate.Subgate) bool {
		return subgate.ID == cleanTreeFreshnessSubgateID
	})
	if index < 0 {
		t.Fatalf("reviewed manifest has no %q subgate", cleanTreeFreshnessSubgateID)
	}
	subgate := manifest.Subgates[index]
	if !slices.Equal(subgate.Command, reviewedFreshnessCommand) {
		t.Fatalf(
			"reviewed %q command = %q, want %q; reuse containment depends on this exact vector",
			cleanTreeFreshnessSubgateID,
			subgate.Command,
			reviewedFreshnessCommand,
		)
	}
	if !slices.Contains(subgate.ProfileIDs, soundnessgate.ProfileComplete) {
		t.Fatalf(
			"reviewed %q profiles = %q, want the completion profile to select it",
			cleanTreeFreshnessSubgateID,
			subgate.ProfileIDs,
		)
	}
	for _, argument := range subgate.Command {
		if argumentEnablesFlag(argument, AllowReusedAggregateFlag) {
			t.Fatalf(
				"reviewed %q command passes -%s, which would make the completion profile accept a record whose "+
					"aggregate was never executed",
				cleanTreeFreshnessSubgateID,
				AllowReusedAggregateFlag,
			)
		}
	}
}

func TestNoReviewedSubgateCommandOptsIntoReusedAggregate(t *testing.T) {
	t.Parallel()

	manifest := loadReviewedGateManifest(t)
	if len(manifest.Subgates) == 0 {
		t.Fatal("reviewed manifest has no subgates")
	}
	for _, subgate := range manifest.Subgates {
		for _, argument := range subgate.Command {
			if argumentEnablesFlag(argument, AllowReusedAggregateFlag) {
				t.Errorf(
					"reviewed subgate %q command passes -%s: no reviewed gate may accept a reused aggregate",
					subgate.ID,
					AllowReusedAggregateFlag,
				)
			}
		}
	}
}

func loadReviewedGateManifest(t *testing.T) soundnessgate.Manifest {
	t.Helper()
	manifest, _, err := soundnessgate.LoadManifest(t.Context(), filepath.FromSlash(reviewedGateManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

// argumentEnablesFlag reports whether one command argument names a flag,
// independent of dash count and of an inline value assignment.
func argumentEnablesFlag(argument, flagName string) bool {
	name, _, _ := strings.Cut(strings.TrimLeft(argument, "-"), "=")
	return name == flagName
}
