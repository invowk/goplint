// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestCaptureTimingManifestUsesOneCensusAndExactFreshSampleCount(t *testing.T) {
	t.Parallel()

	var calls [][]string
	run := func(_ context.Context, directory string, arguments ...string) ([]byte, error) {
		calls = append(calls, append([]string{directory}, arguments...))
		if slices.Contains(arguments, "-list") {
			return []byte("TestAlpha\nFuzzBeta\n"), nil
		}
		return []byte(
			`{"Action":"pass","Test":"TestAlpha","Elapsed":1.2}` + "\n" +
				`{"Action":"pass","Test":"FuzzBeta","Elapsed":0.2}` + "\n",
		), nil
	}
	manifest, err := captureTimingManifest(t.Context(), CaptureOptions{
		ModuleRoot: ".", PackagePath: "./goplint", SampleCount: 3,
		GeneratedAt: time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC),
	}, run)
	if err != nil {
		t.Fatalf("captureTimingManifest() error = %v", err)
	}
	if len(calls) != 4 {
		t.Fatalf("command count = %d, want 4", len(calls))
	}
	if manifest.Entries[1].SampleCount != 3 || manifest.DefaultWeightNanoseconds != 1_200_000_000 {
		t.Fatalf("timing manifest = %+v", manifest)
	}
}
