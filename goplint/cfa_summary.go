// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"strconv"
	"sync"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type calleeTargetSummary struct {
	AlwaysValidatesTarget       bool
	EscapesTargetBeforeValidate bool
	OutcomeReason               pathOutcomeReason
}

type calleeSummaryEntry struct {
	summary calleeTargetSummary
	ok      bool
	reason  pathOutcomeReason
}

type calleeTargetSlotKind string

const (
	calleeTargetSlotReceiver calleeTargetSlotKind = "receiver"
	calleeTargetSlotArg      calleeTargetSlotKind = "arg"
)

type calleeTargetSlot struct {
	kind     calleeTargetSlotKind
	argIndex int
}

func (slot calleeTargetSlot) cacheKey() string {
	if slot.kind == calleeTargetSlotArg {
		return string(slot.kind) + ":" + strconv.Itoa(slot.argIndex)
	}
	return string(slot.kind)
}

type calleeCallTarget struct {
	slot calleeTargetSlot
	expr ast.Expr
}

var calleeSummaryCache sync.Map // map[string]calleeSummaryEntry keyed by objectKey(func)+slot

func resetFirstArgSummaryCache() {
	calleeSummaryCache.Range(func(key, _ any) bool {
		calleeSummaryCache.Delete(key)
		return true
	})
}

func calledFunctionObject(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return nil
	}
	switch fun := stripParens(call.Fun).(type) {
	case *ast.Ident:
		fnObj, _ := objectForIdent(pass, fun).(*types.Func)
		return fnObj
	case *ast.SelectorExpr:
		fnObj, _ := objectForIdent(pass, fun.Sel).(*types.Func)
		return fnObj
	default:
		return nil
	}
}

type summaryStackScope struct {
	order []string
	seen  map[string]bool
}

func stackScopeFromMap(stack map[string]bool) summaryStackScope {
	scope := summaryStackScope{
		order: nil,
		seen:  make(map[string]bool),
	}
	for key, present := range stack {
		if !present {
			continue
		}
		scope.seen[key] = true
		scope.order = append(scope.order, key)
	}
	return scope
}

func (scope *summaryStackScope) contains(key string) bool {
	if scope == nil || scope.seen == nil {
		return false
	}
	return scope.seen[key]
}

func (scope *summaryStackScope) push(key string) {
	if scope == nil {
		return
	}
	if scope.seen == nil {
		scope.seen = make(map[string]bool)
	}
	scope.seen[key] = true
	scope.order = append(scope.order, key)
}

func (scope *summaryStackScope) pop(key string) {
	if scope == nil {
		return
	}
	delete(scope.seen, key)
	if len(scope.order) == 0 {
		return
	}
	for idx := len(scope.order) - 1; idx >= 0; idx-- {
		if scope.order[idx] != key {
			continue
		}
		scope.order = append(scope.order[:idx], scope.order[idx+1:]...)
		return
	}
}

func callCalleeSummaryForTargetWithStack(
	pass *analysis.Pass,
	call *ast.CallExpr,
	target castTarget,
	scope summaryStackScope,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	slots := callTargetSlotsMatchingCastTarget(pass, call, target)
	if len(slots) == 0 {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	best := pathOutcomeReasonUnresolvedTarget
	for _, candidate := range slots {
		summary, ok, reason := callCalleeSummaryForSlotWithStack(pass, call, candidate.slot, scope)
		if ok {
			return summary, true, reason
		}
		if reason == pathOutcomeReasonRecursionCycle {
			return calleeTargetSummary{}, false, reason
		}
		if best == pathOutcomeReasonUnresolvedTarget || best == pathOutcomeReasonNone {
			best = reason
		}
	}
	return calleeTargetSummary{}, false, best
}

func callCalleeSummaryForSlotWithStack(
	pass *analysis.Pass,
	call *ast.CallExpr,
	slot calleeTargetSlot,
	scope summaryStackScope,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	fnObj := calledFunctionObject(pass, call)
	if fnObj == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	return calleeSummaryForFuncSlotWithStack(pass, fnObj, slot, scope)
}

func calleeSummaryForFuncSlotWithStack(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
	scope summaryStackScope,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	if fnObj == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	key := objectKey(fnObj)
	if key == "" {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	cacheKey := key + "|" + slot.cacheKey()
	if cached, ok := calleeSummaryCache.Load(cacheKey); ok {
		entry := cached.(calleeSummaryEntry)
		return entry.summary, entry.ok, entry.reason
	}
	if scope.contains(cacheKey) {
		return calleeTargetSummary{}, false, pathOutcomeReasonRecursionCycle
	}
	scope.push(cacheKey)
	summary, ok, reason := deriveCalleeSummaryForSlotWithStack(pass, fnObj, slot, scope)
	scope.pop(cacheKey)
	summary.OutcomeReason = reason
	calleeSummaryCache.Store(cacheKey, calleeSummaryEntry{summary: summary, ok: ok, reason: reason})
	return summary, ok, reason
}

func deriveCalleeSummaryForSlotWithStack(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
	scope summaryStackScope,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	fnDecl := findFuncDeclForObject(pass, fnObj)
	if fnDecl == nil || fnDecl.Body == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	target, ok := functionTargetForSlot(pass, fnDecl, slot)
	if !ok {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}

	parentMap := buildParentMap(fnDecl.Body)
	_, _, closureCalls, methodValueCalls := collectCFACasts(pass, fnDecl.Body, parentMap, func(*ast.FuncLit, int) {})

	pathSyncLits := collectSynchronousClosureLits(fnDecl.Body)
	pathSyncCalls := collectSynchronousClosureVarCalls(closureCalls)
	ubvSyncLits := collectUBVClosureLits(fnDecl.Body)
	ubvSyncCalls := collectUBVClosureVarCalls(closureCalls)

	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(methodCalls, collectCalleeValidatedCalls(pass, fnDecl.Body, scope))

	cfg := buildFuncCFGForPass(pass, fnDecl.Body)
	entry := cfgEntryBlock(cfg)
	if entry == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	budget := adaptiveBlockVisitBudget(
		cfg,
		blockVisitBudget{maxStates: defaultCFGMaxStates, maxDepth: defaultCFGMaxDepth},
	)

	noReturnAliases := collectNoReturnFuncAliasEvents(pass, fnDecl.Body)
	validateOutcome, validateReason := hasPathToReturnWithoutValidateOutcome(
		pass,
		cfg,
		entry,
		-1,
		target,
		pathSyncLits,
		pathSyncCalls,
		methodCalls,
		noReturnAliases,
		budget.maxStates,
		budget.maxDepth,
	)
	if validateOutcome == pathOutcomeInconclusive {
		return calleeTargetSummary{}, false, validateReason
	}
	alwaysValidates := validateOutcome == pathOutcomeSafe

	inBlockOutcome, inBlockReason := hasUseBeforeValidateInBlockOutcomeModeWithSummaryStack(
		pass,
		entry.Nodes,
		0,
		target,
		ubvSyncLits,
		ubvSyncCalls,
		methodCalls,
		ubvModeEscape,
		scope.seen,
	)
	if inBlockOutcome == pathOutcomeInconclusive {
		return calleeTargetSummary{}, false, inBlockReason
	}
	escapesBeforeValidate := inBlockOutcome == pathOutcomeUnsafe
	if !escapesBeforeValidate {
		outcome, reason := hasUseBeforeValidateCrossBlockModeWithSummaryStack(
			pass,
			entry,
			-1,
			target,
			ubvSyncLits,
			ubvSyncCalls,
			methodCalls,
			ubvModeEscape,
			budget.maxStates,
			budget.maxDepth,
			scope.seen,
		)
		if outcome == pathOutcomeInconclusive {
			return calleeTargetSummary{}, false, reason
		}
		escapesBeforeValidate = outcome == pathOutcomeUnsafe
	}

	return calleeTargetSummary{
		AlwaysValidatesTarget:       alwaysValidates,
		EscapesTargetBeforeValidate: escapesBeforeValidate,
		OutcomeReason:               pathOutcomeReasonNone,
	}, true, pathOutcomeReasonNone
}

func cfgEntryBlock(cfg *gocfg.CFG) *gocfg.Block {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return nil
	}
	return cfg.Blocks[0]
}

func collectCalleeValidatedCalls(pass *analysis.Pass, body *ast.BlockStmt, scope summaryStackScope) methodValueValidateCallSet {
	if pass == nil || body == nil {
		return nil
	}
	var out methodValueValidateCallSet
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, candidate := range allCallTargetSlots(call) {
			summary, ok, _ := callCalleeSummaryForSlotWithStack(pass, call, candidate.slot, scope)
			if !ok {
				continue
			}
			if !summary.AlwaysValidatesTarget || summary.EscapesTargetBeforeValidate {
				continue
			}
			if out == nil {
				out = make(methodValueValidateCallSet)
			}
			out[call] = candidate.expr
			break
		}
		return true
	})
	return out
}

func callTargetSlotsMatchingCastTarget(pass *analysis.Pass, call *ast.CallExpr, target castTarget) []calleeCallTarget {
	if call == nil {
		return nil
	}
	candidates := allCallTargetSlots(call)
	out := make([]calleeCallTarget, 0, len(candidates))
	for _, candidate := range candidates {
		if target.matchesExpr(pass, candidate.expr) {
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
	if fnDecl == nil {
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
		if pass != nil {
			if target, ok := castTargetFromExpr(pass, recv.Names[0]); ok {
				return target, true
			}
		}
		return newCastTargetFromName(recv.Names[0].Name), true
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
			if pass != nil {
				if target, ok := castTargetFromExpr(pass, name); ok {
					return target, true
				}
			}
			return newCastTargetFromName(name.Name), true
		}
	}
	return castTarget{}, false
}
