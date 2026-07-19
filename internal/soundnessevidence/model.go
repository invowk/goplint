// SPDX-License-Identifier: MPL-2.0

// Package soundnessevidence defines the machine-readable evidence exchanged
// between goplint soundness producers, the semantic census, and the aggregate
// gate.
package soundnessevidence

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const (
	// ObservationFormatVersion is the supported semantic-observation format.
	ObservationFormatVersion = 2
	// RegistryFormatVersion is the supported executable evidence registry format.
	RegistryFormatVersion = 2

	// EnvRunID identifies one aggregate execution.
	EnvRunID = "GOPLINT_SOUNDNESS_RUN_ID"
	// EnvWorkspaceDigest binds evidence to the analyzed workspace contents.
	EnvWorkspaceDigest = "GOPLINT_SOUNDNESS_WORKSPACE_DIGEST"
	// EnvManifestDigest binds evidence to the aggregate manifest.
	EnvManifestDigest = "GOPLINT_SOUNDNESS_MANIFEST_DIGEST"
	// EnvCommandDigest binds evidence to the exact producer command.
	EnvCommandDigest = "GOPLINT_SOUNDNESS_COMMAND_DIGEST"
	// EnvSubgateID identifies the producer subgate.
	EnvSubgateID = "GOPLINT_SOUNDNESS_SUBGATE_ID"
	// EnvEvidenceDir is the directory into which producers write observations.
	EnvEvidenceDir = "GOPLINT_SOUNDNESS_EVIDENCE_DIR"

	LayerRuleContract       Layer = "rule-contract"
	LayerOwnerRoute         Layer = "owner-route"
	LayerMustReport         Layer = "must-report"
	LayerMustNotReport      Layer = "must-not-report"
	LayerMustBeInconclusive Layer = "must-be-inconclusive"
	LayerArtifactParity     Layer = "artifact-parity"
	LayerProduction         Layer = "production-integration"
	LayerGenerated          Layer = "generated"
	LayerMetamorphic        Layer = "metamorphic"
	LayerFuzz               Layer = "fuzz"
	LayerMutation           Layer = "mutation"
	LayerDeterminism        Layer = "determinism"

	BoundaryCatalogValidation    Boundary = "catalog-validation"
	BoundaryProductionAnalyzer   Boundary = "production-analyzer"
	BoundaryIndependentModel     Boundary = "independent-model"
	BoundaryIndependentOracle    Boundary = "independent-boundary-oracle"
	BoundaryMetamorphicAnalyzer  Boundary = "metamorphic-analyzer"
	BoundaryFuzzDecoder          Boundary = "fuzz-decoder"
	BoundaryMutationRunner       Boundary = "mutation-runner"
	BoundaryDeterminismAnalyzer  Boundary = "determinism-analyzer"
	BoundaryAggregateAdversarial Boundary = "aggregate-adversarial"

	OutcomeMustReport         Outcome = "must-report"
	OutcomeMustNotReport      Outcome = "must-not-report"
	OutcomeMustBeInconclusive Outcome = "must-be-inconclusive"
	OutcomeModelAgrees        Outcome = "model-agrees"
	OutcomeRelationPreserved  Outcome = "relation-preserved"
	OutcomePropertyDetected   Outcome = "property-detected"
	OutcomeMutantKilled       Outcome = "mutant-killed"
	OutcomeDeterministic      Outcome = "deterministic"
	OutcomeRouteExecuted      Outcome = "route-executed"
	OutcomeGateRejected       Outcome = "gate-rejected"

	StageSourceExtraction  ExecutionStage = "source-extraction"
	StageIdentity          ExecutionStage = "identity"
	StageGraphConstruction ExecutionStage = "graph-construction"
	StagePropagation       ExecutionStage = "propagation"
	StageRefinement        ExecutionStage = "refinement"
	StageAggregation       ExecutionStage = "aggregation"
	StageReporting         ExecutionStage = "reporting"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Layer identifies one independently required semantic evidence layer.
type (
	Layer string

	// Boundary identifies the execution boundary that produced an observation.
	Boundary string

	// Outcome identifies the machine-observed result of one evidence case.
	Outcome string

	// ExecutionStage identifies a production stage causally exercised by evidence.
	ExecutionStage string

	// ObservationBinding prevents observations from being replayed across runs,
	// workspace states, manifests, commands, or subgates.
	ObservationBinding struct {
		RunID           string `json:"run_id"`
		WorkspaceDigest string `json:"workspace_digest"`
		ManifestDigest  string `json:"manifest_digest"`
		CommandDigest   string `json:"command_digest"`
		SubgateID       string `json:"subgate_id"`
	}

	// ObservationResult records the actual population and diagnostic result.
	ObservationResult struct {
		Outcome         Outcome `json:"outcome"`
		CaseCount       int     `json:"case_count"`
		DiagnosticCount int     `json:"diagnostic_count"`
	}

	// SemanticCase is one executed evidence member. Its semantic claims are the
	// authority from which the enclosing observation is derived.
	SemanticCase struct {
		ID                 string           `json:"id"`
		Category           string           `json:"category"`
		Layer              Layer            `json:"layer"`
		FeatureID          string           `json:"feature_id"`
		Boundary           Boundary         `json:"boundary"`
		ExecutedBoundaries []Boundary       `json:"executed_boundaries"`
		Outcome            Outcome          `json:"outcome"`
		DiagnosticCount    int              `json:"diagnostic_count"`
		Stages             []ExecutionStage `json:"stages"`
		Properties         []string         `json:"properties"`
		Dimensions         []string         `json:"dimensions"`
	}

	// SemanticObservation is one causally executed category/layer observation.
	SemanticObservation struct {
		FormatVersion      int                `json:"format_version"`
		Binding            ObservationBinding `json:"binding"`
		RegistrationID     string             `json:"registration_id"`
		Category           string             `json:"category"`
		Layer              Layer              `json:"layer"`
		FeatureID          string             `json:"feature_id"`
		ProducerID         string             `json:"producer_id"`
		TestID             string             `json:"test_id"`
		Boundary           Boundary           `json:"boundary"`
		ExecutedBoundaries []Boundary         `json:"executed_boundaries"`
		Result             ObservationResult  `json:"result"`
		Stages             []ExecutionStage   `json:"stages"`
		Properties         []string           `json:"properties"`
		Dimensions         []string           `json:"dimensions"`
		Cases              []SemanticCase     `json:"cases"`
	}
)

// ObservationFromCases builds an observation whose semantic claims are
// derived exclusively from executed case records.
func ObservationFromCases(
	registrationID,
	producerID,
	testID string,
	cases []SemanticCase,
) (SemanticObservation, error) {
	observation := SemanticObservation{
		FormatVersion:  ObservationFormatVersion,
		RegistrationID: registrationID,
		ProducerID:     producerID,
		TestID:         testID,
		Cases:          cloneSemanticCases(cases),
	}
	summary, err := summarizeSemanticCases(observation.Cases)
	if err != nil {
		return SemanticObservation{}, err
	}
	observation.Category = summary.category
	observation.Layer = summary.layer
	observation.FeatureID = summary.featureID
	observation.Boundary = summary.boundary
	observation.ExecutedBoundaries = summary.executedBoundaries
	observation.Result = summary.result
	observation.Stages = summary.stages
	observation.Properties = summary.properties
	observation.Dimensions = summary.dimensions
	return observation, nil
}

// Validate verifies that a binding is complete and canonical.
func (binding ObservationBinding) Validate() error {
	if strings.TrimSpace(binding.RunID) == "" {
		return errors.New("observation binding run_id is empty")
	}
	digests := []struct {
		name  string
		value string
	}{
		{name: "workspace_digest", value: binding.WorkspaceDigest},
		{name: "manifest_digest", value: binding.ManifestDigest},
		{name: "command_digest", value: binding.CommandDigest},
	}
	for _, digest := range digests {
		if err := ValidateDigest("observation binding "+digest.name, digest.value); err != nil {
			return err
		}
	}
	if err := validateIdentifier("observation binding subgate_id", binding.SubgateID); err != nil {
		return err
	}
	return nil
}

// ValidateDigest verifies the canonical SHA-256 representation shared by all
// soundness evidence identities.
func ValidateDigest(name, digest string) error {
	if !digestPattern.MatchString(digest) {
		return fmt.Errorf("%s %q is not a canonical SHA-256 digest", name, digest)
	}
	return nil
}

// Validate verifies that an observation is complete, canonical, and
// non-vacuous. It rejects duplicate list elements instead of normalizing them.
func (observation SemanticObservation) Validate() error {
	if observation.FormatVersion != ObservationFormatVersion {
		return fmt.Errorf("observation format_version = %d, want %d", observation.FormatVersion, ObservationFormatVersion)
	}
	if err := observation.Binding.Validate(); err != nil {
		return err
	}
	identifiers := []struct {
		name  string
		value string
	}{
		{name: "registration_id", value: observation.RegistrationID},
		{name: "category", value: observation.Category},
		{name: "feature_id", value: observation.FeatureID},
		{name: "producer_id", value: observation.ProducerID},
		{name: "test_id", value: observation.TestID},
	}
	for _, identifier := range identifiers {
		if err := validateIdentifier("observation "+identifier.name, identifier.value); err != nil {
			return err
		}
	}
	if observation.ProducerID != observation.Binding.SubgateID {
		return fmt.Errorf("observation producer_id %q does not match binding subgate_id %q", observation.ProducerID, observation.Binding.SubgateID)
	}
	if !validLayer(observation.Layer) {
		return fmt.Errorf("observation layer %q is unsupported", observation.Layer)
	}
	if !validBoundary(observation.Boundary) {
		return fmt.Errorf("observation boundary %q is unsupported", observation.Boundary)
	}
	if err := validateBoundaries("observation executed_boundaries", observation.ExecutedBoundaries); err != nil {
		return err
	}
	if !validOutcome(observation.Result.Outcome) {
		return fmt.Errorf("observation outcome %q is unsupported", observation.Result.Outcome)
	}
	if observation.Result.CaseCount <= 0 {
		return fmt.Errorf("observation case_count = %d, want a positive population", observation.Result.CaseCount)
	}
	if observation.Result.DiagnosticCount < 0 {
		return fmt.Errorf("observation diagnostic_count = %d, want a non-negative population", observation.Result.DiagnosticCount)
	}
	if err := validateStages("observation stages", observation.Stages); err != nil {
		return err
	}
	if err := validateCanonicalStrings("observation properties", observation.Properties); err != nil {
		return err
	}
	if err := validateCanonicalStrings("observation dimensions", observation.Dimensions); err != nil {
		return err
	}
	summary, err := summarizeSemanticCases(observation.Cases)
	if err != nil {
		return fmt.Errorf("observation cases: %w", err)
	}
	if observation.Category != summary.category {
		return fmt.Errorf("observation category %q is not derived from executed cases %q", observation.Category, summary.category)
	}
	if observation.Layer != summary.layer {
		return fmt.Errorf("observation layer %q is not derived from executed cases %q", observation.Layer, summary.layer)
	}
	if observation.FeatureID != summary.featureID {
		return fmt.Errorf("observation feature_id %q is not derived from executed cases %q", observation.FeatureID, summary.featureID)
	}
	if observation.Boundary != summary.boundary {
		return fmt.Errorf("observation boundary %q is not derived from executed cases %q", observation.Boundary, summary.boundary)
	}
	if !slices.Equal(observation.ExecutedBoundaries, summary.executedBoundaries) {
		return errors.New("observation executed_boundaries are not derived from executed cases")
	}
	if observation.Result != summary.result {
		return fmt.Errorf("observation result %+v is not derived from executed cases %+v", observation.Result, summary.result)
	}
	if !slices.Equal(observation.Stages, summary.stages) {
		return errors.New("observation stages are not derived from executed cases")
	}
	if !slices.Equal(observation.Properties, summary.properties) {
		return errors.New("observation properties are not derived from executed cases")
	}
	if !slices.Equal(observation.Dimensions, summary.dimensions) {
		return errors.New("observation dimensions are not derived from executed cases")
	}
	return nil
}

type semanticCaseSummary struct {
	category           string
	layer              Layer
	featureID          string
	boundary           Boundary
	executedBoundaries []Boundary
	result             ObservationResult
	stages             []ExecutionStage
	properties         []string
	dimensions         []string
}

func summarizeSemanticCases(cases []SemanticCase) (semanticCaseSummary, error) {
	if len(cases) == 0 {
		return semanticCaseSummary{}, errors.New("executed case population is empty")
	}
	first := cases[0]
	summary := semanticCaseSummary{
		category:  first.Category,
		layer:     first.Layer,
		featureID: first.FeatureID,
		boundary:  first.Boundary,
		result: ObservationResult{
			Outcome:   first.Outcome,
			CaseCount: len(cases),
		},
	}
	stageSet := make(map[ExecutionStage]bool)
	boundarySet := make(map[Boundary]bool)
	propertySet := make(map[string]bool)
	dimensionSet := make(map[string]bool)
	previousID := ""
	for index, current := range cases {
		name := fmt.Sprintf("case[%d]", index)
		if err := validateIdentifier(name+" id", current.ID); err != nil {
			return semanticCaseSummary{}, err
		}
		if previousID != "" && current.ID <= previousID {
			return semanticCaseSummary{}, fmt.Errorf("case ids must be unique and use canonical lexical order: %q then %q", previousID, current.ID)
		}
		previousID = current.ID
		if err := validateIdentifier(name+" category", current.Category); err != nil {
			return semanticCaseSummary{}, err
		}
		if err := validateIdentifier(name+" feature_id", current.FeatureID); err != nil {
			return semanticCaseSummary{}, err
		}
		if !validLayer(current.Layer) {
			return semanticCaseSummary{}, fmt.Errorf("%s layer %q is unsupported", name, current.Layer)
		}
		if !validBoundary(current.Boundary) {
			return semanticCaseSummary{}, fmt.Errorf("%s boundary %q is unsupported", name, current.Boundary)
		}
		if err := validateBoundaries(name+" executed_boundaries", current.ExecutedBoundaries); err != nil {
			return semanticCaseSummary{}, err
		}
		if !slices.Contains(current.ExecutedBoundaries, current.Boundary) {
			return semanticCaseSummary{}, fmt.Errorf(
				"%s credited boundary %q is absent from executed_boundaries",
				name,
				current.Boundary,
			)
		}
		if !validOutcome(current.Outcome) {
			return semanticCaseSummary{}, fmt.Errorf("%s outcome %q is unsupported", name, current.Outcome)
		}
		if current.DiagnosticCount < 0 {
			return semanticCaseSummary{}, fmt.Errorf("%s diagnostic_count = %d, want a non-negative population", name, current.DiagnosticCount)
		}
		if current.Category != summary.category || current.Layer != summary.layer ||
			current.FeatureID != summary.featureID || current.Boundary != summary.boundary ||
			current.Outcome != summary.result.Outcome {
			return semanticCaseSummary{}, fmt.Errorf("%s semantic identity does not match the observation case set", name)
		}
		if err := validateSemanticCaseEvidenceContract(name, current); err != nil {
			return semanticCaseSummary{}, err
		}
		if err := validateStages(name+" stages", current.Stages); err != nil {
			return semanticCaseSummary{}, err
		}
		if err := validateCanonicalStrings(name+" properties", current.Properties); err != nil {
			return semanticCaseSummary{}, err
		}
		if err := validateCanonicalStrings(name+" dimensions", current.Dimensions); err != nil {
			return semanticCaseSummary{}, err
		}
		summary.result.DiagnosticCount += current.DiagnosticCount
		for _, stage := range current.Stages {
			stageSet[stage] = true
		}
		for _, boundary := range current.ExecutedBoundaries {
			boundarySet[boundary] = true
		}
		for _, property := range current.Properties {
			propertySet[property] = true
		}
		for _, dimension := range current.Dimensions {
			dimensionSet[dimension] = true
		}
	}
	summary.stages = canonicalStageSet(stageSet)
	summary.executedBoundaries = canonicalBoundarySet(boundarySet)
	summary.properties = canonicalStringSet(propertySet)
	summary.dimensions = canonicalStringSet(dimensionSet)
	return summary, nil
}

func validateSemanticCaseEvidenceContract(name string, current SemanticCase) error {
	requireProduction := func() error {
		if !slices.Contains(current.ExecutedBoundaries, BoundaryProductionAnalyzer) {
			return fmt.Errorf("%s evidence is disconnected from the production analyzer", name)
		}
		return nil
	}
	requireFeatureDimension := func() error {
		if !slices.Contains(current.Dimensions, current.FeatureID) {
			return fmt.Errorf("%s dimensions omit category feature %q", name, current.FeatureID)
		}
		return nil
	}
	requireProperty := func(property string) error {
		if !slices.Contains(current.Properties, property) {
			return fmt.Errorf("%s properties omit %q", name, property)
		}
		return nil
	}

	switch current.Layer {
	case LayerGenerated:
		if current.Boundary == BoundaryIndependentModel || current.Boundary == BoundaryIndependentOracle ||
			slices.Contains(current.ExecutedBoundaries, BoundaryIndependentModel) ||
			slices.Contains(current.ExecutedBoundaries, BoundaryIndependentOracle) {
			if err := requireProduction(); err != nil {
				return err
			}
			if err := requireFeatureDimension(); err != nil {
				return err
			}
			if err := requireProperty("production-analyzer-executed"); err != nil {
				return err
			}
		}
	case LayerMetamorphic:
		if current.Boundary != BoundaryMetamorphicAnalyzer {
			return fmt.Errorf("%s metamorphic evidence uses boundary %q", name, current.Boundary)
		}
		if err := requireProduction(); err != nil {
			return err
		}
		if err := requireFeatureDimension(); err != nil {
			return err
		}
		if err := requireProperty("production-analyzer-executed"); err != nil {
			return err
		}
		if err := requireProperty("relation-checked"); err != nil {
			return err
		}
	case LayerFuzz:
		if current.Boundary != BoundaryFuzzDecoder {
			return fmt.Errorf("%s fuzz evidence uses boundary %q", name, current.Boundary)
		}
		digest, _, _ := strings.Cut(current.ID, "/")
		if !digestPattern.MatchString(digest) {
			return fmt.Errorf("%s fuzz case id %q is not bound to a canonical seed digest", name, current.ID)
		}
		if err := requireProduction(); err != nil {
			return err
		}
		if !slices.Contains(current.ExecutedBoundaries, BoundaryIndependentModel) &&
			!slices.Contains(current.ExecutedBoundaries, BoundaryIndependentOracle) {
			return fmt.Errorf("%s fuzz evidence has no executed independent model or oracle", name)
		}
		if err := requireFeatureDimension(); err != nil {
			return err
		}
		if !slices.ContainsFunc(current.Properties, func(property string) bool {
			return strings.Contains(property, "decoded")
		}) {
			return fmt.Errorf("%s fuzz properties contain no decoded-structure claim", name)
		}
		if err := requireProperty("independent-property-checked"); err != nil {
			return err
		}
	case LayerDeterminism:
		if current.Boundary != BoundaryDeterminismAnalyzer {
			return fmt.Errorf("%s determinism evidence uses boundary %q", name, current.Boundary)
		}
		if err := requireProduction(); err != nil {
			return err
		}
		if err := requireFeatureDimension(); err != nil {
			return err
		}
		if err := requireProperty("equivalent-schedules-compared"); err != nil {
			return err
		}
		if err := requireProperty("production-analyzer-executed"); err != nil {
			return err
		}
	case LayerRuleContract,
		LayerOwnerRoute,
		LayerMustReport,
		LayerMustNotReport,
		LayerMustBeInconclusive,
		LayerArtifactParity,
		LayerProduction,
		LayerMutation:
	}
	return nil
}

func cloneSemanticCases(cases []SemanticCase) []SemanticCase {
	cloned := make([]SemanticCase, len(cases))
	for index, current := range cases {
		cloned[index] = current
		cloned[index].ExecutedBoundaries = slices.Clone(current.ExecutedBoundaries)
		cloned[index].Stages = slices.Clone(current.Stages)
		cloned[index].Properties = slices.Clone(current.Properties)
		cloned[index].Dimensions = slices.Clone(current.Dimensions)
	}
	return cloned
}

func canonicalStageSet(set map[ExecutionStage]bool) []ExecutionStage {
	order := []ExecutionStage{
		StageSourceExtraction,
		StageIdentity,
		StageGraphConstruction,
		StagePropagation,
		StageRefinement,
		StageAggregation,
		StageReporting,
	}
	result := make([]ExecutionStage, 0, len(set))
	for _, stage := range order {
		if set[stage] {
			result = append(result, stage)
		}
	}
	return result
}

func validateBoundaries(name string, boundaries []Boundary) error {
	if len(boundaries) == 0 {
		return fmt.Errorf("%s is empty", name)
	}
	previous := Boundary("")
	for _, boundary := range boundaries {
		if !validBoundary(boundary) {
			return fmt.Errorf("%s contains unsupported boundary %q", name, boundary)
		}
		if previous != "" && boundary <= previous {
			return fmt.Errorf("%s must be unique and use canonical lexical order", name)
		}
		previous = boundary
	}
	return nil
}

func canonicalBoundarySet(set map[Boundary]bool) []Boundary {
	result := make([]Boundary, 0, len(set))
	for boundary := range set {
		result = append(result, boundary)
	}
	slices.Sort(result)
	return result
}

// CanonicalBoundaries returns a sorted, duplicate-free boundary set suitable
// for SemanticCase.ExecutedBoundaries.
func CanonicalBoundaries(boundaries ...Boundary) []Boundary {
	set := make(map[Boundary]bool, len(boundaries))
	for _, boundary := range boundaries {
		set[boundary] = true
	}
	return canonicalBoundarySet(set)
}

func canonicalStringSet(set map[string]bool) []string {
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	slices.Sort(result)
	return result
}

func validLayer(layer Layer) bool {
	switch layer {
	case LayerRuleContract,
		LayerOwnerRoute,
		LayerMustReport,
		LayerMustNotReport,
		LayerMustBeInconclusive,
		LayerArtifactParity,
		LayerProduction,
		LayerGenerated,
		LayerMetamorphic,
		LayerFuzz,
		LayerMutation,
		LayerDeterminism:
		return true
	default:
		return false
	}
}

func validBoundary(boundary Boundary) bool {
	switch boundary {
	case BoundaryCatalogValidation,
		BoundaryProductionAnalyzer,
		BoundaryIndependentModel,
		BoundaryIndependentOracle,
		BoundaryMetamorphicAnalyzer,
		BoundaryFuzzDecoder,
		BoundaryMutationRunner,
		BoundaryDeterminismAnalyzer,
		BoundaryAggregateAdversarial:
		return true
	default:
		return false
	}
}

func validOutcome(outcome Outcome) bool {
	switch outcome {
	case OutcomeMustReport,
		OutcomeMustNotReport,
		OutcomeMustBeInconclusive,
		OutcomeModelAgrees,
		OutcomeRelationPreserved,
		OutcomePropertyDetected,
		OutcomeMutantKilled,
		OutcomeDeterministic,
		OutcomeRouteExecuted,
		OutcomeGateRejected:
		return true
	default:
		return false
	}
}

func validateStages(name string, stages []ExecutionStage) error {
	seen := make(map[ExecutionStage]bool, len(stages))
	for index, stage := range stages {
		switch stage {
		case StageSourceExtraction,
			StageIdentity,
			StageGraphConstruction,
			StagePropagation,
			StageRefinement,
			StageAggregation,
			StageReporting:
		default:
			return fmt.Errorf("%s[%d] %q is unsupported", name, index, stage)
		}
		if seen[stage] {
			return fmt.Errorf("%s contains duplicate %q", name, stage)
		}
		seen[stage] = true
	}
	return nil
}

func validateCanonicalStrings(name string, values []string) error {
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateIdentifier(fmt.Sprintf("%s[%d]", name, index), value); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", name, value)
		}
		seen[value] = true
	}
	if !slices.IsSorted(values) {
		return fmt.Errorf("%s must use canonical lexical order", name)
	}
	return nil
}

func validateIdentifier(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s %q has surrounding whitespace", name, value)
	}
	return nil
}
