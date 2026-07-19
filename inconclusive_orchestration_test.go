// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

//nolint:paralleltest // Serial helper-process proofs must finish before the aggregate report is emitted.
func TestInconclusiveSuppressionOrchestration(t *testing.T) {
	moduleRoot, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	repositoryRoot := filepath.Clean(filepath.Join(moduleRoot, "..", ".."))

	t.Run("real analyzer ignores matching mixed-category baseline", func(t *testing.T) {
		proveRealAnalyzerInconclusiveIgnoresBaseline(t, moduleRoot)
	})

	surfaces := []struct {
		name  string
		prove func(*testing.T)
	}{
		{
			name: "check-baseline",
			prove: func(t *testing.T) {
				proveMakeAnalyzerSurface(t, repositoryRoot, "check-baseline")
			},
		},
		{
			name: "check-goplint-full-scan",
			prove: func(t *testing.T) {
				proveMakeAnalyzerSurface(t, repositoryRoot, "check-goplint-full-scan")
			},
		},
		{
			name: "pre-commit",
			prove: func(t *testing.T) {
				provePreCommitSurface(t, repositoryRoot)
			},
		},
		{
			name: "ci",
			prove: func(t *testing.T) {
				proveCISurface(t, repositoryRoot)
			},
		},
		{
			name: "aggregate",
			prove: func(t *testing.T) {
				proveAggregateSurface(t, repositoryRoot)
			},
		},
	}
	for _, surface := range surfaces { //nolint:paralleltest // Surface proofs must complete serially before report emission.
		t.Run(surface.name, surface.prove)
	}
	observations := make([]soundnessgate.ObservedMember, 0, len(surfaces))
	for _, surface := range surfaces {
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "suppression-surfaces",
			MemberID:     surface.name,
		})
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers(observations)
	if err != nil {
		t.Fatalf("PopulationsFromObservedMembers() error: %v", err)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(t.Context(), populations); err != nil {
		t.Fatalf("EmitReportFromEnvironment() error: %v", err)
	}
}

func proveRealAnalyzerInconclusiveIgnoresBaseline(t *testing.T, moduleRoot string) {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "goplint")
	buildContext, cancelBuild := context.WithTimeout(t.Context(), time.Minute)
	defer cancelBuild()
	build := exec.CommandContext(buildContext, "go", "build", "-o", binaryPath, ".")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real analyzer: %v\n%s", err, output)
	}
	fixture := "./goplint/testdata/src/boundaryrequest"
	configPath := filepath.Join(moduleRoot, "goplint", "testdata", "src", "boundaryrequest", "goplint.toml")
	firstRecords := runInconclusiveAnalyzer(t, moduleRoot, binaryPath, configPath, "", fixture)
	inconclusive := findInjectedInconclusive(t, firstRecords)
	baselinePath := filepath.Join(t.TempDir(), "baseline.toml")
	baseline := fmt.Sprintf(
		"[unvalidated-boundary-request]\nentries = [\n  { id = %q, message = %q },\n]\n",
		inconclusive.ID,
		inconclusive.Message,
	)
	if err := os.WriteFile(baselinePath, []byte(baseline), 0o600); err != nil {
		t.Fatalf("write matching baseline: %v", err)
	}
	secondRecords := runInconclusiveAnalyzer(t, moduleRoot, binaryPath, configPath, baselinePath, fixture)
	observed := findInjectedInconclusive(t, secondRecords)
	if observed.ID != inconclusive.ID || observed.Message != inconclusive.Message {
		t.Fatalf("matching baseline changed inconclusive identity: first=%+v second=%+v", inconclusive, observed)
	}
}

func runInconclusiveAnalyzer(
	t *testing.T,
	moduleRoot, binaryPath, configPath, baselinePath, fixture string,
) []goplint.FindingStreamRecord {
	t.Helper()

	streamPath := filepath.Join(t.TempDir(), "findings.jsonl")
	args := []string{
		"-test=false",
		"-check-boundary-request-validation",
		"-config=" + configPath,
		"-emit-findings-jsonl=" + streamPath,
	}
	if baselinePath != "" {
		args = append(args, "-baseline="+baselinePath)
	}
	args = append(args, fixture)
	commandContext, cancelCommand := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancelCommand()
	command := exec.CommandContext(commandContext, binaryPath, args...)
	command.Dir = moduleRoot
	output, err := command.CombinedOutput()
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		t.Fatalf("real analyzer exceeded 45s fixture budget: %v\n%s", commandContext.Err(), output)
	}
	if err == nil {
		t.Fatalf("real analyzer unexpectedly accepted injected inconclusive\n%s", output)
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("real analyzer command error = %v, want diagnostic exit\n%s", err, output)
	}
	file, err := os.Open(streamPath)
	if err != nil {
		t.Fatalf("open findings stream: %v\n%s", err, output)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close findings stream: %v", closeErr)
		}
	}()
	var records []goplint.FindingStreamRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record goplint.FindingStreamRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode findings stream: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan findings stream: %v", err)
	}
	return records
}

func findInjectedInconclusive(t *testing.T, records []goplint.FindingStreamRecord) goplint.FindingStreamRecord {
	t.Helper()

	for _, record := range records {
		if record.Category == goplint.CategoryUnvalidatedBoundaryRequest &&
			record.Meta["cfg_outcome_status"] == "inconclusive" {
			return record
		}
	}
	t.Fatalf("real analyzer emitted no mixed-category inconclusive: %+v", records)
	return goplint.FindingStreamRecord{}
}

func proveMakeAnalyzerSurface(t *testing.T, repositoryRoot, target string) {
	t.Helper()

	output := makeDryRun(t, repositoryRoot, target)
	analyzerLine := commandLineContaining(t, output, "./bin/goplint -test=false")
	for _, required := range []string{"-test=false", "-check-all", "-baseline=tools/goplint/baseline.toml"} {
		if !strings.Contains(analyzerLine, required) {
			t.Fatalf("make target %s analyzer command lacks %q: %s", target, required, analyzerLine)
		}
	}
	if strings.Contains(analyzerLine, "|| true") {
		t.Fatalf("make target %s masks analyzer failure: %s", target, analyzerLine)
	}
	reporterLine := commandLineContaining(t, output, "go run ./cmd/subgate-report")
	if !strings.Contains(reporterLine, "-observation repository-scans=") || strings.Contains(reporterLine, "-population") {
		t.Fatalf("make target %s does not report an observed scan member: %s", target, reporterLine)
	}
}

func provePreCommitSurface(t *testing.T, repositoryRoot string) {
	t.Helper()

	content := readContractFile(t, filepath.Join(repositoryRoot, ".pre-commit-config.yaml"))
	assertPattern(t, content, `(?s)- id: goplint-baseline\b.*?entry: "bash -c 'make check-baseline 2>&1'"`)
	assertPattern(t, content, `(?s)- id: goplint-behavior\b.*?entry: "bash -c 'make check-goplint-soundness-core 2>&1'"`)
}

func proveCISurface(t *testing.T, repositoryRoot string) {
	t.Helper()

	content := readContractFile(t, filepath.Join(repositoryRoot, ".github", "workflows", "lint.yml"))
	assertPattern(t, content, `(?s)goplint-baseline:\s+name: goplint \(baseline\).*?run: \|\s+make check-baseline`)
	assertPattern(t, content, `(?s)goplint-tests:\s+name: goplint tests.*?run: make check-goplint-soundness-core`)
}

func proveAggregateSurface(t *testing.T, repositoryRoot string) {
	t.Helper()

	manifestPath := filepath.Join(repositoryRoot, "tools", "goplint", "spec", "soundness-gate.v1.json")
	manifest, _, err := soundnessgate.LoadManifest(t.Context(), manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	core, err := manifest.SubgatesForProfile(soundnessgate.ProfileCore)
	if err != nil {
		t.Fatalf("SubgatesForProfile(core) error: %v", err)
	}
	commands := make(map[string][]string, len(core))
	for _, subgate := range core {
		commands[subgate.ID] = subgate.Command
	}
	want := map[string][]string{
		"baseline":                 {"make", "check-baseline"},
		"full-scan":                {"make", "check-goplint-full-scan"},
		"inconclusive-suppression": {"./scripts/check-inconclusive-suppression.sh"},
	}
	for subgateID, command := range want {
		if !slices.Equal(commands[subgateID], command) {
			t.Fatalf("core aggregate subgate %q command = %v, want %v", subgateID, commands[subgateID], command)
		}
	}
	makeOutput := makeDryRun(t, repositoryRoot, "check-goplint-soundness-core")
	line := commandLineContaining(t, makeOutput, "go run ./cmd/soundness-gate")
	if !strings.Contains(line, "-profile core") || !strings.Contains(line, "-manifest tools/goplint/spec/soundness-gate.v1.json") {
		t.Fatalf("core Make target does not execute reviewed aggregate manifest: %s", line)
	}
}

func makeDryRun(t *testing.T, repositoryRoot, target string) string {
	t.Helper()

	command := exec.CommandContext(t.Context(), "make", "-n", target)
	command.Dir = repositoryRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n %s: %v\n%s", target, err, output)
	}
	return string(output)
}

func commandLineContaining(t *testing.T, output, fragment string) string {
	t.Helper()

	for line := range strings.SplitSeq(output, "\n") {
		if strings.Contains(line, fragment) {
			return line
		}
	}
	t.Fatalf("dry-run output has no command containing %q:\n%s", fragment, output)
	return ""
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertPattern(t *testing.T, content, pattern string) {
	t.Helper()

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("compile contract pattern: %v", err)
	}
	if !compiled.MatchString(content) {
		t.Fatalf("contract does not match %q", pattern)
	}
}
