// SPDX-License-Identifier: MPL-2.0

package repositoryaudit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/invowk/invowk/tools/goplint/goplint"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBuildSeparatesBaselineAndExceptionVerdicts(t *testing.T) {
	t.Parallel()

	baselinePath := filepath.Join(t.TempDir(), "baseline.toml")
	if err := goplint.WriteBaseline(baselinePath, map[string][]goplint.BaselineFinding{
		goplint.CategoryPrimitive: {
			{ID: "accepted", Message: "accepted primitive"},
			{ID: "stale", Message: "stale primitive"},
		},
	}); err != nil {
		t.Fatalf("WriteBaseline() error = %v", err)
	}
	baseline, err := goplint.LoadBaseline(baselinePath)
	if err != nil {
		t.Fatalf("LoadBaseline() error = %v", err)
	}
	exceptionsPath := filepath.Join(t.TempDir(), "exceptions.toml")
	if err := os.WriteFile(exceptionsPath, []byte(`
[[exceptions]]
pattern = "matched.pattern"
reason = "reviewed"

[[exceptions]]
pattern = "stale.pattern"
reason = "reviewed"
`), 0o600); err != nil {
		t.Fatalf("write exceptions: %v", err)
	}
	exceptions, err := goplint.LoadExceptionConfig(exceptionsPath)
	if err != nil {
		t.Fatalf("LoadExceptionConfig() error = %v", err)
	}
	result, err := Build(BuildOptions{
		Inputs: validInputs(),
		Records: []goplint.FindingStreamRecord{
			{Package: "example.com/a", Category: goplint.CategoryPrimitive, ID: "accepted", Message: "accepted primitive", Posn: "a.go:1:1"},
			{Package: "example.com/a", Category: goplint.CategoryUnvalidatedCastInconclusive, ID: "inconclusive", Message: "always visible", Posn: "a.go:2:1"},
		},
		Baseline: baseline, Exceptions: exceptions,
		StalePatterns: []string{"stale.pattern"}, PackageIDs: []string{"example.com/a"},
		Scan: validScan(),
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(result.Baseline.Matched) != 1 || len(result.Baseline.New) != 1 || len(result.Baseline.Stale) != 1 {
		t.Fatalf("baseline verdict = %+v, want one matched/new/stale", result.Baseline)
	}
	if result.Baseline.New[0].Category != goplint.CategoryUnvalidatedCastInconclusive {
		t.Fatalf("always-visible finding was not retained as new: %+v", result.Baseline.New)
	}
	if len(result.Exceptions.MatchedPatterns) != 1 || len(result.Exceptions.StalePatterns) != 1 {
		t.Fatalf("exception verdict = %+v", result.Exceptions)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestResultNormalizedJSONIgnoresOnlyExecutionTiming(t *testing.T) {
	t.Parallel()

	left := minimalValidResult(t)
	right := left
	right.Scan.StartedAt = right.Scan.StartedAt.Add(time.Minute)
	right.Scan.FinishedAt = right.Scan.FinishedAt.Add(2 * time.Minute)
	right.Scan.WallDurationNanoseconds += int64(time.Minute)
	assignResultID(t, &right)
	leftJSON, err := left.NormalizedJSON()
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, err := right.NormalizedJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("normalized timing changed semantic bytes:\nleft: %s\nright: %s", leftJSON, rightJSON)
	}
}

func TestValidateConsumerRejectsEveryChangedInputBinding(t *testing.T) {
	t.Parallel()

	mutations := map[string]func(*InputBinding){
		"workspace":  func(input *InputBinding) { input.WorkspaceDigest = changedTestDigest },
		"analyzer":   func(input *InputBinding) { input.AnalyzerDigest = changedTestDigest },
		"baseline":   func(input *InputBinding) { input.BaselineDigest = changedTestDigest },
		"exceptions": func(input *InputBinding) { input.ExceptionsDigest = changedTestDigest },
		"manifest":   func(input *InputBinding) { input.SemanticManifestDigest = changedTestDigest },
		"toolchain":  func(input *InputBinding) { input.ToolchainDigest = changedTestDigest },
		"command":    func(input *InputBinding) { input.CommandDigest = changedTestDigest },
		"mode":       func(input *InputBinding) { input.AnalyzerMode = "weakened" },
		"flags":      func(input *InputBinding) { input.Flags = append(input.Flags, "-weakened") },
		"patterns":   func(input *InputBinding) { input.PackagePatterns = []string{"./pkg/..."} },
		"cache":      func(input *InputBinding) { input.CachePolicy = "cold" },
		"purpose":    func(input *InputBinding) { input.Purpose = "update-baseline" },
	}
	result := minimalValidResult(t)
	result.Baseline.New = nil
	result.Findings = nil
	result.Scan.FindingCount = 0
	assignResultID(t, &result)
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			expected := validInputs()
			mutate(&expected)
			if err := ValidateConsumer(result, expected, result.Packages.PackageIDs, "full-scan"); err == nil {
				t.Fatal("ValidateConsumer() accepted changed input")
			}
		})
	}
}

func TestBuildRejectsDuplicateAndCollidedFindingIDs(t *testing.T) {
	t.Parallel()

	base := minimalBuildOptions(t)
	base.Records = []goplint.FindingStreamRecord{
		{Package: "example.com/a", Category: goplint.CategoryPrimitive, ID: "same", Message: "first"},
		{Package: "example.com/a", Category: goplint.CategoryPrimitive, ID: "same", Message: "first"},
	}
	if _, err := Build(base); err == nil {
		t.Fatal("Build() accepted duplicate finding ID")
	}
	base.Records[1].Message = "collided"
	if _, err := Build(base); err == nil {
		t.Fatal("Build() accepted collided finding ID")
	}
}

func TestValidateConsumerRejectsStaleInputsAndDistinctFailures(t *testing.T) {
	t.Parallel()

	result := minimalValidResult(t)
	changed := validInputs()
	changed.WorkspaceDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if err := ValidateConsumer(result, changed, []string{"example.com/a"}, "full-scan"); err == nil {
		t.Fatal("ValidateConsumer() accepted stale workspace")
	}
	if err := ValidateConsumer(result, validInputs(), []string{"example.com/a"}, "full-scan"); err == nil {
		t.Fatal("ValidateConsumer() accepted a blocking full-scan finding")
	}
	result.Baseline.New = nil
	result.Baseline.Stale = []FindingReference{{Category: goplint.CategoryPrimitive, ID: "stale", Message: "stale"}}
	assignResultID(t, &result)
	if err := ValidateConsumer(result, validInputs(), []string{"example.com/a"}, "full-scan"); err != nil {
		t.Fatalf("full-scan consumer rejected baseline-only stale ID: %v", err)
	}
	if err := ValidateConsumer(result, validInputs(), []string{"example.com/a"}, "baseline"); err != nil {
		t.Fatalf("baseline consumer rejected visible stale ID: %v", err)
	}
}

func minimalValidResult(t *testing.T) Result {
	t.Helper()
	options := minimalBuildOptions(t)
	options.Records = []goplint.FindingStreamRecord{{
		Package: "example.com/a", Category: goplint.CategoryUnvalidatedCastInconclusive,
		ID: "inconclusive", Message: "always visible", Posn: "a.go:1:1",
	}}
	result, err := Build(options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func minimalBuildOptions(t *testing.T) BuildOptions {
	t.Helper()
	baselinePath := filepath.Join(t.TempDir(), "baseline.toml")
	if err := goplint.WriteBaseline(baselinePath, map[string][]goplint.BaselineFinding{}); err != nil {
		t.Fatal(err)
	}
	baseline, err := goplint.LoadBaseline(baselinePath)
	if err != nil {
		t.Fatal(err)
	}
	exceptionsPath := filepath.Join(t.TempDir(), "exceptions.toml")
	if err := os.WriteFile(exceptionsPath, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	exceptions, err := goplint.LoadExceptionConfig(exceptionsPath)
	if err != nil {
		t.Fatal(err)
	}
	return BuildOptions{
		Inputs: validInputs(), Baseline: baseline, Exceptions: exceptions,
		PackageIDs: []string{"example.com/a"}, Scan: validScan(),
	}
}

func validInputs() InputBinding {
	return InputBinding{
		WorkspaceDigest: testDigest, AnalyzerDigest: testDigest, BaselineDigest: testDigest,
		ExceptionsDigest: testDigest, SemanticManifestDigest: testDigest,
		ToolchainDigest: testDigest, CommandDigest: testDigest,
		AnalyzerMode: "check-all+enum-sync+audit-exceptions", PackagePatterns: []string{"./..."},
		Flags:       []string{"-audit-exceptions", "-check-all", "-check-enum-sync", "-test=false"},
		CachePolicy: "warm", Purpose: canonicalPurpose,
	}
}

const changedTestDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func validScan() ScanMetadata {
	started := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	return ScanMetadata{StartedAt: started, FinishedAt: started.Add(time.Second), WallDurationNanoseconds: int64(time.Second)}
}

func assignResultID(t *testing.T, result *Result) {
	t.Helper()
	result.ResultID = ""
	id, err := result.CalculateID()
	if err != nil {
		t.Fatal(err)
	}
	result.ResultID = id
}
