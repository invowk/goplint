// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

// isVarUse reports whether the given AST node contains a "use" of varName.
// This wrapper keeps tests and call sites that only need name matching.
func isVarUse(node ast.Node, varName string) bool {
	return isVarUseTarget(nil, node, newCastTargetFromName(varName), nil, nil, nil)
}

// isVarUseTarget reports whether the given AST node contains a "use" of target
// that is not a display-only or validation call. A use means the variable's
// value is consumed by a non-trivial operation before it is validated.
//
// What counts as a use:
//   - Passing varName as a function argument: useFunc(x)
//   - Method call on varName where the method is not Validate, String,
//     Error, or GoString: x.Setup()
//   - Composite literal field value: SomeStruct{Field: x} or map[K]V{"k": x}
//   - Channel send value: ch <- x
//
// What does NOT count as a use:
//   - x.Validate() — the validation call itself
//   - x.String(), x.Error(), x.GoString() — display-only methods
//
// Closures are NOT descended into by default. When syncLits is provided,
// only those closure literals are descended into (for example, deferred
// closures and IIFEs that execute synchronously in the current path).
func isVarUseTarget(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	seen := make(map[*ast.FuncLit]bool)
	return isVarUseTargetSeen(pass, node, target, syncLits, syncCalls, methodCalls, seen)
}

func isVarUseTargetSeen(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}

		// Composite literal field value: SomeStruct{Field: x} or
		// map[K]V{x: v}. Placing an unvalidated value into a
		// struct or map field is a meaningful consumption.
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if target.matchesExpr(pass, kv.Key) {
				found = true
				return false
			}
			if target.matchesExpr(pass, kv.Value) {
				found = true
				return false
			}
			return true
		}

		// Index expression: m[x]. Using a value as an index/key is a
		// meaningful consumption before validation.
		if idx, ok := n.(*ast.IndexExpr); ok {
			if target.matchesExpr(pass, idx.Index) {
				found = true
				return false
			}
			return true
		}

		// Channel send: ch <- x. Sending an unvalidated value on
		// a channel propagates it to another goroutine without
		// validation guarantees.
		if send, ok := n.(*ast.SendStmt); ok {
			if target.matchesExpr(pass, send.Value) {
				found = true
				return false
			}
			return true
		}
		// Alias/copy assignment: y := x, var y = x, dst = f(x), etc.
		// Any RHS consumption of target before Validate() is a use.
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, rhs := range assign.Rhs {
				if target.matchesExpr(pass, rhs) || isVarUseTargetSeen(pass, rhs, target, syncLits, syncCalls, methodCalls, seen) {
					found = true
					return false
				}
			}
			return true
		}
		if valueSpec, ok := n.(*ast.ValueSpec); ok {
			for _, value := range valueSpec.Values {
				if target.matchesExpr(pass, value) || isVarUseTargetSeen(pass, value, target, syncLits, syncCalls, methodCalls, seen) {
					found = true
					return false
				}
			}
			return true
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lit := syncCalls[call]; lit != nil && lit.Body != nil && !seen[lit] {
			seen[lit] = true
			order := firstUseValidateOrderInNodeSeen(pass, lit.Body, target, syncLits, syncCalls, methodCalls, seen)
			delete(seen, lit)
			if order == ubvOrderUseBeforeValidate {
				found = true
				return false
			}
		}

		// Check for method call on varName: x.Method(...)
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if target.matchesExpr(pass, sel.X) {
				switch sel.Sel.Name {
				case validateMethodName, "String", "Error", "GoString":
					return true // display-only or validation — not a use
				default:
					found = true
					return false
				}
			}
		}

		// Check for varName appearing as a function argument.
		for _, arg := range call.Args {
			if target.matchesExpr(pass, arg) {
				found = true
				return false
			}
		}

		return true
	})
	return found
}

type ubvOrderResult int

const (
	ubvOrderNone ubvOrderResult = iota
	ubvOrderUseBeforeValidate
	ubvOrderValidateBeforeUse
)

func firstUseValidateOrderInNode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) ubvOrderResult {
	seen := make(map[*ast.FuncLit]bool)
	return firstUseValidateOrderInNodeSeen(pass, node, target, syncLits, syncCalls, methodCalls, seen)
}

func firstUseValidateOrderInNodeSeen(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
) ubvOrderResult {
	if node == nil {
		return ubvOrderNone
	}

	result := ubvOrderNone
	parentMap := buildParentMap(node)
	ast.Inspect(node, func(n ast.Node) bool {
		if result != ubvOrderNone {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if lit := syncCalls[call]; lit != nil && lit.Body != nil && !seen[lit] {
				seen[lit] = true
				order := firstUseValidateOrderInNodeSeen(pass, lit.Body, target, syncLits, syncCalls, methodCalls, seen)
				delete(seen, lit)
				if order != ubvOrderNone {
					result = order
					return false
				}
			}
		}

		if isValidateCallNode(pass, n, target, parentMap, methodCalls) {
			result = ubvOrderValidateBeforeUse
			return false
		}
		if isUseNode(pass, n, target, syncLits, syncCalls, methodCalls) {
			result = ubvOrderUseBeforeValidate
			return false
		}
		return true
	})
	return result
}

func isValidateCallNode(
	pass *analysis.Pass,
	n ast.Node,
	target castTarget,
	parentMap map[ast.Node]ast.Node,
	methodCalls methodValueValidateCallSet,
) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	if receiver := methodCalls[call]; receiver != nil {
		if !target.matchesExpr(pass, receiver) {
			return false
		}
		if parent, ok := parentMap[call]; ok {
			if _, isDefer := parent.(*ast.DeferStmt); isDefer {
				return false
			}
		}
		return !isConditionallyEvaluated(call, parentMap)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != validateMethodName {
		return false
	}
	if !target.matchesExpr(pass, sel.X) {
		return false
	}
	// UBV ordering requires a Validate call to execute before the use in the
	// same execution path. Direct defer/go wrappers do not satisfy that
	// requirement, and conditionally evaluated calls are not guaranteed.
	if parent, ok := parentMap[call]; ok {
		if _, isDefer := parent.(*ast.DeferStmt); isDefer {
			return false
		}
	}
	return !isConditionallyEvaluated(call, parentMap)
}

func isUseNode(
	pass *analysis.Pass,
	n ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	switch node := n.(type) {
	case *ast.KeyValueExpr:
		return target.matchesExpr(pass, node.Key) || target.matchesExpr(pass, node.Value)
	case *ast.IndexExpr:
		return target.matchesExpr(pass, node.Index)
	case *ast.SendStmt:
		return target.matchesExpr(pass, node.Value)
	case *ast.AssignStmt:
		for _, rhs := range node.Rhs {
			if target.matchesExpr(pass, rhs) || isVarUseTarget(pass, rhs, target, syncLits, syncCalls, methodCalls) {
				return true
			}
		}
		return false
	case *ast.ValueSpec:
		for _, value := range node.Values {
			if target.matchesExpr(pass, value) || isVarUseTarget(pass, value, target, syncLits, syncCalls, methodCalls) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if sel, ok := node.Fun.(*ast.SelectorExpr); ok && target.matchesExpr(pass, sel.X) {
			switch sel.Sel.Name {
			case validateMethodName, "String", "Error", "GoString":
				return false
			default:
				return true
			}
		}
		validateSeen := false
		for _, arg := range node.Args {
			switch firstUseValidateOrderInNode(pass, arg, target, syncLits, syncCalls, methodCalls) {
			case ubvOrderUseBeforeValidate:
				if !validateSeen {
					return true
				}
				continue
			case ubvOrderValidateBeforeUse:
				validateSeen = true
				continue
			case ubvOrderNone:
				// Continue with additional direct checks below.
			}
			if target.matchesExpr(pass, arg) || isVarUseTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
				if !validateSeen {
					return true
				}
			}
			if containsValidateCallTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
				validateSeen = true
			}
		}
		return false
	}
	return false
}

// hasUseBeforeValidateInBlock checks whether, in the nodes of a block
// starting at startIdx, a "use" of varName appears before a Validate()
// call. Returns true if the variable is used (as an argument or non-display
// method receiver) before Validate() is encountered. Closures
// in syncLits are recognized when checking for Validate() calls.
//
// Algorithm:
//  1. Scan nodes[startIdx:] in order.
//  2. If a Validate() call on varName is found first → return false (safe).
//  3. If a "use" of varName is found first → return true (UBV detected).
//  4. If neither is found → return false (no use in this block).
func hasUseBeforeValidateInBlock(
	pass *analysis.Pass,
	nodes []ast.Node,
	startIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	return hasUseBeforeValidateInBlockMode(pass, nodes, startIdx, target, syncLits, syncCalls, methodCalls, ubvModeOrder)
}

func hasUseBeforeValidateInBlockMode(
	pass *analysis.Pass,
	nodes []ast.Node,
	startIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
) bool {
	outcome, _ := hasUseBeforeValidateInBlockOutcomeModeWithSummaryStack(
		pass,
		nodes,
		startIdx,
		target,
		syncLits,
		syncCalls,
		methodCalls,
		ubvMode,
		nil,
	)
	return outcome != pathOutcomeSafe
}

func hasUseBeforeValidateInBlockOutcomeModeWithSummaryStack(
	pass *analysis.Pass,
	nodes []ast.Node,
	startIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason) {
	for i := startIdx; i < len(nodes); i++ {
		node := nodes[i]
		if ubvMode == ubvModeEscape {
			if containsValidateCallTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
				return pathOutcomeSafe, pathOutcomeReasonNone
			}
			outcome, reason := isVarEscapeTargetOutcomeWithSummaryStack(pass, node, target, syncLits, syncCalls, methodCalls, ubvMode, summaryStack)
			if outcome != pathOutcomeSafe {
				return outcome, reason
			}
			continue
		}
		switch firstUseValidateOrderInNode(pass, node, target, syncLits, syncCalls, methodCalls) {
		case ubvOrderUseBeforeValidate:
			return pathOutcomeUnsafe, pathOutcomeReasonNone
		case ubvOrderValidateBeforeUse:
			return pathOutcomeSafe, pathOutcomeReasonNone
		case ubvOrderNone:
			// Continue scanning subsequent checks in this block.
		}
		if isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
			return pathOutcomeUnsafe, pathOutcomeReasonNone // use before Validate() — flagged
		}
	}
	return pathOutcomeSafe, pathOutcomeReasonNone
}

func hasUseBeforeValidateCrossBlockOutcomeModeWithWitness(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	defIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	maxStates int,
	maxDepth int,
) (pathOutcome, pathOutcomeReason, []int32) {
	return hasUseBeforeValidateCrossBlockModeWithSummaryStackWithWitness(
		pass,
		defBlock,
		defIdx,
		target,
		syncLits,
		syncCalls,
		methodCalls,
		ubvMode,
		maxStates,
		maxDepth,
		nil,
	)
}

func hasUseBeforeValidateCrossBlockModeWithSummaryStack(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	defIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	maxStates int,
	maxDepth int,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason) {
	outcome, reason, _ := hasUseBeforeValidateCrossBlockModeWithSummaryStackWithWitness(
		pass,
		defBlock,
		defIdx,
		target,
		syncLits,
		syncCalls,
		methodCalls,
		ubvMode,
		maxStates,
		maxDepth,
		summaryStack,
	)
	return outcome, reason
}

func hasUseBeforeValidateCrossBlockModeWithSummaryStackWithWitness(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	defIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	maxStates int,
	maxDepth int,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason, []int32) {
	if defBlock == nil {
		return pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil
	}
	// First check remainder of defBlock for use (same-block already
	// handled) — skip directly to successor blocks.
	// But we need to check if defBlock remainder has validate, which
	// would prune all successor paths.
	remainder := defBlock.Nodes[defIdx+1:]
	if nodeSliceContainsValidateCall(pass, remainder, target, syncLits, syncCalls, methodCalls) {
		return pathOutcomeSafe, pathOutcomeReasonNone, nil // validated in same block — successors are safe
	}

	if len(defBlock.Succs) == 0 {
		return pathOutcomeSafe, pathOutcomeReasonNone, nil // return block — no successors to check
	}

	mode := cfgTraversalModeUBVOrder
	if ubvMode == ubvModeEscape {
		mode = cfgTraversalModeUBVEscape
	}
	starts := make([]*gocfg.Block, 0, len(defBlock.Succs)+1)
	starts = append(starts, defBlock)
	starts = append(starts, defBlock.Succs...)
	ctx := newCFGTraversalContextFromBlocks(
		mode,
		target.key(),
		cfgValidationStateNeedsValidateBeforeUse,
		starts,
	)
	ctx.markVisitState(defBlock.Index, cfgVisitAnyPredecessor)
	seenStates := 1
	budget := adaptiveBlockVisitBudgetForBlocks(
		starts,
		blockVisitBudget{maxStates: maxStates, maxDepth: maxDepth},
	)

	return dfsUseBeforeValidateModeWithSummaryStackWithWitness(
		pass,
		defBlock.Succs,
		target,
		defBlock.Index,
		ctx,
		syncLits,
		syncCalls,
		methodCalls,
		ubvMode,
		0,
		[]int32{defBlock.Index},
		&seenStates,
		budget.maxStates,
		budget.maxDepth,
		summaryStack,
	)
}

func dfsUseBeforeValidateModeWithSummaryStackWithWitness(
	pass *analysis.Pass,
	succs []*gocfg.Block,
	target castTarget,
	predecessor int32,
	ctx *cfgTraversalContext,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	depth int,
	path []int32,
	seenStates *int,
	maxStates int,
	maxDepth int,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason, []int32) {
	if maxDepth > 0 && depth > maxDepth {
		return pathOutcomeInconclusive, pathOutcomeReasonDepthBudget, cloneCFGPath(path)
	}

	for _, succ := range succs {
		if succ == nil {
			continue
		}
		if entry, ok := ctx.memoLookup(succ.Index, predecessor); ok {
			if entry.outcome == pathOutcomeSafe {
				continue
			}
			return entry.outcome, entry.reason, mergeCFGWitness(path, entry.witness)
		}
		if ctx.shouldSkip(succ.Index, predecessor) {
			continue
		}
		ctx.markVisitState(succ.Index, predecessor)
		activeKey := ctx.pushActive(succ.Index, predecessor)
		nextPath := appendCFGPath(path, succ.Index)
		if seenStates != nil {
			*seenStates++
			if maxStates > 0 && *seenStates > maxStates {
				ctx.memoStore(succ.Index, predecessor, pathOutcomeInconclusive, pathOutcomeReasonStateBudget, nextPath)
				ctx.popActive(activeKey)
				return pathOutcomeInconclusive, pathOutcomeReasonStateBudget, nextPath
			}
		}

		if !succ.Live {
			ctx.memoStore(succ.Index, predecessor, pathOutcomeSafe, pathOutcomeReasonNone, nil)
			ctx.popActive(activeKey)
			continue
		}

		// Scan this block's nodes in order: use vs validate.
		foundUse := false
		foundValidate := false
		for _, node := range succ.Nodes {
			if ubvMode == ubvModeEscape {
				if containsValidateCallTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
					foundValidate = true
					break
				}
				escapeOutcome, escapeReason := isVarEscapeTargetOutcomeWithSummaryStack(pass, node, target, syncLits, syncCalls, methodCalls, ubvMode, summaryStack)
				if escapeOutcome == pathOutcomeInconclusive {
					ctx.memoStore(succ.Index, predecessor, pathOutcomeInconclusive, escapeReason, nextPath)
					ctx.popActive(activeKey)
					return pathOutcomeInconclusive, escapeReason, nextPath
				}
				if escapeOutcome == pathOutcomeUnsafe {
					foundUse = true
					break
				}
				continue
			}
			order := firstUseValidateOrderInNode(pass, node, target, syncLits, syncCalls, methodCalls)
			if order == ubvOrderUseBeforeValidate {
				foundUse = true
				break // use found before Validate in this node
			}
			if order == ubvOrderValidateBeforeUse {
				foundValidate = true
				break // Validate found before use in this node
			}
			if isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
				foundUse = true
				break // use found before Validate in this block
			}
		}

		if foundUse {
			ctx.memoStore(succ.Index, predecessor, pathOutcomeUnsafe, pathOutcomeReasonNone, nextPath)
			ctx.popActive(activeKey)
			return pathOutcomeUnsafe, pathOutcomeReasonNone, nextPath // cross-block UBV detected
		}
		if foundValidate {
			ctx.memoStore(succ.Index, predecessor, pathOutcomeSafe, pathOutcomeReasonNone, nil)
			ctx.popActive(activeKey)
			continue // this path is validated — skip successors
		}

		// Neither use nor validate in this block — continue DFS.
		outcome, reason, witness := dfsUseBeforeValidateModeWithSummaryStackWithWitness(
			pass,
			succ.Succs,
			target,
			succ.Index,
			ctx,
			syncLits,
			syncCalls,
			methodCalls,
			ubvMode,
			depth+1,
			nextPath,
			seenStates,
			maxStates,
			maxDepth,
			summaryStack,
		)
		if outcome != pathOutcomeSafe {
			ctx.memoStore(succ.Index, predecessor, outcome, reason, witness)
			ctx.popActive(activeKey)
			return outcome, reason, witness
		}
		ctx.memoStore(succ.Index, predecessor, pathOutcomeSafe, pathOutcomeReasonNone, nil)
		ctx.popActive(activeKey)
	}
	return pathOutcomeSafe, pathOutcomeReasonNone, nil
}

func callIsNonEscapingBuiltin(call *ast.CallExpr) bool {
	if call == nil {
		return false
	}
	ident, ok := stripParens(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	switch ident.Name {
	case "len", "cap", "copy":
		return true
	default:
		return false
	}
}

func callUsesTargetOutcomeWithSummaryStack(
	pass *analysis.Pass,
	call *ast.CallExpr,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason) {
	if call == nil {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	if receiver := methodCalls[call]; receiver != nil && target.matchesExpr(pass, receiver) {
		return pathOutcomeUnsafe, pathOutcomeReasonNone
	}
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok && target.matchesExpr(pass, sel.X) {
		switch sel.Sel.Name {
		case validateMethodName, "String", "Error", "GoString":
			return pathOutcomeSafe, pathOutcomeReasonNone
		default:
			return pathOutcomeUnsafe, pathOutcomeReasonNone
		}
	}
	if ubvMode == ubvModeEscape {
		summary, ok, summaryReason := callCalleeSummaryForTargetWithStack(pass, call, target, stackScopeFromMap(summaryStack))
		if ok && summary.AlwaysValidatesTarget && !summary.EscapesTargetBeforeValidate {
			return pathOutcomeSafe, pathOutcomeReasonNone
		}
		if !ok && summaryReason == pathOutcomeReasonRecursionCycle && summaryStackHasRecursionFallback(summaryStack) {
			goto directFallback
		}
		if !ok && (summaryReason == pathOutcomeReasonRecursionCycle ||
			summaryReason == pathOutcomeReasonStateBudget ||
			summaryReason == pathOutcomeReasonDepthBudget) {
			return pathOutcomeInconclusive, summaryReason
		}
		if ok && summary.EscapesTargetBeforeValidate {
			return pathOutcomeUnsafe, pathOutcomeReasonNone
		}
	}
directFallback:
	if callIsNonEscapingBuiltin(call) {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	for _, arg := range call.Args {
		if target.matchesExpr(pass, arg) || isVarUseTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
			return pathOutcomeUnsafe, pathOutcomeReasonNone
		}
	}
	return pathOutcomeSafe, pathOutcomeReasonNone
}

func isVarEscapeTargetOutcomeWithSummaryStack(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	ubvMode string,
	summaryStack map[string]bool,
) (pathOutcome, pathOutcomeReason) {
	escaped := false
	reason := pathOutcomeReasonNone
	ast.Inspect(node, func(n ast.Node) bool {
		if escaped {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		switch stmt := n.(type) {
		case *ast.ReturnStmt:
			for _, result := range stmt.Results {
				if target.matchesExpr(pass, result) || isVarUseTarget(pass, result, target, syncLits, syncCalls, methodCalls) {
					escaped = true
					return false
				}
			}
		case *ast.SendStmt:
			if target.matchesExpr(pass, stmt.Value) || isVarUseTarget(pass, stmt.Value, target, syncLits, syncCalls, methodCalls) {
				escaped = true
				return false
			}
		case *ast.GoStmt:
			callOutcome, callReason := callUsesTargetOutcomeWithSummaryStack(pass, stmt.Call, target, syncLits, syncCalls, methodCalls, ubvMode, summaryStack)
			if callOutcome == pathOutcomeInconclusive {
				reason = callReason
				escaped = true
				return false
			}
			if callOutcome == pathOutcomeUnsafe {
				escaped = true
				return false
			}
		case *ast.AssignStmt:
			for i, rhs := range stmt.Rhs {
				if !(target.matchesExpr(pass, rhs) || isVarUseTarget(pass, rhs, target, syncLits, syncCalls, methodCalls)) {
					continue
				}
				if i < len(stmt.Lhs) && target.matchesExpr(pass, stmt.Lhs[i]) {
					continue
				}
				escaped = true
				return false
			}
		case *ast.ValueSpec:
			for _, value := range stmt.Values {
				if target.matchesExpr(pass, value) || isVarUseTarget(pass, value, target, syncLits, syncCalls, methodCalls) {
					escaped = true
					return false
				}
			}
		case *ast.KeyValueExpr:
			if target.matchesExpr(pass, stmt.Key) || target.matchesExpr(pass, stmt.Value) {
				escaped = true
				return false
			}
		case *ast.CallExpr:
			callOutcome, callReason := callUsesTargetOutcomeWithSummaryStack(pass, stmt, target, syncLits, syncCalls, methodCalls, ubvMode, summaryStack)
			if callOutcome == pathOutcomeInconclusive {
				reason = callReason
				escaped = true
				return false
			}
			if callOutcome == pathOutcomeUnsafe {
				escaped = true
				return false
			}
		}
		return true
	})
	if !escaped {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	if reason != pathOutcomeReasonNone {
		return pathOutcomeInconclusive, reason
	}
	return pathOutcomeUnsafe, pathOutcomeReasonNone
}
