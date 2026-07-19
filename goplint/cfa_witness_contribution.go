// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"cmp"
	"slices"
	"strings"
)

const interprocWitnessAntichainLimit = 64

const (
	interprocWitnessReplayMissingNode     = "missing-node"
	interprocWitnessReplayNonEdge         = "non-edge"
	interprocWitnessReplayDisconnected    = "disconnected-transition"
	interprocWitnessReplayCrossProcedure  = "cross-procedure-transition"
	interprocWitnessReplayUnmatchedReturn = "unmatched-return"
	interprocWitnessReplayChangedFact     = "changed-fact"
	interprocWitnessReplayChangedState    = "changed-state"
	interprocWitnessReplayInvalidState    = "invalid-state"
	interprocWitnessReplayInvalidEdgeKind = "invalid-edge-kind"
	interprocWitnessReplayMissingCallSite = "missing-call-site"
)

// interprocWitnessEdge is one immutable, fully qualified contribution step.
// Block indexes remain available separately for diagnostic display, but never
// identify semantic witness transitions on their own.
type interprocWitnessEdge struct {
	From                interprocNodeID
	To                  interprocNodeID
	Kind                interprocEdgeKind
	CallSite            string
	FactFamily          ifdsFactFamily
	FactKey             string
	StateBefore         protocolAbstractState
	StateAfter          protocolAbstractState
	EdgeFunctionTag     ideEdgeFuncTag
	EdgeReason          pathOutcomeReason
	PredicateProvenance []string
}

func (edge interprocWitnessEdge) key() string {
	return strings.Join([]string{
		"from=" + edge.From.Key(),
		"to=" + edge.To.Key(),
		"kind=" + string(edge.Kind),
		"call-site=" + edge.CallSite,
		"fact-family=" + string(edge.FactFamily),
		"fact-key=" + edge.FactKey,
		"state-before=" + edge.StateBefore.key(),
		"state-after=" + edge.StateAfter.key(),
		"edge-function-tag=" + string(edge.EdgeFunctionTag),
		"edge-reason=" + string(edge.EdgeReason),
		"predicates=" + strings.Join(edge.PredicateProvenance, ","),
	}, "|")
}

func appendInterprocWitnessEdge(
	witness []interprocWitnessEdge,
	edge interprocEdge,
	stateBefore protocolAbstractState,
	stateAfter protocolAbstractState,
	edgeFunctionTag ideEdgeFuncTag,
) []interprocWitnessEdge {
	// Witness edges and their predicate provenance are immutable once
	// published. Reserve the extension slot up front and share the immutable
	// provenance slices instead of deep-copying the prefix and then growing it.
	out := make([]interprocWitnessEdge, len(witness), len(witness)+1)
	copy(out, witness)
	out = append(out, interprocWitnessEdge{
		From:                edge.From,
		To:                  edge.To,
		Kind:                edge.Kind,
		CallSite:            edge.CallSite,
		StateBefore:         stateBefore,
		StateAfter:          stateAfter,
		EdgeFunctionTag:     edgeFunctionTag,
		EdgeReason:          edge.Reason,
		PredicateProvenance: append([]string(nil), edge.PredicateProvenance...),
	})
	return out
}

func mergeInterprocWitnessEdges(parts ...[]interprocWitnessEdge) []interprocWitnessEdge {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	if total == 0 {
		return nil
	}
	out := make([]interprocWitnessEdge, 0, total)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func cloneInterprocWitnessEdges(witness []interprocWitnessEdge) []interprocWitnessEdge {
	if len(witness) == 0 {
		return nil
	}
	return slices.Clone(witness)
}

func qualifyInterprocWitnessFact(
	witness []interprocWitnessEdge,
	family ifdsFactFamily,
	key string,
) []interprocWitnessEdge {
	out := cloneInterprocWitnessEdges(witness)
	for index := range out {
		out[index].FactFamily = family
		out[index].FactKey = key
	}
	return out
}

func interprocWitnessIdentity(witness []interprocWitnessEdge) string {
	if len(witness) == 0 {
		return "root"
	}
	parts := make([]string, 0, len(witness))
	for _, edge := range witness {
		parts = append(parts, edge.key())
	}
	return strings.Join(parts, "\n")
}

func compareInterprocWitnessEdges(left, right []interprocWitnessEdge) int {
	limit := min(len(left), len(right))
	for index := range limit {
		if comparison := compareInterprocWitnessEdge(left[index], right[index]); comparison != 0 {
			return comparison
		}
	}
	return cmp.Compare(len(left), len(right))
}

func compareInterprocWitnessEdge(left, right interprocWitnessEdge) int {
	comparisons := []int{
		compareInterprocNodeID(left.From, right.From),
		compareInterprocNodeID(left.To, right.To),
		cmp.Compare(left.Kind, right.Kind),
		cmp.Compare(left.CallSite, right.CallSite),
		cmp.Compare(left.FactFamily, right.FactFamily),
		cmp.Compare(left.FactKey, right.FactKey),
		compareProtocolAbstractState(left.StateBefore, right.StateBefore),
		compareProtocolAbstractState(left.StateAfter, right.StateAfter),
		cmp.Compare(left.EdgeFunctionTag, right.EdgeFunctionTag),
		cmp.Compare(left.EdgeReason, right.EdgeReason),
		slices.Compare(left.PredicateProvenance, right.PredicateProvenance),
	}
	for _, comparison := range comparisons {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareInterprocNodeID(left, right interprocNodeID) int {
	comparisons := []int{
		cmp.Compare(left.FuncKey, right.FuncKey),
		cmp.Compare(left.Kind, right.Kind),
		cmp.Compare(left.BlockIndex, right.BlockIndex),
		cmp.Compare(left.NodeIndex, right.NodeIndex),
		cmp.Compare(left.CallOrdinal, right.CallOrdinal),
	}
	for _, comparison := range comparisons {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

func compareProtocolAbstractState(left, right protocolAbstractState) int {
	comparisons := []int{
		cmp.Compare(left.Validation, right.Validation),
		cmp.Compare(left.Hazards, right.Hazards),
		cmp.Compare(left.Uncertainty, right.Uncertainty),
		cmp.Compare(left.Result, right.Result),
		cmp.Compare(left.DeferredError, right.DeferredError),
		cmp.Compare(left.Identity, right.Identity),
		cmp.Compare(left.PossibleEffects, right.PossibleEffects),
	}
	for _, comparison := range comparisons {
		if comparison != 0 {
			return comparison
		}
	}
	return 0
}

// interprocWitnessSubsumes reports whether retained is no more constrained
// than candidate for the same abstract contribution. A shorter witness with a
// subset of exact normalized SSA predicates covers loop-unrolled and more
// constrained duplicates while preserving incomparable branch evidence.
func interprocWitnessSubsumes(retained, candidate []interprocWitnessEdge) bool {
	if len(retained) > len(candidate) {
		return false
	}
	retainedPredicates := interprocWitnessPredicates(retained)
	candidatePredicates := interprocWitnessPredicates(candidate)
	for predicate := range retainedPredicates {
		if !candidatePredicates[predicate] {
			return false
		}
	}
	return true
}

func interprocWitnessPredicates(witness []interprocWitnessEdge) map[string]bool {
	result := make(map[string]bool)
	for _, edge := range witness {
		for _, predicate := range edge.PredicateProvenance {
			result[predicate] = true
		}
	}
	return result
}

// validateInterprocWitnessReplay accepts only an exact realizable supergraph
// prefix. A witness may end inside a callee, but every observed return must
// match the most recent call edge and its call site.
func validateInterprocWitnessReplay(
	graph interprocSupergraph,
	result interprocPathResult,
) string {
	if result.WitnessTerminal.FuncKey != "" && !interprocWitnessNodeExists(graph, result.WitnessTerminal) {
		return interprocWitnessReplayMissingNode
	}
	if len(result.WitnessEdges) > 0 && result.WitnessTerminal.FuncKey != "" &&
		result.WitnessEdges[len(result.WitnessEdges)-1].To != result.WitnessTerminal {
		return interprocWitnessReplayDisconnected
	}
	type callFrame struct {
		callSite string
		caller   string
		callee   string
	}
	callStack := make([]callFrame, 0)
	for index, witnessEdge := range result.WitnessEdges {
		if !interprocWitnessNodeExists(graph, witnessEdge.From) ||
			!interprocWitnessNodeExists(graph, witnessEdge.To) {
			return interprocWitnessReplayMissingNode
		}
		if witnessEdge.FactFamily != result.FactFamily || witnessEdge.FactKey != result.FactKey {
			return interprocWitnessReplayChangedFact
		}
		if !isProtocolAbstractState(witnessEdge.StateBefore) || !isProtocolAbstractState(witnessEdge.StateAfter) {
			return interprocWitnessReplayInvalidState
		}
		if !protocolWitnessTransferMatches(
			newIDEEdgeFunc(witnessEdge.EdgeFunctionTag).Apply(witnessEdge.StateBefore),
			witnessEdge.StateAfter,
		) {
			return interprocWitnessReplayChangedState
		}
		if index > 0 {
			previous := result.WitnessEdges[index-1]
			if previous.To != witnessEdge.From {
				return interprocWitnessReplayDisconnected
			}
			if previous.StateAfter != witnessEdge.StateBefore {
				return interprocWitnessReplayChangedState
			}
		}
		if !interprocWitnessGraphHasEdge(graph, witnessEdge) {
			return interprocWitnessReplayNonEdge
		}

		switch witnessEdge.Kind {
		case interprocEdgeIntra:
			if witnessEdge.From.FuncKey != witnessEdge.To.FuncKey {
				return interprocWitnessReplayCrossProcedure
			}
			if witnessEdge.CallSite != "" {
				return interprocWitnessReplayMissingCallSite
			}
		case interprocEdgeCallToReturn:
			if witnessEdge.From.FuncKey != witnessEdge.To.FuncKey {
				return interprocWitnessReplayCrossProcedure
			}
			if witnessEdge.CallSite == "" {
				return interprocWitnessReplayMissingCallSite
			}
		case interprocEdgeCall:
			if witnessEdge.CallSite == "" {
				return interprocWitnessReplayMissingCallSite
			}
			callStack = append(callStack, callFrame{
				callSite: witnessEdge.CallSite,
				caller:   witnessEdge.From.FuncKey,
				callee:   witnessEdge.To.FuncKey,
			})
		case interprocEdgeReturn:
			if witnessEdge.CallSite == "" {
				return interprocWitnessReplayMissingCallSite
			}
			if len(callStack) == 0 {
				return interprocWitnessReplayUnmatchedReturn
			}
			frame := callStack[len(callStack)-1]
			if frame.callSite != witnessEdge.CallSite ||
				frame.callee != witnessEdge.From.FuncKey ||
				frame.caller != witnessEdge.To.FuncKey {
				return interprocWitnessReplayUnmatchedReturn
			}
			callStack = callStack[:len(callStack)-1]
		default:
			return interprocWitnessReplayInvalidEdgeKind
		}
	}
	return ""
}

func protocolWitnessTransferMatches(transferred, observed protocolAbstractState) bool {
	return transferred.Validation == observed.Validation &&
		transferred.Hazards == observed.Hazards &&
		transferred.Uncertainty&observed.Uncertainty == transferred.Uncertainty &&
		transferred.Identity <= observed.Identity &&
		transferred.PossibleEffects&observed.PossibleEffects == transferred.PossibleEffects
}

func interprocWitnessNodeExists(graph interprocSupergraph, node interprocNodeID) bool {
	stored, ok := graph.Nodes[node.Key()]
	return ok && stored == node
}

func interprocWitnessGraphHasEdge(graph interprocSupergraph, witnessEdge interprocWitnessEdge) bool {
	for _, edge := range graph.Edges {
		if edge.From == witnessEdge.From &&
			edge.To == witnessEdge.To &&
			edge.Kind == witnessEdge.Kind &&
			edge.CallSite == witnessEdge.CallSite &&
			edge.Reason == witnessEdge.EdgeReason &&
			slices.Equal(edge.PredicateProvenance, witnessEdge.PredicateProvenance) {
			return true
		}
	}
	return false
}

func isProtocolAbstractState(state protocolAbstractState) bool {
	return state.valid()
}
