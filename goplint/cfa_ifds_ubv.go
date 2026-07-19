// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

func ubvNonCallNodeIsObligationSink(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	if node == nil {
		return false
	}
	if ubvNonCallTargetIsUsedAtNode(pass, node, target, syncLits, syncCalls, methodCalls) {
		return true
	}
	returned, ok := node.(*ast.ReturnStmt)
	if !ok {
		return false
	}
	for _, result := range returned.Results {
		if target.matchesExpr(pass, result) ||
			isVarUseTargetWithoutCalls(pass, result, target, syncLits, syncCalls, methodCalls) {
			return true
		}
	}
	return false
}

func ubvNonCallTargetIsUsedAtNode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	if isVarUseTargetWithoutCalls(pass, node, target, syncLits, syncCalls, methodCalls) {
		return true
	}
	if target.flowAliases == nil || !nodeHasAmbiguousTargetBinding(pass, node, target) {
		return false
	}
	bindingTarget := target
	bindingTarget.flowAliases = nil
	bindingTarget.aliasKeys = nil
	return isVarUseTargetWithoutCalls(pass, node, bindingTarget, syncLits, syncCalls, methodCalls)
}

func exactCallUsesTarget(
	pass *analysis.Pass,
	call *ast.CallExpr,
	target castTarget,
	methodCalls methodValueValidateCallSet,
) bool {
	if call == nil {
		return false
	}
	if receiver := methodCalls[call].receiver; receiver != nil && target.matchesExpr(pass, receiver) {
		return false
	}
	if selector, ok := stripParens(call.Fun).(*ast.SelectorExpr); ok && target.matchesExpr(pass, selector.X) {
		switch selector.Sel.Name {
		case validateMethodName, "String", "Error", "GoString":
			return false
		default:
			return true
		}
	}
	for _, argument := range call.Args {
		if expressionContainsTargetOutsideCalls(pass, argument, target) {
			return true
		}
	}
	return false
}

func nodeHasAmbiguousTargetBinding(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if found {
			return false
		}
		expression, ok := candidate.(ast.Expr)
		if !ok || targetKeyForExpr(pass, expression) != target.key() {
			return true
		}
		found = target.aliasResolution(pass, expression) == protocolAliasAmbiguous
		return !found
	})
	return found
}

func (s interprocSolver) evaluateUBVCrossBlockIFDS(input interprocUBVCrossBlockInput) interprocPathResult {
	fact := ifdsUBVNeedsValidateBeforeUseFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
	}
	if !input.SSAAvailability.ready() {
		return unavailableSSAPathResult(input.SSAAvailability, fact.Family(), fact.Key(), input.CallChain)
	}
	if input.DefBlock == nil {
		result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
		result.FactFamily = fact.Family()
		result.FactKey = fact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}

	funcKey := "cfg.ubv." + input.OriginKey
	if funcKey == "cfg.ubv." {
		funcKey = "cfg.ubv"
	}
	graph := buildInterprocSupergraphFromReachableBlocks(input.DefBlock, funcKey)
	if s.pass != nil {
		graph = buildInterprocSupergraphFromReachableBlocksWithResolution(s.pass, input.DefBlock, funcKey)
	}
	validationProgram := buildProtocolValidationProgram(s.pass, s.ssa, input.MethodCalls)
	start := interprocNodeID{
		FuncKey:    funcKey,
		BlockIndex: input.DefBlock.Index,
		NodeIndex:  input.DefIdx,
		Kind:       interprocNodeKindCFG,
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		start.NodeIndex = 0
	}
	var definitionNode ast.Node
	if !input.OriginAtEntry && input.DefIdx >= 0 && input.DefIdx < len(input.DefBlock.Nodes) {
		definitionNode = input.DefBlock.Nodes[input.DefIdx]
	}
	result := runIFDSPropagationWithSinkControlled(
		graph,
		start,
		input.MaxStates,
		input.CallChain,
		input.DischargedWitnesses,
		newInterprocWitnessHashFunc(input.CallChain, fact.Family(), fact.Key()),
		func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason) {
			if definitionNode != nil && (nodeID.Key() == start.Key() || node == definitionNode) {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
			if input.IgnoredNodes[node] {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
			if input.TerminalUncertaintyIsBlocking && nodeEscapesTargetToPackageState(s.pass, node, input.Target) {
				return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
			}
			if nodeID.Kind == interprocNodeKindReturn && state.validationProven() {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
			if nodeID.Kind == interprocNodeKindReturn {
				validationNode := node
				if event, exists := graph.callEvent(nodeID); exists {
					validationNode = event.Call
				}
				switch validationProgram.nodeTargetSuccessfulReturnResolution(s.pass, validationNode, input.Target) {
				case protocolAliasMust:
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				case protocolAliasAmbiguous:
					return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
				case protocolAliasUnknown:
				}
			}
			if nodeID.Kind == interprocNodeKindCall {
				switch validationProgram.nodeTargetInvocationResolution(s.pass, node, input.Target) {
				case protocolAliasMust:
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				case protocolAliasAmbiguous:
					return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
				case protocolAliasUnknown:
				}
			}
			return ubvGraphNodeEdgeTag(
				graph,
				nodeID,
				s.pass,
				node,
				input.Target,
				input.SyncLits,
				input.SyncCalls,
				input.MethodCalls,
				state,
				input.SummaryStack,
				s.calleeSummaryCache,
			)
		},
		func(nodeID interprocNodeID, _ ast.Node, state protocolAbstractState) bool {
			if nodeID.FuncKey != start.FuncKey {
				return false
			}
			if !state.escapedBeforeValidation() && !state.consumedBeforeValidation() {
				return false
			}
			if graph.isNonReturningNode(nodeID) {
				return true
			}
			if graph.isTerminalCFGNode(nodeID) {
				return true
			}
			return nodeID.Kind == interprocNodeKindReturn && len(graph.outgoing(nodeID)) == 0
		},
		func(nodeID interprocNodeID, node ast.Node, _ protocolAbstractState) bool {
			if nodeID.FuncKey != start.FuncKey || nodeID.Kind != interprocNodeKindCall {
				return false
			}
			if validationProgram.nodeHasTargetInvocation(s.pass, node, input.Target) {
				return false
			}
			return graphCallReferencesTarget(graph, nodeID, s.pass, input.Target)
		},
		func(nodeID interprocNodeID, node ast.Node) bool {
			if nodeID.FuncKey != start.FuncKey {
				return false
			}
			if input.IgnoredNodes[node] {
				return false
			}
			switch nodeID.Kind {
			case interprocNodeKindCFG:
				return ubvNonCallNodeIsObligationSink(
					s.pass, node, input.Target, input.SyncLits, input.SyncCalls, input.MethodCalls,
				)
			case interprocNodeKindCall:
				if event, ok := graph.callEvent(nodeID); ok &&
					(event.Phase == protocolCallEventGo || event.Phase == protocolCallEventDeferRegistration) {
					return false
				}
				call, ok := node.(*ast.CallExpr)
				return ok && exactCallUsesTarget(s.pass, call, input.Target, input.MethodCalls)
			default:
				return false
			}
		},
		interprocSinkPolicy{
			TerminalCanObserve:         input.TerminalUncertaintyIsBlocking,
			UnresolvedIdentityAtSink:   true,
			MustAliasUncertaintyAtSink: input.TerminalUncertaintyIsBlocking,
		},
		s.control,
		validationProgram.targetEdgeTransfer(s.pass, input.Target),
		interprocTabulationOptions{
			PruneEdge: func(_ interprocEdge, state protocolAbstractState) bool {
				return input.ValidationDischargesObligation && state.validationProven()
			},
		},
	)

	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result)
	setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlock.Index})
	return result
}
