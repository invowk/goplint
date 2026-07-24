// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func groupSelectionFixtureUnits() []WorkUnit {
	units := make([]WorkUnit, 0, 64)
	for shard := 1; shard <= 16; shard++ {
		weight := int64(150+shard) * 1_000_000_000
		units = append(units, WorkUnit{
			ID: fmt.Sprintf("race-01-%02d", shard), Mode: "race", Iteration: 1,
			MemberIDs: []string{fmt.Sprintf("TestRace%02d", shard)}, TotalWeight: weight,
			TimeoutSeconds: 1200, BinaryDigest: "sha256:race",
		})
		for iteration := 1; iteration <= 3; iteration++ {
			units = append(units, WorkUnit{
				ID: fmt.Sprintf("normal-%02d-%02d", iteration, shard), Mode: "normal", Iteration: iteration,
				MemberIDs: []string{fmt.Sprintf("TestRace%02d", shard)}, TotalWeight: weight,
				TimeoutSeconds: 600, BinaryDigest: "sha256:normal",
			})
		}
	}
	return units
}

func TestSelectWorkGroupPartitionsEveryUnitExactlyOnce(t *testing.T) {
	t.Parallel()

	units := groupSelectionFixtureUnits()
	for _, count := range []int{1, 2, 3, 6, 7, 16} {
		seen := make(map[string]int, len(units))
		groupWeights := make([]int64, 0, count)
		for index := 1; index <= count; index++ {
			group, err := SelectWorkGroup(units, index, count)
			if err != nil {
				t.Fatalf("SelectWorkGroup(%d/%d) error = %v", index, count, err)
			}
			if len(group) == 0 {
				t.Fatalf("SelectWorkGroup(%d/%d) returned an empty group", index, count)
			}
			var weight int64
			for _, unit := range group {
				seen[unit.ID]++
				weight += effectiveGroupWeight(unit)
			}
			groupWeights = append(groupWeights, weight)
			if !slices.IsSortedFunc(group, func(left, right WorkUnit) int {
				return strings.Compare(left.ID, right.ID)
			}) {
				t.Fatalf("SelectWorkGroup(%d/%d) is not deterministically ordered", index, count)
			}
		}
		if len(seen) != len(units) {
			t.Fatalf("groups of %d cover %d unique units, want %d", count, len(seen), len(units))
		}
		for id, executions := range seen {
			if executions != 1 {
				t.Fatalf("unit %q selected %d times across %d groups, want exactly once", id, executions, count)
			}
		}
		minWeight, maxWeight := slices.Min(groupWeights), slices.Max(groupWeights)
		if count <= 8 && maxWeight > 2*minWeight {
			t.Fatalf("group weights %v for count %d are imbalanced beyond 2x", groupWeights, count)
		}
	}
}

func TestSelectWorkGroupIsDeterministicAcrossInputOrder(t *testing.T) {
	t.Parallel()

	units := groupSelectionFixtureUnits()
	shuffled := slices.Clone(units)
	slices.Reverse(shuffled)
	for index := 1; index <= 6; index++ {
		canonical, err := SelectWorkGroup(units, index, 6)
		if err != nil {
			t.Fatal(err)
		}
		reordered, err := SelectWorkGroup(shuffled, index, 6)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(canonical, reordered, workUnitsEqual) {
			t.Fatalf("group %d/6 depends on input order", index)
		}
	}
}

func TestSelectWorkGroupRejectsInvalidSelectors(t *testing.T) {
	t.Parallel()

	units := groupSelectionFixtureUnits()
	if _, err := SelectWorkGroup(units, 0, 6); err == nil {
		t.Fatal("SelectWorkGroup accepted index 0")
	}
	if _, err := SelectWorkGroup(units, 7, 6); err == nil {
		t.Fatal("SelectWorkGroup accepted index beyond count")
	}
	if _, err := SelectWorkGroup(units, 1, len(units)+1); err == nil {
		t.Fatal("SelectWorkGroup accepted more groups than units")
	}
	if _, err := SelectWorkGroup(nil, 1, 1); err == nil {
		t.Fatal("SelectWorkGroup accepted an empty unit set")
	}
}

func TestParseWorkGroupValidatesSelectorForm(t *testing.T) {
	t.Parallel()

	index, count, err := ParseWorkGroup("3/6")
	if err != nil || index != 3 || count != 6 {
		t.Fatalf("ParseWorkGroup(3/6) = %d, %d, %v", index, count, err)
	}
	for _, invalid := range []string{"", "3", "0/6", "7/6", "a/b", "1/0", "-1/6"} {
		if _, _, err := ParseWorkGroup(invalid); err == nil {
			t.Fatalf("ParseWorkGroup(%q) accepted an invalid selector", invalid)
		}
	}
}

func TestExecutePlanUnitsRejectsUnitsOutsideThePlan(t *testing.T) {
	t.Parallel()

	units := groupSelectionFixtureUnits()
	plan := Plan{WorkUnits: units}
	foreign := units[0]
	foreign.ID = "race-99-99"
	if _, err := ExecutePlanUnits(t.Context(), plan, []WorkUnit{foreign}, ExecuteOptions{}); err == nil ||
		!strings.Contains(err.Error(), "is not planned") {
		t.Fatalf("ExecutePlanUnits accepted a foreign unit: %v", err)
	}
	tampered := units[0]
	tampered.MemberIDs = []string{"TestTampered"}
	if _, err := ExecutePlanUnits(t.Context(), plan, []WorkUnit{tampered}, ExecuteOptions{}); err == nil ||
		!strings.Contains(err.Error(), "does not match its plan binding") {
		t.Fatalf("ExecutePlanUnits accepted a tampered unit: %v", err)
	}
}
