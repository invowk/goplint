// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestInterprocWitnessIdentityBindsPostTransferState(t *testing.T) {
	t.Parallel()

	base := interprocWitnessEdge{
		From:        interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCFG},
		To:          interprocNodeID{FuncKey: "entry", BlockIndex: 1, Kind: interprocNodeKindCFG},
		Kind:        interprocEdgeIntra,
		StateBefore: ideStateNeedsValidate,
		StateAfter:  ideStateNeedsValidate,
	}
	validated := base
	validated.StateAfter = ideStateValidated
	beforeIdentity := interprocWitnessIdentity([]interprocWitnessEdge{base})
	afterIdentity := interprocWitnessIdentity([]interprocWitnessEdge{validated})
	identityState := "distinct"
	if beforeIdentity == afterIdentity {
		identityState = "collapsed"
	}
	requireMutationGuardObservation(
		t,
		"witness-state/post-transfer-state",
		mutationGuardState("witness-post-transfer-identity", "distinct"),
		mutationGuardState("witness-post-transfer-identity", identityState),
	)
	if beforeIdentity == afterIdentity {
		t.Fatalf("witness identities collapse distinct post-transfer states: %q", beforeIdentity)
	}
}

func TestInterprocOutgoingEdgesHaveCanonicalOrder(t *testing.T) {
	t.Parallel()

	from := interprocNodeID{FuncKey: "entry", BlockIndex: 0, Kind: interprocNodeKindCall}
	first := interprocNodeID{FuncKey: "callee-a", BlockIndex: 0, Kind: interprocNodeKindCFG}
	second := interprocNodeID{FuncKey: "callee-z", BlockIndex: 0, Kind: interprocNodeKindCFG}
	graph := newInterprocSupergraph()
	graph.addEdge(interprocEdge{From: from, To: second, Kind: interprocEdgeCall, CallSite: "z"})
	graph.addEdge(interprocEdge{From: from, To: first, Kind: interprocEdgeCall, CallSite: "a"})

	outgoing := graph.outgoing(from)
	orderState := "other"
	if len(outgoing) == 2 {
		orderState = outgoing[0].CallSite + "," + outgoing[1].CallSite
	}
	requireMutationGuardObservation(
		t,
		"determinism/outgoing-edge-order",
		mutationGuardState("outgoing-call-site-order", "a,z"),
		mutationGuardState("outgoing-call-site-order", orderState),
	)
	if len(outgoing) != 2 || outgoing[0].CallSite != "a" || outgoing[1].CallSite != "z" {
		t.Fatalf("outgoing order = %+v, want canonical call-site order a,z", outgoing)
	}
}
