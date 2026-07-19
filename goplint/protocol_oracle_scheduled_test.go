// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const (
	envProtocolOracleProfile = "GOPLINT_PROTOCOL_ORACLE_PROFILE"
	envProtocolOracleShard   = "GOPLINT_PROTOCOL_ORACLE_SHARD"
)

func TestProtocolOracleScheduledGeneratedGo(t *testing.T) {
	t.Parallel()

	profileName := os.Getenv(envProtocolOracleProfile)
	if profileName == "" {
		t.Skip("scheduled generated-Go oracle requires GOPLINT_PROTOCOL_ORACLE_PROFILE=scheduled")
	}
	if profileName != "scheduled" {
		t.Fatalf("%s = %q, want scheduled", envProtocolOracleProfile, profileName)
	}
	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	profile := manifest.Scheduled
	shards := make([]int, 0, profile.Shards)
	shardValue := os.Getenv(envProtocolOracleShard)
	if shardValue == "" {
		for shard := range profile.Shards {
			shards = append(shards, shard)
		}
	} else {
		shard, parseErr := strconv.Atoi(shardValue)
		if parseErr != nil || shard < 0 || shard >= profile.Shards {
			t.Fatalf("%s = %q, want an integer in [0,%d)", envProtocolOracleShard, shardValue, profile.Shards)
		}
		shards = append(shards, shard)
	}

	programCount := 0
	comparisonCounts := generatedComparisonCounts{}
	scheduledFingerprints := make(map[string]bool)
	programObservations := make([]soundnessgate.ObservedMember, 0, profile.ExpectedProgramCount)
	for _, shard := range shards {
		expected, cardinalityErr := protocoloracle.ProfileShardCardinality(manifest, profileName, shard)
		if cardinalityErr != nil {
			t.Fatalf("derive scheduled shard %d cardinality: %v", shard, cardinalityErr)
		}
		shardCount := 0
		err = protocoloracle.EnumerateShard(manifest, profileName, shard, func(program protocoloracle.Program) error {
			counts, compareErr := compareGeneratedGoProgram(t, program, profile.MaxStates, "")
			if compareErr != nil {
				return compareErr
			}
			comparisonCounts.add(counts)
			scheduledFingerprints[program.Fingerprint()] = true
			programObservations = append(programObservations, soundnessgate.ObservedMember{
				PopulationID: "generated-programs",
				MemberID:     program.Fingerprint(),
			})
			shardCount++
			programCount++
			return nil
		})
		if err != nil {
			t.Fatalf("scheduled shard %d: %v", shard, err)
		}
		if shardCount != expected || shardCount == 0 {
			t.Fatalf("scheduled shard %d count = %d, want derived nonzero count %d", shard, shardCount, expected)
		}
	}
	if shardValue != "" {
		return
	}
	if programCount != profile.ExpectedProgramCount || programCount < profile.MinimumPrograms {
		t.Fatalf("scheduled corpus count = %d, want %d and at least %d", programCount, profile.ExpectedProgramCount, profile.MinimumPrograms)
	}
	blockingFingerprints := make(map[string]bool, manifest.Blocking.ExpectedProgramCount)
	if err := protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		blockingFingerprints[program.Fingerprint()] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(scheduledFingerprints) <= len(blockingFingerprints) {
		t.Fatalf("scheduled corpus contains %d programs, want a strict superset of blocking %d", len(scheduledFingerprints), len(blockingFingerprints))
	}
	for fingerprint := range blockingFingerprints {
		if !scheduledFingerprints[fingerprint] {
			t.Errorf("scheduled analyzer corpus omitted blocking program %s", fingerprint)
		}
	}
	if comparisonCounts.ViolationCases == 0 || comparisonCounts.InconclusiveCases == 0 {
		t.Fatalf("scheduled analyzer outcomes are vacuous: %+v", comparisonCounts)
	}
	observations := programObservations
	if comparisonCounts.ViolationCases > 0 {
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "protocol-categories",
			MemberID:     "violation",
		})
	}
	if comparisonCounts.InconclusiveCases > 0 {
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "protocol-categories",
			MemberID:     "inconclusive",
		})
	}
	emitSoundnessSubgateReport(t, observedPopulations(t, observations))
}
