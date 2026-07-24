// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

type buildCommandFunc func(context.Context, string, string, ...string) error

type binaryCensusCommandFunc func(context.Context, string, string) ([]byte, error)

// BuildBinaries builds the normal and race analyzer test binaries exactly
// once into a private output directory and returns their digest bindings.
func BuildBinaries(ctx context.Context, moduleRoot, packagePath, outputDirectory string) ([]BinaryBinding, error) {
	return buildBinaries(ctx, moduleRoot, packagePath, outputDirectory, runBuildCommand)
}

func buildBinaries(
	ctx context.Context,
	moduleRoot, packagePath, outputDirectory string,
	run buildCommandFunc,
) ([]BinaryBinding, error) {
	if moduleRoot == "" || packagePath == "" || outputDirectory == "" {
		return nil, errors.New("race/repeat binary build has an empty module root, package, or output directory")
	}
	if err := os.MkdirAll(outputDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create race/repeat binary output directory: %w", err)
	}
	bindings := make([]BinaryBinding, 0, 2)
	for _, mode := range []string{"normal", "race"} {
		fileName := "goplint-" + mode + ".test"
		path := filepath.Join(outputDirectory, fileName)
		arguments := []string{"test", "-c", "-trimpath", "-o", path}
		if mode == "race" {
			arguments = append(arguments, "-race")
		}
		arguments = append(arguments, packagePath)
		if err := run(ctx, moduleRoot, "go", arguments...); err != nil {
			return nil, fmt.Errorf("build %s analyzer test binary: %w", mode, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s analyzer test binary: %w", mode, err)
		}
		bindings = append(bindings, BinaryBinding{Mode: mode, FileName: fileName, Digest: soundnessevidence.DigestBytes(data)})
	}
	return bindings, nil
}

func runBuildCommand(ctx context.Context, directory, executable string, arguments ...string) error {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", executable, arguments, err)
	}
	return nil
}

// CensusFromBinary lists the exact top-level Test, Fuzz, and Example members
// compiled into one prebuilt test binary.
func CensusFromBinary(ctx context.Context, directory, binaryPath string) ([]CensusEntry, error) {
	return censusFromBinary(ctx, directory, binaryPath, runBinaryCensusCommand)
}

func censusFromBinary(
	ctx context.Context,
	directory, binaryPath string,
	run binaryCensusCommandFunc,
) ([]CensusEntry, error) {
	if directory == "" || binaryPath == "" {
		return nil, errors.New("race/repeat binary census requires a directory and binary path")
	}
	output, err := run(ctx, directory, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("list race/repeat binary census: %w", err)
	}
	census, err := ParseCensus(output)
	if err != nil {
		return nil, fmt.Errorf("parse race/repeat binary census: %w", err)
	}
	if len(census) == 0 {
		return nil, errors.New("race/repeat binary census is empty")
	}
	return census, nil
}

// RequireEquivalentModeCensuses rejects build-tag drift between the normal
// and race binaries before either mode can claim exhaustive coverage.
func RequireEquivalentModeCensuses(normal, race []CensusEntry) error {
	if slices.Equal(normal, race) {
		return nil
	}
	normalIDs := make(map[string]bool, len(normal))
	raceIDs := make(map[string]bool, len(race))
	for _, entry := range normal {
		normalIDs[entry.ID] = true
	}
	for _, entry := range race {
		raceIDs[entry.ID] = true
	}
	normalOnly := make([]string, 0)
	raceOnly := make([]string, 0)
	for id := range normalIDs {
		if !raceIDs[id] {
			normalOnly = append(normalOnly, id)
		}
	}
	for id := range raceIDs {
		if !normalIDs[id] {
			raceOnly = append(raceOnly, id)
		}
	}
	slices.Sort(normalOnly)
	slices.Sort(raceOnly)
	return fmt.Errorf(
		"race/repeat binary censuses differ: normal_only=[%s] race_only=[%s]",
		strings.Join(normalOnly, ","), strings.Join(raceOnly, ","),
	)
}

func runBinaryCensusCommand(ctx context.Context, directory, binaryPath string) ([]byte, error) {
	command := exec.CommandContext(ctx, binaryPath, "-test.list", "^(Test|Fuzz|Example)")
	command.Dir = directory
	command.WaitDelay = 5 * time.Second
	command.Env = append(slices.DeleteFunc(os.Environ(), func(entry string) bool {
		return strings.HasPrefix(entry, "GOPLINT_PROTOCOL_ORACLE_PROFILE=")
	}), ScheduledOracleEnvironment)
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s -test.list: %w\n%s", binaryPath, err, output)
	}
	return output, nil
}
