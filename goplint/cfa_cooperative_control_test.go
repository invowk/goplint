// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
	"time"
)

type switchableProtocolControl struct {
	expired bool
}

func (control *switchableProtocolControl) Expired() bool {
	return control.expired
}

func TestSSAConstraintExtractionStopsCooperatively(t *testing.T) {
	t.Parallel()

	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{
		{subject: "value", op: "eq", value: "one"},
		{subject: "value", op: "neq", value: "one"},
	}}}
	provenance := encodeCFGSSAPredicateFormula(formula)
	if provenance == "" {
		t.Fatal("failed to encode extraction fixture")
	}
	witness := cfgWitnessRecord{WitnessEdges: []interprocWitnessEdge{
		{PredicateProvenance: []string{provenance, provenance}},
		{PredicateProvenance: []string{provenance}},
	}}
	control := &steppedFeasibilityDeadline{expiresAt: 6}

	got, expired := extractSSAConstraintsForWitnessRecordWithControl(nil, nil, witness, control)
	if !expired {
		t.Fatalf("extraction completed after %d checkpoints, want cooperative stop", control.checks)
	}
	if got.unsupported || len(got.alternatives) != 0 {
		t.Fatalf("expired extraction returned semantic formula %+v, want empty non-semantic result", got)
	}
}

func TestIFDSTabulationStopsCooperativelyBeforeAcceptingSafe(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG}
	middle := interprocNodeID{FuncKey: "entry", BlockIndex: 1, Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "entry", BlockIndex: 2, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: middle, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: middle, To: exit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[exit.Key()] = true
	control := &steppedFeasibilityDeadline{expiresAt: 12}

	result := runIFDSPropagationControlled(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(interprocNodeID, ast.Node, ideValidationState) bool { return false },
		nil,
		control,
		nil,
	)
	reasonState := string(result.Reason)
	if result.Reason == pathOutcomeReasonNone {
		reasonState = "none"
	}
	requireMutationGuardObservation(
		t,
		"timeout/cooperative-tabulation-deadline",
		mutationGuardState("tabulation-outcome", "inconclusive:timeout"),
		mutationGuardState("tabulation-outcome", string(result.Class)+":"+reasonState),
	)
	if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonTimeout {
		t.Fatalf("tabulation result = %s (%s), want cooperative timeout", result.Class, result.Reason)
	}
	if control.checks < control.expiresAt {
		t.Fatalf("tabulation used %d checkpoints, want at least %d", control.checks, control.expiresAt)
	}
}

func TestCFGRefinementRejectsCancellationDuringIteration(t *testing.T) {
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
	control := &switchableProtocolControl{}
	controller := newCFGRefinementController(cfgProtocolRefinementOptions{
		RefinementMaxIterations: 2,
		FeasibilityMaxQueries:   2,
		FeasibilityTimeout:      time.Second,
	})
	reruns := 0
	result := controller.Refine(cfgRefinementRequest{
		Pass:      pass,
		CFG:       cfg,
		Result:    interprocPathResult{Class: interprocOutcomeUnsafe, Witness: path},
		Category:  CategoryUnvalidatedCast,
		FindingID: "cancel-during-iteration",
		CallChain: []string{"p.sample"},
		Control:   control,
		Rerun: func(cfgRefinementOverride) interprocPathResult {
			reruns++
			control.expired = true
			return interprocPathResult{Class: interprocOutcomeSafe}
		},
	})
	if reruns != 1 {
		t.Fatalf("reruns = %d, want one iteration before cancellation", reruns)
	}
	if result.Class != interprocOutcomeInconclusive ||
		result.Refinement.FeasibilityResult != cfgFeasibilityResultUnknown ||
		result.Refinement.FeasibilityReason != cfgFeasibilityReasonTimeout {
		t.Fatalf("iteration result = %+v, want blocking timeout", result)
	}
}
