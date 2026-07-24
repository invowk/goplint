// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"bytes"
	"slices"
	"testing"
	"time"
)

func TestBuildTimingManifestAndResolveLiveCensus(t *testing.T) {
	t.Parallel()

	census, err := ParseCensus([]byte("TestAlpha\nFuzzBeta\nExampleGamma\nok  \texample.com/goplint\t0.1s\n"))
	if err != nil {
		t.Fatal(err)
	}
	sample := []byte(
		`{"Action":"pass","Test":"TestAlpha/sub","Elapsed":0.4}` + "\n" +
			`{"Action":"pass","Test":"TestAlpha","Elapsed":1.2}` + "\n" +
			`{"Action":"pass","Test":"FuzzBeta","Elapsed":0.2}` + "\n" +
			`{"Action":"pass","Test":"ExampleGamma","Elapsed":0.1}` + "\n",
	)
	manifest, err := BuildTimingManifest("./goplint", "go1.26.5", time.Now(), census, sample)
	if err != nil {
		t.Fatalf("BuildTimingManifest() error = %v", err)
	}
	resolved, err := manifest.Resolve(census)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved.Entries) != 3 || len(resolved.DefaultedMemberIDs) != 0 {
		t.Fatalf("resolved timing = %+v", resolved)
	}
	if manifest.Entries[2].NestedCaseCount != 1 {
		t.Fatalf("TestAlpha nested count = %d, want 1", manifest.Entries[2].NestedCaseCount)
	}
}

func TestBuildTimingManifestPromotesDominatingNestedCaseToShardWeight(t *testing.T) {
	t.Parallel()

	census := []CensusEntry{{ID: "TestParallelFamily", Kind: KindTest}}
	sample := []byte(
		`{"Action":"pass","Test":"TestParallelFamily/case","Elapsed":4.2}` + "\n" +
			`{"Action":"pass","Test":"TestParallelFamily","Elapsed":0}` + "\n",
	)
	manifest, err := BuildTimingManifest("./goplint", "go1.26.5", time.Now(), census, sample)
	if err != nil {
		t.Fatalf("BuildTimingManifest() error = %v", err)
	}
	entry := manifest.Entries[0]
	if entry.DurationWeightNanoseconds != entry.MaximumNestedCaseNanoseconds || entry.DurationWeightNanoseconds != 4_200_000_000 {
		t.Fatalf("nested family timing = %+v", entry)
	}

	entry.DurationWeightNanoseconds--
	manifest.Entries[0] = entry
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() accepted a nested case that dominates its top-level shard weight")
	}
}

func TestResolveRejectsUnknownAndDefaultsNewMembersConservatively(t *testing.T) {
	t.Parallel()

	manifest := testTimingManifest()
	census := []CensusEntry{{ID: "TestA", Kind: KindTest}, {ID: "TestB", Kind: KindTest}, {ID: "TestNew", Kind: KindTest}}
	resolved, err := manifest.Resolve(census)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !slices.Equal(resolved.DefaultedMemberIDs, []string{"TestNew"}) || resolved.Entries[2].DurationWeightNanoseconds != 20 {
		t.Fatalf("resolved default = %+v", resolved)
	}
	manifest.Entries = append(manifest.Entries, TimingEntry{ID: "TestRemoved", Kind: KindTest, DurationWeightNanoseconds: 1, SampleCount: 1})
	slices.SortFunc(manifest.Entries, func(left, right TimingEntry) int { return bytes.Compare([]byte(left.ID), []byte(right.ID)) })
	if _, err := manifest.Resolve(census); err == nil {
		t.Fatal("Resolve() accepted unknown timing entry")
	}
}

func TestAllocateLPTIsCompleteDeterministicAndBetterThanModulo(t *testing.T) {
	t.Parallel()

	entries := []TimingEntry{
		{ID: "TestA", Kind: KindTest, DurationWeightNanoseconds: 10, SampleCount: 1},
		{ID: "TestB", Kind: KindTest, DurationWeightNanoseconds: 9, SampleCount: 1},
		{ID: "TestC", Kind: KindTest, DurationWeightNanoseconds: 8, SampleCount: 1},
		{ID: "TestD", Kind: KindTest, DurationWeightNanoseconds: 7, SampleCount: 1},
		{ID: "TestE", Kind: KindTest, DurationWeightNanoseconds: 6, SampleCount: 1},
		{ID: "TestF", Kind: KindTest, DurationWeightNanoseconds: 5, SampleCount: 1},
	}
	left, err := AllocateLPT(entries, 2, "normal")
	if err != nil {
		t.Fatal(err)
	}
	right, err := AllocateLPT(entries, 2, "normal")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(left, right, func(a, b Shard) bool {
		return a.ID == b.ID && a.TotalWeight == b.TotalWeight && slices.Equal(a.MemberIDs, b.MemberIDs)
	}) {
		t.Fatalf("LPT is nondeterministic: %+v != %+v", left, right)
	}
	seen := map[string]bool{}
	for _, shard := range left {
		for _, member := range shard.MemberIDs {
			if seen[member] {
				t.Fatalf("member %q overlaps shards", member)
			}
			seen[member] = true
		}
	}
	if len(seen) != len(entries) {
		t.Fatalf("LPT covered %d of %d entries", len(seen), len(entries))
	}
	moduloWeights := []int64{10 + 8 + 6, 9 + 7 + 5}
	if max(left[0].TotalWeight, left[1].TotalWeight) >= max(moduloWeights[0], moduloWeights[1]) {
		t.Fatalf("LPT maximum = %d, modulo maximum = %d", max(left[0].TotalWeight, left[1].TotalWeight), max(moduloWeights[0], moduloWeights[1]))
	}
}

func testTimingManifest() TimingManifest {
	return TimingManifest{
		FormatVersion: TimingFormatVersion, Package: "./goplint", Toolchain: "go1.26.5",
		GeneratedAt:              time.Date(2026, time.July, 19, 0, 0, 0, 0, time.UTC),
		DefaultWeightNanoseconds: 20, ReviewedInternalShardIDs: []string{},
		Environment: []string{ScheduledOracleEnvironment},
		Entries: []TimingEntry{
			{ID: "TestA", Kind: KindTest, DurationWeightNanoseconds: 10, SampleCount: 1},
			{ID: "TestB", Kind: KindTest, DurationWeightNanoseconds: 20, SampleCount: 1},
		},
	}
}
