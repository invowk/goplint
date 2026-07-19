// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"slices"
	"sort"
	"testing"
)

func TestProtocolDeterminismAcrossWorklistAndPackageOrder(t *testing.T) {
	t.Parallel()

	t.Run("edge insertion order preserves complete solver result", func(t *testing.T) {
		t.Parallel()

		forward := deterministicBranchGraph(false)
		reversed := deterministicBranchGraph(true)
		first := deterministicBranchResult(forward, realDeterminismOrderControl{order: protocolWorklistFIFO})
		second := deterministicBranchResult(reversed, realDeterminismOrderControl{order: protocolWorklistLIFO})
		assertDeterministicSolverEvidenceNonVacuous(t, first)
		assertDeterministicSolverEvidenceNonVacuous(t, second)
		assertCanonicalBytesEqual(t, first, second)
	})

	t.Run("package summary order preserves facts and reasons", func(t *testing.T) {
		t.Parallel()

		facts := []ProtocolSummaryFact{
			{
				FormatVersion:    protocolSummaryFactVersion,
				PackagePath:      "example.com/b",
				Complete:         true,
				FunctionName:     "ValidateB",
				FunctionIdentity: "example.com/b.ValidateB",
				Effects: []ProtocolSummaryEffectFact{
					newProtocolSummaryEffect(protocolSummaryTargetResult, 0, 1),
				},
			},
			{
				FormatVersion:    protocolSummaryFactVersion,
				PackagePath:      "example.com/a",
				Complete:         true,
				FunctionName:     "ValidateA",
				FunctionIdentity: "example.com/a.ValidateA",
				Effects: []ProtocolSummaryEffectFact{
					newProtocolSummaryEffect(protocolSummaryTargetParameter, 0, 1),
				},
			},
		}
		reordered := slices.Clone(facts)
		slices.Reverse(reordered)
		assertCanonicalBytesEqual(t, canonicalSummaryEnvelope(facts), canonicalSummaryEnvelope(reordered))
	})
}

func TestRefinementEvidenceIsByteDeterministic(t *testing.T) {
	t.Parallel()

	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{
		{
			{subject: "fn|*ssa.BinOp|t1|1", op: "eq", value: "nil"},
			{subject: "fn|*ssa.BinOp|t1|1", op: "neq", value: "nil"},
		},
	}}
	first, firstUNSAT := buildSSAConstraintEvidence(formula)
	second, secondUNSAT := buildSSAConstraintEvidence(formula)
	if !firstUNSAT || !secondUNSAT {
		t.Fatal("contradictory formula was not UNSAT")
	}
	first.WitnessPath, second.WitnessPath = []int32{0, 2}, []int32{0, 2}
	first.Subjects, second.Subjects = []string{"fn|*ssa.BinOp|t1|1"}, []string{"fn|*ssa.BinOp|t1|1"}
	assertCanonicalBytesEqual(t, first, second)
	if firstDigest, secondDigest := cfgSSAConstraintEvidenceDigest(first), cfgSSAConstraintEvidenceDigest(second); firstDigest == "" || firstDigest != secondDigest {
		t.Fatalf("evidence digest is not deterministic: first=%q second=%q", firstDigest, secondDigest)
	}
	corrupt := first
	corrupt.Contradictions = append([]cfgSSAConstraintContradiction(nil), first.Contradictions...)
	corrupt.Contradictions[0].Right.value = "different"
	if cfgSSAConstraintEvidenceDigest(corrupt) == cfgSSAConstraintEvidenceDigest(first) {
		t.Fatal("evidence digest did not bind the checked contradiction atoms")
	}
}

func deterministicBranchGraph(reverse bool) interprocSupergraph {
	start := interprocNodeID{FuncKey: "example.com/determinism.root", BlockIndex: 0, Kind: interprocNodeKindCFG}
	left := interprocNodeID{FuncKey: "example.com/determinism.root", BlockIndex: 1, Kind: interprocNodeKindCFG}
	right := interprocNodeID{FuncKey: "example.com/determinism.root", BlockIndex: 2, Kind: interprocNodeKindCFG}
	edges := []interprocEdge{
		{From: start, To: left, Kind: interprocEdgeIntra},
		{From: start, To: right, Kind: interprocEdgeIntra},
	}
	if reverse {
		slices.Reverse(edges)
	}
	graph := newInterprocSupergraph()
	for _, edge := range edges {
		graph.addEdge(edge)
	}
	return graph
}

func deterministicBranchResult(graph interprocSupergraph, control protocolAnalysisControl) interprocPathResult {
	start := interprocNodeID{FuncKey: "example.com/determinism.root", BlockIndex: 0, Kind: interprocNodeKindCFG}
	result := runIFDSPropagationControlled(
		graph,
		start,
		16,
		nil,
		nil,
		nil,
		func(interprocNodeID, ast.Node, ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
			return nodeID.BlockIndex > 0 && state != ideStateValidated
		},
		nil,
		control,
		nil,
	)
	result.FactFamily = ifdsFactFamilyCastNeedsValidate
	result.FactKey = "example.com/determinism.root:target"
	result.WitnessEdges = qualifyInterprocWitnessFact(result.WitnessEdges, result.FactFamily, result.FactKey)
	setInterprocWitnessHash(&result, []string{"example.com/determinism.root"}, result.Witness)
	return result
}

func assertDeterministicSolverEvidenceNonVacuous(t *testing.T, result interprocPathResult) {
	t.Helper()

	if result.Class != interprocOutcomeUnsafe || len(result.WitnessEdges) == 0 || result.WitnessHash == "" ||
		result.FactFamily == "" || result.FactKey == "" || result.WitnessTerminal.FuncKey == "" {
		t.Fatalf("solver determinism evidence is vacuous: %+v", result)
	}
	for _, edge := range result.WitnessEdges {
		if edge.From.FuncKey == "" || edge.To.FuncKey == "" || edge.FactFamily == "" || edge.FactKey == "" {
			t.Fatalf("solver witness edge is not fully qualified: %+v", edge)
		}
	}
}

func canonicalSummaryEnvelope(facts []ProtocolSummaryFact) []byte {
	type summaryResult struct {
		Fact   ProtocolSummaryFact
		Status protocolSummaryFactStatus
	}
	results := make([]summaryResult, 0, len(facts))
	for index := range facts {
		fact := facts[index]
		results = append(results, summaryResult{
			Fact:   fact,
			Status: validateProtocolSummaryFactShape(&fact, fact.PackagePath),
		})
	}
	sort.Slice(results, func(i, j int) bool {
		left := results[i].Fact.FunctionIdentity
		right := results[j].Fact.FunctionIdentity
		return left < right
	})
	encoded, err := json.Marshal(results)
	if err != nil {
		panic("marshal canonical summary envelope: " + err.Error())
	}
	return encoded
}

func assertCanonicalBytesEqual(t *testing.T, first, second any) {
	t.Helper()

	firstBytes, firstErr := json.Marshal(first)
	secondBytes, secondErr := json.Marshal(second)
	if firstErr != nil || secondErr != nil {
		t.Fatalf("marshal canonical envelopes: first=%v second=%v", firstErr, secondErr)
	}
	if !bytes.Equal(firstBytes, secondBytes) {
		t.Fatalf("canonical envelopes differ:\nfirst:  %s\nsecond: %s", firstBytes, secondBytes)
	}
}
