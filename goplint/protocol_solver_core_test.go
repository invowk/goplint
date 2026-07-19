// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"sync"
	"testing"
)

func TestIFDSPropagationMatchesReturnsToCallingSite(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "caller", Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "caller", Kind: interprocNodeKindCall}
	entry := interprocNodeID{FuncKey: "callee", Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "callee", NodeIndex: 1, Kind: interprocNodeKindCFG}
	matchedReturn := interprocNodeID{FuncKey: "caller", Kind: interprocNodeKindReturn}
	foreignReturn := interprocNodeID{FuncKey: "other-caller", Kind: interprocNodeKindReturn}
	graph.addEdge(interprocEdge{From: start, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "site-a"})
	graph.addEdge(interprocEdge{From: entry, To: exit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: exit, To: matchedReturn, Kind: interprocEdgeReturn, CallSite: "site-a"})
	graph.addEdge(interprocEdge{From: exit, To: foreignReturn, Kind: interprocEdgeReturn, CallSite: "site-b"})
	graph.functionExitNodes[matchedReturn.Key()] = true
	graph.functionExitNodes[foreignReturn.Key()] = true

	result := runIFDSPropagation(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == matchedReturn.Key() {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state == ideStateNeedsValidate
		},
		nil,
	)
	requireMutationGuardObservation(
		t,
		"matched-returns/calling-site-outcome",
		mutationGuardState("matched-return-propagation", string(interprocOutcomeSafe)),
		mutationGuardState("matched-return-propagation", string(result.Class)),
	)
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("matched propagation outcome = %s, want safe", result.Class)
	}
}

func TestIFDSTabulationConvergesRecursiveSummaryWithoutDepthCutoff(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	entry := interprocNodeID{FuncKey: "recursive", BlockIndex: 0, Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindCall}
	baseExit := interprocNodeID{FuncKey: "recursive", BlockIndex: 2, Kind: interprocNodeKindCFG}
	returnSite := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindReturn}
	recursiveExit := interprocNodeID{FuncKey: "recursive", BlockIndex: 3, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: entry, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: entry, To: baseExit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "self"})
	graph.addEdge(interprocEdge{From: baseExit, To: returnSite, Kind: interprocEdgeReturn, CallSite: "self"})
	graph.addEdge(interprocEdge{From: returnSite, To: recursiveExit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[baseExit.Key()] = true
	graph.functionExitNodes[recursiveExit.Key()] = true

	result := runIFDSPropagation(
		graph,
		entry,
		100,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == baseExit.Key() {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state != ideStateValidated
		},
		nil,
	)
	reuseState := "summary-reused"
	if result.Class != interprocOutcomeSafe ||
		result.Tabulation.Summaries == 0 || result.Tabulation.SummaryReuses == 0 {
		reuseState = "summary-not-reused"
	}
	requireMutationGuardObservation(
		t,
		"recursive-summary/direct-fixed-point-reuse",
		mutationGuardState("recursive-summary-fixed-point", "summary-reused"),
		mutationGuardState("recursive-summary-fixed-point", reuseState),
	)
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("recursive tabulation outcome = %s (%s), want safe", result.Class, result.Reason)
	}
	if result.Tabulation.Summaries == 0 || result.Tabulation.SummaryReuses == 0 {
		t.Fatalf("tabulation stats = %+v, want a produced and reused recursive summary", result.Tabulation)
	}
	if result.Tabulation.PathEdges >= 100 {
		t.Fatalf("path-edge count = %d, want finite convergence below the state budget", result.Tabulation.PathEdges)
	}
}

func TestIFDSTabulationRecursiveViolationRemainsUnsafe(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	entry := interprocNodeID{FuncKey: "recursive", BlockIndex: 0, Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindCall}
	baseExit := interprocNodeID{FuncKey: "recursive", BlockIndex: 2, Kind: interprocNodeKindCFG}
	returnSite := interprocNodeID{FuncKey: "recursive", BlockIndex: 1, Kind: interprocNodeKindReturn}
	graph.addEdge(interprocEdge{From: entry, To: call, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: entry, To: baseExit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "self"})
	graph.addEdge(interprocEdge{From: baseExit, To: returnSite, Kind: interprocEdgeReturn, CallSite: "self"})
	graph.functionExitNodes[baseExit.Key()] = true

	result := runIFDSPropagation(
		graph,
		entry,
		100,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state != ideStateValidated
		},
		nil,
	)
	if result.Class != interprocOutcomeUnsafe {
		t.Fatalf("recursive violation outcome = %s (%s), want unsafe", result.Class, result.Reason)
	}
}

func TestIFDSTabulationConvergesMutuallyRecursiveSummaries(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	aEntry := interprocNodeID{FuncKey: "a", BlockIndex: 0, Kind: interprocNodeKindCFG}
	aCallB := interprocNodeID{FuncKey: "a", BlockIndex: 1, Kind: interprocNodeKindCall}
	aBase := interprocNodeID{FuncKey: "a", BlockIndex: 2, Kind: interprocNodeKindCFG}
	aReturn := interprocNodeID{FuncKey: "a", BlockIndex: 1, Kind: interprocNodeKindReturn}
	aExit := interprocNodeID{FuncKey: "a", BlockIndex: 3, Kind: interprocNodeKindCFG}
	bEntry := interprocNodeID{FuncKey: "b", BlockIndex: 0, Kind: interprocNodeKindCFG}
	bCallA := interprocNodeID{FuncKey: "b", BlockIndex: 1, Kind: interprocNodeKindCall}
	bBase := interprocNodeID{FuncKey: "b", BlockIndex: 2, Kind: interprocNodeKindCFG}
	bReturn := interprocNodeID{FuncKey: "b", BlockIndex: 1, Kind: interprocNodeKindReturn}
	bExit := interprocNodeID{FuncKey: "b", BlockIndex: 3, Kind: interprocNodeKindCFG}

	graph.addEdge(interprocEdge{From: aEntry, To: aCallB, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: aEntry, To: aBase, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: aCallB, To: bEntry, Kind: interprocEdgeCall, CallSite: "a-b"})
	graph.addEdge(interprocEdge{From: bBase, To: aReturn, Kind: interprocEdgeReturn, CallSite: "a-b"})
	graph.addEdge(interprocEdge{From: aReturn, To: aExit, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: bEntry, To: bCallA, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: bEntry, To: bBase, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: bCallA, To: aEntry, Kind: interprocEdgeCall, CallSite: "b-a"})
	graph.addEdge(interprocEdge{From: aBase, To: bReturn, Kind: interprocEdgeReturn, CallSite: "b-a"})
	graph.addEdge(interprocEdge{From: bReturn, To: bExit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[aBase.Key()] = true
	graph.functionExitNodes[aExit.Key()] = true
	graph.functionExitNodes[bBase.Key()] = true
	graph.functionExitNodes[bExit.Key()] = true

	result := runIFDSPropagation(
		graph,
		aEntry,
		200,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == aBase.Key() || node.Key() == bBase.Key() {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state != ideStateValidated
		},
		nil,
	)
	reuseState := "summaries-reused-across-both-procedures"
	if result.Class != interprocOutcomeSafe ||
		result.Tabulation.Summaries < 2 || result.Tabulation.SummaryReuses < 2 {
		reuseState = "summaries-not-reused-across-both-procedures"
	}
	requireMutationGuardObservation(
		t,
		"recursive-summary/mutual-fixed-point-reuse",
		mutationGuardState("mutual-recursive-summary-fixed-point", "summaries-reused-across-both-procedures"),
		mutationGuardState("mutual-recursive-summary-fixed-point", reuseState),
	)
	if result.Class != interprocOutcomeSafe {
		t.Fatalf("mutual-recursion outcome = %s (%s), want safe", result.Class, result.Reason)
	}
	if result.Tabulation.Summaries < 2 || result.Tabulation.SummaryReuses < 2 {
		t.Fatalf("tabulation stats = %+v, want summaries reused across both procedures", result.Tabulation)
	}
	if result.Tabulation.PathEdges >= 200 {
		t.Fatalf("path-edge count = %d, want finite mutual-recursion convergence", result.Tabulation.PathEdges)
	}
}

func TestIFDSTabulationPreservesEffectsAcrossSummaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		initialTag ideEdgeFuncTag
		calleeTag  ideEdgeFuncTag
		reason     pathOutcomeReason
		wantClass  interprocOutcomeClass
		wantReason pathOutcomeReason
	}{
		{
			name:       "conditional validation",
			initialTag: ideEdgeFuncIdentity,
			calleeTag:  ideEdgeFuncValidate,
			wantClass:  interprocOutcomeSafe,
		},
		{
			name:       "escape hazard",
			initialTag: ideEdgeFuncIdentity,
			calleeTag:  ideEdgeFuncEscape,
			wantClass:  interprocOutcomeUnsafe,
		},
		{
			name:       "post-validation mutation uncertainty",
			initialTag: ideEdgeFuncValidate,
			calleeTag:  ideEdgeFuncIdentity,
			reason:     pathOutcomeReasonConcurrentMutation,
			wantClass:  interprocOutcomeInconclusive,
			wantReason: pathOutcomeReasonConcurrentMutation,
		},
		{
			name:       "identity uncertainty",
			initialTag: ideEdgeFuncIdentity,
			calleeTag:  ideEdgeFuncIdentity,
			reason:     pathOutcomeReasonAmbiguousIdentity,
			wantClass:  interprocOutcomeInconclusive,
			wantReason: pathOutcomeReasonAmbiguousIdentity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			graph := newInterprocSupergraph()
			start := interprocNodeID{FuncKey: "caller", BlockIndex: 0, Kind: interprocNodeKindCFG}
			call := interprocNodeID{FuncKey: "caller", BlockIndex: 1, Kind: interprocNodeKindCall}
			entry := interprocNodeID{FuncKey: "callee", BlockIndex: 0, Kind: interprocNodeKindCFG}
			exit := interprocNodeID{FuncKey: "callee", BlockIndex: 1, Kind: interprocNodeKindCFG}
			ret := interprocNodeID{FuncKey: "caller", BlockIndex: 2, Kind: interprocNodeKindReturn}
			graph.addEdge(interprocEdge{From: start, To: call, Kind: interprocEdgeIntra})
			graph.addEdge(interprocEdge{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "caller-callee"})
			graph.addEdge(interprocEdge{From: entry, To: exit, Kind: interprocEdgeIntra})
			graph.addEdge(interprocEdge{From: exit, To: ret, Kind: interprocEdgeReturn, CallSite: "caller-callee"})
			graph.functionExitNodes[ret.Key()] = true

			result := runIFDSPropagation(
				graph,
				start,
				100,
				nil,
				nil,
				nil,
				func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
					switch node.Key() {
					case start.Key():
						return tt.initialTag, pathOutcomeReasonNone
					case exit.Key():
						return tt.calleeTag, tt.reason
					default:
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
				},
				func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
					return graph.isFunctionExitNode(node) && state != ideStateValidated
				},
				nil,
			)
			if result.Class != tt.wantClass || result.Reason != tt.wantReason {
				t.Fatalf("summary result = %s (%s), want %s (%s)", result.Class, result.Reason, tt.wantClass, tt.wantReason)
			}
			if result.Tabulation.Summaries == 0 || result.Tabulation.SummaryReuses == 0 {
				t.Fatalf("tabulation stats = %+v, want a produced and reused summary", result.Tabulation)
			}
		})
	}
}

func TestInterprocWitnessReplayRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "caller", BlockIndex: 0, Kind: interprocNodeKindCFG}
	call := interprocNodeID{FuncKey: "caller", BlockIndex: 1, Kind: interprocNodeKindCall}
	entry := interprocNodeID{FuncKey: "callee", BlockIndex: 0, Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "callee", BlockIndex: 1, Kind: interprocNodeKindCFG}
	ret := interprocNodeID{FuncKey: "caller", BlockIndex: 2, Kind: interprocNodeKindReturn}
	other := interprocNodeID{FuncKey: "caller", BlockIndex: 3, Kind: interprocNodeKindCFG}
	foreign := interprocNodeID{FuncKey: "foreign", BlockIndex: 0, Kind: interprocNodeKindCFG}
	edges := []interprocEdge{
		{From: start, To: call, Kind: interprocEdgeIntra},
		{From: call, To: entry, Kind: interprocEdgeCall, CallSite: "site-a"},
		{From: entry, To: exit, Kind: interprocEdgeIntra},
		{From: exit, To: ret, Kind: interprocEdgeReturn, CallSite: "site-a"},
		{From: exit, To: ret, Kind: interprocEdgeReturn, CallSite: "site-b"},
		{From: other, To: ret, Kind: interprocEdgeIntra},
		{From: start, To: foreign, Kind: interprocEdgeIntra},
	}
	for _, edge := range edges {
		graph.addEdge(edge)
	}

	witness := make([]interprocWitnessEdge, 0, 4)
	for _, edge := range edges[:4] {
		witness = appendInterprocWitnessEdge(
			witness,
			edge,
			ideStateNeedsValidate,
			ideStateNeedsValidate,
			ideEdgeFuncIdentity,
		)
	}
	result := interprocPathResult{
		Class:        interprocOutcomeUnsafe,
		FactFamily:   ifdsFactFamilyCastNeedsValidate,
		FactKey:      "caller:target",
		WitnessEdges: qualifyInterprocWitnessFact(witness, ifdsFactFamilyCastNeedsValidate, "caller:target"),
	}
	if reason := validateInterprocWitnessReplay(graph, result); reason != "" {
		t.Fatalf("valid replay rejected: %s", reason)
	}

	tests := []struct {
		name       string
		wantReason string
		mutate     func([]interprocWitnessEdge) []interprocWitnessEdge
	}{
		{
			name:       "missing node",
			wantReason: interprocWitnessReplayMissingNode,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[0].From.NodeIndex = 99
				return candidate
			},
		},
		{
			name:       "non-edge",
			wantReason: interprocWitnessReplayNonEdge,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[0].To = other
				return candidate[:1]
			},
		},
		{
			name:       "disconnected transition",
			wantReason: interprocWitnessReplayDisconnected,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[1] = qualifyInterprocWitnessFact([]interprocWitnessEdge{{
					From: other, To: ret, Kind: interprocEdgeIntra,
					StateBefore: ideStateNeedsValidate, StateAfter: ideStateNeedsValidate,
					EdgeFunctionTag: ideEdgeFuncIdentity,
				}}, ifdsFactFamilyCastNeedsValidate, "caller:target")[0]
				return candidate[:2]
			},
		},
		{
			name:       "cross-procedure intra edge",
			wantReason: interprocWitnessReplayCrossProcedure,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[0].To = foreign
				return candidate[:1]
			},
		},
		{
			name:       "unmatched return",
			wantReason: interprocWitnessReplayUnmatchedReturn,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[3].CallSite = "site-b"
				return candidate
			},
		},
		{
			name:       "changed fact",
			wantReason: interprocWitnessReplayChangedFact,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[2].FactKey = "other-target"
				return candidate
			},
		},
		{
			name:       "changed state",
			wantReason: interprocWitnessReplayChangedState,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[2].StateBefore = ideStateValidated
				return candidate
			},
		},
		{
			name:       "changed edge reason",
			wantReason: interprocWitnessReplayNonEdge,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[2].EdgeReason = pathOutcomeReasonUnresolvedTarget
				return candidate
			},
		},
		{
			name:       "changed predicate provenance",
			wantReason: interprocWitnessReplayNonEdge,
			mutate: func(candidate []interprocWitnessEdge) []interprocWitnessEdge {
				candidate[2].PredicateProvenance = []string{"ssa-formula-v1:corrupt"}
				return candidate
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := result
			candidate.WitnessEdges = tt.mutate(cloneInterprocWitnessEdges(result.WitnessEdges))
			if reason := validateInterprocWitnessReplay(graph, candidate); reason != tt.wantReason {
				t.Fatalf("replay rejection = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestIFDSWitnessAntichainPreservesIncomparableContributions(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG}
	left := interprocNodeID{FuncKey: "entry", BlockIndex: 1, Kind: interprocNodeKindCFG}
	right := interprocNodeID{FuncKey: "entry", BlockIndex: 2, Kind: interprocNodeKindCFG}
	join := interprocNodeID{FuncKey: "entry", BlockIndex: 3, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{
		From: start, To: left, Kind: interprocEdgeIntra,
		PredicateProvenance: []string{"ssa-formula|condition=flag|truthy=true|formula=flag|eq|true"},
	})
	graph.addEdge(interprocEdge{
		From: start, To: right, Kind: interprocEdgeIntra,
		PredicateProvenance: []string{"ssa-formula|condition=flag|truthy=false|formula=flag|eq|false"},
	})
	graph.addEdge(interprocEdge{From: left, To: join, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{From: right, To: join, Kind: interprocEdgeIntra})
	graph.functionExitNodes[join.Key()] = true

	const factKey = "entry:target"
	witnessHash := newInterprocWitnessHashFunc(nil, ifdsFactFamilyCastNeedsValidate, factKey)
	run := func(discharged map[string]bool) interprocPathResult {
		result := runIFDSPropagation(
			graph,
			start,
			100,
			nil,
			discharged,
			witnessHash,
			func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
				return node == join && state == ideStateNeedsValidate
			},
			nil,
		)
		result.FactFamily = ifdsFactFamilyCastNeedsValidate
		result.FactKey = factKey
		result.EdgeFunctionTag = edgeTagFromPathResult(result)
		setInterprocWitnessHash(&result, nil, nil)
		return result
	}

	first := run(nil)
	if first.Class != interprocOutcomeUnsafe || first.WitnessHash == "" {
		t.Fatalf("first contribution = %s hash=%q, want hashed unsafe", first.Class, first.WitnessHash)
	}
	firstDischarge := first.WitnessHash + "|cfgd1_test"
	second := run(map[string]bool{firstDischarge: true})
	if second.Class != interprocOutcomeUnsafe || second.WitnessHash == "" || second.WitnessHash == first.WitnessHash {
		t.Fatalf(
			"second contribution = %s hash=%q, want a different hashed unsafe after discharging %q",
			second.Class,
			second.WitnessHash,
			first.WitnessHash,
		)
	}
	secondDischarge := second.WitnessHash + "|cfgd1_test"
	third := run(map[string]bool{firstDischarge: true, secondDischarge: true})
	if third.Class != interprocOutcomeSafe {
		t.Fatalf("fully discharged antichain = %s (%s), want safe", third.Class, third.Reason)
	}
}

func TestIFDSWitnessAntichainBoundIsInconclusive(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG}
	join := interprocNodeID{FuncKey: "entry", BlockIndex: 100, Kind: interprocNodeKindCFG}
	for index := range interprocWitnessAntichainLimit + 1 {
		branch := interprocNodeID{
			FuncKey: "entry", BlockIndex: int32(index + 1), Kind: interprocNodeKindCFG,
		}
		graph.addEdge(interprocEdge{
			From: start, To: branch, Kind: interprocEdgeIntra,
			PredicateProvenance: []string{"ssa-formula|branch=" + strconvItoa(index)},
		})
		graph.addEdge(interprocEdge{From: branch, To: join, Kind: interprocEdgeIntra})
	}
	graph.functionExitNodes[join.Key()] = true

	result := runIFDSPropagation(
		graph,
		start,
		1000,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return node == join && state == ideStateNeedsValidate
		},
		nil,
	)
	if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonWitnessBudget {
		t.Fatalf("antichain bound = %s (%s), want inconclusive witness budget", result.Class, result.Reason)
	}
}

func TestInterprocWitnessSubsumptionRequiresNoMoreConstraints(t *testing.T) {
	t.Parallel()

	edge := interprocEdge{
		From: interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG},
		To:   interprocNodeID{FuncKey: "entry", BlockIndex: 1, Kind: interprocNodeKindCFG},
		Kind: interprocEdgeIntra,
	}
	unconditional := appendInterprocWitnessEdge(
		nil,
		edge,
		ideStateNeedsValidate,
		ideStateNeedsValidate,
		ideEdgeFuncIdentity,
	)
	conditional := cloneInterprocWitnessEdges(unconditional)
	conditional[0].PredicateProvenance = []string{"ssa-formula|flag=true"}
	incomparable := cloneInterprocWitnessEdges(unconditional)
	incomparable[0].PredicateProvenance = []string{"ssa-formula|other=true"}

	if !interprocWitnessSubsumes(unconditional, conditional) {
		t.Fatal("unconditional witness should subsume the more constrained contribution")
	}
	if interprocWitnessSubsumes(conditional, unconditional) {
		t.Fatal("conditional witness must not subsume an unconditional contribution")
	}
	if interprocWitnessSubsumes(conditional, incomparable) || interprocWitnessSubsumes(incomparable, conditional) {
		t.Fatal("different branch predicates must remain incomparable")
	}
}

func TestIFDSTabulationBudgetExhaustionRejectsPartialResult(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "entry", BlockIndex: 1, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: exit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[exit.Key()] = true

	result := runIFDSPropagation(
		graph,
		start,
		1,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state != ideStateValidated
		},
		nil,
	)
	if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonStateBudget {
		t.Fatalf("budget result = %s (%s), want inconclusive state budget", result.Class, result.Reason)
	}
	if result.Tabulation.PathEdges != 1 {
		t.Fatalf("path edges = %d, want only the admitted root edge", result.Tabulation.PathEdges)
	}
}

func TestIFDSViolationOutranksObservedInconclusivePath(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindCFG}
	unknownCall := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindCall}
	unknownReturn := interprocNodeID{FuncKey: "entry", Kind: interprocNodeKindReturn}
	unsafeExit := interprocNodeID{FuncKey: "entry", NodeIndex: 1, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: unknownCall, Kind: interprocEdgeIntra})
	graph.addEdge(interprocEdge{
		From: unknownCall, To: unknownReturn, Kind: interprocEdgeCallToReturn, Reason: pathOutcomeReasonUnresolvedTarget,
	})
	graph.addEdge(interprocEdge{From: start, To: unsafeExit, Kind: interprocEdgeIntra})
	graph.functionExitNodes[unsafeExit.Key()] = true

	result := runIFDSPropagation(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(node interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return graph.isFunctionExitNode(node) && state == ideStateNeedsValidate
		},
		func(interprocNodeID, ast.Node, ideValidationState) bool { return true },
	)
	if result.Class != interprocOutcomeUnsafe {
		t.Fatalf("aggregated outcome = %s, want unsafe to outrank inconclusive", result.Class)
	}
}

func TestInterprocGraphHasNoUnconditionalResolvedCallBypass(t *testing.T) {
	t.Parallel()

	const source = `package probe
func helper() {}
func sink() {}
func entry() {
	helper()
	sink()
}
`
	pass, file := buildTypedPassFromSource(t, source)
	entry := findFuncDecl(t, file, "entry")
	graph := buildInterprocSupergraphForFunc(pass, entry, buildSSAForPass(pass))
	entryKey := interprocFunctionKey(pass, entry)
	callCFG := interprocNodeID{FuncKey: entryKey, BlockIndex: 0, NodeIndex: 0, Kind: interprocNodeKindCFG}
	for _, edge := range graph.outgoing(callCFG) {
		if edge.To.Kind == interprocNodeKindCFG && edge.To.NodeIndex == 1 {
			t.Fatal("resolved call CFG node has an unconditional bypass to its continuation")
		}
	}
}

func TestInterprocGraphBuildsRecursiveCallEdges(t *testing.T) {
	t.Parallel()

	const source = `package probe
func recursive(done bool) {
	if done { return }
	recursive(true)
}
`
	pass, file := buildTypedPassFromSource(t, source)
	fn := findFuncDecl(t, file, "recursive")
	functionKey := interprocFunctionKey(pass, fn)
	graph := buildInterprocSupergraphForFunc(pass, fn, buildSSAForPass(pass))
	found := false
	for _, edge := range graph.Edges {
		if edge.Kind == interprocEdgeCall && edge.From.FuncKey == functionKey && edge.To.FuncKey == functionKey && edge.CallSite != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("recursive call did not produce a call-site-qualified supergraph edge")
	}
}

func TestInterprocGraphPrunesKnownNonReturningCallsAndAliases(t *testing.T) {
	t.Parallel()

	const source = `package probe
import "log"

func direct() {
	log.Fatal("stop")
}

func aliased() {
	fatal := log.Fatalf
	fatal("%s", "stop")
}
`
	pass, file := buildTypedPassFromSource(t, source)
	for _, functionName := range []string{"direct", "aliased"} {
		fn := findFuncDecl(t, file, functionName)
		graph := buildInterprocSupergraphForFunc(pass, fn, buildSSAForPass(pass))
		foundNonReturning := false
		hasContinuation := false
		for key := range graph.nonReturningNodes {
			foundNonReturning = true
			for _, edge := range graph.Edges {
				if edge.From.Key() == key {
					hasContinuation = true
				}
			}
		}
		if functionName == "direct" {
			pruningState := "pruned"
			if !foundNonReturning || hasContinuation {
				pruningState = "continuation-allowed"
			}
			requireMutationGuardObservation(
				t,
				"terminal-pruning/direct-known-noreturn",
				mutationGuardState("known-noreturn-continuation", "pruned"),
				mutationGuardState("known-noreturn-continuation", pruningState),
			)
		}
		if hasContinuation {
			t.Fatalf("%s non-returning call has a continuation edge", functionName)
		}
		if !foundNonReturning {
			t.Fatalf("%s did not classify its terminal call as non-returning", functionName)
		}
	}
}

func TestCanonicalCastSolverRequiresCheckedValidationSuccess(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func Checked(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	return nil
}
func Continued(raw string) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { _ = err }
	return nil
}
func Ignored(raw string) error {
	value := Value(raw)
	_ = value.Validate()
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	ssaRes := buildSSAForPass(pass)
	tests := []struct {
		functionName string
		want         interprocOutcomeClass
	}{
		{functionName: "Checked", want: interprocOutcomeSafe},
		{functionName: "Continued", want: interprocOutcomeUnsafe},
		{functionName: "Ignored", want: interprocOutcomeUnsafe},
	}
	for _, tt := range tests {
		declaration := findFuncDecl(t, file, tt.functionName)
		parentMap := buildParentMap(declaration.Body)
		assigned, _, closureCalls, methodValueCalls := collectCFACasts(
			pass, declaration.Body, parentMap, func(*ast.FuncLit, int) {},
		)
		if len(assigned) != 1 {
			t.Fatalf("%s assigned casts = %d, want 1", tt.functionName, len(assigned))
		}
		cfg := buildProtocolCFG(pass, declaration.Body, ssaRes)
		definitionBlock, definitionIndex := findDefiningBlock(cfg, assigned[0].assign)
		result := newInterprocSolverWithSSA(pass, ssaRes).EvaluateCastPath(interprocCastPathInput{
			Decl:            declaration,
			CFG:             cfg,
			DefBlock:        definitionBlock,
			DefIdx:          definitionIndex,
			Target:          assigned[0].target,
			TypeName:        assigned[0].typeName,
			SyncCalls:       collectSynchronousClosureVarCalls(closureCalls),
			MethodCalls:     collectMethodValueValidateCallSet(methodValueCalls),
			MaxStates:       defaultCFGMaxStates,
			SSAAvailability: ssaRes.Availability,
		})
		if result.Class != tt.want {
			t.Fatalf("%s canonical result = %s, want %s (edges=%+v)", tt.functionName, result.Class, tt.want, buildInterprocSupergraphForFunc(pass, declaration, ssaRes).Edges)
		}
	}
}

func TestPostValidationRecursiveMutationUsesLocalSummaryOnce(t *testing.T) {
	t.Parallel()

	const source = `package probe
type Value string
func (value Value) Validate() error { return nil }
func recursiveMutate(value *Value, depth int) {
	if depth <= 0 { *value = "changed"; return }
	recursiveMutate(value, depth-1)
}
func caller(raw string, depth int) error {
	value := Value(raw)
	if err := value.Validate(); err != nil { return err }
	recursiveMutate(&value, depth)
	return nil
}
`
	pass, file := buildTypedPassFromSource(t, source)
	declaration := findFuncDecl(t, file, "caller")
	parentMap := buildParentMap(declaration.Body)
	assigned, _, _, _ := collectCFACasts(pass, declaration.Body, parentMap, func(*ast.FuncLit, int) {})
	if len(assigned) != 1 {
		t.Fatalf("assigned casts = %d, want 1", len(assigned))
	}
	ssaResult := buildSSAForPass(pass)
	if availability := enrichAssignedCastsWithSSA(pass, ssaResult, declaration, assigned); !availability.ready() {
		t.Fatalf("SSA alias enrichment = %+v, want ready", availability)
	}
	target := assigned[0].target
	var mutationCall *ast.CallExpr
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if function := calledFunctionObject(pass, call); function != nil && function.Name() == "recursiveMutate" {
			mutationCall = call
			return false
		}
		return true
	})
	if mutationCall == nil {
		t.Fatal("recursive mutation call not found")
	}
	tag, reason := postValidationTargetEffect(pass, mutationCall, target, &sync.Map{})
	if tag != ideEdgeFuncInvalidate || reason != pathOutcomeReasonNone {
		t.Fatalf("post-validation recursive mutation = (%s, %s), want invalidate", tag, reason)
	}
	cfg := buildProtocolCFG(pass, declaration.Body, ssaResult)
	definitionBlock, definitionIndex := findDefiningBlock(cfg, assigned[0].assign)
	result := newInterprocSolverWithSSA(pass, ssaResult).EvaluateCastPath(interprocCastPathInput{
		Decl:            declaration,
		CFG:             cfg,
		DefBlock:        definitionBlock,
		DefIdx:          definitionIndex,
		Target:          target,
		TypeName:        assigned[0].typeName,
		MaxStates:       defaultCFGMaxStates,
		SSAAvailability: ssaResult.Availability,
	})
	if result.Class != interprocOutcomeUnsafe {
		t.Fatalf("recursive post-validation mutation result = %s (%s), want unsafe", result.Class, result.Reason)
	}
}
