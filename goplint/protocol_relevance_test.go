// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"testing"
)

func TestProtocolRelevanceQueryLaws(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query protocolRelevanceQuery
		state ideValidationState
		want  bool
	}{
		{
			name: "forward unreachable",
			query: protocolRelevanceQuery{
				SinkReachable: true, Identity: protocolIdentityMustAlias, PossibleEffects: protocolPossibleEffectMutate,
			},
			state: ideStateValidated,
		},
		{
			name: "sink unreachable",
			query: protocolRelevanceQuery{
				ForwardReachable: true, Identity: protocolIdentityMustAlias, PossibleEffects: protocolPossibleEffectMutate,
			},
			state: ideStateValidated,
		},
		{
			name: "proven unrelated identity",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, PossibleEffects: protocolPossibleEffectMutate,
			},
			state: ideStateValidated,
		},
		{
			name: "pure or preserving effect set",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMustAlias,
			},
			state: ideStateValidated,
		},
		{
			name: "validation cannot invalidate validated state",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMustAlias,
				PossibleEffects: protocolPossibleEffectValidate,
			},
			state: ideStateValidated,
		},
		{
			name: "must-alias mutation reaches validated sink",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMustAlias,
				PossibleEffects: protocolPossibleEffectMutate,
			},
			state: ideStateValidated,
			want:  true,
		},
		{
			name: "may-alias identity blocks an otherwise definite unvalidated sink",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMayAlias,
				PossibleEffects: protocolPossibleEffectReplace,
			},
			state: ideStateNeedsValidate,
			want:  true,
		},
		{
			name: "unvalidated violation outranks must-alias replacement uncertainty",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMustAlias,
				PossibleEffects: protocolPossibleEffectReplace,
			},
			state: ideStateNeedsValidate,
		},
		{
			name: "constraint uncertainty reaches sink",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityUnknown,
				PossibleEffects: protocolPossibleEffectConstraint,
			},
			state: ideStateValidated,
			want:  true,
		},
		{
			name: "termination alone does not affect a realized returning path",
			query: protocolRelevanceQuery{
				ForwardReachable: true, SinkReachable: true, Identity: protocolIdentityMustAlias,
				PossibleEffects: protocolPossibleEffectTerminate,
			},
			state: ideStateValidated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.query.relevant(tt.state); got != tt.want {
				t.Fatalf("relevant(%v) = %t, want %t", tt.state, got, tt.want)
			}
		})
	}
}

func TestProtocolUnresolvedEffectsCarryUnknownIdentityAtSink(t *testing.T) {
	t.Parallel()

	for _, reason := range []pathOutcomeReason{
		pathOutcomeReasonUnresolvedTarget,
		pathOutcomeReasonReflection,
		pathOutcomeReasonUnsafe,
	} {
		if got := protocolIdentityAtUnresolvedSink(
			reason,
			protocolIdentityForUncertainty(reason),
		); got != protocolIdentityUnknown {
			t.Errorf("identity for %s = %d, want unknown", reason, got)
		}
	}
	if got := protocolIdentityForUncertainty(pathOutcomeReasonConcurrentMutation); got != protocolIdentityMustAlias {
		t.Errorf("identity for concurrent mutation = %d, want must-alias", got)
	}
}

func TestProtocolRelevanceRequiresAReachableObligationSink(t *testing.T) {
	t.Parallel()

	run := func(sinkReachable bool) interprocPathResult {
		graph := newInterprocSupergraph()
		start := interprocNodeID{FuncKey: "root", BlockIndex: 0, Kind: interprocNodeKindCFG}
		exit := interprocNodeID{FuncKey: "root", BlockIndex: 1, Kind: interprocNodeKindCFG}
		graph.addEdge(interprocEdge{From: start, To: exit, Kind: interprocEdgeIntra})
		graph.terminalCFGNodes[exit.Key()] = true
		return runIFDSPropagation(
			graph,
			start,
			100,
			nil,
			nil,
			nil,
			func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
				if node.Key() == start.Key() {
					return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
				}
				if node.Key() == exit.Key() {
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				}
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(node interprocNodeID, _ ast.Node, _ ideValidationState) bool {
				return sinkReachable && node.Key() == exit.Key()
			},
			nil,
		)
	}

	if got := run(false); got.Class != interprocOutcomeSafe {
		t.Fatalf("effect outside the sink slice = %s (%s), want safe", got.Class, got.Reason)
	}
	if got := run(true); got.Class != interprocOutcomeInconclusive || got.Reason != pathOutcomeReasonConcurrentMutation {
		t.Fatalf("sink-reaching effect = %s (%s), want inconclusive concurrent mutation", got.Class, got.Reason)
	}
}

func TestProtocolRelevanceIncludesSafeValidationSinks(t *testing.T) {
	t.Parallel()

	graph := newInterprocSupergraph()
	start := interprocNodeID{FuncKey: "root", BlockIndex: 0, Kind: interprocNodeKindCFG}
	exit := interprocNodeID{FuncKey: "root", BlockIndex: 1, Kind: interprocNodeKindCFG}
	graph.addEdge(interprocEdge{From: start, To: exit, Kind: interprocEdgeIntra})
	graph.terminalCFGNodes[exit.Key()] = true

	result := runIFDSPropagationWithSink(
		graph,
		start,
		100,
		nil,
		nil,
		nil,
		func(node interprocNodeID, _ ast.Node, _ ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if node.Key() == start.Key() {
				return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
			}
			return ideEdgeFuncValidate, pathOutcomeReasonNone
		},
		func(interprocNodeID, ast.Node, ideValidationState) bool {
			return false
		},
		nil,
		func(node interprocNodeID, _ ast.Node) bool {
			return node.Key() == exit.Key()
		},
		interprocSinkPolicy{},
	)

	if result.Class != interprocOutcomeInconclusive || result.Reason != pathOutcomeReasonConcurrentMutation {
		t.Fatalf("safe validation sink = %s (%s), want inconclusive concurrent mutation", result.Class, result.Reason)
	}
}
