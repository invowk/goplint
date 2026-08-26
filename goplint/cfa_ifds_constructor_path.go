// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
)

func (s interprocSolver) evaluateConstructorPathIFDS(input interprocConstructorPathInput) interprocPathResult {
	baseFact := ifdsCtorReturnNeedsValidateFact{
		ConstructorKey: input.Constructor,
		ReturnTypeKey:  input.ReturnTypeKey,
	}
	if !input.SSAAvailability.ready() {
		return unavailableSSAPathResult(input.SSAAvailability, baseFact.Family(), baseFact.Key(), input.CallChain)
	}
	result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
	if input.Decl == nil || input.Decl.Body == nil {
		result.FactFamily = baseFact.Family()
		result.FactKey = baseFact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}

	identityModel, identityAvailability := buildConstructorSSAIdentityModel(
		s.pass,
		s.ssa,
		input.Decl,
		input.ResultSlot,
	)
	if !identityAvailability.ready() {
		return unavailableSSAPathResult(identityAvailability, baseFact.Family(), baseFact.Key(), input.CallChain)
	}
	parentMap := buildParentMap(input.Decl.Body)
	_, _, closureCalls, methodValueCalls := collectCFACasts(
		s.pass,
		input.Decl.Body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(
		methodCalls,
		collectSynchronousClosureValidationCalls(collectSynchronousClosureVarCalls(closureCalls)),
	)
	methodCalls = mergeMethodValueValidateCallSets(
		methodCalls,
		collectCalleeValidatedCalls(
			s.pass,
			input.Decl.Body,
			s.ssa,
			stackScopeFromMap(input.SummaryStack, s.ssa),
			s.calleeSummaryCache,
		),
	)
	validationProgram := buildProtocolValidationProgram(s.pass, s.ssa, methodCalls)
	deferredPlanner := newConstructorDeferredPlanner(s.pass, s.ssa, input.Decl, s.calleeSummaryCache)
	returnTargets := identityModel.returnObjectKeys()
	if len(returnTargets) == 0 {
		outcome := pathOutcomeSafe
		reason := pathOutcomeReasonNone
		if len(identityModel.uncertainReturns) > 0 {
			outcome = pathOutcomeInconclusive
			reason = pathOutcomeReasonUnresolvedTarget
		}
		result = interprocPathResultFromOutcome(outcome, reason, nil)
		result.FactFamily = baseFact.Family()
		result.FactKey = baseFact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}
	graph := buildInterprocSupergraphForFunc(s.pass, input.Decl, s.ssa)
	start := interprocNodeID{
		FuncKey:    interprocFunctionKey(s.pass, input.Decl),
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCFG,
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		start, ok = graph.firstCFGNode()
		if !ok {
			result.FactFamily = baseFact.Family()
			result.FactKey = baseFact.Key()
			result.EdgeFunctionTag = edgeTagFromPathResult(result)
			setInterprocWitnessHash(&result, input.CallChain, nil)
			return result
		}
	}

	var safeResult *interprocPathResult
	var inconclusiveResult *interprocPathResult
	// evaluateLocalReturnTarget runs the caller-local IFDS propagation for one
	// return target. A direct checked Validate() on the exact returned object
	// discharges the obligation regardless of the value's provenance (struct
	// literal or helper-call result), so this local proof is consulted before
	// any delegation to the producing callee.
	evaluateLocalReturnTarget := func(returnTarget string, fact ifdsCtorReturnNeedsValidateFact) (interprocPathResult, bool) {
		target, targetOK := identityModel.targetForObject(returnTarget)
		if !targetOK {
			return interprocPathResult{}, false
		}
		target.typeKey = input.ReturnTypeKey
		identityResult := runIFDSPropagationWithSinkControlled(
			graph,
			start,
			input.MaxStates,
			input.CallChain,
			input.DischargedWitnesses,
			newInterprocWitnessHashFunc(input.CallChain, fact.Family(), fact.Key()),
			func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason) {
				if nodeID.FuncKey != start.FuncKey {
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				}
				if ret, ok := node.(*ast.ReturnStmt); ok && graph.isFunctionExitNode(nodeID) {
					initiallyValidated := state.validationProven() ||
						validationProgram.returnPropagatesTargetValidationError(s.pass, ret, target)
					if tag, reason := deferredPlanner.returnEffect(ret, target, initiallyValidated); tag != ideEdgeFuncIdentity || reason != pathOutcomeReasonNone {
						return tag, reason
					}
				}
				if state.validationProven() {
					if nodeID.Kind == interprocNodeKindReturn {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
					return postValidationTargetEffectWithSummaryStack(
						s.pass,
						node,
						target,
						input.SummaryStack,
						s.calleeSummaryCache,
					)
				}
				if !state.validationRequired() || state.Result == protocolErrorResultNonNil {
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				}
				if node == nil {
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				}
				if ret, ok := node.(*ast.ReturnStmt); ok &&
					validationProgram.returnPropagatesTargetValidationError(s.pass, ret, target) {
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				}
				if ret, ok := node.(*ast.ReturnStmt); ok &&
					identityModel.returnPositionHasObject(ret.Pos(), returnTarget) &&
					identityModel.returnErrorResult(ret.Pos()) == protocolErrorResultUnknown {
					return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
				}
				if nodeID.Kind == interprocNodeKindReturn {
					validationNode := node
					if event, exists := graph.callEvent(nodeID); exists {
						validationNode = event.Call
					}
					switch validationProgram.nodeTargetSuccessfulReturnResolution(s.pass, validationNode, target) {
					case protocolAliasMust:
						return ideEdgeFuncValidate, pathOutcomeReasonNone
					case protocolAliasAmbiguous:
						return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
					case protocolAliasUnknown:
					}
				}
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			},
			func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) bool {
				if nodeID.FuncKey != start.FuncKey || !graph.isTerminalCFGNode(nodeID) {
					return false
				}
				ret, ok := node.(*ast.ReturnStmt)
				if !ok || !identityModel.returnPositionHasObject(ret.Pos(), returnTarget) {
					return false
				}
				if state.Result == protocolErrorResultNonNil {
					return false
				}
				relation := identityModel.returnErrorResult(ret.Pos())
				if relation == protocolErrorResultNonNil {
					return false
				}
				if relation == protocolErrorResultUnknown && state.pathOutcomeReason() != pathOutcomeReasonNone {
					return false
				}
				return !state.validationProven()
			},
			func(nodeID interprocNodeID, node ast.Node, _ protocolAbstractState) bool {
				if nodeID.FuncKey != start.FuncKey {
					return false
				}
				if validationProgram.nodeHasTargetInvocation(s.pass, node, target) {
					return false
				}
				if event, exists := graph.callEvent(nodeID); exists && event.Phase == protocolCallEventDeferRegistration {
					return false
				}
				return nodeHasTargetRelevantUnresolvedCall(s.pass, node, target)
			},
			func(nodeID interprocNodeID, node ast.Node) bool {
				if nodeID.FuncKey != start.FuncKey || !graph.isTerminalCFGNode(nodeID) {
					return false
				}
				ret, ok := node.(*ast.ReturnStmt)
				return ok && identityModel.returnPositionHasObject(ret.Pos(), returnTarget)
			},
			interprocSinkPolicy{
				TerminalCanObserve:         true,
				MustAliasUncertaintyAtSink: true,
			},
			s.control,
			validationProgram.targetEdgeTransfer(s.pass, target),
		)
		identityResult.FactFamily = fact.Family()
		identityResult.FactKey = fact.Key()
		identityResult.EdgeFunctionTag = edgeTagFromPathResult(identityResult)
		setInterprocWitnessHash(&identityResult, input.CallChain, nil)
		return identityResult, true
	}
	// localValidationPossible reports whether the constructor body contains any
	// Must-resolved Validate() invocation on the return target, so a delegated
	// return skips the caller-local run when it cannot possibly prove safety.
	localValidationPossible := func(returnTarget string) bool {
		target, targetOK := identityModel.targetForObject(returnTarget)
		if !targetOK {
			return false
		}
		target.typeKey = input.ReturnTypeKey
		return validationProgram.firstTargetValidationPosition(s.pass, target) != token.NoPos
	}
	for _, returnTarget := range returnTargets {
		fact := baseFact
		fact.ReturnIdentity = returnTarget
		delegatedCall, delegatedSlot, delegated := identityModel.delegatedCallForObject(returnTarget)
		localResult := interprocPathResult{}
		localTargetOK := false
		if !delegated || localValidationPossible(returnTarget) {
			localResult, localTargetOK = evaluateLocalReturnTarget(returnTarget, fact)
			if localTargetOK && localResult.Class == interprocOutcomeSafe {
				safeResult = preferInterprocResult(safeResult, localResult)
				continue
			}
		}
		if delegated {
			callee := delegatedCall.Common().StaticCallee()
			var calleeObject *types.Func
			if callee != nil {
				calleeObject, _ = callee.Object().(*types.Func)
			}
			calleeDeclaration := findFuncDeclForObject(s.pass, calleeObject)
			if calleeDeclaration != nil {
				calleeKey := objectKey(calleeObject)
				if input.SummaryStack[calleeKey] {
					cycleResult := interprocPathResultFromOutcome(
						pathOutcomeInconclusive,
						pathOutcomeReasonUnresolvedTarget,
						nil,
					)
					cycleResult.FactFamily = fact.Family()
					cycleResult.FactKey = fact.Key()
					cycleResult.EdgeFunctionTag = edgeTagFromPathResult(cycleResult)
					setInterprocWitnessHash(&cycleResult, input.CallChain, nil)
					inconclusiveResult = preferInterprocResult(inconclusiveResult, cycleResult)
					continue
				}
				nextStack := cloneSummaryStack(input.SummaryStack)
				nextStack[calleeKey] = true
				delegatedResult := s.EvaluateConstructorPath(interprocConstructorPathInput{
					Decl:            calleeDeclaration,
					ReturnTypeKey:   input.ReturnTypeKey,
					ResultSlot:      delegatedSlot,
					Constructor:     calleeKey,
					MaxStates:       input.MaxStates,
					CallChain:       append(cloneCallChain(input.CallChain), calleeKey),
					SummaryStack:    nextStack,
					SSAAvailability: protocolSSAAvailabilityForDecl(s.pass, s.ssa, calleeDeclaration),
				})
				delegatedResult.FactFamily = fact.Family()
				delegatedResult.FactKey = fact.Key()
				delegatedResult.WitnessEdges = qualifyInterprocWitnessFact(
					delegatedResult.WitnessEdges,
					fact.Family(),
					fact.Key(),
				)
				delegatedResult.EdgeFunctionTag = edgeTagFromPathResult(delegatedResult)
				setInterprocWitnessHash(&delegatedResult, input.CallChain, nil)
				switch delegatedResult.Class {
				case interprocOutcomeUnsafe:
					return delegatedResult
				case interprocOutcomeInconclusive:
					inconclusiveResult = preferInterprocResult(inconclusiveResult, delegatedResult)
				default:
					safeResult = preferInterprocResult(safeResult, delegatedResult)
				}
				continue
			}
		}
		if !localTargetOK {
			identityResult := interprocPathResultFromOutcome(
				pathOutcomeInconclusive,
				pathOutcomeReasonUnresolvedTarget,
				nil,
			)
			identityResult.FactFamily = fact.Family()
			identityResult.FactKey = fact.Key()
			identityResult.EdgeFunctionTag = edgeTagFromPathResult(identityResult)
			setInterprocWitnessHash(&identityResult, input.CallChain, nil)
			inconclusiveResult = preferInterprocResult(inconclusiveResult, identityResult)
			continue
		}
		switch localResult.Class {
		case interprocOutcomeUnsafe:
			return localResult
		case interprocOutcomeInconclusive:
			inconclusiveResult = preferInterprocResult(inconclusiveResult, localResult)
		default:
			safeResult = preferInterprocResult(safeResult, localResult)
		}
	}
	if inconclusiveResult != nil {
		return *inconclusiveResult
	}
	if len(identityModel.uncertainReturns) > 0 {
		result = interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
		result.FactFamily = baseFact.Family()
		result.FactKey = baseFact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}
	if safeResult != nil {
		return *safeResult
	}
	return result
}
