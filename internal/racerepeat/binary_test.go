// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCensusFromBinaryUsesExactBinary(t *testing.T) {
	t.Parallel()

	var gotDirectory, gotBinary string
	census, err := censusFromBinary(t.Context(), "/module", "/tmp/goplint-race.test", func(
		_ context.Context, directory, binaryPath string,
	) ([]byte, error) {
		gotDirectory, gotBinary = directory, binaryPath
		return []byte("TestAlpha\nFuzzBeta\n"), nil
	})
	if err != nil {
		t.Fatalf("censusFromBinary() error = %v", err)
	}
	if gotDirectory != "/module" || gotBinary != "/tmp/goplint-race.test" {
		t.Fatalf("binary census command = %q %q", gotDirectory, gotBinary)
	}
	want := []CensusEntry{{ID: "FuzzBeta", Kind: "fuzz"}, {ID: "TestAlpha", Kind: "test"}}
	if !slices.Equal(census, want) {
		t.Fatalf("binary census = %#v, want %#v", census, want)
	}
}

func TestCensusFromBinaryRejectsCommandFailureAndEmptyCensus(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("list failed")
	_, err := censusFromBinary(t.Context(), "/module", "/tmp/test", func(context.Context, string, string) ([]byte, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("censusFromBinary() error = %v, want %v", err, wantErr)
	}
	_, err = censusFromBinary(t.Context(), "/module", "/tmp/test", func(context.Context, string, string) ([]byte, error) {
		return []byte{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty census error = %v", err)
	}
}

func TestRequireEquivalentModeCensuses(t *testing.T) {
	t.Parallel()

	common := []CensusEntry{{ID: "TestAlpha", Kind: "test"}}
	if err := RequireEquivalentModeCensuses(common, slices.Clone(common)); err != nil {
		t.Fatalf("RequireEquivalentModeCensuses() error = %v", err)
	}
	normal := append(slices.Clone(common), CensusEntry{ID: "TestNormal", Kind: "test"})
	race := append(slices.Clone(common), CensusEntry{ID: "TestRace", Kind: "test"})
	err := RequireEquivalentModeCensuses(normal, race)
	if err == nil || !strings.Contains(err.Error(), "normal_only=[TestNormal]") ||
		!strings.Contains(err.Error(), "race_only=[TestRace]") {
		t.Fatalf("mode census drift error = %v", err)
	}
}
