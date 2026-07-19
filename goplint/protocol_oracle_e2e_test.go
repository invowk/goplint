// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"go/types"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/invowk/invowk/tools/goplint/internal/protocoloracle"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

type generatedAnalyzerResult struct {
	Outcome         protocoloracle.Outcome
	Reason          string
	DiagnosticCount int
	Trace           generatedExecutionTrace
	Pipeline        generatedAnalyzerPipelineTrace
}

type generatedAnalyzerPipelineTrace map[string]bool

const (
	generatedPipelineParsing     = "parsing"
	generatedPipelineTyping      = "typing"
	generatedPipelineSSA         = "ssa"
	generatedPipelineGraph       = "graph-construction"
	generatedPipelinePropagation = "propagation"
	generatedPipelineAggregation = "aggregation"
	generatedPipelineReporting   = "reporting"
)

func requiredGeneratedAnalyzerPipeline() []string {
	return []string{
		generatedPipelineParsing,
		generatedPipelineTyping,
		generatedPipelineSSA,
		generatedPipelineGraph,
		generatedPipelinePropagation,
		generatedPipelineAggregation,
		generatedPipelineReporting,
	}
}

func (trace generatedAnalyzerPipelineTrace) add(stages ...string) {
	for _, stage := range stages {
		trace[stage] = true
	}
}

func (trace generatedAnalyzerPipelineTrace) merge(other generatedAnalyzerPipelineTrace) {
	for stage := range other {
		trace[stage] = true
	}
}

type generatedComparisonCounts struct {
	Identities              int
	ViolationCases          int
	ViolationDiagnostics    int
	InconclusiveCases       int
	InconclusiveDiagnostics int
	Trace                   generatedExecutionTrace
	Cases                   []generatedIdentityComparison
}

type generatedIdentityComparison struct {
	CaseID          string
	Identity        protocoloracle.Identity
	Outcome         protocoloracle.Outcome
	DiagnosticCount int
	Trace           generatedExecutionTrace
}

type generatedExecutionTrace struct {
	stages     map[soundnessevidence.ExecutionStage]bool
	dimensions map[string]bool
	properties map[string]bool
}

func TestGeneratedAnalyzerPipelineTraceCoversBenchmarkStages(t *testing.T) {
	t.Parallel()

	const source = `package generated
type Name string
func (Name) Validate() error { return nil }
func consume(Name) {}
func Entry(raw string) {
	name := Name(raw)
	consume(name)
}`
	result, err := runGeneratedGoAnalyzer(t, source, defaultCFGMaxStates)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiagnosticCount == 0 {
		t.Fatal("pipeline trace fixture emitted no production diagnostic")
	}
	for _, stage := range requiredGeneratedAnalyzerPipeline() {
		if !result.Pipeline[stage] {
			t.Errorf("production analyzer pipeline omitted stage %q", stage)
		}
	}
}

func TestGeneratedAnalyzerReasonMatchesProductionPriority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		uncertainty []string
		want        string
	}{
		{name: "state budget", uncertainty: []string{"unresolved", "state-budget"}, want: "state-budget"},
		{name: "conditional result", uncertainty: []string{"concurrent-mutation", "conditional-result"}, want: "feasibility-unknown"},
		{name: "concurrent mutation", uncertainty: []string{"unresolved", "concurrent-mutation"}, want: "concurrent-mutation"},
		{name: "escaped heap", uncertainty: []string{"unresolved", "escaped-heap-mutation"}, want: "escaped-heap-mutation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := generatedAnalyzerReason(test.uncertainty); got != test.want {
				t.Fatalf("generatedAnalyzerReason(%v) = %q, want %q", test.uncertainty, got, test.want)
			}
		})
	}
}

func TestGeneratedAnalyzerDiagnosticAggregationClearsDominatedReason(t *testing.T) {
	t.Parallel()

	inconclusive := analysis.Diagnostic{
		Category: CategoryUnvalidatedCastInconclusive,
		URL: DiagnosticURLForFindingWithMeta("test", map[string]string{
			"cfg_inconclusive_reason": string(pathOutcomeReasonConcurrentMutation),
		}),
	}
	violation := analysis.Diagnostic{Category: CategoryUnvalidatedCast}
	orders := [][]analysis.Diagnostic{
		{inconclusive, violation},
		{violation, inconclusive},
	}
	for _, diagnostics := range orders {
		result := generatedAnalyzerResult{Outcome: protocoloracle.OutcomeNone}
		for _, diagnostic := range diagnostics {
			aggregateGeneratedAnalyzerDiagnostic(&result, diagnostic)
		}
		if result.Outcome != protocoloracle.OutcomeViolation || result.Reason != "" || result.DiagnosticCount != 2 {
			t.Errorf("aggregate diagnostics = %+v, want violation with no reason and two diagnostics", result)
		}
	}
}

func (trace *generatedExecutionTrace) addStage(stage soundnessevidence.ExecutionStage) {
	if trace.stages == nil {
		trace.stages = make(map[soundnessevidence.ExecutionStage]bool)
	}
	trace.stages[stage] = true
}

func (trace *generatedExecutionTrace) addDimensions(dimensions ...string) {
	if trace.dimensions == nil {
		trace.dimensions = make(map[string]bool)
	}
	for _, dimension := range dimensions {
		if dimension != "" {
			trace.dimensions[dimension] = true
		}
	}
}

func (trace *generatedExecutionTrace) addProperties(properties ...string) {
	if trace.properties == nil {
		trace.properties = make(map[string]bool)
	}
	for _, property := range properties {
		if property != "" {
			trace.properties[property] = true
		}
	}
}

func (trace *generatedExecutionTrace) merge(other generatedExecutionTrace) {
	for stage := range other.stages {
		trace.addStage(stage)
	}
	for dimension := range other.dimensions {
		trace.addDimensions(dimension)
	}
	for property := range other.properties {
		trace.addProperties(property)
	}
}

func (trace generatedExecutionTrace) orderedStages() []soundnessevidence.ExecutionStage {
	order := []soundnessevidence.ExecutionStage{
		soundnessevidence.StageSourceExtraction,
		soundnessevidence.StageIdentity,
		soundnessevidence.StageGraphConstruction,
		soundnessevidence.StagePropagation,
		soundnessevidence.StageRefinement,
		soundnessevidence.StageAggregation,
		soundnessevidence.StageReporting,
	}
	stages := make([]soundnessevidence.ExecutionStage, 0, len(trace.stages))
	for _, stage := range order {
		if trace.stages[stage] {
			stages = append(stages, stage)
		}
	}
	return stages
}

func (trace generatedExecutionTrace) sortedDimensions() []string {
	return sortedTraceKeys(trace.dimensions)
}

func (trace generatedExecutionTrace) sortedProperties() []string {
	return sortedTraceKeys(trace.properties)
}

func sortedTraceKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func requireGeneratedTrace(
	t testing.TB,
	trace generatedExecutionTrace,
	wantStages []soundnessevidence.ExecutionStage,
	wantProperties []string,
	wantDimensions []string,
) {
	t.Helper()

	if got := trace.orderedStages(); !slices.Equal(got, wantStages) {
		t.Fatalf("executed stages = %v, want %v from actual analyzer/projection trace", got, wantStages)
	}
	if got := trace.sortedProperties(); !slices.Equal(got, wantProperties) {
		t.Fatalf("checked properties = %v, want %v", got, wantProperties)
	}
	if got := trace.sortedDimensions(); !slices.Equal(got, wantDimensions) {
		t.Fatalf("projected dimensions = %v, want %v", got, wantDimensions)
	}
}

type generatedAnalyzerPerturbation string

const (
	perturbExtraction    generatedAnalyzerPerturbation = "extraction"
	perturbConditioning  generatedAnalyzerPerturbation = "edge-conditioning"
	perturbIdentity      generatedAnalyzerPerturbation = "identity"
	perturbMatchedReturn generatedAnalyzerPerturbation = "matched-return"
	perturbJoin          generatedAnalyzerPerturbation = "join"
	perturbUncertainty   generatedAnalyzerPerturbation = "uncertainty"
	perturbWitness       generatedAnalyzerPerturbation = "evidence-witness"
	perturbRefinement    generatedAnalyzerPerturbation = "evidence-refinement"
	perturbSummary       generatedAnalyzerPerturbation = "evidence-summary"
	perturbReason        generatedAnalyzerPerturbation = "evidence-reason"
)

type generatedComparisonMismatch struct {
	caseID       string
	identity     protocoloracle.Identity
	perturbation generatedAnalyzerPerturbation
	field        string
	analyzer     string
	reference    string
}

func (m *generatedComparisonMismatch) Error() string {
	return fmt.Sprintf(
		"program %s identity %d under %s perturbation: analyzer %s=%q reference %s=%q",
		m.caseID,
		m.identity,
		m.perturbation,
		m.field,
		m.analyzer,
		m.field,
		m.reference,
	)
}

// TestProtocolOracleGeneratedGoEndToEnd is the blocking production-boundary
// comparison. Unlike the solver-core component test, it runs parsing, type
// checking, SSA extraction, graph construction, propagation, refinement,
// aggregation, and diagnostic reporting through the registered analyzer.
func TestProtocolOracleGeneratedGoEndToEnd(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		requireMutationGuardObservation(
			t,
			"oracle-bounds/reviewed-cardinality",
			mutationGuardState("blocking-oracle-cardinality", "reviewed"),
			mutationGuardState("blocking-oracle-cardinality", "drifted"),
		)
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	programObservations := make([]soundnessgate.ObservedMember, 0, manifest.ExpectedProgramCount)
	comparisonCounts := generatedComparisonCounts{}
	profile := manifest.Blocking
	for shard := range profile.Shards {
		err = protocoloracle.EnumerateShard(manifest, "blocking", shard, func(program protocoloracle.Program) error {
			counts, compareErr := compareGeneratedGoProgram(t, program, manifest.Blocking.MaxStates, "")
			comparisonCounts.add(counts)
			if compareErr == nil {
				programObservations = append(programObservations, soundnessgate.ObservedMember{
					PopulationID: "generated-programs",
					MemberID:     program.CaseID,
				})
			}
			return compareErr
		})
		if err != nil {
			t.Fatalf("blocking shard %d: %v", shard, err)
		}
	}
	requireMutationGuardObservation(
		t,
		"oracle-bounds/blocking-program-count",
		mutationGuardState("blocking-generated-program-count", strconv.Itoa(manifest.ExpectedProgramCount)),
		mutationGuardState("blocking-generated-program-count", strconv.Itoa(len(programObservations))),
	)
	if len(programObservations) != manifest.ExpectedProgramCount || len(programObservations) < profile.MinimumPrograms {
		t.Fatalf("end-to-end generated corpus count = %d, want %d and at least %d", len(programObservations), manifest.ExpectedProgramCount, profile.MinimumPrograms)
	}
	perturbations := detectGeneratedGoPerturbations(t, manifest)
	if comparisonCounts.ViolationCases == 0 || comparisonCounts.InconclusiveCases == 0 {
		t.Fatalf("generated analyzer outcomes are vacuous: %+v", comparisonCounts)
	}
	observations := slices.Concat(programObservations, perturbations)
	emitSoundnessSubgateReport(t, observedPopulations(t, observations))
}

func TestProtocolOracleGeneratedGoPerturbationsAreDetected(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	_ = detectGeneratedGoPerturbations(t, manifest)
}

func TestGeneratedAnalyzerEvidenceCorruptionEntersBeforeCheckingAndAggregation(t *testing.T) {
	t.Parallel()

	manifest, err := protocoloracle.LoadBoundsManifest(filepath.Join("..", "spec", "protocol-oracle-bounds.v1.json"))
	if err != nil {
		t.Fatalf("LoadBoundsManifest() error: %v", err)
	}
	programs := make(map[string]protocoloracle.Program)
	if err := protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		caseID       string
		perturbation generatedAnalyzerPerturbation
		wantField    string
	}{
		{name: "witness", caseID: "scenario/refinement-unsat", perturbation: perturbWitness, wantField: "outcome"},
		{name: "refinement", caseID: "scenario/refinement-unsat", perturbation: perturbRefinement, wantField: "outcome"},
		{name: "summary", caseID: "scenario/matched-return", perturbation: perturbSummary, wantField: "outcome"},
		{name: "reason", caseID: "scenario/post-validation-unknown", perturbation: perturbReason, wantField: "reason"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			program, ok := programs[test.caseID]
			if !ok {
				t.Fatalf("generated case %q is missing", test.caseID)
			}
			if _, cleanErr := compareGeneratedGoProgram(t, program, manifest.Blocking.MaxStates, ""); cleanErr != nil {
				t.Fatalf("clean control failed: %v", cleanErr)
			}
			_, compareErr := compareGeneratedGoProgram(t, program, manifest.Blocking.MaxStates, test.perturbation)
			var mismatch *generatedComparisonMismatch
			if !errors.As(compareErr, &mismatch) {
				t.Fatalf("corruption produced non-differential failure: %v", compareErr)
			}
			if mismatch.perturbation != test.perturbation || mismatch.field != test.wantField {
				t.Fatalf("corruption mismatch = %+v, want perturbation=%q field=%q", mismatch, test.perturbation, test.wantField)
			}
		})
	}
}

func detectGeneratedGoPerturbations(
	t *testing.T,
	manifest protocoloracle.BoundsManifest,
) []soundnessgate.ObservedMember {
	t.Helper()

	programs := make(map[string]protocoloracle.Program)
	if err := protocoloracle.Enumerate(manifest, func(program protocoloracle.Program) error {
		programs[program.CaseID] = program
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name         string
		caseID       string
		perturbation generatedAnalyzerPerturbation
	}{
		{name: "extraction", caseID: "scenario/validated-nil", perturbation: perturbExtraction},
		{name: "edge conditioning", caseID: "scenario/validated-nil", perturbation: perturbConditioning},
		{name: "identity", caseID: "scenario/alias-copy", perturbation: perturbIdentity},
		{name: "matching returns", caseID: "scenario/matched-return", perturbation: perturbMatchedReturn},
		{name: "joins", caseID: "scenario/validated-nil", perturbation: perturbJoin},
		{name: "uncertainty", caseID: "scenario/post-validation-unknown", perturbation: perturbUncertainty},
		{name: "witness evidence", caseID: "scenario/refinement-unsat", perturbation: perturbWitness},
		{name: "refinement evidence", caseID: "scenario/refinement-unsat", perturbation: perturbRefinement},
		{name: "summary evidence", caseID: "scenario/matched-return", perturbation: perturbSummary},
		{name: "reason evidence", caseID: "scenario/post-validation-unknown", perturbation: perturbReason},
	}
	observations := make([]soundnessgate.ObservedMember, 0, len(tests))
	for _, test := range tests {
		program, ok := programs[test.caseID]
		if !ok {
			t.Fatalf("generated case %q is missing", test.caseID)
		}
		_, compareErr := compareGeneratedGoProgram(t, program, manifest.Blocking.MaxStates, test.perturbation)
		if compareErr == nil {
			t.Errorf("%s perturbation was not detected", test.perturbation)
			continue
		}
		var mismatch *generatedComparisonMismatch
		if !errors.As(compareErr, &mismatch) {
			t.Errorf("%s perturbation produced non-differential failure: %v", test.perturbation, compareErr)
			continue
		}
		if mismatch.perturbation != test.perturbation {
			t.Errorf("%s perturbation mismatch was attributed to %s", test.perturbation, mismatch.perturbation)
			continue
		}
		observations = append(observations, soundnessgate.ObservedMember{
			PopulationID: "perturbations",
			MemberID:     test.name,
		})
	}
	return observations
}

func compareGeneratedGoProgram(
	t *testing.T,
	program protocoloracle.Program,
	maxStates int,
	perturbation generatedAnalyzerPerturbation,
) (generatedComparisonCounts, error) {
	t.Helper()

	reference := protocoloracle.Interpret(program, maxStates)
	counts := generatedComparisonCounts{}
	for _, identity := range program.Identities {
		source, projectionTrace, err := protocoloracle.GoSourceForIdentityWithTrace(program, identity)
		if err != nil {
			return generatedComparisonCounts{}, fmt.Errorf("generate Go for %s identity %d: %w", program.CaseID, identity, err)
		}
		source = perturbGeneratedSource(source, perturbation)
		got, err := runGeneratedGoAnalyzerWithPerturbation(t, source, maxStates, perturbation)
		if err != nil {
			return generatedComparisonCounts{}, fmt.Errorf("analyze %s identity %d: %w", program.CaseID, identity, err)
		}
		want := reference.Evidence[identity]
		if got.Outcome != want.Outcome {
			return generatedComparisonCounts{}, &generatedComparisonMismatch{
				caseID:       program.CaseID,
				identity:     identity,
				perturbation: perturbation,
				field:        "outcome",
				analyzer:     string(got.Outcome),
				reference:    string(want.Outcome),
			}
		}
		wantReason := ""
		if want.Outcome == protocoloracle.OutcomeInconclusive {
			wantReason = generatedAnalyzerReason(want.Uncertainty)
		}
		if got.Reason != wantReason {
			return generatedComparisonCounts{}, &generatedComparisonMismatch{
				caseID:       program.CaseID,
				identity:     identity,
				perturbation: perturbation,
				field:        "reason",
				analyzer:     got.Reason,
				reference:    wantReason,
			}
		}
		identityTrace := got.Trace
		identityTrace.addProperties("independent-model-compared")
		identityTrace.addDimensions(projectionTrace.Dimensions()...)
		counts.Trace.merge(identityTrace)
		counts.Cases = append(counts.Cases, generatedIdentityComparison{
			CaseID:          program.CaseID,
			Identity:        identity,
			Outcome:         got.Outcome,
			DiagnosticCount: got.DiagnosticCount,
			Trace:           identityTrace,
		})
		counts.Identities++
		switch got.Outcome {
		case protocoloracle.OutcomeNone:
			// A no-diagnostic outcome contributes no violation or inconclusive counts.
		case protocoloracle.OutcomeViolation:
			counts.ViolationCases++
			counts.ViolationDiagnostics += got.DiagnosticCount
		case protocoloracle.OutcomeInconclusive:
			counts.InconclusiveCases++
			counts.InconclusiveDiagnostics += got.DiagnosticCount
		}
	}
	return counts, nil
}

func (counts *generatedComparisonCounts) add(other generatedComparisonCounts) {
	counts.Identities += other.Identities
	counts.ViolationCases += other.ViolationCases
	counts.ViolationDiagnostics += other.ViolationDiagnostics
	counts.InconclusiveCases += other.InconclusiveCases
	counts.InconclusiveDiagnostics += other.InconclusiveDiagnostics
	counts.Trace.merge(other.Trace)
	counts.Cases = append(counts.Cases, other.Cases...)
}

func generatedAnalyzerReason(uncertainty []string) string {
	reasons := make(map[string]bool, len(uncertainty))
	for _, reason := range uncertainty {
		reasons[reason] = true
	}
	for _, priority := range []struct {
		model    string
		analyzer string
	}{
		{model: "state-budget", analyzer: "state-budget"},
		{model: "conditional-result", analyzer: "feasibility-unknown"},
		{model: "concurrent-mutation", analyzer: "concurrent-mutation"},
		{model: "escaped-heap-mutation", analyzer: "escaped-heap-mutation"},
		{model: "unresolved", analyzer: "unresolved-target"},
	} {
		if reasons[priority.model] {
			return priority.analyzer
		}
	}
	return ""
}

func runGeneratedGoAnalyzer(t testing.TB, source string, maxStates int) (generatedAnalyzerResult, error) {
	return runGeneratedGoAnalyzerWithPerturbation(t, source, maxStates, "")
}

type generatedEvidenceInjectionControl struct {
	perturbation generatedAnalyzerPerturbation
	applied      bool
}

func (control *generatedEvidenceInjectionControl) Expired() bool {
	return false
}

func (control *generatedEvidenceInjectionControl) injectProtocolWitnessEvidence(
	evidence cfgSSAConstraintEvidence,
) cfgSSAConstraintEvidence {
	if control.perturbation != perturbWitness || control.applied || evidence.FormatVersion == 0 {
		return evidence
	}
	evidence.WitnessPath = append(cloneCFGPath(evidence.WitnessPath), -1)
	control.applied = true
	return evidence
}

func (control *generatedEvidenceInjectionControl) injectProtocolRefinementEvidence(
	decision cfgFeasibilityDecision,
) cfgFeasibilityDecision {
	if control.perturbation != perturbRefinement || control.applied ||
		decision.Result != cfgFeasibilityResultUNSAT {
		return decision
	}
	decision.Evidence.FormatVersion++
	control.applied = true
	return decision
}

func (control *generatedEvidenceInjectionControl) injectProtocolSummaryEvidence(
	summary interprocProcedureSummary,
) interprocProcedureSummary {
	if control.perturbation != perturbSummary {
		return summary
	}
	summary.state = newProtocolRequiredState()
	summary.edgeFunction = identityInterprocConditionalEdgeFunc()
	summary.exitStateBefore = newProtocolRequiredState()
	summary.exitTransferTag = ideEdgeFuncIdentity
	control.applied = true
	return summary
}

func (control *generatedEvidenceInjectionControl) injectProtocolReasonEvidence(
	reason pathOutcomeReason,
) pathOutcomeReason {
	if control.perturbation != perturbReason || control.applied ||
		reason != pathOutcomeReasonUnresolvedTarget {
		return reason
	}
	control.applied = true
	return pathOutcomeReasonEscapedHeapMutation
}

func isGeneratedEvidencePerturbation(perturbation generatedAnalyzerPerturbation) bool {
	switch perturbation {
	case perturbWitness, perturbRefinement, perturbSummary, perturbReason:
		return true
	default:
		return false
	}
}

func runGeneratedGoAnalyzerWithPerturbation(
	t testing.TB,
	source string,
	maxStates int,
	perturbation generatedAnalyzerPerturbation,
) (generatedAnalyzerResult, error) {
	t.Helper()

	pass, _ := buildTypedPassFromSource(t, source)
	trace := generatedExecutionTrace{}
	pipeline := generatedAnalyzerPipelineTrace{}
	pipeline.add(generatedPipelineParsing, generatedPipelineTyping)
	trace.addStage(soundnessevidence.StageSourceExtraction)
	harness := newAnalyzerHarness()
	var evidenceInjection *generatedEvidenceInjectionControl
	if isGeneratedEvidencePerturbation(perturbation) {
		evidenceInjection = &generatedEvidenceInjectionControl{perturbation: perturbation}
		harness.state.protocolControlFactory = func(time.Duration) protocolAnalysisControl {
			return evidenceInjection
		}
	}
	ssaBuilder := harness.state.ssaBuilder
	harness.state.ssaBuilder = func(pass *analysis.Pass) *ssaResult {
		result := ssaBuilder(pass)
		if result != nil && result.Availability.ready() {
			pipeline.add(generatedPipelineSSA)
		}
		return result
	}
	setFlag(t, harness.Analyzer, "check-cast-validation", "true")
	setFlag(t, harness.Analyzer, "cfg-max-states", strconv.Itoa(maxStates))
	diagnostics := make([]analysis.Diagnostic, 0)
	pass.Analyzer = harness.Analyzer
	pass.ResultOf = map[*analysis.Analyzer]any{inspect.Analyzer: inspector.New(pass.Files)}
	pass.Report = func(diagnostic analysis.Diagnostic) {
		diagnostics = append(diagnostics, diagnostic)
		trace.addStage(soundnessevidence.StageReporting)
		pipeline.add(generatedPipelineReporting)
	}
	pass.ImportObjectFact = func(types.Object, analysis.Fact) bool { return false }
	pass.ImportPackageFact = func(*types.Package, analysis.Fact) bool { return false }
	pass.ExportObjectFact = func(types.Object, analysis.Fact) {}
	pass.ExportPackageFact = func(analysis.Fact) {}
	pass.AllPackageFacts = func() []analysis.PackageFact { return nil }
	pass.AllObjectFacts = func() []analysis.ObjectFact { return nil }
	if _, err := harness.Analyzer.Run(pass); err != nil {
		return generatedAnalyzerResult{}, err
	}
	trace.addProperties("production-analyzer-executed")
	result := generatedAnalyzerResult{
		Outcome:  protocoloracle.OutcomeNone,
		Trace:    trace,
		Pipeline: pipeline,
	}
	for _, diagnostic := range diagnostics {
		aggregateGeneratedAnalyzerDiagnostic(&result, diagnostic)
	}
	if result.Trace.stages[soundnessevidence.StageGraphConstruction] {
		result.Pipeline.add(generatedPipelineGraph)
	}
	if result.Trace.stages[soundnessevidence.StagePropagation] {
		result.Pipeline.add(generatedPipelinePropagation)
	}
	if result.Trace.stages[soundnessevidence.StageAggregation] {
		result.Pipeline.add(generatedPipelineAggregation)
	}
	if evidenceInjection != nil && !evidenceInjection.applied {
		return generatedAnalyzerResult{}, fmt.Errorf(
			"%s perturbation did not reach its pre-validation or pre-aggregation injection point",
			perturbation,
		)
	}
	return result, nil
}

func aggregateGeneratedAnalyzerDiagnostic(result *generatedAnalyzerResult, diagnostic analysis.Diagnostic) {
	switch diagnostic.Category {
	case CategoryUnvalidatedCast:
		addGeneratedDiagnosticStages(&result.Trace, diagnostic)
		result.Outcome = protocoloracle.OutcomeViolation
		result.Reason = ""
		result.DiagnosticCount++
	case CategoryUnvalidatedCastInconclusive:
		addGeneratedDiagnosticStages(&result.Trace, diagnostic)
		result.DiagnosticCount++
		if result.Outcome != protocoloracle.OutcomeViolation {
			result.Outcome = protocoloracle.OutcomeInconclusive
			result.Reason = FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_inconclusive_reason")
		}
	}
}

func addGeneratedDiagnosticStages(trace *generatedExecutionTrace, diagnostic analysis.Diagnostic) {
	// A category-specific cast diagnostic can only be emitted after production
	// resolves the conversion and its obligation to one tracked identity.
	trace.addStage(soundnessevidence.StageIdentity)
	trace.addStage(soundnessevidence.StagePropagation)
	trace.addStage(soundnessevidence.StageAggregation)
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_kind") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_witness_call_chain") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_tabulation_path_edges") != "" {
		trace.addStage(soundnessevidence.StageGraphConstruction)
	}
	if FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_refinement_iterations") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_refinement_status") != "" ||
		FindingMetaFromDiagnosticURL(diagnostic.URL, "cfg_feasibility_engine") != "" {
		trace.addStage(soundnessevidence.StageRefinement)
	}
}

func perturbGeneratedSource(source string, perturbation generatedAnalyzerPerturbation) string {
	switch perturbation {
	case perturbExtraction:
		return strings.Replace(source, "\tif err := n0.Validate(); err != nil { return err }\n", "", 1)
	case perturbConditioning:
		return strings.Replace(source, "\tif err := n0.Validate(); err != nil { return err }\n", "\terr = n0.Validate()\n\t_ = err\n", 1)
	case perturbIdentity:
		source = strings.Replace(source, "\tn0 := Name(raw)\n", "\tn0 := Name(raw)\n\tdecoy := Name(\"decoy\")\n", 1)
		return strings.Replace(source, "n0.Validate()", "decoy.Validate()", 1)
	case perturbMatchedReturn:
		return strings.Replace(
			source,
			"\terr = procedure1(n0)\n\tif err != nil { return err }\n",
			"\terr = procedure1(n0)\n\tif err != nil { return err }\n\tn0 = Name(raw)\n",
			1,
		)
	case perturbJoin:
		return strings.Replace(
			source,
			"\tif err := n0.Validate(); err != nil { return err }\n",
			"\tif err := n0.Validate(); err != nil { return err }\n\tif choose { n0 = Name(raw) }\n",
			1,
		)
	case perturbUncertainty:
		return strings.ReplaceAll(source, "\teffect.Mutate(&n0)\n", "")
	default:
		return source
	}
}
