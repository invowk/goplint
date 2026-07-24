// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"time"
)

type (
	// CaptureOptions configures a fresh timing-manifest capture.
	CaptureOptions struct {
		ModuleRoot  string
		PackagePath string
		SampleCount int
		GeneratedAt time.Time
	}

	commandOutputFunc func(context.Context, string, ...string) ([]byte, error)
)

// CaptureTimingManifest records a live census and fresh uncached JSON timing
// samples from one analyzer package.
func CaptureTimingManifest(ctx context.Context, options CaptureOptions) (TimingManifest, error) {
	return captureTimingManifest(ctx, options, commandOutput)
}

// LiveCensus returns the canonical top-level Test, Fuzz, and Example census
// for one package without running its test bodies.
func LiveCensus(ctx context.Context, moduleRoot, packagePath string) ([]CensusEntry, error) {
	output, err := commandOutput(ctx, moduleRoot, "test", "-list", "^(Test|Fuzz|Example)", packagePath)
	if err != nil {
		return nil, fmt.Errorf("capture race/repeat test census: %w", err)
	}
	return ParseCensus(output)
}

func captureTimingManifest(
	ctx context.Context,
	options CaptureOptions,
	run commandOutputFunc,
) (TimingManifest, error) {
	if options.ModuleRoot == "" || options.PackagePath == "" {
		return TimingManifest{}, errors.New("race/repeat timing capture requires a module root and package")
	}
	if options.SampleCount <= 0 {
		return TimingManifest{}, errors.New("race/repeat timing capture sample count must be positive")
	}
	if options.GeneratedAt.IsZero() {
		return TimingManifest{}, errors.New("race/repeat timing capture generation time is required")
	}
	censusOutput, err := run(ctx, options.ModuleRoot, "test", "-list", "^(Test|Fuzz|Example)", options.PackagePath)
	if err != nil {
		return TimingManifest{}, fmt.Errorf("capture race/repeat test census: %w", err)
	}
	census, err := ParseCensus(censusOutput)
	if err != nil {
		return TimingManifest{}, fmt.Errorf("parse race/repeat test census: %w", err)
	}
	if len(census) == 0 {
		return TimingManifest{}, errors.New("race/repeat timing capture found an empty test census")
	}
	samples := make([][]byte, 0, options.SampleCount)
	for sampleIndex := range options.SampleCount {
		output, err := run(ctx, options.ModuleRoot, "test", "-json", "-count=1", options.PackagePath)
		if err != nil {
			return TimingManifest{}, fmt.Errorf("capture race/repeat timing sample %d: %w", sampleIndex+1, err)
		}
		samples = append(samples, output)
	}
	return BuildTimingManifest(options.PackagePath, runtime.Version(), options.GeneratedAt, census, samples...)
}

func commandOutput(ctx context.Context, directory string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Dir = directory
	command.Env = append(slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "GOPLINT_PROTOCOL_ORACLE_PROFILE=")
	}), ScheduledOracleEnvironment)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go %v: %w\n%s", arguments, err, output)
	}
	return output, nil
}
