// SPDX-License-Identifier: MPL-2.0

// Command race-repeat-timings refreshes the reviewed analyzer timing manifest.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/racerepeat"
)

func main() {
	moduleRoot := flag.String("module-root", ".", "goplint Go module root")
	packagePath := flag.String("package", "./goplint", "analyzer package to time")
	sampleCount := flag.Int("samples", 3, "number of fresh go test -json timing samples")
	shardCount := flag.Int("shards", racerepeat.DefaultShardCount, "reviewed analyzer shard count")
	outputPath := flag.String("output", "spec/goplint-test-timings.v1.json", "timing manifest output")
	flag.Parse()

	manifest, err := racerepeat.CaptureTimingManifest(context.Background(), racerepeat.CaptureOptions{
		ModuleRoot: *moduleRoot, PackagePath: *packagePath, SampleCount: *sampleCount, GeneratedAt: time.Now().UTC(),
	})
	if err == nil {
		var resolved racerepeat.ResolvedTiming
		resolved, err = manifest.Resolve(timingCensus(manifest))
		if err == nil {
			err = racerepeat.ValidateNestedFamilies(manifest, resolved, *shardCount)
		}
	}
	if err == nil {
		var encoded []byte
		encoded, err = racerepeat.CanonicalTimingJSON(manifest)
		if err == nil {
			err = publish(*outputPath, encoded)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate goplint race/repeat timings:", err)
		os.Exit(1)
	}
}

func timingCensus(manifest racerepeat.TimingManifest) []racerepeat.CensusEntry {
	census := make([]racerepeat.CensusEntry, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		census = append(census, racerepeat.CensusEntry{ID: entry.ID, Kind: entry.Kind})
	}
	return census
}

func publish(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".goplint-test-timings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary timing manifest: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath) //nolint:errcheck // Best-effort cleanup after publication or failure.
	if _, err := temporary.Write(data); err != nil {
		temporary.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write temporary timing manifest: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary timing manifest: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish timing manifest: %w", err)
	}
	return nil
}
