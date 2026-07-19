// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strconv"
	"sync"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type calleeTargetSummary struct {
	Effects       []ProtocolSummaryEffectFact
	Complete      bool
	OutcomeReason pathOutcomeReason
}

func (summary calleeTargetSummary) hasEffect(kind string) bool {
	for _, effect := range summary.Effects {
		if effect.Kind == kind {
			return true
		}
	}
	return false
}

func (summary calleeTargetSummary) protocolContinuationState() (protocolAbstractState, bool) {
	return summary.protocolContinuationStateFrom(false)
}

func (summary calleeTargetSummary) protocolContinuationStateFrom(validated bool) (protocolAbstractState, bool) {
	if !summary.Complete || len(summary.Effects) == 0 {
		return protocolAbstractState{}, false
	}
	state := newProtocolRequiredState()
	if validated {
		state.Validation = protocolValidationProven
	}
	for _, effect := range summary.Effects {
		switch effect.Kind {
		case protocolSummaryEffectPure, protocolSummaryEffectPreserve:
		case protocolSummaryEffectValidate:
			switch effect.Condition {
			case protocolSummaryConditionSuccessfulReturn:
				// Reaching the caller continuation proves this condition: the
				// callee cannot return normally on validation failure.
				state = state.apply(
					protocolConditionalEffect{Kind: protocolEffectValidate},
					protocolErrorResultNil,
					0,
				)
			case protocolSummaryConditionResultNil:
				// Result-conditioned validation is applied only by the canonical
				// SSA edge transfer once the exact result is proven nil. The call
				// continuation alone cannot discharge the obligation.
				state = state.apply(
					protocolConditionalEffect{Kind: protocolEffectValidate},
					protocolErrorResultUnknown,
					0,
				)
			default:
				return protocolAbstractState{}, false
			}
		case protocolSummaryEffectMutate:
			state.Validation = protocolValidationRequired
			state.PossibleEffects |= protocolPossibleEffectMutate
		case protocolSummaryEffectReplace:
			state.Validation = protocolValidationRequired
			state.PossibleEffects |= protocolPossibleEffectReplace
		case protocolSummaryEffectEscape:
			state = state.apply(protocolConditionalEffect{Kind: protocolEffectEscape}, state.Result, 0)
			state.PossibleEffects |= protocolPossibleEffectEscape
		case protocolSummaryEffectConsume:
			state = state.apply(protocolConditionalEffect{Kind: protocolEffectConsume}, state.Result, 0)
			state.PossibleEffects |= protocolPossibleEffectConsume
		case protocolSummaryEffectTerminal:
			state.PossibleEffects |= protocolPossibleEffectTerminate
		default:
			return protocolAbstractState{}, false
		}
	}
	return state, true
}

func (summary calleeTargetSummary) conditionallyValidatesWithoutHazard() bool {
	if !summary.Complete || len(summary.Effects) == 0 {
		return false
	}
	validated := false
	hazard := false
	for _, effect := range summary.Effects {
		switch effect.Kind {
		case protocolSummaryEffectPure, protocolSummaryEffectPreserve:
		case protocolSummaryEffectValidate:
			if effect.Condition != protocolSummaryConditionResultNil &&
				effect.Condition != protocolSummaryConditionSuccessfulReturn {
				return false
			}
			validated = true
		case protocolSummaryEffectMutate, protocolSummaryEffectReplace:
			validated = false
		case protocolSummaryEffectEscape:
			hazard = true
		case protocolSummaryEffectConsume:
			hazard = hazard || !validated
		case protocolSummaryEffectTerminal:
			return false
		default:
			return false
		}
	}
	return validated && !hazard
}

func (summary calleeTargetSummary) hasSuccessfulReturnValidation() bool {
	for _, effect := range summary.Effects {
		if effect.Kind == protocolSummaryEffectValidate &&
			effect.Condition == protocolSummaryConditionSuccessfulReturn {
			return true
		}
	}
	return false
}

type calleeSummaryEntry struct {
	summary    calleeTargetSummary
	ok         bool
	reason     pathOutcomeReason
	inProgress bool
	ssa        *ssaResult
}

type positionedSummaryEffect struct {
	position token.Pos
	effect   ProtocolSummaryEffectFact
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

func ensureCalleeSummaryCache(cache *sync.Map) *sync.Map {
	if cache != nil {
		return cache
	}
	return &sync.Map{}
}

func calledFunctionObject(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	normalized, ok := normalizeProtocolCall(pass, call)
	if !ok {
		return nil
	}
	return normalized.Function
}

func callHasLocalResolvedBody(pass *analysis.Pass, call *ast.CallExpr) bool {
	function := calledFunctionObject(pass, call)
	if function == nil {
		return false
	}
	declaration := findFuncDeclForObject(pass, function)
	return declaration != nil && declaration.Body != nil
}

type summaryStackScope struct {
	order []string
	seen  map[string]bool
	ssa   *ssaResult
}

func stackScopeFromMap(stack map[string]bool, ssaResults ...*ssaResult) summaryStackScope {
	scope := summaryStackScope{
		order: nil,
		seen:  make(map[string]bool),
	}
	if len(ssaResults) > 0 {
		scope.ssa = ssaResults[0]
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
	for idx := range slices.Backward(scope.order) {
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
	cache *sync.Map,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	return callCalleeSummaryForMatcherWithStack(
		pass,
		call,
		castTargetMatcher(pass, target),
		scope,
		cache,
	)
}

func callCalleeSummaryForMatcherWithStack(
	pass *analysis.Pass,
	call *ast.CallExpr,
	matcher validateReceiverMatcher,
	scope summaryStackScope,
	cache *sync.Map,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	cache = ensureCalleeSummaryCache(cache)
	slots := callTargetSlotsMatchingReceiverMatcher(pass, call, matcher)
	if len(slots) == 0 {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	best := pathOutcomeReasonUnresolvedTarget
	for _, candidate := range slots {
		summary, ok, reason := callCalleeSummaryForSlotWithStack(pass, call, candidate.slot, scope, cache)
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
	cache *sync.Map,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	cache = ensureCalleeSummaryCache(cache)
	fnObj := calledFunctionObject(pass, call)
	if fnObj == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	return calleeSummaryForFuncSlotWithStack(pass, fnObj, slot, scope, cache)
}

func calleeSummaryForFuncSlotWithStack(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
	scope summaryStackScope,
	cache *sync.Map,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	if fnObj == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	cache = ensureCalleeSummaryCache(cache)
	key := objectKey(fnObj)
	if key == "" {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	cacheKey := key + "|" + slot.cacheKey()
	if scope.contains(cacheKey) {
		return recursiveCalleeSummary(pass, fnObj, slot, scope.ssa)
	}
	if cached, ok := cache.Load(cacheKey); ok {
		if entry, ok := cached.(calleeSummaryEntry); ok {
			if entry.inProgress {
				return recursiveCalleeSummary(pass, fnObj, slot, entry.ssa)
			}
			return entry.summary, entry.ok, entry.reason
		}
	}
	if scope.ssa == nil {
		scope.ssa = buildSSAForPass(pass)
	}
	pending := calleeSummaryEntry{
		reason:     pathOutcomeReasonRecursionCycle,
		inProgress: true,
		ssa:        scope.ssa,
	}
	if cached, loaded := cache.LoadOrStore(cacheKey, pending); loaded {
		if entry, ok := cached.(calleeSummaryEntry); ok {
			if entry.inProgress {
				return recursiveCalleeSummary(pass, fnObj, slot, entry.ssa)
			}
			return entry.summary, entry.ok, entry.reason
		}
	}
	scope.push(cacheKey)
	summary, ok, reason := deriveCalleeSummaryForSlotWithStack(pass, fnObj, slot, scope, cache)
	scope.pop(cacheKey)
	summary.OutcomeReason = reason
	cache.Store(cacheKey, calleeSummaryEntry{summary: summary, ok: ok, reason: reason})
	return summary, ok, reason
}

func recursiveCalleeSummary(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
	ssaRes *ssaResult,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	if ssaRes == nil {
		ssaRes = buildSSAForPass(pass)
	}
	resolution := resolveSSAFunction(ssaRes, fnObj)
	if resolution.Availability.ready() {
		if summary, ok := buildProtocolRecursiveSummary(resolution.Function, slot); ok {
			return summary, true, pathOutcomeReasonNone
		}
	}
	return calleeTargetSummary{}, false, pathOutcomeReasonRecursionCycle
}

func deriveCalleeSummaryForSlotWithStack(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
	scope summaryStackScope,
	cache *sync.Map,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	fnDecl := findFuncDeclForObject(pass, fnObj)
	if fnDecl == nil || fnDecl.Body == nil {
		return importedProtocolSummaryForSlot(pass, fnObj, slot)
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

	ssaResult := scope.ssa
	if ssaResult == nil {
		ssaResult = buildSSAForPass(pass)
		scope.ssa = ssaResult
	}
	methodCalls := collectMethodValueValidateCallSet(methodValueCalls)
	methodCalls = mergeMethodValueValidateCallSets(methodCalls, collectCalleeValidatedCalls(pass, fnDecl.Body, ssaResult, scope, cache))
	ssaAvailability := protocolSSAAvailabilityForDecl(pass, ssaResult, fnDecl)
	if !ssaAvailability.ready() {
		return calleeTargetSummary{}, false, ssaAvailability.pathOutcomeReason()
	}
	ssaFunction := resolveSSAFunction(ssaResult, fnObj).Function
	if recursiveSummary, recursiveOK := buildProtocolRecursiveSummary(ssaFunction, slot); recursiveOK {
		return recursiveSummary, true, pathOutcomeReasonNone
	}
	validationProgram := buildProtocolValidationProgram(pass, ssaResult, methodCalls)

	cfg := buildFuncCFGForPass(pass, fnDecl.Body, ssaResult)
	entry := cfgEntryBlock(cfg)
	if entry == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	budget := adaptiveBlockVisitBudget(
		cfg,
		blockVisitBudget{maxStates: defaultCFGMaxStates},
	)

	noReturnCalls := newNoReturnCallResolver(pass, fnDecl.Body, ssaResult)
	solver := newInterprocSolverWithSSA(pass, ssaResult, cache)
	originKey := "summary:" + objectKey(fnObj) + "|" + slot.cacheKey()
	validateResult := solver.EvaluateCastPath(interprocCastPathInput{
		Decl:            fnDecl,
		CFG:             cfg,
		DefBlock:        entry,
		DefIdx:          0,
		Target:          target,
		TypeName:        qualifiedTypeName(functionTargetType(fnObj, slot), pass.Pkg),
		OriginKey:       originKey,
		SyncLits:        pathSyncLits,
		SyncCalls:       pathSyncCalls,
		MethodCalls:     methodCalls,
		MaxStates:       budget.maxStates,
		SummaryStack:    scope.seen,
		SSAAvailability: ssaAvailability,
	})
	validateOutcome := validateResult.toPathOutcome()
	validateReason := validateResult.Reason
	if validateOutcome == pathOutcomeInconclusive {
		return calleeTargetSummary{}, false, validateReason
	}
	alwaysValidates := validateOutcome == pathOutcomeSafe

	escapeResult := solver.EvaluateUBVCrossBlock(interprocUBVCrossBlockInput{
		Target:          target,
		DefBlock:        entry,
		DefIdx:          -1,
		OriginKey:       originKey,
		TypeName:        qualifiedTypeName(functionTargetType(fnObj, slot), pass.Pkg),
		SyncLits:        ubvSyncLits,
		SyncCalls:       ubvSyncCalls,
		MethodCalls:     methodCalls,
		MaxStates:       budget.maxStates,
		SummaryStack:    scope.seen,
		SSAAvailability: ssaAvailability,
	})
	if escapeResult.toPathOutcome() == pathOutcomeInconclusive {
		return calleeTargetSummary{}, false, escapeResult.Reason
	}
	escapesBeforeValidate := escapeResult.toPathOutcome() == pathOutcomeUnsafe

	targetKind, targetSlot := protocolSummaryTargetForCalleeSlot(slot)
	positionedEffects := make([]positionedSummaryEffect, 0, 4)
	validationPosition := validationProgram.firstTargetValidationPosition(pass, target)
	hasExactValidationEffect := false
	for _, effect := range buildProtocolSummaryFact(pass.Pkg.Path(), fnObj.Name(), ssaFunction).Effects {
		if effect.Kind != protocolSummaryEffectValidate ||
			effect.TargetKind != targetKind || effect.TargetSlot != targetSlot ||
			!validationPosition.IsValid() {
			continue
		}
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: validationPosition,
			effect:   effect,
		})
		hasExactValidationEffect = true
	}
	if alwaysValidates && !hasExactValidationEffect && validationPosition.IsValid() {
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: validationPosition,
			effect:   newProtocolSuccessfulReturnSummaryEffect(targetKind, targetSlot),
		})
	}
	if mutation, position, found := targetMutationSummaryEffect(pass, fnDecl, slot, target); found {
		positionedEffects = append(positionedEffects, positionedSummaryEffect{position: position, effect: mutation})
	}
	directEscapePosition, directlyEscapes := firstDirectTargetEscapePosition(pass, cfg, target)
	switch {
	case targetOnlyDiscardedInBody(pass, fnDecl.Body, target):
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: fnDecl.Body.Pos(),
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectPreserve, targetKind, targetSlot),
		})
	case directlyEscapes:
		// A direct escape is stronger than an ordinary consumption. This
		// preserves a recursive base-case store even when forwarding calls
		// also consume the target on recursive routes.
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: directEscapePosition,
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, targetKind, targetSlot),
		})
	case targetConsumedInBody(pass, fnDecl.Body, target, validationProgram):
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: firstTargetConsumptionPosition(pass, fnDecl.Body, target, validationProgram),
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectConsume, targetKind, targetSlot),
		})
	case escapesBeforeValidate:
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: firstTargetReferencePosition(pass, fnDecl.Body, target),
			effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, targetKind, targetSlot),
		})
	}
	if !protocolCFGHasReturningPath(pass, cfg, noReturnCalls) {
		positionedEffects = append(positionedEffects, positionedSummaryEffect{
			position: fnDecl.End(),
			effect:   newProtocolCallSummaryEffect(protocolSummaryEffectTerminal),
		})
	}
	if len(positionedEffects) == 0 {
		if !targetReferencedInBody(pass, fnDecl.Body, target) && functionBodyIsObviouslyPure(fnDecl.Body) {
			positionedEffects = append(positionedEffects, positionedSummaryEffect{
				position: fnDecl.Body.Pos(),
				effect:   newProtocolCallSummaryEffect(protocolSummaryEffectPure),
			})
		} else {
			positionedEffects = append(positionedEffects, positionedSummaryEffect{
				position: fnDecl.Body.Pos(),
				effect:   newProtocolTargetSummaryEffect(protocolSummaryEffectPreserve, targetKind, targetSlot),
			})
		}
	}
	sort.SliceStable(positionedEffects, func(left, right int) bool {
		return positionedEffects[left].position < positionedEffects[right].position
	})
	effects := make([]ProtocolSummaryEffectFact, 0, len(positionedEffects))
	for _, positioned := range positionedEffects {
		effects = append(effects, positioned.effect)
	}

	return calleeTargetSummary{
		Effects:       effects,
		Complete:      true,
		OutcomeReason: pathOutcomeReasonNone,
	}, true, pathOutcomeReasonNone
}

func firstDirectTargetEscapePosition(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	target castTarget,
) (token.Pos, bool) {
	position := token.NoPos
	if cfg == nil {
		return position, false
	}
	for _, block := range cfg.Blocks {
		if block == nil {
			continue
		}
		for _, node := range block.Nodes {
			_, reason := postValidationNonCallTargetEffect(pass, node, target)
			if reason != pathOutcomeReasonEscapedHeapMutation {
				continue
			}
			if !position.IsValid() || node.Pos() < position {
				position = node.Pos()
			}
		}
	}
	return position, position.IsValid()
}

func functionTargetType(function *types.Func, slot calleeTargetSlot) types.Type {
	if function == nil {
		return types.Typ[types.Invalid]
	}
	signature, ok := types.Unalias(function.Type()).(*types.Signature)
	if !ok {
		return types.Typ[types.Invalid]
	}
	if slot.kind == calleeTargetSlotReceiver {
		if signature.Recv() != nil {
			return signature.Recv().Type()
		}
		return types.Typ[types.Invalid]
	}
	if slot.kind == calleeTargetSlotArg && slot.argIndex >= 0 && slot.argIndex < signature.Params().Len() {
		return signature.Params().At(slot.argIndex).Type()
	}
	return types.Typ[types.Invalid]
}

func protocolCFGHasReturningPath(pass *analysis.Pass, cfg *gocfg.CFG, noReturnCalls noReturnCallResolver) bool {
	if cfg == nil {
		return false
	}
	for _, block := range cfg.Blocks {
		if block == nil || len(block.Nodes) == 0 || len(block.Succs) != 0 {
			continue
		}
		returns := true
		for _, call := range protocolOrderedCallsInNode(block.Nodes[len(block.Nodes)-1]) {
			if !callMayReturn(pass, call, noReturnCalls) {
				returns = false
				break
			}
		}
		if returns {
			return true
		}
	}
	return false
}

func importedProtocolSummaryForSlot(
	pass *analysis.Pass,
	fnObj *types.Func,
	slot calleeTargetSlot,
) (calleeTargetSummary, bool, pathOutcomeReason) {
	if pass == nil || pass.ImportObjectFact == nil || fnObj == nil || fnObj.Pkg() == nil {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	fact := &ProtocolSummaryFact{}
	if !pass.ImportObjectFact(fnObj, fact) || validateProtocolSummaryFact(fact, fnObj) != 0 {
		return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
	}
	summary := calleeTargetSummary{Complete: true, OutcomeReason: pathOutcomeReasonNone}
	for _, effect := range fact.Effects {
		matches := (slot.kind == calleeTargetSlotReceiver && effect.TargetKind == protocolSummaryTargetReceiver) ||
			(slot.kind == calleeTargetSlotArg && effect.TargetKind == protocolSummaryTargetParameter &&
				effect.TargetSlot == slot.argIndex)
		if matches || effect.Kind == protocolSummaryEffectPure || effect.Kind == protocolSummaryEffectTerminal {
			summary.Effects = append(summary.Effects, effect)
		}
	}
	if len(summary.Effects) > 0 {
		return summary, true, pathOutcomeReasonNone
	}
	return calleeTargetSummary{}, false, pathOutcomeReasonUnresolvedTarget
}

func cfgEntryBlock(cfg *gocfg.CFG) *gocfg.Block {
	if cfg == nil || len(cfg.Blocks) == 0 {
		return nil
	}
	return cfg.Blocks[0]
}

func collectCalleeValidatedCalls(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	ssaResult *ssaResult,
	scope summaryStackScope,
	cache *sync.Map,
) methodValueValidateCallSet {
	if pass == nil || body == nil {
		return nil
	}
	cache = ensureCalleeSummaryCache(cache)
	if scope.ssa == nil {
		scope.ssa = ssaResult
	}
	var candidates methodValueValidateCallSet
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := stripParens(call.Fun).(*ast.SelectorExpr); ok && selector.Sel.Name == validateMethodName {
			// Direct Validate calls are modeled by the invocation relation and
			// flow-sensitive receiver identity. Treating the method body as a
			// helper summary would bypass receiver rebinding at the call site.
			return true
		}
		for _, candidate := range allCallTargetSlots(call) {
			summary, ok, _ := callCalleeSummaryForSlotWithStack(pass, call, candidate.slot, scope, cache)
			if !ok {
				continue
			}
			if !summary.conditionallyValidatesWithoutHazard() {
				continue
			}
			if candidates == nil {
				candidates = make(methodValueValidateCallSet)
			}
			candidates[call] = methodValueValidationCall{
				receiver:           candidate.expr,
				onSuccessfulReturn: summary.hasSuccessfulReturnValidation(),
			}
			break
		}
		return true
	})
	if len(candidates) == 0 {
		return nil
	}
	validationProgram := buildProtocolValidationProgram(pass, ssaResult, candidates)
	for call, validation := range candidates {
		if validation.onSuccessfulReturn {
			continue
		}
		if !validationProgram.callHasCheckedSuccess(call) {
			delete(candidates, call)
		}
	}
	return candidates
}

func protocolSummaryTargetForCalleeSlot(slot calleeTargetSlot) (string, int) {
	if slot.kind == calleeTargetSlotReceiver {
		return protocolSummaryTargetReceiver, 0
	}
	return protocolSummaryTargetParameter, slot.argIndex
}

func targetReferencedInBody(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) bool {
	if pass == nil || body == nil {
		return true
	}
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		expression, ok := node.(ast.Expr)
		if ok && target.matchesExpr(pass, expression) {
			found = true
			return false
		}
		return true
	})
	return found
}

func targetConsumedInBody(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	target castTarget,
	validationPrograms ...protocolValidationProgram,
) bool {
	return firstTargetConsumptionPosition(pass, body, target, validationPrograms...).IsValid()
}

func firstTargetConsumptionPosition(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	target castTarget,
	validationPrograms ...protocolValidationProgram,
) token.Pos {
	if pass == nil || body == nil {
		return token.NoPos
	}
	position := token.NoPos
	ast.Inspect(body, func(node ast.Node) bool {
		if position.IsValid() {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, program := range validationPrograms {
			if program.nodeTargetInvocationResolution(pass, call, target) == protocolAliasMust {
				return false
			}
		}
		if selector, selectorOK := stripParens(call.Fun).(*ast.SelectorExpr); selectorOK &&
			target.matchesExpr(pass, selector.X) {
			if selector.Sel.Name != validateMethodName {
				position = call.Pos()
			}
			return false
		}
		for _, argument := range call.Args {
			if target.matchesExpr(pass, argument) {
				position = call.Pos()
				return false
			}
		}
		return true
	})
	return position
}

func firstTargetValidationPosition(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	target castTarget,
	methodCalls methodValueValidateCallSet,
) token.Pos {
	if pass == nil || body == nil {
		return token.NoPos
	}
	position := token.NoPos
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		receiver := methodCalls[call].receiver
		if receiver == nil {
			if selector, selectorOK := stripParens(call.Fun).(*ast.SelectorExpr); selectorOK &&
				selector.Sel.Name == validateMethodName {
				receiver = selector.X
			}
		}
		if receiver != nil && target.matchesExpr(pass, receiver) {
			position = call.Pos()
			return false
		}
		return true
	})
	return position
}

func firstTargetReferencePosition(pass *analysis.Pass, body *ast.BlockStmt, target castTarget) token.Pos {
	if pass == nil || body == nil {
		return token.NoPos
	}
	position := token.NoPos
	ast.Inspect(body, func(node ast.Node) bool {
		expression, ok := node.(ast.Expr)
		if ok && target.matchesExpr(pass, expression) {
			position = expression.Pos()
			return false
		}
		return true
	})
	return position
}
