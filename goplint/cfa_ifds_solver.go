// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type interprocSolver struct {
	pass    *analysis.Pass
	backend string
	engine  string
}

func newInterprocSolver(pass *analysis.Pass, backend, engine string) interprocSolver {
	if backend == "" {
		backend = defaultCFGBackend
	}
	return interprocSolver{
		pass:    pass,
		backend: backend,
		engine:  normalizeInterprocEngine(engine),
	}
}

func normalizeInterprocEngine(engine string) string {
	switch engine {
	case cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
		return engine
	default:
		return cfgInterprocEngineLegacy
	}
}

type interprocCastPathInput struct {
	Decl                *ast.FuncDecl
	CFG                 *gocfg.CFG
	DefBlock            *gocfg.Block
	DefIdx              int
	Target              castTarget
	TypeName            string
	OriginKey           string
	SyncLits            map[*ast.FuncLit]bool
	SyncCalls           closureVarCallSet
	MethodCalls         methodValueValidateCallSet
	NoReturnAliases     noReturnAliasSet
	MaxStates           int
	MaxDepth            int
	CallChain           []string
	DischargedWitnesses map[string]bool
	AllowSafe           bool
	ResolveCFGCalls     bool
}

type interprocUBVInBlockInput struct {
	Target        castTarget
	Nodes         []ast.Node
	StartIndex    int
	Mode          string
	OriginKey     string
	TypeName      string
	SyncLits      map[*ast.FuncLit]bool
	SyncCalls     closureVarCallSet
	MethodCalls   methodValueValidateCallSet
	DefBlockIndex int32
	CallChain     []string
	SummaryStack  map[string]bool
}

type interprocUBVCrossBlockInput struct {
	Target              castTarget
	DefBlock            *gocfg.Block
	DefIdx              int
	Mode                string
	OriginKey           string
	TypeName            string
	SyncLits            map[*ast.FuncLit]bool
	SyncCalls           closureVarCallSet
	MethodCalls         methodValueValidateCallSet
	MaxStates           int
	MaxDepth            int
	CallChain           []string
	DischargedWitnesses map[string]bool
	ResolveCFGCalls     bool
	SummaryStack        map[string]bool
}

type interprocConstructorPathInput struct {
	Decl                *ast.FuncDecl
	ReturnTypeKey       string
	ReturnTypePkgPath   string
	Constructor         string
	ReturnType          string
	MaxStates           int
	MaxDepth            int
	CallChain           []string
	DischargedWitnesses map[string]bool
	SummaryStack        map[string]bool
}

func (s interprocSolver) EvaluateCastPath(input interprocCastPathInput) interprocPathResult {
	switch s.engine {
	case cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
		return s.evaluateCastPathIFDS(input)
	default:
		return s.evaluateCastPathLegacy(input)
	}
}

func (s interprocSolver) EvaluateCastPathLegacy(input interprocCastPathInput) interprocPathResult {
	return s.evaluateCastPathLegacy(input)
}

func (s interprocSolver) EvaluateCastPathIFDS(input interprocCastPathInput) interprocPathResult {
	return s.evaluateCastPathIFDS(input)
}

func (s interprocSolver) EvaluateUBVInBlock(input interprocUBVInBlockInput) interprocPathResult {
	switch s.engine {
	case cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
		return s.evaluateUBVInBlockIFDS(input)
	default:
		return s.evaluateUBVInBlockLegacy(input)
	}
}

func (s interprocSolver) EvaluateUBVInBlockLegacy(input interprocUBVInBlockInput) interprocPathResult {
	return s.evaluateUBVInBlockLegacy(input)
}

func (s interprocSolver) EvaluateUBVInBlockIFDS(input interprocUBVInBlockInput) interprocPathResult {
	return s.evaluateUBVInBlockIFDS(input)
}

func (s interprocSolver) EvaluateUBVCrossBlock(input interprocUBVCrossBlockInput) interprocPathResult {
	switch s.engine {
	case cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
		return s.evaluateUBVCrossBlockIFDS(input)
	default:
		return s.evaluateUBVCrossBlockLegacy(input)
	}
}

func (s interprocSolver) EvaluateUBVCrossBlockLegacy(input interprocUBVCrossBlockInput) interprocPathResult {
	return s.evaluateUBVCrossBlockLegacy(input)
}

func (s interprocSolver) EvaluateUBVCrossBlockIFDS(input interprocUBVCrossBlockInput) interprocPathResult {
	return s.evaluateUBVCrossBlockIFDS(input)
}

func (s interprocSolver) EvaluateConstructorPath(input interprocConstructorPathInput) interprocPathResult {
	switch s.engine {
	case cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
		return s.evaluateConstructorPathIFDS(input)
	default:
		return s.evaluateConstructorPathLegacy(input)
	}
}

func (s interprocSolver) EvaluateConstructorPathLegacy(input interprocConstructorPathInput) interprocPathResult {
	return s.evaluateConstructorPathLegacy(input)
}

func (s interprocSolver) EvaluateConstructorPathIFDS(input interprocConstructorPathInput) interprocPathResult {
	return s.evaluateConstructorPathIFDS(input)
}

func (s interprocSolver) String() string {
	return fmt.Sprintf("interproc-solver(engine=%s, backend=%s)", s.engine, s.backend)
}

func (s interprocSolver) evaluateCastPathLegacy(input interprocCastPathInput) interprocPathResult {
	outcome, reason, witness := hasPathToReturnWithoutValidateOutcomeWithWitness(
		s.pass,
		input.CFG,
		input.DefBlock,
		input.DefIdx,
		input.Target,
		input.SyncLits,
		input.SyncCalls,
		input.MethodCalls,
		input.NoReturnAliases,
		input.MaxStates,
		input.MaxDepth,
	)
	result := interprocPathResultFromOutcome(outcome, reason, witness)
	fact := ifdsCastNeedsValidateFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
	}
	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
	setInterprocWitnessHash(&result, input.CallChain, blockIndexPath(input.DefBlock))
	return result
}

func (s interprocSolver) evaluateCastPathIFDS(input interprocCastPathInput) interprocPathResult {
	fact := ifdsCastNeedsValidateFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
	}

	result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
	if input.DefBlock != nil {
		var graph interprocSupergraph
		start := interprocNodeID{}
		ok := false

		if input.Decl != nil {
			graph = buildInterprocSupergraphForFunc(s.pass, input.Decl, s.backend)
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
			if input.ResolveCFGCalls {
				graph = buildInterprocSupergraphFromCFGWithResolution(s.pass, input.CFG, funcKey, s.backend)
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
			result = runIFDSPropagation(
				graph,
				start,
				input.MaxStates,
				input.MaxDepth,
				input.CallChain,
				input.DischargedWitnesses,
				newInterprocWitnessHashFunc(input.CallChain, fact.Family(), fact.Key(), ubvModeOrder),
				func(_ interprocNodeID, node ast.Node, state ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
					if state != ideStateNeedsValidate {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
					if node == nil {
						return ideEdgeFuncIdentity, pathOutcomeReasonNone
					}
					if containsValidateCallTarget(s.pass, node, input.Target, input.SyncLits, input.SyncCalls, input.MethodCalls) {
						return ideEdgeFuncValidate, pathOutcomeReasonNone
					}
					return ideEdgeFuncIdentity, pathOutcomeReasonNone
				},
				func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
					if state == ideStateValidated {
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
				func(interprocNodeID, ast.Node, ideValidationState) bool {
					return false
				},
			)
		}
	}

	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
	setInterprocWitnessHash(&result, input.CallChain, blockIndexPath(input.DefBlock))
	return result
}

func (s interprocSolver) evaluateUBVInBlockLegacy(input interprocUBVInBlockInput) interprocPathResult {
	outcome, reason := hasUseBeforeValidateInBlockOutcomeModeWithSummaryStack(
		s.pass,
		input.Nodes,
		input.StartIndex,
		input.Target,
		input.SyncLits,
		input.SyncCalls,
		input.MethodCalls,
		input.Mode,
		input.SummaryStack,
	)
	result := interprocPathResultFromOutcome(outcome, reason, []int32{input.DefBlockIndex})
	fact := ifdsUBVNeedsValidateBeforeUseFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
		Mode:      input.Mode,
	}
	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
	setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlockIndex})
	return result
}

func (s interprocSolver) evaluateUBVInBlockIFDS(input interprocUBVInBlockInput) interprocPathResult {
	fact := ifdsUBVNeedsValidateBeforeUseFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
		Mode:      input.Mode,
	}

	state := ideStateNeedsValidate
	start := max(input.StartIndex, 0)
	for idx := start; idx < len(input.Nodes); idx++ {
		tag, reason := ubvNodeEdgeTag(
			s.pass,
			input.Nodes[idx],
			input.Target,
			input.SyncLits,
			input.SyncCalls,
			input.MethodCalls,
			input.Mode,
			state,
			input.SummaryStack,
		)
		if reason != pathOutcomeReasonNone {
			result := interprocPathResultFromOutcome(pathOutcomeInconclusive, reason, []int32{input.DefBlockIndex})
			result.FactFamily = fact.Family()
			result.FactKey = fact.Key()
			result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
			setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlockIndex})
			return result
		}
		state = newIDEEdgeFunc(tag).Apply(state)
		if state == ideStateEscapedBeforeValidate || state == ideStateConsumedBeforeValidate {
			result := interprocPathResultFromOutcome(pathOutcomeUnsafe, pathOutcomeReasonNone, []int32{input.DefBlockIndex})
			result.FactFamily = fact.Family()
			result.FactKey = fact.Key()
			result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
			setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlockIndex})
			return result
		}
		if state == ideStateValidated {
			result := interprocPathResultFromOutcome(pathOutcomeSafe, pathOutcomeReasonNone, nil)
			result.FactFamily = fact.Family()
			result.FactKey = fact.Key()
			result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
			setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlockIndex})
			return result
		}
	}

	result := interprocPathResultFromOutcome(pathOutcomeSafe, pathOutcomeReasonNone, nil)
	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
	setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlockIndex})
	return result
}

func (s interprocSolver) evaluateUBVCrossBlockLegacy(input interprocUBVCrossBlockInput) interprocPathResult {
	outcome, reason, witness := hasUseBeforeValidateCrossBlockModeWithSummaryStackWithWitness(
		s.pass,
		input.DefBlock,
		input.DefIdx,
		input.Target,
		input.SyncLits,
		input.SyncCalls,
		input.MethodCalls,
		input.Mode,
		input.MaxStates,
		input.MaxDepth,
		input.SummaryStack,
	)
	result := interprocPathResultFromOutcome(outcome, reason, witness)
	fact := ifdsUBVNeedsValidateBeforeUseFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
		Mode:      input.Mode,
	}
	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
	setInterprocWitnessHash(&result, input.CallChain, blockIndexPath(input.DefBlock))
	return result
}

func (s interprocSolver) evaluateUBVCrossBlockIFDS(input interprocUBVCrossBlockInput) interprocPathResult {
	fact := ifdsUBVNeedsValidateBeforeUseFact{
		OriginKey: input.OriginKey,
		TargetKey: input.Target.key(),
		TypeKey:   input.TypeName,
		Mode:      input.Mode,
	}
	if input.DefBlock == nil {
		result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
		result.FactFamily = fact.Family()
		result.FactKey = fact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}

	funcKey := "cfg.ubv." + input.OriginKey
	if funcKey == "cfg.ubv." {
		funcKey = "cfg.ubv"
	}
	graph := buildInterprocSupergraphFromReachableBlocks(input.DefBlock, funcKey)
	if input.ResolveCFGCalls {
		graph = buildInterprocSupergraphFromReachableBlocksWithResolution(s.pass, input.DefBlock, funcKey, s.backend)
	}
	start := interprocNodeID{
		FuncKey:    funcKey,
		BlockIndex: input.DefBlock.Index,
		NodeIndex:  input.DefIdx,
		Kind:       interprocNodeKindCFG,
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		start.NodeIndex = 0
	}
	result := runIFDSPropagation(
		graph,
		start,
		input.MaxStates,
		input.MaxDepth,
		input.CallChain,
		input.DischargedWitnesses,
		newInterprocWitnessHashFunc(input.CallChain, fact.Family(), fact.Key(), input.Mode),
		func(_ interprocNodeID, node ast.Node, state ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			return ubvNodeEdgeTag(
				s.pass,
				node,
				input.Target,
				input.SyncLits,
				input.SyncCalls,
				input.MethodCalls,
				input.Mode,
				state,
				input.SummaryStack,
			)
		},
		func(nodeID interprocNodeID, _ ast.Node, state ideValidationState) bool {
			if state != ideStateEscapedBeforeValidate && state != ideStateConsumedBeforeValidate {
				return false
			}
			if graph.isTerminalCFGNode(nodeID) {
				return true
			}
			return nodeID.Kind == interprocNodeKindReturn && len(graph.outgoing(nodeID)) == 0
		},
		func(_ interprocNodeID, node ast.Node, state ideValidationState) bool {
			if state != ideStateNeedsValidate && state != ideStateEscapedBeforeValidate && state != ideStateConsumedBeforeValidate {
				return false
			}
			return nodeHasTargetRelevantUnresolvedCall(s.pass, node, input.Target)
		},
	)

	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, input.Mode)
	setInterprocWitnessHash(&result, input.CallChain, []int32{input.DefBlock.Index})
	return result
}

func (s interprocSolver) evaluateConstructorPathLegacy(input interprocConstructorPathInput) interprocPathResult {
	outcome, reason, witness := constructorReturnPathOutcomeWithWitness(
		s.pass,
		input.Decl,
		input.ReturnType,
		input.ReturnTypePkgPath,
		input.ReturnTypeKey,
		s.backend,
		input.MaxStates,
		input.MaxDepth,
		input.SummaryStack,
	)
	result := interprocPathResultFromOutcome(outcome, reason, witness)
	fact := ifdsCtorReturnNeedsValidateFact{
		ConstructorKey: input.Constructor,
		ReturnTypeKey:  input.ReturnTypeKey,
	}
	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
	setInterprocWitnessHash(&result, input.CallChain, nil)
	return result
}

func (s interprocSolver) evaluateConstructorPathIFDS(input interprocConstructorPathInput) interprocPathResult {
	fact := ifdsCtorReturnNeedsValidateFact{
		ConstructorKey: input.Constructor,
		ReturnTypeKey:  input.ReturnTypeKey,
	}
	result := interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
	if input.Decl == nil || input.Decl.Body == nil {
		result.FactFamily = fact.Family()
		result.FactKey = fact.Key()
		result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
		setInterprocWitnessHash(&result, input.CallChain, nil)
		return result
	}

	parentMap := buildParentMap(input.Decl.Body)
	_, _, closureCalls, methodValueCalls := collectCFACasts(
		s.pass,
		input.Decl.Body,
		parentMap,
		func(*ast.FuncLit, int) {},
	)
	syncLits := collectSynchronousClosureLits(input.Decl.Body)
	syncCalls := collectSynchronousClosureVarCalls(closureCalls)
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(
		methodCalls,
		collectCalleeValidatedCalls(s.pass, input.Decl.Body, stackScopeFromMap(input.SummaryStack)),
	)
	bareReturnIncludesTarget := constructorBareReturnIncludesType(s.pass, input.Decl, input.ReturnTypeKey)
	returnTargetKeys := collectConstructorReturnTargetKeys(s.pass, input.Decl, input.ReturnTypeKey, bareReturnIncludesTarget)
	matcher := constructorReturnTargetMatcher(input.ReturnTypeKey, returnTargetKeys)

	graph := buildInterprocSupergraphForFunc(s.pass, input.Decl, s.backend)
	start := interprocNodeID{
		FuncKey:    interprocFunctionKey(s.pass, input.Decl),
		BlockIndex: 0,
		NodeIndex:  0,
		Kind:       interprocNodeKindCFG,
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		for _, nodeID := range graph.Nodes {
			if nodeID.Kind == interprocNodeKindCFG {
				start = nodeID
				ok = true
				break
			}
		}
		if !ok {
			result.FactFamily = fact.Family()
			result.FactKey = fact.Key()
			result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
			setInterprocWitnessHash(&result, input.CallChain, nil)
			return result
		}
	}

	result = runIFDSPropagation(
		graph,
		start,
		input.MaxStates,
		input.MaxDepth,
		input.CallChain,
		input.DischargedWitnesses,
		newInterprocWitnessHashFunc(input.CallChain, fact.Family(), fact.Key(), ubvModeOrder),
		func(_ interprocNodeID, node ast.Node, state ideValidationState) (ideEdgeFuncTag, pathOutcomeReason) {
			if state != ideStateNeedsValidate {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
			if node == nil {
				return ideEdgeFuncIdentity, pathOutcomeReasonNone
			}
			if containsValidateOnReceiver(s.pass, node, matcher, syncLits, syncCalls, methodCalls) {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			}
			if validated, reason := nodeUsesCalleeSummaryForType(s.pass, node, input.ReturnTypeKey, input.SummaryStack); validated {
				return ideEdgeFuncValidate, pathOutcomeReasonNone
			} else if reason != pathOutcomeReasonNone {
				return ideEdgeFuncIdentity, reason
			}
			if stmt, ok := node.(ast.Stmt); ok {
				stmtBody := &ast.BlockStmt{List: []ast.Stmt{stmt}}
				if bodyCallsValidateTransitive(
					s.pass,
					stmtBody,
					input.ReturnType,
					input.ReturnTypePkgPath,
					input.ReturnTypeKey,
					nil,
					0,
				) {
					return ideEdgeFuncValidate, pathOutcomeReasonNone
				}
			}
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		},
		func(nodeID interprocNodeID, node ast.Node, state ideValidationState) bool {
			if !graph.isTerminalCFGNode(nodeID) {
				return false
			}
			ret, ok := node.(*ast.ReturnStmt)
			if !ok {
				return false
			}
			if !returnStmtReturnsType(s.pass, ret, input.ReturnTypeKey, bareReturnIncludesTarget) {
				return false
			}
			return state != ideStateValidated
		},
		func(_ interprocNodeID, node ast.Node, state ideValidationState) bool {
			if state != ideStateNeedsValidate {
				return false
			}
			return nodeHasTypeRelevantUnresolvedCall(s.pass, node, input.ReturnTypeKey)
		},
	)

	result.FactFamily = fact.Family()
	result.FactKey = fact.Key()
	result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvModeOrder)
	setInterprocWitnessHash(&result, input.CallChain, nil)
	return result
}

type interprocNodeTransferFn func(nodeID interprocNodeID, node ast.Node, state ideValidationState) (ideEdgeFuncTag, pathOutcomeReason)

type interprocTerminalUnsafeFn func(nodeID interprocNodeID, node ast.Node, state ideValidationState) bool

type interprocUnresolvedCallFn func(nodeID interprocNodeID, node ast.Node, state ideValidationState) bool

type interprocWitnessHashFunc func(path []int32, trigger string) string

type interprocNodeSnapshot struct {
	state ideValidationState
	depth int
	path  []int32
}

func callTargetSlotsMatchingType(
	pass *analysis.Pass,
	call *ast.CallExpr,
	returnTypeKey string,
) []calleeCallTarget {
	if pass == nil || call == nil || returnTypeKey == "" {
		return nil
	}
	candidates := allCallTargetSlots(call)
	out := make([]calleeCallTarget, 0, len(candidates))
	for _, candidate := range candidates {
		exprType := pass.TypesInfo.TypeOf(candidate.expr)
		if exprType == nil {
			continue
		}
		if typeIdentityKey(exprType) == returnTypeKey {
			out = append(out, candidate)
		}
	}
	return out
}

func nodeHasTargetRelevantUnresolvedCall(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) bool {
	if pass == nil || node == nil {
		return true
	}
	relevant := false
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if len(callTargetSlotsMatchingCastTarget(pass, call, target)) > 0 {
			relevant = true
			return false
		}
		return true
	})
	return relevant
}

func nodeHasTypeRelevantUnresolvedCall(
	pass *analysis.Pass,
	node ast.Node,
	returnTypeKey string,
) bool {
	if pass == nil || node == nil {
		return true
	}
	relevant := false
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if len(callTargetSlotsMatchingType(pass, call, returnTypeKey)) > 0 {
			relevant = true
			return false
		}
		return true
	})
	return relevant
}

func runIFDSPropagation(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	maxDepth int,
	_ []string,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
) interprocPathResult {
	if maxStates <= 0 {
		maxStates = defaultCFGMaxStates
	}
	if maxDepth <= 0 {
		maxDepth = defaultCFGMaxDepth
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil)
	}

	snapshots := map[string]interprocNodeSnapshot{
		start.Key(): {
			state: ideStateNeedsValidate,
			depth: 0,
			path:  []int32{start.BlockIndex},
		},
	}
	queue := []interprocNodeID{start}
	explored := 0

	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]
		snap, ok := snapshots[nodeID.Key()]
		if !ok {
			continue
		}
		if snap.depth > maxDepth {
			if witnessIsDischarged(witnessHash, snap.path, string(pathOutcomeReasonDepthBudget), dischargedWitnesses) {
				continue
			}
			return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonDepthBudget, snap.path)
		}

		node := graph.astNode(nodeID)
		nodeTag, reason := transfer(nodeID, node, snap.state)
		if reason != pathOutcomeReasonNone {
			if witnessIsDischarged(witnessHash, snap.path, string(reason), dischargedWitnesses) {
				continue
			}
			return interprocPathResultFromOutcome(pathOutcomeInconclusive, reason, snap.path)
		}
		nodeState := newIDEEdgeFunc(nodeTag).Apply(snap.state)
		if terminalUnsafe(nodeID, node, nodeState) {
			if witnessIsDischarged(witnessHash, snap.path, cfgRefinementTriggerUnsafeCandidate, dischargedWitnesses) {
				continue
			}
			return interprocPathResultFromOutcome(pathOutcomeUnsafe, pathOutcomeReasonNone, snap.path)
		}

		edges := graph.outgoing(nodeID)
		for _, edge := range edges {
			nextPath := appendWitnessBlock(snap.path, edge.To.BlockIndex)
			if edge.Kind == interprocEdgeCallToReturn && edge.Reason == pathOutcomeReasonUnresolvedTarget {
				if nodeState == ideStateNeedsValidate || nodeState == ideStateEscapedBeforeValidate || nodeState == ideStateConsumedBeforeValidate {
					originNode := node
					if originNode == nil {
						originNode = graph.astNode(interprocNodeID{
							FuncKey:    edge.From.FuncKey,
							BlockIndex: edge.From.BlockIndex,
							NodeIndex:  edge.From.NodeIndex,
							Kind:       interprocNodeKindCFG,
						})
					}
					if unresolvedCallRelevant != nil && !unresolvedCallRelevant(nodeID, originNode, nodeState) {
						continue
					}
					if witnessIsDischarged(witnessHash, nextPath, string(pathOutcomeReasonUnresolvedTarget), dischargedWitnesses) {
						continue
					}
					return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nextPath)
				}
			}

			nextState := composeIDEEdgeFuncs(newIDEEdgeFunc(nodeTag), newIDEEdgeFunc(ideEdgeFuncIdentity)).Apply(snap.state)
			nextDepth := snap.depth + 1
			if nextDepth > maxDepth {
				if witnessIsDischarged(witnessHash, nextPath, string(pathOutcomeReasonDepthBudget), dischargedWitnesses) {
					continue
				}
				return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonDepthBudget, nextPath)
			}
			explored++
			if explored > maxStates {
				if witnessIsDischarged(witnessHash, nextPath, string(pathOutcomeReasonStateBudget), dischargedWitnesses) {
					continue
				}
				return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonStateBudget, nextPath)
			}

			key := edge.To.Key()
			prev, exists := snapshots[key]
			if !exists {
				snapshots[key] = interprocNodeSnapshot{state: nextState, depth: nextDepth, path: nextPath}
				queue = append(queue, edge.To)
				continue
			}
			joined := joinIDEStates(prev.state, nextState)
			if joined == prev.state && nextDepth >= prev.depth {
				continue
			}
			updated := prev
			updated.state = joined
			if nextDepth < updated.depth || len(updated.path) == 0 {
				updated.depth = nextDepth
				updated.path = nextPath
			}
			snapshots[key] = updated
			queue = append(queue, edge.To)
		}
	}

	return interprocPathResultFromOutcome(pathOutcomeSafe, pathOutcomeReasonNone, nil)
}

func newInterprocWitnessHashFunc(
	callChain []string,
	factFamily ifdsFactFamily,
	factKey string,
	ubvMode string,
) interprocWitnessHashFunc {
	return func(path []int32, trigger string) string {
		result := interprocPathResultForDischargeTrigger(factFamily, factKey, ubvMode, trigger)
		return computeInterprocWitnessHash(result, callChain, path)
	}
}

func witnessIsDischarged(
	witnessHash interprocWitnessHashFunc,
	path []int32,
	trigger string,
	dischargedWitnesses map[string]bool,
) bool {
	if len(dischargedWitnesses) == 0 || len(path) == 0 || witnessHash == nil {
		return false
	}
	hash := witnessHash(path, trigger)
	return hash != "" && dischargedWitnesses[hash]
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
	mode string,
	state ideValidationState,
	summaryStack map[string]bool,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if state != ideStateNeedsValidate {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if node == nil {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	if containsValidateCallTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
		return ideEdgeFuncValidate, pathOutcomeReasonNone
	}
	if mode == ubvModeEscape {
		outcome, reason := isVarEscapeTargetOutcomeWithSummaryStack(pass, node, target, syncLits, syncCalls, methodCalls, mode, summaryStack)
		switch outcome {
		case pathOutcomeInconclusive:
			return ideEdgeFuncIdentity, reason
		case pathOutcomeUnsafe:
			return ideEdgeFuncEscape, pathOutcomeReasonNone
		default:
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
	}
	switch firstUseValidateOrderInNode(pass, node, target, syncLits, syncCalls, methodCalls) {
	case ubvOrderUseBeforeValidate:
		return ideEdgeFuncConsume, pathOutcomeReasonNone
	case ubvOrderValidateBeforeUse:
		return ideEdgeFuncValidate, pathOutcomeReasonNone
	case ubvOrderNone:
		// Fall through to the broader use-target check below.
	}
	if isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
		return ideEdgeFuncConsume, pathOutcomeReasonNone
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
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

func edgeTagFromPathResult(result interprocPathResult, ubvMode string) ideEdgeFuncTag {
	switch result.Class {
	case interprocOutcomeSafe:
		return ideEdgeFuncValidate
	case interprocOutcomeUnsafe:
		if ubvMode == ubvModeEscape {
			return ideEdgeFuncEscape
		}
		if result.FactFamily == ifdsFactFamilyUBVNeedsValidateBefore {
			return ideEdgeFuncConsume
		}
		return ideEdgeFuncIdentity
	default:
		return ideEdgeFuncIdentity
	}
}
