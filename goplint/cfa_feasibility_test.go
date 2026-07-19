// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

func TestCFGSSAConstraintsFeasibilityBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		src        string
		choices    []bool
		timeout    time.Duration
		wantResult string
		wantReason string
	}{
		{
			name: "feasible witness is sat",
			src: `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultSAT,
		},
		{
			name: "contradictory guards are unsat",
			src: `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`,
			choices:    []bool{true, true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUNSAT,
		},
		{
			name: "branch inversion remains exact",
			src: `package p
func sample(raw string) {
	if raw == "" {
		if !(raw != "") {
			return
		}
	}
}
`,
			choices:    []bool{true, false},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUNSAT,
		},
		{
			name: "reassignment keeps distinct SSA subjects",
			src: `package p
func sample(raw string) {
	if raw == "" {
		raw = "ready"
		if raw != "" {
			return
		}
	}
}
`,
			choices:    []bool{true, true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultSAT,
		},
		{
			name: "contradictory ordered integer guards are unsat",
			src: `package p
func sample(depth int) {
	if depth > 10 {
		if depth <= 10 {
			return
		}
	}
}
`,
			choices:    []bool{true, true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUNSAT,
		},
		{
			name: "unsupported predicate degrades to unknown",
			src: `package p
import "strings"
func sample(raw string) {
	if strings.HasPrefix(raw, "prod") {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUnknown,
			wantReason: cfgFeasibilityReasonUnsupportedPredicate,
		},
		{
			name: "unsupported prefix preserves supported contradiction",
			src: `package p
import "strings"
func sample(raw string) {
	if strings.HasPrefix(raw, "prod") {
		if raw == "" {
			if raw != "" {
				return
			}
		}
	}
}
`,
			choices:    []bool{true, true, true},
			timeout:    50 * time.Millisecond,
			wantResult: cfgFeasibilityResultUNSAT,
		},
		{
			name: "timeout degrades to unknown",
			src: `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`,
			choices:    []bool{true},
			timeout:    0,
			wantResult: cfgFeasibilityResultUnknown,
			wantReason: cfgFeasibilityReasonTimeout,
		},
	}

	backend := cfgSSAConstraintsFeasibilityBackend{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pass, cfg := mustBuildTypedTestCFG(t, tt.src)
			path := witnessPathFromChoices(t, cfg, tt.choices...)
			decision := backend.Check(cfgFeasibilityQuery{
				Pass:    pass,
				CFG:     cfg,
				Witness: cfgWitnessRecord{CFGPath: path},
				Timeout: tt.timeout,
			})
			if decision.Result != tt.wantResult {
				t.Fatalf("Check() result = %q, want %q", decision.Result, tt.wantResult)
			}
			if decision.Reason != tt.wantReason {
				t.Fatalf("Check() reason = %q, want %q", decision.Reason, tt.wantReason)
			}
		})
	}
}

func TestCFGSSAUnsupportedFormulaOverApproximation(t *testing.T) {
	t.Parallel()

	contradiction := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{
		{subject: "raw", op: "eq", value: `"ready"`},
		{subject: "raw", op: "neq", value: `"ready"`},
	}}}
	unknown := cfgSSAUnsupportedFormula()

	conjunction, expired := cfgSSAFormulaAndWithControl(unknown, contradiction, nil)
	if expired || !conjunction.unsupported {
		t.Fatalf("unknown conjunction = %+v, expired=%t, want incomplete over-approximation", conjunction, expired)
	}
	if _, unsat := buildSSAConstraintEvidence(conjunction); !unsat {
		t.Fatal("unknown AND contradiction must retain the supported UNSAT proof")
	}

	disjunction, expired := cfgSSAFormulaOrWithControl(unknown, contradiction, nil)
	if expired || !disjunction.unsupported {
		t.Fatalf("unknown disjunction = %+v, expired=%t, want incomplete over-approximation", disjunction, expired)
	}
	if _, unsat := buildSSAConstraintEvidence(disjunction); unsat {
		t.Fatal("unknown OR contradiction must remain satisfiable in the over-approximation")
	}
}

func TestCFGSSAConstraintEvidenceChecker(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`)
	path := witnessPathFromChoices(t, cfg, true, true)
	query := cfgFeasibilityQuery{
		Pass:    pass,
		CFG:     cfg,
		Witness: cfgWitnessRecord{CFGPath: path},
		Timeout: 50 * time.Millisecond,
	}
	backend := cfgSSAConstraintsFeasibilityBackend{}
	decision := backend.Check(query)
	if decision.Result != cfgFeasibilityResultUNSAT {
		t.Fatalf("Check() result = %q, want %q", decision.Result, cfgFeasibilityResultUNSAT)
	}
	if !checkSSAConstraintEvidence(query, decision.Evidence) {
		t.Fatal("expected independently checked evidence to be accepted")
	}

	controller := newCFGRefinementController(cfgProtocolRefinementOptions{FeasibilityTimeout: 50 * time.Millisecond})
	tests := []struct {
		name     string
		evidence cfgSSAConstraintEvidence
	}{
		{name: "missing evidence"},
		{
			name: "wrong evidence version",
			evidence: cfgSSAConstraintEvidence{
				FormatVersion: cfgSSAConstraintEvidenceVersion + 1,
			},
		},
		{
			name:     "corrupt contradiction",
			evidence: corruptSSAConstraintEvidence(decision.Evidence),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := controller.checkFeasibility(
				fixedCFGFeasibilityBackend{decision: cfgFeasibilityDecision{
					Result:   cfgFeasibilityResultUNSAT,
					Evidence: tt.evidence,
				}},
				pass,
				0,
				cfg,
				cfgWitnessRecord{CFGPath: path},
			)
			if got.Result != cfgFeasibilityResultUnknown || got.Reason != cfgFeasibilityReasonEvidenceRejected {
				t.Fatalf("checkFeasibility() = (%q, %q), want (%q, %q)",
					got.Result,
					got.Reason,
					cfgFeasibilityResultUnknown,
					cfgFeasibilityReasonEvidenceRejected,
				)
			}
		})
	}
}

func TestCFGRefinementDischargesMandatoryContradictoryPrefix(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func consume(string) {}
func sample(raw string) {
	if raw == "left" {
		if raw != "left" {
			consume(raw)
		}
	}
}
`)
	origin := cfgBlockContainingCall(t, cfg, "consume")
	witness := []int32{origin.Index}
	controller := newCFGRefinementController(cfgProtocolRefinementOptions{
		RefinementMaxIterations: 2,
		FeasibilityMaxQueries:   2,
		FeasibilityTimeout:      50 * time.Millisecond,
	})
	reruns := 0
	result := controller.Refine(cfgRefinementRequest{
		Pass:      pass,
		CFG:       cfg,
		Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: witness},
		Category:  CategoryUnvalidatedCast,
		FindingID: "mandatory-prefix-unsat",
		CallChain: []string{"p.sample"},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			reruns++
			if len(override.DischargedWitnesses) != 1 {
				t.Fatalf("rerun has %d discharged witnesses, want 1", len(override.DischargedWitnesses))
			}
			return interprocPathResult{Class: interprocOutcomeSafe}
		},
	})
	if reruns != 1 {
		t.Fatalf("reruns = %d, want 1", reruns)
	}
	if result.Class != interprocOutcomeSafe ||
		result.Refinement.FeasibilityResult != cfgFeasibilityResultUNSAT ||
		result.Refinement.RefinementStatus != cfgRefinementStatusDischargedInfeasible {
		t.Fatalf("result = %+v, want checked UNSAT discharge", result)
	}
	if result.Refinement.EvidenceDigest == "" {
		t.Fatal("accepted UNSAT discharge has empty evidence digest")
	}
	if !checkSSAConstraintEvidence(cfgFeasibilityQuery{
		Pass:    pass,
		CFG:     cfg,
		Witness: result.WitnessRecord,
		Timeout: 50 * time.Millisecond,
	}, result.Refinement.Evidence) {
		t.Fatal("mandatory-prefix UNSAT evidence was not independently accepted")
	}
}

func TestCFGFeasibilityDoesNotAssumeBranchPolarityAcrossJoin(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func consume(string) {}
func sample(raw string) {
	if raw == "left" {
		raw = "right"
	}
	consume(raw)
}
`)
	origin := cfgBlockContainingCall(t, cfg, "consume")
	decision := (cfgSSAConstraintsFeasibilityBackend{}).Check(cfgFeasibilityQuery{
		Pass:    pass,
		CFG:     cfg,
		Witness: cfgWitnessRecord{CFGPath: []int32{origin.Index}},
		Timeout: 50 * time.Millisecond,
	})
	if decision.Result != cfgFeasibilityResultSAT {
		t.Fatalf("join witness feasibility = %q (%q), want SAT", decision.Result, decision.Reason)
	}
}

func TestCFGFeasibilityUsesFactOriginForMandatoryPrefix(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func consume(string) {}
func sample(raw string) {
	value := raw
	if raw == "left" {
		consume(value)
	}
}
`)
	entry := cfg.Blocks[0]
	hazard := cfgBlockContainingCall(t, cfg, "consume")
	falsePath := witnessPathFromChoices(t, cfg, false)
	falseFormula, expired := extractSSAWitnessConstraintsWithControl(pass, cfg, falsePath, nil)
	if expired || falseFormula.unsupported {
		t.Fatalf("false-branch formula = %+v, expired=%t", falseFormula, expired)
	}
	provenance := encodeCFGSSAPredicateFormula(falseFormula)
	decision := (cfgSSAConstraintsFeasibilityBackend{}).Check(cfgFeasibilityQuery{
		Pass: pass,
		CFG:  cfg,
		Witness: cfgWitnessRecord{
			FactOriginPath: []int32{entry.Index},
			CFGPath:        []int32{hazard.Index},
			WitnessEdges: []interprocWitnessEdge{{
				PredicateProvenance: []string{provenance},
			}},
		},
		Timeout: 50 * time.Millisecond,
	})
	if decision.Result != cfgFeasibilityResultSAT {
		t.Fatalf("fact-origin witness feasibility = %q (%q), want SAT", decision.Result, decision.Reason)
	}
}

func TestExactWitnessPredicateProvenanceDrivesFeasibility(t *testing.T) {
	t.Parallel()

	const subject = "callee|*ssa.Parameter|value&with||delimiters|17"
	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{
		{subject: subject, op: "eq", value: "ready&waiting||done"},
		{subject: subject, op: "neq", value: "ready&waiting||done"},
	}}}
	provenance := encodeCFGSSAPredicateFormula(formula)
	if provenance == "" {
		t.Fatal("exact SSA formula provenance was not encoded")
	}
	decoded := cfgSSAFormulaFromPredicateProvenance(provenance)
	if decoded.unsupported || cfgSSAConstraintFormulaDigest(decoded) != cfgSSAConstraintFormulaDigest(formula) {
		t.Fatalf("predicate provenance round trip changed formula: %+v", decoded)
	}

	from := interprocNodeID{FuncKey: "callee", BlockIndex: 0, Kind: interprocNodeKindCFG}
	to := interprocNodeID{FuncKey: "callee", BlockIndex: 1, Kind: interprocNodeKindCFG}
	witness := interprocWitnessEdge{
		From: from, To: to, Kind: interprocEdgeIntra,
		PredicateProvenance: []string{provenance},
	}
	decision := (cfgSSAConstraintsFeasibilityBackend{}).Check(cfgFeasibilityQuery{
		Witness: cfgWitnessRecord{
			CFGPath:      []int32{99},
			WitnessEdges: []interprocWitnessEdge{witness},
		},
		Timeout: 50 * time.Millisecond,
	})
	if decision.Result != cfgFeasibilityResultUNSAT {
		t.Fatalf("exact witness feasibility = %q (%q), want UNSAT", decision.Result, decision.Reason)
	}
}

type steppedFeasibilityDeadline struct {
	checks    int
	expiresAt int
}

func (deadline *steppedFeasibilityDeadline) Expired() bool {
	deadline.checks++
	return deadline.checks >= deadline.expiresAt
}

func TestCFGFeasibilityChecksDeadlineInsideEvidenceLoops(t *testing.T) {
	t.Parallel()

	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{
		{subject: "value", op: "eq", value: "one"},
		{subject: "value", op: "eq", value: "two"},
		{subject: "value", op: "eq", value: "three"},
	}}}
	searchDeadline := &steppedFeasibilityDeadline{expiresAt: 4}
	if evidence, unsat, expired := buildSSAConstraintEvidenceWithDeadline(
		formula,
		searchDeadline,
	); !expired || unsat || evidence.FormatVersion != 0 {
		t.Fatalf("mid-search result = (%+v, unsat=%t, expired=%t), want empty expired result", evidence, unsat, expired)
	}

	contradictory := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{
		{subject: "value", op: "eq", value: "one"},
		{subject: "value", op: "neq", value: "one"},
	}}}
	evidence, unsat := buildSSAConstraintEvidence(contradictory)
	if !unsat {
		t.Fatal("contradictory control formula was not UNSAT")
	}
	evidence.WitnessPath = []int32{0, 1}
	evidence.Subjects = ssaConstraintSubjects(contradictory)
	replayDeadline := &steppedFeasibilityDeadline{expiresAt: 8}
	if accepted, expired := checkSSAConstraintFormulaEvidenceWithDeadline(
		contradictory,
		evidence.WitnessPath,
		evidence,
		replayDeadline,
	); !expired || accepted {
		t.Fatalf("mid-replay result = (accepted=%t, expired=%t), want rejected timeout", accepted, expired)
	}
}

func TestCFGRefinementRejectsLateUNSATAndEvidence(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`)
	path := witnessPathFromChoices(t, cfg, true, true)
	query := cfgFeasibilityQuery{
		Pass:    pass,
		CFG:     cfg,
		Witness: cfgWitnessRecord{CFGPath: path},
		Timeout: 50 * time.Millisecond,
	}
	unsat := (cfgSSAConstraintsFeasibilityBackend{}).Check(query)
	if unsat.Result != cfgFeasibilityResultUNSAT {
		t.Fatalf("fixture decision = %q, want UNSAT", unsat.Result)
	}

	tests := []struct {
		name      string
		expiresAt int
	}{
		{name: "UNSAT completed after deadline", expiresAt: 2},
		{name: "evidence completed after deadline", expiresAt: 4},
		{name: "deadline expires before refinement rerun", expiresAt: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deadline := &steppedFeasibilityDeadline{expiresAt: tt.expiresAt}
			controller := newCFGRefinementController(cfgProtocolRefinementOptions{
				FeasibilityTimeout: 50 * time.Millisecond,
			})
			controller.deadlineFactory = func(time.Duration) cfgFeasibilityDeadline {
				return deadline
			}
			controller.backend = fixedCFGFeasibilityBackend{decision: unsat}
			result := controller.Refine(cfgRefinementRequest{
				Pass:      pass,
				CFG:       cfg,
				Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
				Category:  CategoryUnvalidatedCast,
				FindingID: "late-unsat",
				CallChain: []string{"probe.sample"},
				Rerun: func(cfgRefinementOverride) interprocPathResult {
					t.Fatal("late UNSAT or evidence must not discharge or rerun the witness")
					return interprocPathResult{}
				},
			})
			if result.Class != interprocOutcomeInconclusive ||
				result.Refinement.FeasibilityResult != cfgFeasibilityResultUnknown ||
				result.Refinement.FeasibilityReason != cfgFeasibilityReasonTimeout {
				t.Fatalf("late refinement result = %+v, want blocking timeout", result)
			}
		})
	}
}

func TestCFGSSAConstraintsMissingSSAReturnsUnknown(t *testing.T) {
	t.Parallel()

	_, cfg := mustBuildTypedTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		return
	}
}
`)
	path := witnessPathFromChoices(t, cfg, true)
	decision := (cfgSSAConstraintsFeasibilityBackend{}).Check(cfgFeasibilityQuery{
		CFG:     cfg,
		Witness: cfgWitnessRecord{CFGPath: path},
		Timeout: 50 * time.Millisecond,
	})
	if decision.Result != cfgFeasibilityResultUnknown || decision.Reason != cfgFeasibilityReasonUnsupportedPredicate {
		t.Fatalf("Check() = (%q, %q), want (%q, %q)",
			decision.Result,
			decision.Reason,
			cfgFeasibilityResultUnknown,
			cfgFeasibilityReasonUnsupportedPredicate,
		)
	}
}

func TestCFGRefinementUnknownResultStaysInconclusive(t *testing.T) {
	t.Parallel()

	controller := newCFGRefinementController(cfgProtocolRefinementOptions{
		RefinementMaxIterations: 3,
		FeasibilityMaxQueries:   16,
		FeasibilityTimeout:      50 * time.Millisecond,
	})
	result := controller.Refine(cfgRefinementRequest{
		Result: interprocPathResult{Class: interprocOutcomeUnsafe},
		Rerun: func(cfgRefinementOverride) interprocPathResult {
			t.Fatal("unknown feasibility must not rerun or discharge the witness")
			return interprocPathResult{}
		},
	})
	finalizationState := "blocking-inconclusive"
	if result.Class == interprocOutcomeSafe {
		finalizationState = "optimistic-safe"
	} else if result.Class != interprocOutcomeInconclusive {
		finalizationState = "other-outcome"
	}
	requireMutationGuardObservation(
		t,
		"unknown-to-safe/feasibility-finalization",
		mutationGuardState("unknown-feasibility-finalization", "blocking-inconclusive"),
		mutationGuardState("unknown-feasibility-finalization", finalizationState),
	)
	if result.Class != interprocOutcomeInconclusive {
		t.Fatalf("unknown feasibility outcome = %s, want inconclusive", result.Class)
	}
	if result.Reason != pathOutcomeReasonFeasibilityUnknown {
		t.Fatalf("unknown feasibility reason = %q, want %q", result.Reason, pathOutcomeReasonFeasibilityUnknown)
	}
}

type fixedCFGFeasibilityBackend struct {
	decision cfgFeasibilityDecision
}

func (backend fixedCFGFeasibilityBackend) Check(cfgFeasibilityQuery) cfgFeasibilityDecision {
	return backend.decision
}

func corruptSSAConstraintEvidence(evidence cfgSSAConstraintEvidence) cfgSSAConstraintEvidence {
	result := evidence
	result.Contradictions = append([]cfgSSAConstraintContradiction(nil), evidence.Contradictions...)
	if len(result.Contradictions) > 0 {
		result.Contradictions[0].Left.value = "\"corrupt\""
	}
	return result
}

func TestCFGRefinementResourceLimitsStayInconclusive(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`)
	path := witnessPathFromChoices(t, cfg, true, true)
	tests := []struct {
		name           string
		queryBudget    int
		iterationLimit int
		wantReruns     int
		wantReason     string
	}{
		{
			name:           "query limit",
			queryBudget:    1,
			iterationLimit: 3,
			wantReruns:     0,
			wantReason:     cfgFeasibilityReasonQueryLimit,
		},
		{
			name:           "iteration limit",
			queryBudget:    4,
			iterationLimit: 1,
			wantReruns:     1,
			wantReason:     cfgFeasibilityReasonIterationLimit,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := newCFGRefinementController(cfgProtocolRefinementOptions{
				RefinementMaxIterations: tt.iterationLimit,
				FeasibilityMaxQueries:   tt.queryBudget,
				FeasibilityTimeout:      50 * time.Millisecond,
			})
			reruns := 0
			result := controller.Refine(cfgRefinementRequest{
				Pass:      pass,
				CFG:       cfg,
				Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
				Category:  CategoryUnvalidatedCast,
				FindingID: "resource-limit",
				CallChain: []string{"testpkg.sample"},
				Rerun: func(cfgRefinementOverride) interprocPathResult {
					reruns++
					return interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path}
				},
			})
			if reruns != tt.wantReruns {
				t.Fatalf("reruns = %d, want %d", reruns, tt.wantReruns)
			}
			if result.Class != interprocOutcomeInconclusive ||
				result.Refinement.FeasibilityResult != cfgFeasibilityResultUnknown ||
				result.Refinement.FeasibilityReason != tt.wantReason {
				t.Fatalf("result = %+v, want blocking inconclusive with reason %q", result, tt.wantReason)
			}
		})
	}
}

func TestCFGRefinementIteratesCheckedUNSATWitnesses(t *testing.T) {
	t.Parallel()

	pass, cfg := mustBuildTypedTestCFG(t, `package p
func sample(raw string) {
	if raw == "" {
		if raw != "" {
			return
		}
	}
}
`)
	path := witnessPathFromChoices(t, cfg, true, true)
	controller := newCFGRefinementController(cfgProtocolRefinementOptions{
		RefinementMaxIterations: 3,
		FeasibilityMaxQueries:   4,
		FeasibilityTimeout:      50 * time.Millisecond,
	})
	reruns := 0
	result := controller.Refine(cfgRefinementRequest{
		Pass:      pass,
		CFG:       cfg,
		Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
		Category:  CategoryUnvalidatedCast,
		FindingID: "iterative-discharge",
		CallChain: []string{"testpkg.sample"},
		Rerun: func(override cfgRefinementOverride) interprocPathResult {
			reruns++
			switch reruns {
			case 1:
				if len(override.DischargedWitnesses) != 1 {
					t.Fatalf("first rerun has %d discharged witnesses, want 1", len(override.DischargedWitnesses))
				}
				return interprocPathResult{
					Class:       interprocOutcomeUnsafe,
					Witness:     path,
					WitnessHash: "second-checked-witness",
				}
			case 2:
				if len(override.DischargedWitnesses) != 2 {
					t.Fatalf("second rerun has %d discharged witnesses, want 2", len(override.DischargedWitnesses))
				}
				return interprocPathResult{Class: interprocOutcomeSafe}
			default:
				t.Fatalf("unexpected rerun %d", reruns)
				return interprocPathResult{}
			}
		},
	})
	if reruns != 2 {
		t.Fatalf("reruns = %d, want 2", reruns)
	}
	if result.Class != interprocOutcomeSafe ||
		result.Refinement.RefinementStatus != cfgRefinementStatusDischargedInfeasible ||
		result.Refinement.RefinementIterations != 2 {
		t.Fatalf("result = %+v, want two checked discharges followed by no remaining witness", result)
	}
}

func TestCFGWitnessHashDeterministic(t *testing.T) {
	t.Parallel()

	record := cfgWitnessRecord{
		OriginAnchors: map[string]string{
			"witness_cast_pos": "sample.go:10:5",
		},
		FactFamily:    "cast-needs-validate",
		FactKey:       "origin|target|type",
		EdgeTag:       string(ideEdgeFuncIdentity),
		CFGPath:       []int32{0, 1, 3},
		CallChain:     []string{"pkg.Func"},
		TriggerReason: cfgRefinementTriggerUnsafeCandidate,
	}
	got1 := computeCFGWitnessHash(record)
	got2 := computeCFGWitnessHash(record)
	if got1 == "" {
		t.Fatal("computeCFGWitnessHash() returned empty hash")
	}
	if got1 != got2 {
		t.Fatalf("computeCFGWitnessHash() = %q, want deterministic %q", got1, got2)
	}
}

func TestCFGUnsupportedPredicateProvenanceIgnoresLayoutDrift(t *testing.T) {
	t.Parallel()

	base := unsupportedPredicateProvenanceForSource(t, `package p
func opaque(string) bool { return false }
func sample(raw string) { if opaque(raw) { return } }
`)
	shifted := unsupportedPredicateProvenanceForSource(t, `package p
const unrelatedDeclarationWithDifferentLength = "layout drift"
func opaque(string) bool { return false }
func sample(raw string) { if opaque(raw) { return } }
`)
	if base != shifted {
		t.Fatalf("unsupported predicate provenance changed under layout drift:\nbase=%q\nshifted=%q", base, shifted)
	}
}

func unsupportedPredicateProvenanceForSource(t *testing.T, source string) string {
	t.Helper()

	pass, cfg := mustBuildTypedTestCFG(t, source)
	values := buildCFGSSAValueIndexFromResult(buildSSAForPass(pass))
	for _, block := range cfg.Blocks {
		if block == nil || len(block.Succs) != 2 {
			continue
		}
		provenance := cfgSSAPredicateProvenance(pass, values, block, block.Succs[0])
		if len(provenance) != 1 {
			t.Fatalf("predicate provenance = %q, want one record", provenance)
		}
		if !strings.HasPrefix(provenance[0], "ssa-unsupported|") {
			t.Fatalf("predicate provenance = %q, want unsupported record", provenance[0])
		}
		return provenance[0]
	}
	t.Fatal("conditional CFG block not found")
	return ""
}

func mustBuildTypedTestCFG(t *testing.T, src string) (*analysis.Pass, *gocfg.CFG) {
	t.Helper()

	pass, file := buildTypedPassFromSource(t, src)
	fn := findFuncDecl(t, file, "sample")
	return pass, buildFuncCFGForPass(pass, fn.Body, buildSSAForPass(pass))
}

func witnessPathFromChoices(t *testing.T, cfg *gocfg.CFG, choices ...bool) []int32 {
	t.Helper()
	if cfg == nil || len(cfg.Blocks) == 0 {
		t.Fatal("cfg is empty")
	}
	block := cfg.Blocks[0]
	path := []int32{block.Index}
	for _, choice := range choices {
		if block == nil || len(block.Succs) != 2 {
			t.Fatalf("block %d does not have a conditional successor set", block.Index)
		}
		if choice {
			block = block.Succs[0]
		} else {
			block = block.Succs[1]
		}
		path = appendWitnessBlock(path, block.Index)
	}
	return path
}

func cfgBlockContainingCall(t *testing.T, cfg *gocfg.CFG, name string) *gocfg.Block {
	t.Helper()

	for _, block := range cfg.Blocks {
		if block == nil {
			continue
		}
		for _, node := range block.Nodes {
			found := false
			ast.Inspect(node, func(candidate ast.Node) bool {
				call, ok := candidate.(*ast.CallExpr)
				if !ok {
					return true
				}
				ident, ok := call.Fun.(*ast.Ident)
				if ok && ident.Name == name {
					found = true
					return false
				}
				return true
			})
			if found {
				return block
			}
		}
	}
	t.Fatalf("CFG has no call to %s", name)
	return nil
}
