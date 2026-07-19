// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sync"

	"golang.org/x/tools/go/analysis"
)

type interprocNodeTransferFn func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason)

type interprocEdgeTransferFn func(edge interprocEdge, state protocolAbstractState) (ideEdgeFuncTag, pathOutcomeReason)

type interprocTerminalUnsafeFn func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) bool

type interprocObligationSinkFn func(nodeID interprocNodeID, node ast.Node) bool

type interprocSinkPolicy struct {
	TerminalCanObserve         bool
	UnresolvedIdentityAtSink   bool
	MustAliasUncertaintyAtSink bool
}

type interprocUnresolvedCallFn func(nodeID interprocNodeID, node ast.Node, state protocolAbstractState) bool

type interprocWitnessHashFunc func(
	path []int32,
	witness []interprocWitnessEdge,
	terminal interprocNodeID,
	trigger string,
) string

func nodeHasTargetRelevantUnresolvedCall(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) bool {
	if pass == nil || node == nil {
		return true
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	for _, candidate := range callTargetSlotsMatchingCastTarget(pass, call, target) {
		if protocolCallTargetMayAffectIdentity(pass, candidate.expr) {
			return true
		}
	}
	for _, candidate := range allCallTargetSlots(call) {
		if expressionContainsTargetOutsideCalls(pass, candidate.expr, target) &&
			protocolCallTargetMayAffectIdentity(pass, candidate.expr) {
			return true
		}
	}
	return false
}

func graphCallReferencesTarget(
	graph interprocSupergraph,
	nodeID interprocNodeID,
	pass *analysis.Pass,
	target castTarget,
) bool {
	node := graph.astNode(nodeID)
	if nodeHasTargetRelevantUnresolvedCall(pass, node, target) {
		return true
	}
	event, ok := graph.callEvent(nodeID)
	if !ok || event.Instruction == nil {
		return false
	}
	// A go statement can invoke a closure whose target reference exists only
	// in the source-level capture or argument list. Preserve that exact event
	// boundary even when StaticCallee or its capture bindings cannot recover
	// the reference from SSA alone.
	if event.Phase == protocolCallEventGo && event.Call != nil &&
		protocolExpressionContainsTarget(pass, event.Call, target) {
		return true
	}
	procedure, ok := graph.procedureIndex.resolveCall(event.Instruction)
	if !ok || procedure.Literal == nil || procedure.Body == nil {
		return false
	}
	found := false
	root := ast.Node(procedure.Literal)
	ast.Inspect(procedure.Body, func(candidate ast.Node) bool {
		if found || candidate == nil {
			return false
		}
		if literal, isLiteral := candidate.(*ast.FuncLit); isLiteral && ast.Node(literal) != root {
			return false
		}
		expression, isExpression := candidate.(ast.Expr)
		if isExpression && (target.matchesExpr(pass, expression) ||
			target.aliasResolution(pass, expression) == protocolAliasMust) {
			found = true
			return false
		}
		return true
	})
	return found
}

func expressionContainsTargetOutsideCalls(
	pass *analysis.Pass,
	expression ast.Expr,
	target castTarget,
) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if found {
			return false
		}
		if _, isCall := node.(*ast.CallExpr); isCall {
			return false
		}
		candidate, ok := node.(ast.Expr)
		if ok && (target.matchesExpr(pass, candidate) ||
			target.aliasResolution(pass, candidate) != protocolAliasUnknown) {
			found = true
			return false
		}
		return true
	})
	return found
}

func protocolCallTargetMayAffectIdentity(pass *analysis.Pass, expression ast.Expr) bool {
	if pass == nil || pass.TypesInfo == nil || expression == nil {
		return true
	}
	typeOf := pass.TypesInfo.TypeOf(expression)
	if typeOf == nil {
		return true
	}
	switch types.Unalias(typeOf).Underlying().(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface, *types.Chan:
		return true
	default:
		return false
	}
}

func postValidationTargetEffect(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	return postValidationTargetEffectWithSummaryStack(pass, node, target, nil, calleeSummaryCache)
}

func postValidationTargetEffectWithSummaryStack(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if nodeHasAmbiguousTargetRelevantCallWithSummaryStack(
		pass,
		node,
		target,
		summaryStack,
		calleeSummaryCache,
	) {
		return ideEdgeFuncIdentity, pathOutcomeReasonAmbiguousIdentity
	}
	// A value argument is copied into its callee slot. Callee mutation or
	// escape summaries therefore cannot invalidate the caller's already
	// validated identity. Pointer-like arguments (including an explicitly
	// addressed value) continue through the summary path below.
	if call, ok := node.(*ast.CallExpr); ok {
		matched := callTargetSlotsMatchingCastTarget(pass, call, target)
		if len(matched) > 0 && !slices.ContainsFunc(matched, func(candidate calleeCallTarget) bool {
			return protocolCallTargetMayAffectIdentity(pass, candidate.expr)
		}) {
			return ideEdgeFuncIdentity, pathOutcomeReasonNone
		}
	}
	return postValidationReceiverEffectWithSummaryStack(
		pass,
		node,
		castTargetMatcher(pass, target),
		summaryStack,
		calleeSummaryCache,
	)
}

func postValidationNonCallTargetEffect(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if nodeHasTargetRelevantUnsafeConversion(pass, node, target) {
		return ideEdgeFuncIdentity, pathOutcomeReasonUnsafe
	}
	if protocolNodeCapturesTarget(pass, node, target) {
		return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
	}
	if !protocolNodeTransportsTargetIdentity(pass, node, target) {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	escapeOutcome, escapeReason := isVarEscapeTargetOutcomeWithoutCalls(
		pass,
		node,
		target,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if escapeOutcome == pathOutcomeInconclusive {
		return ideEdgeFuncIdentity, escapeReason
	}
	if escapeOutcome == pathOutcomeUnsafe &&
		(protocolTargetMayShareMutableIdentity(pass, target) || protocolNodeTakesTargetAddress(pass, node, target)) {
		return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
}

func protocolNodeTransportsTargetIdentity(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	if pass == nil || node == nil {
		return false
	}
	switch typed := node.(type) {
	case *ast.AssignStmt:
		for _, expression := range typed.Rhs {
			if protocolExpressionTransportsTargetIdentity(pass, expression, target) {
				return true
			}
		}
	case *ast.SendStmt:
		return protocolExpressionTransportsTargetIdentity(pass, typed.Value, target)
	case *ast.ReturnStmt:
		for _, expression := range typed.Results {
			if protocolExpressionTransportsTargetIdentity(pass, expression, target) {
				return true
			}
		}
	case *ast.DeclStmt:
		declaration, ok := typed.Decl.(*ast.GenDecl)
		if !ok {
			return false
		}
		for _, spec := range declaration.Specs {
			valueSpec, valueOK := spec.(*ast.ValueSpec)
			if !valueOK {
				continue
			}
			for _, expression := range valueSpec.Values {
				if protocolExpressionTransportsTargetIdentity(pass, expression, target) {
					return true
				}
			}
		}
	case *ast.ValueSpec:
		for _, expression := range typed.Values {
			if protocolExpressionTransportsTargetIdentity(pass, expression, target) {
				return true
			}
		}
	}
	return false
}

func protocolExpressionTransportsTargetIdentity(
	pass *analysis.Pass,
	expression ast.Expr,
	target castTarget,
) bool {
	if expression == nil {
		return false
	}
	expression = stripParens(expression)
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		return target.matchesExpr(pass, selector)
	}
	if target.matchesExpr(pass, expression) || target.aliasResolution(pass, expression) == protocolAliasMust {
		return true
	}
	switch typed := expression.(type) {
	case *ast.UnaryExpr:
		return typed.Op == token.AND && protocolExpressionContainsTarget(pass, typed.X, target)
	case *ast.CompositeLit:
		for _, element := range typed.Elts {
			switch value := element.(type) {
			case *ast.KeyValueExpr:
				if protocolExpressionTransportsTargetIdentity(pass, value.Value, target) {
					return true
				}
			case ast.Expr:
				if protocolExpressionTransportsTargetIdentity(pass, value, target) {
					return true
				}
			}
		}
	case *ast.CallExpr:
		if pass.TypesInfo != nil {
			if typeAndValue, ok := pass.TypesInfo.Types[typed.Fun]; ok && typeAndValue.IsType() {
				for _, argument := range typed.Args {
					if protocolExpressionTransportsTargetIdentity(pass, argument, target) {
						return true
					}
				}
			}
		}
	}
	return false
}

func protocolExpressionContainsTarget(pass *analysis.Pass, expression ast.Expr, target castTarget) bool {
	found := false
	targetKey := target.key()
	ast.Inspect(expression, func(node ast.Node) bool {
		if found || node == nil {
			return false
		}
		candidate, ok := node.(ast.Expr)
		if ok && ((targetKey != "" && targetKeyForExpr(pass, candidate) == targetKey) ||
			target.matchesExpr(pass, candidate) ||
			target.aliasResolution(pass, candidate) == protocolAliasMust) {
			found = true
			return false
		}
		return true
	})
	return found
}

func protocolNodeTakesTargetAddress(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	if pass == nil || node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if found || candidate == nil {
			return false
		}
		unary, ok := candidate.(*ast.UnaryExpr)
		if !ok || unary.Op != token.AND {
			return true
		}
		if target.aliasResolution(pass, unary.X) != protocolAliasUnknown {
			found = true
			return false
		}
		return true
	})
	return found
}

func protocolNodeCapturesTarget(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	if pass == nil || node == nil {
		return false
	}
	parents := buildParentMap(node)
	captured := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if captured || candidate == nil {
			return false
		}
		literal, ok := candidate.(*ast.FuncLit)
		if !ok {
			return true
		}
		if protocolLiteralIsImmediateSynchronousCall(literal, parents) {
			return false
		}
		ast.Inspect(literal.Body, func(inner ast.Node) bool {
			if captured || inner == nil {
				return false
			}
			expression, expressionOK := inner.(ast.Expr)
			if expressionOK && (targetKeyForExpr(pass, expression) == target.key() ||
				target.aliasResolution(pass, expression) != protocolAliasUnknown) {
				captured = true
				return false
			}
			return true
		})
		return false
	})
	return captured
}

func protocolLiteralIsImmediateSynchronousCall(literal *ast.FuncLit, parents map[ast.Node]ast.Node) bool {
	if literal == nil {
		return false
	}
	current := ast.Node(literal)
	for {
		parent := parents[current]
		if parenthesized, ok := parent.(*ast.ParenExpr); ok && parenthesized.X == current {
			current = parenthesized
			continue
		}
		call, ok := parent.(*ast.CallExpr)
		if !ok || stripParens(call.Fun) != literal {
			return false
		}
		for ancestor := parents[call]; ancestor != nil; ancestor = parents[ancestor] {
			switch ancestor.(type) {
			case *ast.GoStmt, *ast.DeferStmt:
				return false
			}
		}
		return true
	}
}

func protocolTargetMayShareMutableIdentity(pass *analysis.Pass, target castTarget) bool {
	if pass == nil || pass.TypesInfo == nil || target.originExpr == nil {
		return true
	}
	return protocolTypeMayShareMutableIdentity(pass.TypesInfo.TypeOf(target.originExpr), make(map[types.Type]bool))
}

func protocolTypeMayShareMutableIdentity(candidate types.Type, seen map[types.Type]bool) bool {
	if candidate == nil {
		return true
	}
	candidate = types.Unalias(candidate)
	if seen[candidate] {
		return false
	}
	seen[candidate] = true
	switch underlying := candidate.Underlying().(type) {
	case *types.Basic:
		return false
	case *types.Array:
		return protocolTypeMayShareMutableIdentity(underlying.Elem(), seen)
	case *types.Struct:
		for field := range underlying.Fields() {
			if protocolTypeMayShareMutableIdentity(field.Type(), seen) {
				return true
			}
		}
		return false
	case *types.Pointer, *types.Slice, *types.Map, *types.Interface, *types.Chan,
		*types.Signature, *types.TypeParam:
		return true
	default:
		return true
	}
}

func nodeHasTargetRelevantUnsafeConversion(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	if pass == nil || pass.TypesInfo == nil || node == nil {
		return false
	}
	found := false
	ast.Inspect(node, func(candidate ast.Node) bool {
		if found || candidate == nil {
			return false
		}
		call, ok := candidate.(*ast.CallExpr)
		if !ok {
			return true
		}
		typeAndValue, isTyped := pass.TypesInfo.Types[call.Fun]
		if !isTyped || !typeAndValue.IsType() {
			return false
		}
		selector, ok := stripParens(call.Fun).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Pointer" {
			return false
		}
		identifier, ok := stripParens(selector.X).(*ast.Ident)
		if !ok {
			return false
		}
		packageName, ok := pass.TypesInfo.Uses[identifier].(*types.PkgName)
		if !ok || packageName.Imported() == nil || packageName.Imported().Path() != "unsafe" {
			return false
		}
		for _, argument := range call.Args {
			if expressionContainsTargetOutsideCalls(pass, argument, target) {
				found = true
				break
			}
		}
		return false
	})
	return found
}

func nodeHasAmbiguousTargetRelevantCall(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	calleeSummaryCache *sync.Map,
) bool {
	return nodeHasAmbiguousTargetRelevantCallWithSummaryStack(
		pass,
		node,
		target,
		nil,
		calleeSummaryCache,
	)
}

func nodeHasAmbiguousTargetRelevantCallWithSummaryStack(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) bool {
	if pass == nil || node == nil {
		return false
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	found := false
	for _, slot := range allCallTargetSlots(call) {
		if target.aliasResolution(pass, slot.expr) != protocolAliasAmbiguous ||
			!protocolCallTargetMayAffectIdentity(pass, slot.expr) {
			continue
		}
		summary, summaryOK, _ := callCalleeSummaryForSlotWithStack(
			pass,
			call,
			slot.slot,
			stackScopeFromMap(summaryStack),
			calleeSummaryCache,
		)
		if summaryOK {
			state, complete := summary.protocolContinuationStateFrom(true)
			if complete && state.validationProven() &&
				state.PossibleEffects&protocolPossibleEffectEscape == 0 {
				continue
			}
		}
		found = true
		break
	}
	return found
}

func postValidationReceiverEffectWithSummaryStack(
	pass *analysis.Pass,
	node ast.Node,
	matcher validateReceiverMatcher,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (ideEdgeFuncTag, pathOutcomeReason) {
	if nodeHasConcurrentReceiverCall(pass, node, matcher) {
		return ideEdgeFuncIdentity, pathOutcomeReasonConcurrentMutation
	}
	if nodeEscapesReceiverToPackageState(pass, node, matcher) {
		return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	summary, ok, _ := callCalleeSummaryForMatcherWithStack(
		pass,
		call,
		matcher,
		stackScopeFromMap(summaryStack),
		calleeSummaryCache,
	)
	if !ok {
		return ideEdgeFuncIdentity, pathOutcomeReasonNone
	}
	effectState, complete := summary.protocolContinuationStateFrom(true)
	if !complete {
		return ideEdgeFuncIdentity, pathOutcomeReasonUnresolvedTarget
	}
	if effectState.PossibleEffects&protocolPossibleEffectEscape != 0 {
		return ideEdgeFuncIdentity, pathOutcomeReasonEscapedHeapMutation
	}
	if effectState.validationRequired() {
		return ideEdgeFuncInvalidate, pathOutcomeReasonNone
	}
	return ideEdgeFuncIdentity, pathOutcomeReasonNone
}

func nodeHasConcurrentReceiverCall(pass *analysis.Pass, node ast.Node, matcher validateReceiverMatcher) bool {
	goStatement, ok := node.(*ast.GoStmt)
	if !ok || goStatement.Call == nil {
		return false
	}
	for _, candidate := range callTargetSlotsMatchingReceiverMatcher(pass, goStatement.Call, matcher) {
		if protocolCallTargetMayAffectIdentity(pass, candidate.expr) {
			return true
		}
	}
	return false
}

func nodeEscapesTargetToPackageState(pass *analysis.Pass, node ast.Node, target castTarget) bool {
	return nodeEscapesReceiverToPackageState(pass, node, castTargetMatcher(pass, target))
}

func nodeEscapesReceiverToPackageState(
	pass *analysis.Pass,
	node ast.Node,
	matcher validateReceiverMatcher,
) bool {
	assignment, ok := node.(*ast.AssignStmt)
	if !ok || pass == nil || pass.TypesInfo == nil || matcher == nil || len(assignment.Lhs) != len(assignment.Rhs) {
		return false
	}
	for index, left := range assignment.Lhs {
		identifier, ok := stripParens(left).(*ast.Ident)
		if !ok {
			continue
		}
		variable, ok := objectForIdent(pass, identifier).(*types.Var)
		if !ok || variable.Parent() != pass.Pkg.Scope() {
			continue
		}
		if matcher(pass, assignment.Rhs[index]) {
			return true
		}
	}
	return false
}

func nodeHasUnresolvedCall(pass *analysis.Pass, node ast.Node) bool {
	if pass == nil || node == nil {
		return true
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	function := calledFunctionObject(pass, call)
	if function == nil {
		return true
	}
	if findFuncDeclForObject(pass, function) != nil {
		return false
	}
	if function.Pkg() == nil || pass.ImportObjectFact == nil {
		return true
	}
	fact := &ProtocolSummaryFact{}
	return !pass.ImportObjectFact(function, fact) || validateProtocolSummaryFact(fact, function) != 0
}
