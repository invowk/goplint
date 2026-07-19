// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"maps"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type interprocSolver struct {
	pass               *analysis.Pass
	ssa                *ssaResult
	calleeSummaryCache *sync.Map
	control            protocolAnalysisControl
}

func newInterprocSolver(pass *analysis.Pass, caches ...*sync.Map) interprocSolver {
	var cache *sync.Map
	if len(caches) > 0 {
		cache = caches[0]
	}
	return interprocSolver{
		pass:               pass,
		calleeSummaryCache: ensureCalleeSummaryCache(cache),
	}
}

func newInterprocSolverWithSSA(pass *analysis.Pass, ssaResult *ssaResult, caches ...*sync.Map) interprocSolver {
	solver := newInterprocSolver(pass, caches...)
	solver.ssa = ssaResult
	return solver
}

type interprocCastPathInput struct {
	Decl                *ast.FuncDecl
	CFG                 *gocfg.CFG
	ParentMap           map[ast.Node]ast.Node
	DefBlock            *gocfg.Block
	DefIdx              int
	Target              castTarget
	TypeName            string
	OriginKey           string
	SyncLits            map[*ast.FuncLit]bool
	SyncCalls           closureVarCallSet
	MethodCalls         methodValueValidateCallSet
	MaxStates           int
	CallChain           []string
	DischargedWitnesses map[string]bool
	AllowSafe           bool
	ResolveCFGCalls     bool
	SummaryStack        map[string]bool
	SSAAvailability     ssaAvailability
}

type interprocUBVCrossBlockInput struct {
	Target                         castTarget
	DefBlock                       *gocfg.Block
	DefIdx                         int
	OriginKey                      string
	TypeName                       string
	SyncLits                       map[*ast.FuncLit]bool
	SyncCalls                      closureVarCallSet
	MethodCalls                    methodValueValidateCallSet
	MaxStates                      int
	CallChain                      []string
	DischargedWitnesses            map[string]bool
	ResolveCFGCalls                bool
	SummaryStack                   map[string]bool
	SSAAvailability                ssaAvailability
	OriginAtEntry                  bool
	IgnoredNodes                   map[ast.Node]bool
	TerminalUncertaintyIsBlocking  bool
	ValidationDischargesObligation bool
}

type interprocConstructorPathInput struct {
	Decl                *ast.FuncDecl
	ReturnTypeKey       string
	ResultSlot          int
	Constructor         string
	MaxStates           int
	CallChain           []string
	DischargedWitnesses map[string]bool
	SummaryStack        map[string]bool
	SSAAvailability     ssaAvailability
}

func (s interprocSolver) EvaluateCastPath(input interprocCastPathInput) interprocPathResult {
	return s.evaluateCastPathIFDS(input)
}

func (s interprocSolver) EvaluateUBVCrossBlock(input interprocUBVCrossBlockInput) interprocPathResult {
	return s.evaluateUBVCrossBlockIFDS(input)
}

func (s interprocSolver) EvaluateConstructorPath(input interprocConstructorPathInput) interprocPathResult {
	return s.evaluateConstructorPathIFDS(input)
}

func (s interprocSolver) String() string {
	return "interproc-solver(canonical-ifds-ssa)"
}

func (s interprocSolver) withControl(control protocolAnalysisControl) interprocSolver {
	s.control = control
	return s
}

func cloneSummaryStack(stack map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(stack)+1)
	maps.Copy(clone, stack)
	return clone
}

func unavailableSSAPathResult(
	availability ssaAvailability,
	factFamily ifdsFactFamily,
	factKey string,
	callChain []string,
) interprocPathResult {
	result := interprocPathResultFromOutcome(pathOutcomeInconclusive, availability.pathOutcomeReason(), nil)
	result.FactFamily = factFamily
	result.FactKey = factKey
	result.EdgeFunctionTag = edgeTagFromPathResult(result)
	result.SSAAvailability = availability
	setInterprocWitnessHash(&result, callChain, nil)
	return result
}

func (s interprocSolver) evaluateCastPathIFDS(input interprocCastPathInput) interprocPathResult {
	fact := ifdsCastNeedsValidateFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
	}
	if !input.SSAAvailability.ready() {
		return unavailableSSAPathResult(input.SSAAvailability, fact.Family(), fact.Key(), input.CallChain)
	}
	result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
	if input.DefBlock != nil {
		var graph interprocSupergraph
		start := interprocNodeID{}
		ok := false

		if input.Decl != nil {
			graph = buildInterprocSupergraphForFunc(s.pass, input.Decl, s.ssa)
			start = interprocNodeID{
				FuncKey:    interprocFunctionKey(s.pass, input.Decl),
				BlockIndex: input.DefBlock.Index,
				NodeIndex:  input.DefIdx,
				Kind:       interprocNodeKindCFG,
			}
			_, ok = graph.Nodes[start.Key()]
		}
		if !ok && input.CFG != nil {
			funcKey := "cfg.cast." + input.OriginKey
			if funcKey == "cfg.cast." {
				funcKey = "cfg.cast"
			}
			if s.pass != nil {
				graph = buildInterprocSupergraphFromCFGWithResolution(s.pass, input.CFG, funcKey)
			} else {
				graph = buildInterprocSupergraphFromCFG(input.CFG, funcKey)
			}
			start = interprocNodeID{
				FuncKey:    funcKey,
				BlockIndex: input.DefBlock.Index,
				NodeIndex:  input.DefIdx,
				Kind:       interprocNodeKindCFG,
			}
			_, ok = graph.Nodes[start.Key()]
		}
		if ok {
			validationProgram := buildProtocolValidationProgram(s.pass, s.ssa, input.MethodCalls)
			parentMap := input.ParentMap
			if parentMap == nil && input.Decl != nil {
				parentMap = buildParentMap(input.Decl.Body)
			}
			var definitionNode ast.Node
			if input.DefBlock != nil && input.DefIdx >= 0 && input.DefIdx < len(input.DefBlock.Nodes) {
				definitionNode = input.DefBlock.Nodes[input.DefIdx]
			}
			result = runIFDSPropagationWithSinkControlled(
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
					if node == nil {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
					if nodeID.Kind == interprocNodeKindCall {
						if event, eventOK := graph.callEvent(nodeID); eventOK && event.Phase == protocolCallEventGo &&
							graphCallReferencesTarget(graph, nodeID, s.pass, input.Target) {
							return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
						}
					}
					if state.validationProven() {
						if nodeID.Kind == interprocNodeKindReturn {
							return ideEdgeFuncIdentity, pathOutcomeReasonNone
						}
						if nodeID.Kind == interprocNodeKindCFG {
							return postValidationNonCallTargetEffect(s.pass, node, input.Target)
						}
						return postValidationTargetEffectWithSummaryStack(
							s.pass,
							node,
							input.Target,
							input.SummaryStack,
							s.calleeSummaryCache,
						)
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
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
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
					if nodeID.Kind == interprocNodeKindCFG && state.Result == protocolErrorResultNonNil &&
						isVarUseTargetWithoutCalls(
							s.pass, node, input.Target, input.SyncLits, input.SyncCalls, input.MethodCalls,
						) {
						return ideEdgeFuncConsume, pathOutcomeReasonNone
					}
					if nodeID.Kind == interprocNodeKindCall && state.Result == protocolErrorResultNonNil {
						call, callOK := node.(*ast.CallExpr)
						if !callOK {
							return ideEdgeFuncIdentity, pathOutcomeReasonCallMapping
						}
						if exactCallUsesTarget(s.pass, call, input.Target, input.MethodCalls) {
							return ideEdgeFuncConsume, pathOutcomeReasonNone
						}
					}
					var escapeOutcome pathOutcome
					var escapeReason pathOutcomeReason
					switch nodeID.Kind {
					case interprocNodeKindCFG:
						escapeOutcome, escapeReason = isVarEscapeTargetOutcomeWithoutCalls(
							s.pass, node, input.Target, input.SyncLits, input.SyncCalls,
							input.MethodCalls, input.SummaryStack, s.calleeSummaryCache,
						)
					case interprocNodeKindCall:
						call, callOK := node.(*ast.CallExpr)
						if !callOK {
							return ideEdgeFuncIdentity, pathOutcomeReasonCallMapping
						}
						escapeOutcome, escapeReason = callUsesTargetOutcomeWithSummaryStack(
							s.pass, call, input.Target, input.SyncLits, input.SyncCalls,
							input.MethodCalls, input.SummaryStack, s.calleeSummaryCache,
						)
					default:
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
					if escapeOutcome == pathOutcomeInconclusive {
						return ideEdgeFuncIdentity, escapeReason
					}
					if escapeOutcome == pathOutcomeUnsafe {
						return ideEdgeFuncEscape, pathOutcomeReasonNone
					}
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				},
				func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) bool {
					if state.validationProven() {
						return false
					}
					if nodeID.FuncKey != start.FuncKey {
						return false
					}
					if ret, isReturn := node.(*ast.ReturnStmt); isReturn &&
						validationProgram.returnPropagatesTargetValidationError(s.pass, ret, input.Target) {
						return false
					}
					if graph.isTerminalCFGNode(nodeID) {
						return true
					}
					if nodeID.Kind == interprocNodeKindReturn && len(graph.outgoing(nodeID)) == 0 {
						return true
					}
					return false
				},
				func(nodeID interprocNodeID, node ast.Node, _ protocolAbstractState) bool {
					if nodeID.FuncKey != start.FuncKey || nodeID.Kind != interprocNodeKindCall {
						return false
					}
					if validationProgram.nodeHasTargetInvocation(s.pass, node, input.Target) {
						return false
					}
					return nodeHasTargetRelevantUnresolvedCall(s.pass, node, input.Target)
				},
				func(nodeID interprocNodeID, node ast.Node) bool {
					if nodeID.FuncKey != start.FuncKey {
						return false
					}
					_, isReturn := node.(*ast.ReturnStmt)
					if !isReturn {
						return false
					}
					validationNode := node
					if event, exists := graph.callEvent(nodeID); exists {
						validationNode = event.Call
					}
					return validationProgram.nodeHasTargetInvocation(s.pass, validationNode, input.Target)
				},
				interprocSinkPolicy{TerminalCanObserve: true},
				s.control,
				validationProgram.targetEdgeTransfer(s.pass, input.Target),
				interprocTabulationOptions{
					PruneEdge: func(edge interprocEdge, state protocolAbstractState) bool {
						return state.validationRequired() && validationProgram.targetFailureDischargesObligationOnEdge(
							s.pass,
							parentMap,
							input.Target,
							edge,
							graph.astNode(edge.To),
						)
					},
				},
			)
		}
	}

	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result)
	setInterprocWitnessHash(&result, input.CallChain, blockIndexPath(input.DefBlock))
	return result
}

func nodeHasConcurrentTargetUse(
	pass *analysis.Pass,
	declaration *ast.FuncDecl,
	node ast.Node,
	parentMap map[ast.Node]ast.Node,
	target castTarget,
) bool {
	var goStatement *ast.GoStmt
	for current := node; current != nil; current = parentMap[current] {
		if statement, ok := current.(*ast.GoStmt); ok {
			goStatement = statement
			break
		}
	}
	if goStatement == nil && declaration != nil && declaration.Body != nil && node != nil {
		ast.Inspect(declaration.Body, func(candidate ast.Node) bool {
			if goStatement != nil {
				return false
			}
			statement, ok := candidate.(*ast.GoStmt)
			if ok && node.Pos() >= statement.Pos() && node.End() <= statement.End() {
				goStatement = statement
				return false
			}
			return true
		})
	}
	if goStatement == nil || goStatement.Call == nil {
		return false
	}
	found := false
	ast.Inspect(goStatement.Call, func(candidate ast.Node) bool {
		if found {
			return false
		}
		expression, isExpression := candidate.(ast.Expr)
		if isExpression && (targetKeyForExpr(pass, expression) == target.key() ||
			target.aliasResolution(pass, expression) != protocolAliasUnknown) {
			found = true
			return false
		}
		return true
	})
	return found
}

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
	for _, returnTarget := range returnTargets {
		fact := baseFact
		fact.ReturnIdentity = returnTarget
		if delegatedCall, delegatedSlot, delegated := identityModel.delegatedCallForObject(returnTarget); delegated {
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
		target, targetOK := identityModel.targetForObject(returnTarget)
		if !targetOK {
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
		switch identityResult.Class {
		case interprocOutcomeUnsafe:
			return identityResult
		case interprocOutcomeInconclusive:
			inconclusiveResult = preferInterprocResult(inconclusiveResult, identityResult)
		default:
			safeResult = preferInterprocResult(safeResult, identityResult)
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

func runIFDSPropagationControlled(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	callChain []string,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	control protocolAnalysisControl,
	edgeTransfer interprocEdgeTransferFn,
) interprocPathResult {
	return runIFDSPropagationWithSinkControlled(
		graph,
		start,
		maxStates,
		callChain,
		dischargedWitnesses,
		witnessHash,
		transfer,
		terminalUnsafe,
		unresolvedCallRelevant,
		nil,
		interprocSinkPolicy{TerminalCanObserve: true},
		control,
		edgeTransfer,
	)
}

func runIFDSPropagationWithSinkControlled(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	_ []string,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	obligationSink interprocObligationSinkFn,
	sinkPolicy interprocSinkPolicy,
	control protocolAnalysisControl,
	edgeTransfer interprocEdgeTransferFn,
	options ...interprocTabulationOptions,
) interprocPathResult {
	var result interprocPathResult
	var stats interprocTabulationStats
	if len(options) == 0 {
		result, stats = runIFDSPropagationWithStats(
			graph,
			start,
			maxStates,
			dischargedWitnesses,
			witnessHash,
			transfer,
			terminalUnsafe,
			unresolvedCallRelevant,
			obligationSink,
			sinkPolicy,
			edgeTransfer,
			control,
		)
	} else {
		result, stats = runIFDSPropagationWithStatsOptions(
			graph,
			start,
			maxStates,
			dischargedWitnesses,
			witnessHash,
			transfer,
			terminalUnsafe,
			unresolvedCallRelevant,
			obligationSink,
			sinkPolicy,
			edgeTransfer,
			control,
			options[0],
		)
	}
	result.Tabulation = stats
	return result
}

func preferInterprocResult(current *interprocPathResult, candidate interprocPathResult) *interprocPathResult {
	if current == nil {
		return &candidate
	}
	if len(candidate.Witness) < len(current.Witness) {
		*current = candidate
		return current
	}
	if len(candidate.Witness) == len(current.Witness) &&
		compareInterprocWitnessEdges(candidate.WitnessEdges, current.WitnessEdges) < 0 {
		*current = candidate
		return current
	}
	return current
}

func newInterprocWitnessHashFunc(
	callChain []string,
	factFamily ifdsFactFamily,
	factKey string,
) interprocWitnessHashFunc {
	return func(path []int32, witness []interprocWitnessEdge, terminal interprocNodeID, trigger string) string {
		result := interprocPathResultForDischargeTrigger(factFamily, factKey, trigger)
		result.WitnessEdges = qualifyInterprocWitnessFact(witness, factFamily, factKey)
		result.WitnessTerminal = terminal
		return computeInterprocWitnessHash(result, callChain, path)
	}
}

func witnessIsDischarged(
	witnessHash interprocWitnessHashFunc,
	path []int32,
	witness []interprocWitnessEdge,
	terminal interprocNodeID,
	trigger string,
	dischargedWitnesses map[string]bool,
) bool {
	if len(dischargedWitnesses) == 0 || len(path) == 0 || witnessHash == nil {
		return false
	}
	hash := witnessHash(path, witness, terminal, trigger)
	if hash == "" {
		return false
	}
	prefix := hash + "|cfgd1_"
	for dischargeToken := range dischargedWitnesses {
		if strings.HasPrefix(dischargeToken, prefix) {
			return true
		}
	}
	return false
}

func blockIndexPath(block *gocfg.Block) []int32 {
	if block == nil {
		return nil
	}
	return []int32{block.Index}
}

func ubvNodeEdgeTag(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	state protocolAbstractState,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if node == nil {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if state.validationProven() {
		for _, call := range protocolOrderedCallsInNode(node) {
			if tag, reason := postValidationTargetEffectWithSummaryStack(
				pass,
				call,
				target,
				summaryStack,
				calleeSummaryCache,
			); reason != pathOutcomeReasonNone || tag != ideEdgeFuncIdentity {
				return tag, reason
			}
			if nodeHasUnresolvedCall(pass, call) && nodeHasTargetRelevantUnresolvedCall(pass, call, target) {
				return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
			}
		}
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if !state.validationRequired() {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	escapeOutcome, escapeReason := isVarEscapeTargetOutcomeWithSummaryStack(
		pass, node, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache,
	)
	if escapeOutcome == pathOutcomeInconclusive {
		return ideEdgeFuncIdentity, escapeReason
	}
	if escapeOutcome == pathOutcomeUnsafe {
		return ideEdgeFuncEscape, pathOutcomeReasonNone
	}
	if firstUseBeforeValidationInNode(pass, node, target, syncLits, syncCalls, methodCalls) {
		return ideEdgeFuncConsume, pathOutcomeReasonNone
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
}

func ubvGraphNodeEdgeTag(
	graph interprocSupergraph,
	nodeID interprocNodeID,
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	state protocolAbstractState,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if node == nil || nodeID.Kind == interprocNodeKindReturn {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if nodeID.Kind == interprocNodeKindCFG {
		if state.validationProven() {
			return postValidationNonCallTargetEffect(pass, node, target)
		}
		if !state.validationRequired() {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
		escapeOutcome, escapeReason := isVarEscapeTargetOutcomeWithoutCalls(
			pass, node, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache,
		)
		if escapeOutcome == pathOutcomeInconclusive {
			return ideEdgeFuncIdentity, escapeReason
		}
		if escapeOutcome == pathOutcomeUnsafe {
			return ideEdgeFuncEscape, pathOutcomeReasonNone
		}
		if isVarUseTargetWithoutCalls(pass, node, target, syncLits, syncCalls, methodCalls) {
			return ideEdgeFuncConsume, pathOutcomeReasonNone
		}
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if nodeID.Kind != interprocNodeKindCall {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return ideEdgeFuncIdentity, pathOutcomeReasonCallMapping
	}
	if event, eventOK := graph.callEvent(nodeID); eventOK && event.Phase == protocolCallEventGo &&
		graphCallReferencesTarget(graph, nodeID, pass, target) {
		return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
	}
	if state.validationProven() {
		return postValidationTargetEffectWithSummaryStack(
			pass,
			call,
			target,
			summaryStack,
			calleeSummaryCache,
		)
	}
	if !state.validationRequired() {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	escapeOutcome, escapeReason := callUsesTargetOutcomeWithSummaryStack(
		pass, call, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache,
	)
	if escapeOutcome == pathOutcomeInconclusive {
		return ideEdgeFuncIdentity, escapeReason
	}
	if escapeOutcome == pathOutcomeUnsafe {
		return ideEdgeFuncEscape, pathOutcomeReasonNone
	}
	if exactCallUsesTarget(pass, call, target, methodCalls) {
		return ideEdgeFuncConsume, pathOutcomeReasonNone
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
}

func firstUseBeforeValidationInNode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	return isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls)
}

func appendWitnessBlock(path []int32, blockIndex int32) []int32 {
	if len(path) == 0 {
		return []int32{blockIndex}
	}
	out := cloneCFGPath(path)
	if out[len(out)-1] == blockIndex {
		return out
	}
	return append(out, blockIndex)
}

func edgeTagFromPathResult(result interprocPathResult) ideEdgeFuncTag {
	switch result.Class {
	case interprocOutcomeSafe:
		return ideEdgeFuncValidate
	case interprocOutcomeUnsafe:
		if result.FactFamily == ifdsFactFamilyUBVNeedsValidateBefore {
			return ideEdgeFuncEscape
		}
		return ideEdgeFuncIdentity
	default:
		return ideEdgeFuncIdentity
	}
}
