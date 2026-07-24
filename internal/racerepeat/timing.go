// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type timingSample struct {
	durations map[string]int64
	nested    map[string]map[string]int64
}

type testEvent struct {
	Action  string      `json:"Action"`
	Test    string      `json:"Test"`
	Elapsed json.Number `json:"Elapsed"`
}

// ParseTimingSample extracts top-level and nested successful test durations
// from one `go test -json` event stream.
func ParseTimingSample(data []byte) (timingSample, error) {
	sample := timingSample{durations: make(map[string]int64), nested: make(map[string]map[string]int64)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 1024*1024)
	for scanner.Scan() {
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.UseNumber()
		var event testEvent
		if err := decoder.Decode(&event); err != nil {
			return timingSample{}, fmt.Errorf("decode race/repeat timing event: %w", err)
		}
		if event.Action != "pass" || event.Test == "" {
			continue
		}
		duration, err := elapsedNanoseconds(event.Elapsed)
		if err != nil {
			return timingSample{}, fmt.Errorf("parse race/repeat duration for %q: %w", event.Test, err)
		}
		topLevel, nested, _ := strings.Cut(event.Test, "/")
		if nested == "" {
			if _, exists := sample.durations[topLevel]; exists {
				return timingSample{}, fmt.Errorf("race/repeat timing sample contains duplicate pass for %q", topLevel)
			}
			sample.durations[topLevel] = max(duration, 1)
			continue
		}
		if sample.nested[topLevel] == nil {
			sample.nested[topLevel] = make(map[string]int64)
		}
		sample.nested[topLevel][event.Test] = max(sample.nested[topLevel][event.Test], duration)
	}
	if err := scanner.Err(); err != nil {
		return timingSample{}, fmt.Errorf("scan race/repeat timing sample: %w", err)
	}
	return sample, nil
}

// BuildTimingManifest combines fresh samples using an upper median and binds
// every exact live census member.
func BuildTimingManifest(
	packagePath, toolchain string,
	generatedAt time.Time,
	census []CensusEntry,
	sampleData ...[]byte,
) (TimingManifest, error) {
	if len(sampleData) == 0 {
		return TimingManifest{}, errors.New("race/repeat timing manifest requires at least one sample")
	}
	canonical, err := canonicalCensus(census)
	if err != nil {
		return TimingManifest{}, err
	}
	samples := make([]timingSample, 0, len(sampleData))
	for _, data := range sampleData {
		sample, err := ParseTimingSample(data)
		if err != nil {
			return TimingManifest{}, err
		}
		samples = append(samples, sample)
	}
	manifest := TimingManifest{
		FormatVersion: TimingFormatVersion, Package: packagePath, Toolchain: toolchain,
		GeneratedAt: generatedAt.UTC(), ReviewedInternalShardIDs: []string{},
		Environment: []string{ScheduledOracleEnvironment},
		Entries:     make([]TimingEntry, 0, len(canonical)),
	}
	for _, censusEntry := range canonical {
		durations := make([]int64, 0, len(samples))
		nestedDurations := make(map[string]int64)
		for sampleIndex, sample := range samples {
			duration, exists := sample.durations[censusEntry.ID]
			if !exists {
				return TimingManifest{}, fmt.Errorf("race/repeat timing sample %d has no terminal pass for %q", sampleIndex+1, censusEntry.ID)
			}
			durations = append(durations, duration)
			for nestedID, nestedDuration := range sample.nested[censusEntry.ID] {
				nestedDurations[nestedID] = max(nestedDurations[nestedID], nestedDuration)
			}
		}
		slices.Sort(durations)
		entry := TimingEntry{
			ID: censusEntry.ID, Kind: censusEntry.Kind,
			DurationWeightNanoseconds: durations[len(durations)/2], SampleCount: len(durations),
			NestedCaseCount: len(nestedDurations),
		}
		for _, duration := range nestedDurations {
			entry.MaximumNestedCaseNanoseconds = max(entry.MaximumNestedCaseNanoseconds, duration)
		}
		entry.DurationWeightNanoseconds = max(entry.DurationWeightNanoseconds, entry.MaximumNestedCaseNanoseconds)
		manifest.DefaultWeightNanoseconds = max(manifest.DefaultWeightNanoseconds, entry.DurationWeightNanoseconds)
		manifest.Entries = append(manifest.Entries, entry)
	}
	if err := manifest.Validate(); err != nil {
		return TimingManifest{}, err
	}
	return manifest, nil
}

// CanonicalTimingJSON returns deterministic checked-in timing bytes.
func CanonicalTimingJSON(manifest TimingManifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat timing manifest: %w", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat timing manifest: %w", err)
	}
	return append(data, '\n'), nil
}

// LoadTimingManifest strictly decodes the reviewed timing manifest.
func LoadTimingManifest(path string) (TimingManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TimingManifest{}, fmt.Errorf("read race/repeat timing manifest %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest TimingManifest
	if err := decoder.Decode(&manifest); err != nil {
		return TimingManifest{}, fmt.Errorf("decode race/repeat timing manifest %s: %w", path, err)
	}
	if err := manifest.Validate(); err != nil {
		return TimingManifest{}, err
	}
	return manifest, nil
}

func elapsedNanoseconds(number json.Number) (int64, error) {
	if number == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseFloat(string(number), 64)
	if err != nil || seconds < 0 {
		return 0, errors.New("elapsed seconds are invalid")
	}
	return int64(seconds * float64(time.Second)), nil
}
