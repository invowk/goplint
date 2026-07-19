// SPDX-License-Identifier: MPL-2.0

package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/mutationguard"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const (
	mutationManifestFormatVersion = 2
	mutationProfileFormatVersion  = 2
	mutationResultFormatVersion   = 2
	mutationGuardTimeout          = 2 * time.Minute
	mutationProcessWaitDelay      = 10 * time.Second
)

var mutationCategories = []string{
	"missing-constructor-validate",
	"missing-constructor-validate-inconclusive",
	"unvalidated-boundary-request",
	"unvalidated-cast",
	"unvalidated-cast-inconclusive",
	"use-before-validate-cross-block",
	"use-before-validate-inconclusive",
	"use-before-validate-same-block",
}

var mutationExecutionStages = []soundnessevidence.ExecutionStage{
	soundnessevidence.StageSourceExtraction,
	soundnessevidence.StageIdentity,
	soundnessevidence.StageGraphConstruction,
	soundnessevidence.StagePropagation,
	soundnessevidence.StageRefinement,
	soundnessevidence.StageAggregation,
	soundnessevidence.StageReporting,
}

type mutationManifest struct {
	FormatVersion int                `json:"format_version"`
	Profile       string             `json:"profile"`
	Policy        mutationPolicy     `json:"policy"`
	Mutations     []targetedMutation `json:"mutations"`
}

type mutationPolicy struct {
	MaxSurvivors      int  `json:"max_survivors"`
	AllowCompileKills bool `json:"allow_compile_kills"`
	AllowBaseline     bool `json:"allow_baseline"`
}

type targetedMutation struct {
	ID            string                             `json:"id"`
	Concern       string                             `json:"concern"`
	ChangedStages []soundnessevidence.ExecutionStage `json:"changed_stages"`
	File          string                             `json:"file"`
	Before        string                             `json:"before"`
	BeforeSHA256  string                             `json:"before_sha256"`
	After         string                             `json:"after"`
	AfterSHA256   string                             `json:"after_sha256"`
	Guard         mutationGuard                      `json:"guard"`
	Categories    []string                           `json:"categories,omitempty"`
}

type mutationGuard struct {
	TestRegex          string                  `json:"test_regex"`
	SelectedTests      []string                `json:"selected_tests"`
	ExpectedMismatches []expectedGuardMismatch `json:"expected_mismatches"`
}

type expectedGuardMismatch struct {
	Assertion guardAssertion            `json:"assertion"`
	Expected  mutationguard.Observation `json:"expected"`
	Actual    mutationguard.Observation `json:"actual"`
}

type guardAssertion struct {
	Test string `json:"test"`
	ID   string `json:"id"`
}

type mutationProfile struct {
	FormatVersion int      `json:"format_version"`
	Manifest      string   `json:"manifest"`
	Count         int      `json:"count"`
	MutationIDs   []string `json:"mutation_ids"`
}

type mutationStatus string

const (
	mutationStatusKilled   mutationStatus = "killed"
	mutationStatusSurvived mutationStatus = "survived"
	mutationStatusInvalid  mutationStatus = "invalid"
)

type mutationReasonCode string

const (
	mutationReasonNone                     mutationReasonCode = ""
	mutationReasonWorkspace                mutationReasonCode = "workspace-error"
	mutationReasonControl                  mutationReasonCode = "control-failure"
	mutationReasonAnchor                   mutationReasonCode = "anchor-failure"
	mutationReasonTransformation           mutationReasonCode = "transformation-failure"
	mutationReasonCompile                  mutationReasonCode = "compile-failure"
	mutationReasonTimeout                  mutationReasonCode = "timeout"
	mutationReasonPanic                    mutationReasonCode = "panic"
	mutationReasonMissingOrSkippedGuard    mutationReasonCode = "missing-or-skipped-guard"
	mutationReasonUnexpectedTestFailure    mutationReasonCode = "unexpected-test-failure"
	mutationReasonUnattributedGuardFailure mutationReasonCode = "unattributed-guard-failure"
	mutationReasonWrongAssertion           mutationReasonCode = "wrong-assertion"
	mutationReasonWrongExpectedObservation mutationReasonCode = "wrong-expected-observation"
	mutationReasonWrongActualObservation   mutationReasonCode = "wrong-actual-observation"
	mutationReasonMalformedAttribution     mutationReasonCode = "malformed-attribution"
	mutationReasonNonRepeatableAttribution mutationReasonCode = "non-repeatable-attribution"
	mutationReasonRestoration              mutationReasonCode = "restoration-failure"
)

type mutationResult struct {
	ID              string                `json:"id"`
	Status          mutationStatus        `json:"status"`
	ReasonCode      mutationReasonCode    `json:"reason_code,omitempty"`
	GuardMismatches []guardMismatchRecord `json:"guard_mismatches,omitempty"`
	Reason          string                `json:"reason,omitempty"`
}

type goTestEvent struct {
	Action string `json:"Action"`
	Test   string `json:"Test"`
	Output string `json:"Output"`
}

type guardMismatchRecord struct {
	MutantID                    string                    `json:"mutant_id"`
	DeclaredConcern             string                    `json:"declared_concern"`
	FailingAssertion            guardAssertion            `json:"failing_assertion"`
	ExpectedSemanticObservation mutationguard.Observation `json:"expected_semantic_observation"`
	ActualSemanticObservation   mutationguard.Observation `json:"actual_semantic_observation"`
}

type commandResult struct {
	Output   string
	Err      error
	TimedOut bool
}

type guardRunner func(workdir, testRegex string, count int) commandResult

type compileRunner func(workdir string) commandResult

type sourceSnapshot struct {
	Content []byte
	Mode    fs.FileMode
}

type mutationRuntime struct {
	ReadSource  func(path string) (sourceSnapshot, error)
	WriteSource func(path string, snapshot sourceSnapshot) error
	Compile     compileRunner
	RunGuard    guardRunner
}

type observedGuardMismatch struct {
	Test  string
	Event mutationguard.AssertionEvent
}

type goTestTrace struct {
	RunCounts      map[string]int
	PassCounts     map[string]int
	FailCounts     map[string]int
	SkipCounts     map[string]int
	Mismatches     []observedGuardMismatch
	PackageFailure bool
	Panicked       bool
	TimedOut       bool
}

type guardClassification struct {
	Status     mutationStatus
	ReasonCode mutationReasonCode
	Reason     string
	Records    []guardMismatchRecord
}

func main() {
	profilePath := flag.String("profile", "testdata/mutation/profiles/blocking-v2.json", "targeted mutation profile")
	flag.Parse()
	if err := run(*profilePath); err != nil {
		fmt.Fprintln(os.Stderr, "targeted mutation gate:", err)
		os.Exit(1)
	}
}

func run(profilePath string) error {
	moduleRoot, err := findModuleRoot()
	if err != nil {
		return fmt.Errorf("find targeted mutation module root: %w", err)
	}
	profile, err := readJSON[mutationProfile](filepath.Join(moduleRoot, profilePath))
	if err != nil {
		return err
	}
	manifest, err := readJSON[mutationManifest](filepath.Join(moduleRoot, filepath.FromSlash(profile.Manifest)))
	if err != nil {
		return err
	}
	mutations, err := validateProfile(profile, manifest)
	if err != nil {
		return err
	}
	originalDigests, err := mutationSourceDigests(moduleRoot, mutations)
	if err != nil {
		return err
	}
	results := make([]mutationResult, 0, len(mutations))
	failed := false
	for _, mutation := range mutations {
		result := executeMutation(moduleRoot, mutation, profile.Count)
		results = append(results, result)
		if result.Status != mutationStatusKilled {
			failed = true
		}
	}
	if err := verifyMutationSourceDigests(moduleRoot, originalDigests); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(struct {
		FormatVersion int              `json:"format_version"`
		Profile       string           `json:"profile"`
		Results       []mutationResult `json:"results"`
	}{FormatVersion: mutationResultFormatVersion, Profile: manifest.Profile, Results: results}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal mutation results: %w", err)
	}
	fmt.Println(string(encoded))
	if failed {
		return errors.New("one or more targeted soundness mutants survived or were invalid")
	}
	if err := emitMutationEvidence(mutations, results); err != nil {
		return err
	}
	observations := make([]soundnessgate.ObservedMember, 0, 6*len(mutations))
	for _, mutation := range mutations {
		observations = append(observations,
			soundnessgate.ObservedMember{PopulationID: "causal-mutants", MemberID: mutation.ID},
			soundnessgate.ObservedMember{PopulationID: "clean-controls", MemberID: mutation.ID + ":pre"},
			soundnessgate.ObservedMember{PopulationID: "clean-controls", MemberID: mutation.ID + ":post"},
			soundnessgate.ObservedMember{PopulationID: "intended-mismatches", MemberID: mutation.ID},
			soundnessgate.ObservedMember{PopulationID: "restorations", MemberID: mutation.ID},
			soundnessgate.ObservedMember{PopulationID: "selected-guards", MemberID: mutation.ID},
		)
	}
	populations, populationErr := soundnessgate.PopulationsFromObservedMembers(observations)
	if populationErr != nil {
		return fmt.Errorf("derive targeted mutation populations: %w", populationErr)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(context.Background(), populations); err != nil {
		return fmt.Errorf("emit targeted mutation report: %w", err)
	}
	return nil
}

func runControlGuards(
	moduleRoot string,
	mutations []targetedMutation,
	count int,
	runner guardRunner,
	stage string,
) error {
	if runner == nil {
		return errors.New("targeted mutation guard runner is nil")
	}
	for _, mutation := range mutations {
		result := runner(moduleRoot, mutation.Guard.TestRegex, count)
		trace, traceErr := parseGoTestTrace(result.Output)
		if traceErr != nil {
			return fmt.Errorf("%s guard %q output is invalid: %w", stage, mutation.Guard.TestRegex, traceErr)
		}
		if controlErr := validateCleanGuardRun(trace, result, mutation.Guard, count); controlErr != nil {
			return fmt.Errorf(
				"%s guard %q failed before causal attribution: %w: %s",
				stage,
				mutation.Guard.TestRegex,
				controlErr,
				compactOutput(result.Output),
			)
		}
	}
	return nil
}

func validateProfile(profile mutationProfile, manifest mutationManifest) ([]targetedMutation, error) {
	if profile.FormatVersion != mutationProfileFormatVersion ||
		manifest.FormatVersion != mutationManifestFormatVersion || profile.Count < 2 ||
		manifest.Policy.MaxSurvivors != 0 || manifest.Policy.AllowCompileKills || manifest.Policy.AllowBaseline {
		return nil, errors.New("unsupported or weakened targeted mutation policy")
	}
	byID := make(map[string]targetedMutation, len(manifest.Mutations))
	for _, mutation := range manifest.Mutations {
		if mutation.ID == "" || mutation.File == "" || mutation.Before == "" || mutation.After == "" ||
			mutation.BeforeSHA256 == "" || mutation.AfterSHA256 == "" || mutation.Concern == "" {
			return nil, fmt.Errorf("mutation %q is incomplete", mutation.ID)
		}
		if err := validateChangedStages(mutation.ID, mutation.ChangedStages); err != nil {
			return nil, err
		}
		if err := validateMutationGuard(mutation.ID, mutation.Guard); err != nil {
			return nil, err
		}
		if _, duplicate := byID[mutation.ID]; duplicate {
			return nil, fmt.Errorf("duplicate mutation %q", mutation.ID)
		}
		byID[mutation.ID] = mutation
	}
	if len(profile.MutationIDs) != len(manifest.Mutations) {
		return nil, errors.New("profile must include every targeted mutation")
	}
	mutations := make([]targetedMutation, 0, len(profile.MutationIDs))
	seen := make(map[string]bool, len(profile.MutationIDs))
	for _, id := range profile.MutationIDs {
		mutation, ok := byID[id]
		if !ok || seen[id] {
			return nil, fmt.Errorf("profile contains missing or duplicate mutation %q", id)
		}
		seen[id] = true
		mutations = append(mutations, mutation)
	}
	categoryCounts := make(map[string]int, len(mutationCategories))
	categoryStages := make(map[string]map[soundnessevidence.ExecutionStage]bool, len(mutationCategories))
	for _, mutation := range mutations {
		if len(mutation.Categories) == 0 {
			return nil, fmt.Errorf("mutation %q has no protocol categories", mutation.ID)
		}
		seenCategories := make(map[string]bool, len(mutation.Categories))
		previousCategory := ""
		for _, category := range mutation.Categories {
			if !slices.Contains(mutationCategories, category) {
				return nil, fmt.Errorf("mutation %q has unknown protocol category %q", mutation.ID, category)
			}
			if seenCategories[category] || (previousCategory != "" && category < previousCategory) {
				return nil, fmt.Errorf("mutation %q categories are duplicate or not canonical", mutation.ID)
			}
			seenCategories[category] = true
			previousCategory = category
			categoryCounts[category]++
			if categoryStages[category] == nil {
				categoryStages[category] = make(map[soundnessevidence.ExecutionStage]bool)
			}
			for _, stage := range mutation.ChangedStages {
				categoryStages[category][stage] = true
			}
		}
	}
	for _, category := range mutationCategories {
		if categoryCounts[category] == 0 {
			return nil, fmt.Errorf("protocol category %q has no targeted mutant", category)
		}
		for _, stage := range mutationExecutionStages {
			if !categoryStages[category][stage] {
				return nil, fmt.Errorf("protocol category %q has no targeted mutant for stage %q", category, stage)
			}
		}
	}
	return mutations, nil
}

func validateChangedStages(mutationID string, stages []soundnessevidence.ExecutionStage) error {
	if len(stages) == 0 {
		return fmt.Errorf("mutation %q changed_stages is empty", mutationID)
	}
	previousIndex := -1
	for _, stage := range stages {
		stageIndex := slices.Index(mutationExecutionStages, stage)
		if stageIndex < 0 {
			return fmt.Errorf("mutation %q has unknown changed stage %q", mutationID, stage)
		}
		if stageIndex <= previousIndex {
			return fmt.Errorf("mutation %q changed_stages is duplicate or not canonical", mutationID)
		}
		previousIndex = stageIndex
	}
	return nil
}

func validateMutationGuard(mutationID string, guard mutationGuard) error {
	if guard.TestRegex == "" || len(guard.SelectedTests) == 0 || len(guard.ExpectedMismatches) == 0 {
		return fmt.Errorf("mutation %q guard is incomplete", mutationID)
	}
	guardPattern, err := regexp.Compile(guard.TestRegex)
	if err != nil {
		return fmt.Errorf("mutation %q has invalid guard test_regex: %w", mutationID, err)
	}
	selected := make(map[string]bool, len(guard.SelectedTests))
	previousTest := ""
	for _, testName := range guard.SelectedTests {
		if testName == "" || strings.Contains(testName, "/") || !guardPattern.MatchString(testName) || selected[testName] {
			return fmt.Errorf("mutation %q has invalid or duplicate selected test %q", mutationID, testName)
		}
		if previousTest != "" && testName < previousTest {
			return fmt.Errorf("mutation %q selected_tests is not canonical", mutationID)
		}
		previousTest = testName
		selected[testName] = true
	}
	seenAssertions := make(map[string]bool, len(guard.ExpectedMismatches))
	previousAssertion := ""
	for _, mismatch := range guard.ExpectedMismatches {
		rootTest, _, _ := strings.Cut(mismatch.Assertion.Test, "/")
		key := mismatch.Assertion.Test + "\x00" + mismatch.Assertion.ID
		if mismatch.Assertion.Test == "" || mismatch.Assertion.ID == "" || !selected[rootTest] || seenAssertions[key] {
			return fmt.Errorf("mutation %q has invalid or duplicate guard assertion %q", mutationID, key)
		}
		if previousAssertion != "" && key < previousAssertion {
			return fmt.Errorf("mutation %q expected_mismatches is not canonical", mutationID)
		}
		event := mutationguard.AssertionEvent{
			FormatVersion: mutationguard.EventFormatVersion,
			AssertionID:   mismatch.Assertion.ID,
			Expected:      mismatch.Expected,
			Actual:        mismatch.Actual,
		}
		if err := event.Validate(); err != nil {
			return fmt.Errorf("mutation %q assertion %q: %w", mutationID, key, err)
		}
		previousAssertion = key
		seenAssertions[key] = true
	}
	return nil
}

func emitMutationEvidence(mutations []targetedMutation, results []mutationResult) error {
	casesByCategory, err := mutationEvidenceCases(mutations, results)
	if err != nil {
		return err
	}
	for _, category := range mutationCategories {
		cases := casesByCategory[category]
		if len(cases) == 0 {
			return fmt.Errorf("protocol category %q has no causal mutation case", category)
		}
		observation, err := soundnessevidence.ObservationFromCases(
			category+".mutation",
			"targeted-mutation",
			"cmd/targeted-mutation/"+category,
			cases,
		)
		if err != nil {
			return fmt.Errorf("build targeted mutation observation %s: %w", category, err)
		}
		if _, err := soundnessevidence.EmitObservationFromEnvironment(context.Background(), observation); err != nil {
			return fmt.Errorf("emit targeted mutation observation %s: %w", category, err)
		}
	}
	return nil
}

func mutationEvidenceCases(
	mutations []targetedMutation,
	results []mutationResult,
) (map[string][]soundnessevidence.SemanticCase, error) {
	featureByCategory := map[string]string{
		"missing-constructor-validate":              "constructor-validation",
		"missing-constructor-validate-inconclusive": "constructor-validation",
		"unvalidated-boundary-request":              "boundary-request-validation",
		"unvalidated-cast":                          "cast-validation",
		"unvalidated-cast-inconclusive":             "cast-validation",
		"use-before-validate-cross-block":           "use-before-validation",
		"use-before-validate-inconclusive":          "use-before-validation",
		"use-before-validate-same-block":            "use-before-validation",
	}
	properties := []string{
		"clean-control-passed",
		"declared-guard-selected",
		"exact-anchor-selected",
		"exact-transformation-applied",
		"intended-mismatch-observed",
		"mismatch-repeatable",
		"mutant-compiled",
		"post-control-passed",
		"source-restored",
	}
	resultByID := make(map[string]mutationResult, len(results))
	for _, result := range results {
		if _, exists := resultByID[result.ID]; exists {
			return nil, fmt.Errorf("duplicate targeted mutation result %q", result.ID)
		}
		resultByID[result.ID] = result
	}
	if len(resultByID) != len(mutations) {
		return nil, fmt.Errorf("targeted mutation result count = %d, want %d", len(resultByID), len(mutations))
	}
	casesByCategory := make(map[string][]soundnessevidence.SemanticCase, len(featureByCategory))
	for _, mutation := range mutations {
		result, exists := resultByID[mutation.ID]
		if !exists || result.Status != mutationStatusKilled || len(result.GuardMismatches) == 0 {
			return nil, fmt.Errorf("targeted mutation %q lacks a causal killed result", mutation.ID)
		}
		for _, category := range mutation.Categories {
			feature, exists := featureByCategory[category]
			if !exists {
				return nil, fmt.Errorf("targeted mutation %q has unknown evidence category %q", mutation.ID, category)
			}
			dimensions := []string{feature, "guards", "mutants"}
			sort.Strings(dimensions)
			casesByCategory[category] = append(casesByCategory[category], soundnessevidence.SemanticCase{
				ID:        mutation.ID,
				Category:  category,
				Layer:     soundnessevidence.LayerMutation,
				FeatureID: feature,
				Boundary:  soundnessevidence.BoundaryMutationRunner,
				ExecutedBoundaries: soundnessevidence.CanonicalBoundaries(
					soundnessevidence.BoundaryMutationRunner,
				),
				Outcome:    soundnessevidence.OutcomeMutantKilled,
				Stages:     slices.Clone(mutation.ChangedStages),
				Properties: slices.Clone(properties),
				Dimensions: dimensions,
			})
		}
	}
	for category, cases := range casesByCategory {
		slices.SortFunc(cases, func(left, right soundnessevidence.SemanticCase) int {
			return strings.Compare(left.ID, right.ID)
		})
		casesByCategory[category] = cases
	}
	return casesByCategory, nil
}

func mutationSourceDigests(moduleRoot string, mutations []targetedMutation) (map[string][sha256.Size]byte, error) {
	digests := make(map[string][sha256.Size]byte)
	for _, mutation := range mutations {
		if _, exists := digests[mutation.File]; exists {
			continue
		}
		content, err := os.ReadFile(filepath.Join(moduleRoot, filepath.FromSlash(mutation.File)))
		if err != nil {
			return nil, fmt.Errorf("read mutation source %s: %w", mutation.File, err)
		}
		digests[mutation.File] = sha256.Sum256(content)
	}
	return digests, nil
}

func verifyMutationSourceDigests(moduleRoot string, original map[string][sha256.Size]byte) error {
	for path, want := range original {
		content, err := os.ReadFile(filepath.Join(moduleRoot, filepath.FromSlash(path)))
		if err != nil {
			return fmt.Errorf("re-read mutation source %s: %w", path, err)
		}
		if got := sha256.Sum256(content); got != want {
			return fmt.Errorf("mutation source %s was not restored", path)
		}
	}
	return nil
}

func copyModule(sourceRoot, targetRoot string) error {
	err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %s: %w", path, walkErr)
		}
		relative, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return fmt.Errorf("relative mutation path for %s: %w", path, err)
		}
		if relative == "." {
			return nil
		}
		if slices.Contains([]string{"artifacts", ".git"}, entry.Name()) && entry.IsDir() {
			return filepath.SkipDir
		}
		destination := filepath.Join(targetRoot, relative)
		if entry.IsDir() {
			if err := os.MkdirAll(destination, 0o755); err != nil {
				return fmt.Errorf("create mutation directory %s: %w", destination, err)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat mutation source %s: %w", path, err)
		}
		return copyFile(path, destination, info.Mode())
	})
	if err != nil {
		return fmt.Errorf("copy mutation module: %w", err)
	}
	return nil
}

func copyFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open mutation source %s: %w", source, err)
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return errors.Join(
			fmt.Errorf("open mutation destination %s: %w", destination, err),
			input.Close(),
		)
	}
	_, copyErr := io.Copy(output, input)
	outputCloseErr := output.Close()
	inputCloseErr := input.Close()
	if copyErr != nil {
		copyErr = fmt.Errorf("copy mutation file %s: %w", source, copyErr)
	}
	if outputCloseErr != nil {
		outputCloseErr = fmt.Errorf("close mutation destination %s: %w", destination, outputCloseErr)
	}
	if inputCloseErr != nil {
		inputCloseErr = fmt.Errorf("close mutation source %s: %w", source, inputCloseErr)
	}
	return errors.Join(copyErr, outputCloseErr, inputCloseErr)
}

func findModuleRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("go.mod not found")
		}
		current = parent
	}
}

func readJSON[T any](path string) (T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("read JSON %s: %w", path, err)
	}
	return decodeJSON[T](string(data), path)
}

func decodeJSON[T any](data, source string) (T, error) {
	var value T
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, fmt.Errorf("decode JSON %s: %w", source, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, fmt.Errorf("decode JSON %s: trailing JSON value", source)
		}
		return value, fmt.Errorf("decode JSON %s trailing data: %w", source, err)
	}
	return value, nil
}

func compactOutput(output string) string {
	lines := strings.Fields(output)
	if len(lines) > 24 {
		lines = lines[:24]
	}
	sort.Strings(lines)
	return strings.Join(lines, " ")
}
