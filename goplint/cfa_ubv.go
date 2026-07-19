// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"sync"

	"golang.org/x/tools/go/analysis"
)

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
	return isVarUseTargetSeenMode(pass, node, target, syncLits, syncCalls, methodCalls, seen, true)
}

func isVarUseTargetWithoutCalls(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	seen := make(map[*ast.FuncLit]bool)
	return isVarUseTargetSeenMode(pass, node, target, syncLits, syncCalls, methodCalls, seen, false)
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
	return isVarUseTargetSeenMode(pass, node, target, syncLits, syncCalls, methodCalls, seen, true)
}

func isVarUseTargetSeenMode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
	includeCalls bool,
) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		if _, ok := n.(*ast.CallExpr); ok && !includeCalls {
			return false
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
		// Reading a field or selecting a non-call member consumes the tracked
		// value. Obtaining a Validate or display-only method value does not
		// consume the receiver; the eventual invocation is modeled separately.
		if selector, ok := n.(*ast.SelectorExpr); ok && target.matchesExpr(pass, selector.X) {
			if selectorIsNonConsumingProtocolMethod(pass, selector) {
				return false
			}
			found = true
			return false
		}
		// Alias/copy assignment: y := x, var y = x, dst = f(x), etc.
		// Any RHS consumption of target before Validate() is a use.
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, rhs := range assign.Rhs {
				if target.matchesExpr(pass, rhs) || isVarUseTargetSeenMode(
					pass, rhs, target, syncLits, syncCalls, methodCalls, seen, includeCalls,
				) {
					found = true
					return false
				}
			}
			return true
		}
		if valueSpec, ok := n.(*ast.ValueSpec); ok {
			for _, value := range valueSpec.Values {
				if target.matchesExpr(pass, value) || isVarUseTargetSeenMode(
					pass, value, target, syncLits, syncCalls, methodCalls, seen, includeCalls,
				) {
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
			used := isVarUseTargetSeenMode(
				pass, lit.Body, target, syncLits, syncCalls, methodCalls, seen, includeCalls,
			)
			delete(seen, lit)
			if used {
				found = true
				return false
			}
		}

		// Check for method call on varName: x.Method(...)
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if target.matchesExpr(pass, sel.X) {
				switch sel.Sel.Name {
				case validateMethodName, "String", "Error", "GoString":
					return false // display-only or validation — not a use
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

func selectorIsNonConsumingProtocolMethod(pass *analysis.Pass, selector *ast.SelectorExpr) bool {
	if pass == nil || pass.TypesInfo == nil || selector == nil {
		return false
	}
	selection := pass.TypesInfo.Selections[selector]
	if selection == nil {
		return false
	}
	if _, ok := selection.Obj().(*types.Func); !ok {
		return false
	}
	switch selector.Sel.Name {
	case validateMethodName, "String", "Error", "GoString":
		return true
	default:
		return false
	}
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
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (pathOutcome, pathOutcomeReason) {
	if call == nil {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	if receiver := methodCalls[call].receiver; receiver != nil && target.matchesExpr(pass, receiver) {
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
	if callIsNonEscapingBuiltin(call) {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	targetRelevant := false
	for _, arg := range call.Args {
		if target.matchesExpr(pass, arg) || isVarUseTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
			targetRelevant = true
			break
		}
	}
	if !targetRelevant {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	if callHasLocalResolvedBody(pass, call) {
		// The interprocedural tabulation visits the local callee with matched
		// call/return edges, so the caller node itself has no summary effect.
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	summary, ok, summaryReason := callCalleeSummaryForTargetWithStack(
		pass, call, target, stackScopeFromMap(summaryStack), calleeSummaryCache,
	)
	if !ok {
		if !callUsesInterfaceDispatch(pass, call) {
			return pathOutcomeUnsafe, pathOutcomeReasonNone
		}
		if summaryReason == pathOutcomeReasonNone {
			summaryReason = pathOutcomeReasonUnresolvedTarget
		}
		return pathOutcomeInconclusive, summaryReason
	}
	effectState, complete := summary.protocolContinuationState()
	if !complete {
		return pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget
	}
	if summary.hasEffect(protocolSummaryEffectPure) || summary.hasEffect(protocolSummaryEffectPreserve) ||
		effectState.PossibleEffects&protocolPossibleEffectTerminate != 0 ||
		effectState.validationProven() && effectState.Hazards == 0 {
		return pathOutcomeSafe, pathOutcomeReasonNone
	}
	if effectState.Hazards != 0 || effectState.validationRequired() {
		return pathOutcomeUnsafe, pathOutcomeReasonNone
	}
	return pathOutcomeUnsafe, pathOutcomeReasonNone
}

func callUsesInterfaceDispatch(pass *analysis.Pass, call *ast.CallExpr) bool {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return false
	}
	selector, ok := stripParens(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiverType := pass.TypesInfo.TypeOf(selector.X)
	if receiverType == nil {
		return false
	}
	_, ok = types.Unalias(receiverType).Underlying().(*types.Interface)
	return ok
}

func isVarEscapeTargetOutcomeWithSummaryStack(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (pathOutcome, pathOutcomeReason) {
	return isVarEscapeTargetOutcomeWithSummaryStackMode(
		pass, node, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache, true,
	)
}

func isVarEscapeTargetOutcomeWithoutCalls(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
) (pathOutcome, pathOutcomeReason) {
	return isVarEscapeTargetOutcomeWithSummaryStackMode(
		pass, node, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache, false,
	)
}

func isVarEscapeTargetOutcomeWithSummaryStackMode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	summaryStack map[string]bool,
	calleeSummaryCache *sync.Map,
	includeCalls bool,
) (pathOutcome, pathOutcomeReason) {
	escaped := false
	reason := pathOutcomeReasonNone
	usesTarget := func(candidate ast.Node) bool {
		if includeCalls {
			return isVarUseTarget(pass, candidate, target, syncLits, syncCalls, methodCalls)
		}
		return isVarUseTargetWithoutCalls(pass, candidate, target, syncLits, syncCalls, methodCalls)
	}
	ast.Inspect(node, func(n ast.Node) bool {
		if escaped {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		if _, ok := n.(*ast.CallExpr); ok && !includeCalls {
			return false
		}
		switch stmt := n.(type) {
		case *ast.ReturnStmt:
			for _, result := range stmt.Results {
				if target.matchesExpr(pass, result) || usesTarget(result) {
					escaped = true
					return false
				}
			}
		case *ast.SendStmt:
			if target.matchesExpr(pass, stmt.Value) || usesTarget(stmt.Value) {
				escaped = true
				return false
			}
		case *ast.GoStmt:
			if !includeCalls {
				return false
			}
			callOutcome, callReason := callUsesTargetOutcomeWithSummaryStack(pass, stmt.Call, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache)
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
				if !(target.matchesExpr(pass, rhs) || usesTarget(rhs)) {
					continue
				}
				// The original storage and a new function-local identifier retain
				// the identity locally. Flow-sensitive SSA matching may also relate
				// an indirect destination (for example *sink) to &target; that
				// destination must remain an escape.
				if i < len(stmt.Lhs) && assignmentRetainsTargetLocally(pass, stmt.Lhs[i], target) {
					continue
				}
				escaped = true
				return false
			}
		case *ast.ValueSpec:
			for _, value := range stmt.Values {
				if target.matchesExpr(pass, value) || usesTarget(value) {
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
			callOutcome, callReason := callUsesTargetOutcomeWithSummaryStack(pass, stmt, target, syncLits, syncCalls, methodCalls, summaryStack, calleeSummaryCache)
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

func assignmentRetainsTargetLocally(pass *analysis.Pass, lhs ast.Expr, target castTarget) bool {
	if targetKeyForExpr(pass, lhs) == target.key() {
		return true
	}
	identifier, ok := stripParens(lhs).(*ast.Ident)
	if !ok || identifier.Name == "_" {
		return false
	}
	variable, ok := objectForIdent(pass, identifier).(*types.Var)
	if !ok || variable.Pkg() == nil || variable.Parent() == nil {
		return false
	}
	return variable.Parent() != variable.Pkg().Scope()
}
