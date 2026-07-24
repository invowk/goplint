// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// raceEffectiveWeightMultiplier is the reviewed race-detector slowdown used
// only to balance work-group assignment. It never changes execution timeouts,
// census membership, or per-unit weights.
const raceEffectiveWeightMultiplier = 4

// ParseWorkGroup parses an "index/count" work-group selector.
func ParseWorkGroup(selector string) (int, int, error) {
	indexText, countText, found := strings.Cut(selector, "/")
	if !found {
		return 0, 0, fmt.Errorf("work group %q is not in index/count form", selector)
	}
	var index, count int
	if _, err := fmt.Sscanf(indexText+" "+countText, "%d %d", &index, &count); err != nil {
		return 0, 0, fmt.Errorf("work group %q is not numeric: %w", selector, err)
	}
	if count < 1 || index < 1 || index > count {
		return 0, 0, fmt.Errorf("work group %q must satisfy 1 <= index <= count", selector)
	}
	return index, count, nil
}

// SelectWorkGroup deterministically partitions every plan work unit into
// exactly `count` disjoint groups and returns group `index` (1-based). The
// same plan and count always produce the same exhaustive partition, so
// distributed group executions are complete by construction: the union of all
// groups is the exact plan work-unit set and no unit appears twice.
func SelectWorkGroup(units []WorkUnit, index, count int) ([]WorkUnit, error) {
	if count < 1 || index < 1 || index > count {
		return nil, errors.New("race/repeat work group selection requires 1 <= index <= count")
	}
	if len(units) == 0 {
		return nil, errors.New("race/repeat work group selection requires at least one work unit")
	}
	if count > len(units) {
		return nil, fmt.Errorf(
			"race/repeat work group count %d exceeds the %d planned work units",
			count, len(units),
		)
	}
	ordered := slices.Clone(units)
	slices.SortFunc(ordered, func(left, right WorkUnit) int {
		leftWeight, rightWeight := effectiveGroupWeight(left), effectiveGroupWeight(right)
		if leftWeight != rightWeight {
			if leftWeight > rightWeight {
				return -1
			}
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	totals := make([]int64, count)
	groups := make([][]WorkUnit, count)
	for _, unit := range ordered {
		selected := 0
		for candidate := 1; candidate < count; candidate++ {
			if totals[candidate] < totals[selected] {
				selected = candidate
			}
		}
		groups[selected] = append(groups[selected], unit)
		totals[selected] += effectiveGroupWeight(unit)
	}
	result := groups[index-1]
	slices.SortFunc(result, func(left, right WorkUnit) int { return strings.Compare(left.ID, right.ID) })
	if len(result) == 0 {
		return nil, fmt.Errorf("race/repeat work group %d of %d is empty", index, count)
	}
	return result, nil
}

func effectiveGroupWeight(unit WorkUnit) int64 {
	weight := unit.TotalWeight
	if unit.Mode == "race" {
		weight *= raceEffectiveWeightMultiplier
	}
	// A zero-weight unit still occupies one scheduling slot.
	return max(weight, 1)
}
