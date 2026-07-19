// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func targetMutationSummaryEffect(
	pass *analysis.Pass,
	fnDecl *ast.FuncDecl,
	targetSlot calleeTargetSlot,
	target castTarget,
) (ProtocolSummaryEffectFact, token.Pos, bool) {
	if pass == nil || fnDecl == nil || fnDecl.Body == nil {
		return ProtocolSummaryEffectFact{}, token.NoPos, false
	}
	var effect ProtocolSummaryEffectFact
	position := token.NoPos
	ast.Inspect(fnDecl.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, lhs := range assignment.Lhs {
			if index >= len(assignment.Rhs) || !writesThroughTarget(pass, lhs, target) {
				continue
			}
			targetKind, targetIndex := protocolSummaryTargetForCalleeSlot(targetSlot)
			effect = newProtocolTargetSummaryEffect(protocolSummaryEffectMutate, targetKind, targetIndex)
			if sourceSlot, found := functionSlotMatchingExpr(pass, fnDecl, assignment.Rhs[index]); found {
				effect.Kind = protocolSummaryEffectReplace
				effect.SourceKind, effect.SourceSlot = protocolSummaryTargetForCalleeSlot(sourceSlot)
			}
			position = assignment.Pos()
			return false
		}
		return true
	})
	return effect, position, position.IsValid()
}

func writesThroughTarget(pass *analysis.Pass, expression ast.Expr, target castTarget) bool {
	switch typed := stripParens(expression).(type) {
	case *ast.StarExpr:
		return target.matchesExpr(pass, typed.X)
	case *ast.SelectorExpr:
		return target.matchesExpr(pass, typed.X)
	case *ast.IndexExpr:
		return target.matchesExpr(pass, typed.X)
	default:
		return false
	}
}

func functionSlotMatchingExpr(pass *analysis.Pass, fnDecl *ast.FuncDecl, expression ast.Expr) (calleeTargetSlot, bool) {
	if fnDecl.Recv != nil && len(fnDecl.Recv.List) > 0 && len(fnDecl.Recv.List[0].Names) > 0 {
		slot := calleeTargetSlot{kind: calleeTargetSlotReceiver}
		if target, ok := functionTargetForSlot(pass, fnDecl, slot); ok && target.matchesExpr(pass, expression) {
			return slot, true
		}
	}
	if fnDecl.Type == nil || fnDecl.Type.Params == nil {
		return calleeTargetSlot{}, false
	}
	parameterIndex := 0
	for _, field := range fnDecl.Type.Params.List {
		for range field.Names {
			slot := calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: parameterIndex}
			if target, ok := functionTargetForSlot(pass, fnDecl, slot); ok && target.matchesExpr(pass, expression) {
				return slot, true
			}
			parameterIndex++
		}
	}
	return calleeTargetSlot{}, false
}

func targetOnlyDiscardedInBody(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) bool {
	if pass == nil || body == nil {
		return false
	}
	parents := buildParentMap(body)
	referenced := false
	onlyDiscarded := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !onlyDiscarded {
			return false
		}
		expression, ok := node.(ast.Expr)
		if !ok || !target.matchesExpr(pass, expression) {
			return true
		}
		referenced = true
		assignment, ok := parents[expression].(*ast.AssignStmt)
		if !ok {
			onlyDiscarded = false
			return false
		}
		for index, rhs := range assignment.Rhs {
			if rhs != expression || index >= len(assignment.Lhs) {
				continue
			}
			identifier, blank := stripParens(assignment.Lhs[index]).(*ast.Ident)
			if !blank || identifier.Name != "_" {
				onlyDiscarded = false
			}
			return onlyDiscarded
		}
		onlyDiscarded = false
		return false
	})
	return referenced && onlyDiscarded
}

func functionBodyIsObviouslyPure(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	pure := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !pure {
			return false
		}
		switch node.(type) {
		case *ast.CallExpr, *ast.AssignStmt, *ast.IncDecStmt, *ast.SendStmt,
			*ast.GoStmt, *ast.DeferStmt, *ast.RangeStmt:
			pure = false
			return false
		default:
			return true
		}
	})
	return pure
}

func callTargetSlotsMatchingCastTarget(pass *analysis.Pass, call *ast.CallExpr, target castTarget) []calleeCallTarget {
	return callTargetSlotsMatchingReceiverMatcher(pass, call, castTargetMatcher(pass, target))
}

func callTargetSlotsMatchingReceiverMatcher(
	pass *analysis.Pass,
	call *ast.CallExpr,
	matcher validateReceiverMatcher,
) []calleeCallTarget {
	if call == nil || matcher == nil {
		return nil
	}
	candidates := allCallTargetSlots(call)
	out := make([]calleeCallTarget, 0, len(candidates))
	for _, candidate := range candidates {
		if matcher(pass, candidate.expr) {
			out = append(out, candidate)
		}
	}
	return out
}

func allCallTargetSlots(call *ast.CallExpr) []calleeCallTarget {
	if call == nil {
		return nil
	}
	out := make([]calleeCallTarget, 0, len(call.Args)+1)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		out = append(out, calleeCallTarget{
			slot: calleeTargetSlot{
				kind: calleeTargetSlotReceiver,
			},
			expr: sel.X,
		})
	}
	for idx, arg := range call.Args {
		out = append(out, calleeCallTarget{
			slot: calleeTargetSlot{
				kind:     calleeTargetSlotArg,
				argIndex: idx,
			},
			expr: arg,
		})
	}
	return out
}

func findFuncDeclForObject(pass *analysis.Pass, fnObj *types.Func) *ast.FuncDecl {
	if pass == nil || pass.TypesInfo == nil || fnObj == nil {
		return nil
	}
	wantKey := objectKey(fnObj)
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fnDecl, ok := decl.(*ast.FuncDecl)
			if !ok || fnDecl.Name == nil {
				continue
			}
			obj := pass.TypesInfo.Defs[fnDecl.Name]
			if obj == nil {
				continue
			}
			if obj == fnObj {
				return fnDecl
			}
			if wantKey != "" && objectKey(obj) == wantKey {
				return fnDecl
			}
		}
	}
	return nil
}

func functionTargetForSlot(pass *analysis.Pass, fnDecl *ast.FuncDecl, slot calleeTargetSlot) (castTarget, bool) {
	if pass == nil || pass.TypesInfo == nil || fnDecl == nil {
		return castTarget{}, false
	}
	if slot.kind == calleeTargetSlotReceiver {
		if fnDecl.Recv == nil || len(fnDecl.Recv.List) == 0 {
			return castTarget{}, false
		}
		recv := fnDecl.Recv.List[0]
		if len(recv.Names) == 0 {
			return castTarget{}, false
		}
		return castTargetFromExpr(pass, recv.Names[0])
	}
	if slot.kind != calleeTargetSlotArg || slot.argIndex < 0 {
		return castTarget{}, false
	}
	if fnDecl.Type == nil || fnDecl.Type.Params == nil || len(fnDecl.Type.Params.List) == 0 {
		return castTarget{}, false
	}
	currentIdx := 0
	for _, field := range fnDecl.Type.Params.List {
		if field == nil {
			continue
		}
		for _, name := range field.Names {
			if currentIdx != slot.argIndex {
				currentIdx++
				continue
			}
			return castTargetFromExpr(pass, name)
		}
	}
	return castTarget{}, false
}
