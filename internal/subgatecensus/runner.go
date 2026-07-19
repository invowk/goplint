// SPDX-License-Identifier: MPL-2.0

package subgatecensus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

type testEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

type runObservation struct {
	run              Run
	resolvedPackages []string
	passes           map[string]int
	skips            map[string]int
	packagePasses    map[string]int
}

// RunManifest executes every declared run and derives exact current populations.
func RunManifest(
	ctx context.Context,
	manifest Manifest,
	output io.Writer,
) ([]soundnessgate.Population, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	observations := make(map[string]runObservation, len(manifest.Runs))
	for _, run := range manifest.Runs {
		observation, err := executeRun(ctx, run, output)
		if err != nil {
			return nil, fmt.Errorf("run %q: %w", run.ID, err)
		}
		observations[run.ID] = observation
	}
	return derivePopulations(manifest, observations)
}

func executeRun(ctx context.Context, run Run, output io.Writer) (runObservation, error) {
	resolvedPackages, err := resolvePackages(ctx, run.Packages)
	if err != nil {
		return runObservation{}, err
	}
	if len(run.Tests) > 0 {
		if err := enumerateRequiredTests(ctx, run, resolvedPackages); err != nil {
			return runObservation{}, err
		}
	}

	arguments := []string{"test", "-json", "-count=" + strconv.Itoa(run.Count)}
	timeout := run.TimeoutSeconds
	if timeout == 0 {
		timeout = 600
	}
	arguments = append(arguments, "-timeout="+(time.Duration(timeout)*time.Second).String())
	if len(run.Tests) > 0 {
		arguments = append(arguments, "-run", exactTestPattern(run.Tests))
	}
	arguments = append(arguments, run.Packages...)
	command := exec.CommandContext(ctx, "go", arguments...)
	command.Env = os.Environ()
	stdout, err := command.StdoutPipe()
	if err != nil {
		return runObservation{}, fmt.Errorf("capture go test output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return runObservation{}, fmt.Errorf("start go test: %w", err)
	}
	observation := runObservation{
		run:              run,
		resolvedPackages: resolvedPackages,
		passes:           make(map[string]int),
		skips:            make(map[string]int),
		packagePasses:    make(map[string]int),
	}
	decoder := json.NewDecoder(stdout)
	for {
		var event testEvent
		if err := decoder.Decode(&event); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			killErr := command.Process.Kill()
			waitErr := command.Wait()
			return runObservation{}, fmt.Errorf(
				"decode go test event and stop process: %w",
				errors.Join(err, killErr, waitErr),
			)
		}
		if event.Output != "" && output != nil {
			if _, err := io.WriteString(output, event.Output); err != nil {
				killErr := command.Process.Kill()
				waitErr := command.Wait()
				return runObservation{}, fmt.Errorf(
					"stream go test output and stop process: %w",
					errors.Join(err, killErr, waitErr),
				)
			}
		}
		key := event.Package + "\x00" + event.Test
		switch event.Action {
		case "pass":
			if event.Test == "" {
				observation.packagePasses[event.Package]++
			} else {
				observation.passes[key]++
			}
		case "skip":
			observation.skips[key]++
		}
	}
	if err := command.Wait(); err != nil {
		return runObservation{}, fmt.Errorf("go test failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if stderr.Len() > 0 && output != nil {
		if _, err := io.Copy(output, &stderr); err != nil {
			return runObservation{}, fmt.Errorf("stream go test stderr: %w", err)
		}
	}
	return observation, nil
}

func resolvePackages(ctx context.Context, packages []string) ([]string, error) {
	arguments := append([]string{"list"}, packages...)
	output, err := exec.CommandContext(ctx, "go", arguments...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("resolve packages: %w: %s", err, strings.TrimSpace(string(output)))
	}
	resolved := strings.Fields(string(output))
	if len(resolved) != len(packages) || duplicatesOrEmpty(resolved) {
		return nil, fmt.Errorf("resolved package census = %v, want %d unique packages", resolved, len(packages))
	}
	return resolved, nil
}

func enumerateRequiredTests(ctx context.Context, run Run, resolvedPackages []string) error {
	pattern := exactTestPattern(run.Tests)
	for index, packageArgument := range run.Packages {
		output, err := exec.CommandContext(ctx, "go", "test", "-list", pattern, packageArgument).CombinedOutput()
		if err != nil {
			return fmt.Errorf("enumerate package %s: %w: %s", packageArgument, err, strings.TrimSpace(string(output)))
		}
		if err := validateRequiredTestEnumeration(
			resolvedPackages[index],
			run.Tests,
			strings.Split(string(output), "\n"),
		); err != nil {
			return err
		}
	}
	return nil
}

func validateRequiredTestEnumeration(packagePath string, required, enumerated []string) error {
	listed := make(map[string]bool, len(required))
	for _, line := range enumerated {
		line = strings.TrimSpace(line)
		if !slices.Contains(required, line) {
			continue
		}
		if listed[line] {
			return fmt.Errorf("package %s enumerated duplicate required test %q", packagePath, line)
		}
		listed[line] = true
	}
	for _, test := range required {
		if !listed[test] {
			return fmt.Errorf("package %s is missing required test %q", packagePath, test)
		}
	}
	return nil
}

func exactTestPattern(tests []string) string {
	quoted := make([]string, 0, len(tests))
	for _, test := range tests {
		quoted = append(quoted, regexp.QuoteMeta(test))
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

func derivePopulations(
	manifest Manifest,
	observations map[string]runObservation,
) ([]soundnessgate.Population, error) {
	expected, err := manifest.ExpectedPopulationCounts()
	if err != nil {
		return nil, err
	}
	populations := make([]soundnessgate.Population, 0, len(manifest.Populations))
	for _, population := range manifest.Populations {
		count := 0
		for _, selector := range population.Selectors {
			observation, ok := observations[selector.Run]
			if !ok {
				return nil, fmt.Errorf("population %q has no current observation for run %q", population.ID, selector.Run)
			}
			selected, err := observedSelectorCount(observation, selector)
			if err != nil {
				return nil, fmt.Errorf("population %q: %w", population.ID, err)
			}
			count += selected
		}
		if count == 0 || count != expected[population.ID] {
			return nil, fmt.Errorf("population %q observed %d members, want exact manifest-derived %d", population.ID, count, expected[population.ID])
		}
		populations = append(populations, soundnessgate.Population{ID: population.ID, Count: count})
	}
	slices.SortFunc(populations, func(left, right soundnessgate.Population) int {
		return strings.Compare(left.ID, right.ID)
	})
	return populations, nil
}

func observedSelectorCount(observation runObservation, selector Selector) (int, error) {
	if len(observation.resolvedPackages) == 0 {
		return 0, fmt.Errorf("run %q has no resolved packages", selector.Run)
	}
	switch selector.Scope {
	case ScopePackages:
		count := 0
		for _, packagePath := range observation.resolvedPackages {
			passes := observation.packagePasses[packagePath]
			if passes != 1 {
				return 0, fmt.Errorf("run %q package %s pass count = %d, want 1", selector.Run, packagePath, passes)
			}
			count++
		}
		return count, nil
	case ScopeRoots:
		members := observation.run.Tests
		return exactTestObservationCount(observation, members)
	case ScopeAllTests:
		if err := rejectUnexpectedSelectedTests(observation, selector.Members, ""); err != nil {
			return 0, err
		}
		return exactTestObservationCount(observation, selector.Members)
	case ScopeImmediateSubtests:
		members := make([]string, 0, len(selector.Members))
		for _, member := range selector.Members {
			members = append(members, selector.Root+"/"+member)
		}
		if err := rejectUnexpectedSelectedTests(observation, members, selector.Root+"/"); err != nil {
			return 0, err
		}
		return exactTestObservationCount(observation, members)
	default:
		return 0, fmt.Errorf("unsupported selector scope %q", selector.Scope)
	}
}

func exactTestObservationCount(observation runObservation, members []string) (int, error) {
	packagePath := observation.resolvedPackages[0]
	count := 0
	for _, member := range members {
		key := packagePath + "\x00" + member
		if observation.skips[key] != 0 {
			return 0, fmt.Errorf("run %q member %q was skipped", observation.run.ID, member)
		}
		passes := observation.passes[key]
		if passes != observation.run.Count {
			return 0, fmt.Errorf("run %q member %q pass count = %d, want %d", observation.run.ID, member, passes, observation.run.Count)
		}
		count += passes
	}
	return count, nil
}

func rejectUnexpectedSelectedTests(observation runObservation, members []string, prefix string) error {
	packagePath := observation.resolvedPackages[0]
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}
	for key := range observation.passes {
		observedPackage, test, found := strings.Cut(key, "\x00")
		if !found || observedPackage != packagePath {
			continue
		}
		if prefix != "" {
			if !strings.HasPrefix(test, prefix) || strings.Contains(strings.TrimPrefix(test, prefix), "/") {
				continue
			}
		} else if !slices.ContainsFunc(observation.run.Tests, func(root string) bool {
			return test == root || strings.HasPrefix(test, root+"/")
		}) {
			continue
		}
		if !wanted[test] {
			return fmt.Errorf("run %q observed undeclared selected test %q", observation.run.ID, test)
		}
	}
	return nil
}
