// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// AllocateLPT deterministically assigns descending-duration entries to the
// lightest shard, with lexical and numeric tie breakers.
func AllocateLPT(entries []TimingEntry, shardCount int, mode string) ([]Shard, error) {
	if shardCount <= 0 || shardCount > len(entries) {
		return nil, errors.New("race/repeat shard count must be positive and no larger than the census")
	}
	ordered := slices.Clone(entries)
	slices.SortFunc(ordered, func(left, right TimingEntry) int {
		if left.DurationWeightNanoseconds != right.DurationWeightNanoseconds {
			if left.DurationWeightNanoseconds > right.DurationWeightNanoseconds {
				return -1
			}
			return 1
		}
		return strings.Compare(left.ID, right.ID)
	})
	shards := make([]Shard, shardCount)
	for index := range shards {
		shards[index].ID = fmt.Sprintf("%s-%02d", mode, index+1)
		shards[index].MemberIDs = []string{}
	}
	for _, entry := range ordered {
		selected := 0
		for index := 1; index < len(shards); index++ {
			if shards[index].TotalWeight < shards[selected].TotalWeight {
				selected = index
			}
		}
		shards[selected].MemberIDs = append(shards[selected].MemberIDs, entry.ID)
		shards[selected].TotalWeight += entry.DurationWeightNanoseconds
	}
	for index := range shards {
		slices.Sort(shards[index].MemberIDs)
		shards[index].TimeoutSeconds = derivedTimeoutSeconds(shards[index].TotalWeight, mode)
	}
	return shards, nil
}

// ValidateNestedFamilies rejects a dominant top-level family with multiple
// independently named cases unless it has a reviewed internal-shard protocol.
func ValidateNestedFamilies(manifest TimingManifest, resolved ResolvedTiming, shardCount int) error {
	if shardCount <= 0 {
		return errors.New("race/repeat nested-family shard count is invalid")
	}
	var total int64
	for _, entry := range resolved.Entries {
		total += entry.DurationWeightNanoseconds
	}
	target := total / int64(shardCount)
	reviewed := make(map[string]bool, len(manifest.ReviewedInternalShardIDs))
	for _, id := range manifest.ReviewedInternalShardIDs {
		reviewed[id] = true
	}
	for _, entry := range resolved.Entries {
		if entry.NestedCaseCount >= 2 && entry.DurationWeightNanoseconds > 2*target && !reviewed[entry.ID] {
			return fmt.Errorf("race/repeat top-level family %q dominates the shard target and requires reviewed internal case sharding", entry.ID)
		}
	}
	return nil
}

func derivedTimeoutSeconds(weightNanoseconds int64, mode string) int {
	seconds := weightNanoseconds / 1_000_000_000
	multiplier := int64(3)
	if mode == "race" {
		multiplier = 6
	}
	result := seconds*multiplier + 60
	return int(max(int64(120), min(result, int64(30*60))))
}
