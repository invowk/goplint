// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"slices"

	"golang.org/x/tools/go/ssa"
)

const (
	protocolRecursiveInvalidatesMutation uint8 = 1 << iota
	protocolRecursiveInvalidatesReplacement
)

type protocolRecursiveSummaryNode struct {
	function  *ssa.Function
	slot      calleeTargetSlot
	routes    []protocolRecursiveSummaryRoute
	invalid   bool
	returning bool
	deps      map[string]protocolRecursiveSummaryNode
}

type protocolRecursiveSummaryRoute struct {
	direct     bool
	dependency protocolRecursiveSummaryNode
	terminal   ssa.CallInstruction
	returned   *ssa.Return
	resultSlot int
	before     []string
	after      []string
}

// protocolRecursiveEffectTransfer is a finite, conservative normal form for
// one or more returning routes. Invalidations before and after the final
// validation remain distinct, so a fixed point cannot republish validation
// after a recursive mutation that actually occurs later.
type protocolRecursiveEffectTransfer struct {
	available          bool
	validation         bool
	preInvalidations   uint8
	postInvalidations  uint8
	escape             bool
	consumeBeforeValid bool
	consumeAfterValid  bool
}

type protocolRecursiveTransferBuilder struct {
	protocolRecursiveEffectTransfer
	validated bool
}

func buildProtocolRecursiveSummary(function *ssa.Function, slot calleeTargetSlot) (calleeTargetSummary, bool) {
	if function == nil {
		return calleeTargetSummary{}, false
	}
	nodes := make(map[string]protocolRecursiveSummaryNode)
	protocolCollectRecursiveSummaryNode(function, slot, nodes, make(map[string]bool))
	if len(nodes) == 0 {
		return calleeTargetSummary{}, false
	}

	valid := make(map[string]bool, len(nodes))
	for key, node := range nodes {
		valid[key] = node.returning && !node.invalid && len(node.routes) > 0
	}
	for changed := true; changed; {
		changed = false
		for key, node := range nodes {
			if !valid[key] {
				continue
			}
			for dependencyKey := range node.deps {
				if valid[dependencyKey] {
					continue
				}
				valid[key] = false
				changed = true
				break
			}
		}
	}

	reachesDirect := make(map[string]bool, len(nodes))
	for key, node := range nodes {
		if !valid[key] {
			continue
		}
		for _, route := range node.routes {
			if route.direct {
				reachesDirect[key] = true
				break
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for key, node := range nodes {
			if !valid[key] || reachesDirect[key] {
				continue
			}
			for dependencyKey := range node.deps {
				if reachesDirect[dependencyKey] {
					reachesDirect[key] = true
					changed = true
					break
				}
			}
		}
	}

	rootKey := protocolRecursiveSummaryKey(function, slot)
	if !valid[rootKey] || !reachesDirect[rootKey] {
		return calleeTargetSummary{}, false
	}

	// Start with direct-return routes, then monotonically add dependency
	// routes. The transfer domain is finite, so recursive and mutually
	// recursive equations converge without a call-depth approximation.
	transfers := make(map[string]protocolRecursiveEffectTransfer, len(nodes))
	for changed := true; changed; {
		changed = false
		for key, node := range nodes {
			if !valid[key] {
				continue
			}
			candidate, ok := protocolRecursiveNodeTransfer(node, transfers, false)
			if !ok {
				continue
			}
			merged := joinProtocolRecursiveTransfers(transfers[key], candidate)
			if merged == transfers[key] {
				continue
			}
			transfers[key] = merged
			changed = true
		}
	}

	// A provisional transfer is useful only for convergence. Publication
	// requires every returning route and every dependency to participate.
	for key, node := range nodes {
		if !valid[key] {
			continue
		}
		complete, ok := protocolRecursiveNodeTransfer(node, transfers, true)
		if !ok {
			return calleeTargetSummary{}, false
		}
		transfers[key] = joinProtocolRecursiveTransfers(transfers[key], complete)
	}

	root := nodes[rootKey]
	resultSlot, ok := protocolRecursiveResultSlot(root)
	if !ok {
		return calleeTargetSummary{}, false
	}
	transfer, ok := transfers[rootKey]
	if !ok || !transfer.available || !transfer.validation {
		return calleeTargetSummary{}, false
	}
	targetKind, targetSlot := protocolSummaryTargetForCalleeSlot(slot)
	effects := transfer.effects(targetKind, targetSlot, resultSlot)
	if len(effects) == 0 {
		return calleeTargetSummary{}, false
	}
	return calleeTargetSummary{
		Effects:       effects,
		Complete:      true,
		OutcomeReason: pathOutcomeReasonNone,
	}, true
}

func protocolRecursiveNodeTransfer(
	node protocolRecursiveSummaryNode,
	transfers map[string]protocolRecursiveEffectTransfer,
	requireAll bool,
) (protocolRecursiveEffectTransfer, bool) {
	var joined protocolRecursiveEffectTransfer
	observed := false
	for _, route := range node.routes {
		var middle protocolRecursiveEffectTransfer
		if route.direct {
			middle = protocolRecursiveValidationTransfer()
		} else {
			dependencyKey := protocolRecursiveSummaryKey(route.dependency.function, route.dependency.slot)
			var ok bool
			middle, ok = transfers[dependencyKey]
			if !ok || !middle.available {
				if requireAll {
					return protocolRecursiveEffectTransfer{}, false
				}
				continue
			}
		}
		builder := protocolRecursiveTransferBuilder{}
		builder.applyLocalEffects(route.before)
		builder.applyTransfer(middle)
		builder.applyLocalEffects(route.after)
		joined = joinProtocolRecursiveTransfers(joined, builder.finish())
		observed = true
	}
	return joined, observed
}

func protocolRecursiveValidationTransfer() protocolRecursiveEffectTransfer {
	builder := protocolRecursiveTransferBuilder{}
	builder.apply(protocolSummaryEffectValidate)
	return builder.finish()
}

func (builder *protocolRecursiveTransferBuilder) applyLocalEffects(effects []string) {
	// Invalidations are applied before observations within the same
	// reachability region. This is the conservative order when branch-local
	// SSA instructions do not have one total execution order.
	for _, kind := range []string{protocolSummaryEffectMutate, protocolSummaryEffectReplace} {
		for _, effect := range effects {
			if effect == kind {
				builder.apply(effect)
			}
		}
	}
	for _, kind := range []string{protocolSummaryEffectEscape, protocolSummaryEffectConsume} {
		for _, effect := range effects {
			if effect == kind {
				builder.apply(effect)
			}
		}
	}
}

func (builder *protocolRecursiveTransferBuilder) applyTransfer(transfer protocolRecursiveEffectTransfer) {
	if !transfer.available {
		return
	}
	if transfer.escape {
		builder.apply(protocolSummaryEffectEscape)
	}
	if transfer.consumeBeforeValid {
		builder.apply(protocolSummaryEffectConsume)
	}
	builder.applyInvalidations(transfer.preInvalidations)
	if transfer.validation {
		builder.apply(protocolSummaryEffectValidate)
	}
	if transfer.consumeAfterValid {
		builder.apply(protocolSummaryEffectConsume)
	}
	builder.applyInvalidations(transfer.postInvalidations)
}

func (builder *protocolRecursiveTransferBuilder) applyInvalidations(invalidations uint8) {
	if invalidations&protocolRecursiveInvalidatesMutation != 0 {
		builder.apply(protocolSummaryEffectMutate)
	}
	if invalidations&protocolRecursiveInvalidatesReplacement != 0 {
		builder.apply(protocolSummaryEffectReplace)
	}
}

func (builder *protocolRecursiveTransferBuilder) apply(kind string) {
	switch kind {
	case protocolSummaryEffectValidate:
		builder.preInvalidations |= builder.postInvalidations
		builder.postInvalidations = 0
		builder.validation = true
		builder.validated = true
	case protocolSummaryEffectMutate:
		builder.applyInvalidation(protocolRecursiveInvalidatesMutation)
	case protocolSummaryEffectReplace:
		builder.applyInvalidation(protocolRecursiveInvalidatesReplacement)
	case protocolSummaryEffectEscape:
		builder.escape = true
	case protocolSummaryEffectConsume:
		if builder.validated {
			builder.consumeAfterValid = true
		} else {
			builder.consumeBeforeValid = true
		}
	}
}

func (builder *protocolRecursiveTransferBuilder) applyInvalidation(kind uint8) {
	if builder.validation {
		builder.postInvalidations |= kind
	} else {
		builder.preInvalidations |= kind
	}
	builder.validated = false
}

func (builder *protocolRecursiveTransferBuilder) finish() protocolRecursiveEffectTransfer {
	builder.available = true
	return builder.protocolRecursiveEffectTransfer
}

func joinProtocolRecursiveTransfers(
	left,
	right protocolRecursiveEffectTransfer,
) protocolRecursiveEffectTransfer {
	if !left.available {
		return right
	}
	if !right.available {
		return left
	}
	return protocolRecursiveEffectTransfer{
		available:          true,
		validation:         left.validation || right.validation,
		preInvalidations:   left.preInvalidations | right.preInvalidations,
		postInvalidations:  left.postInvalidations | right.postInvalidations,
		escape:             left.escape || right.escape,
		consumeBeforeValid: left.consumeBeforeValid || right.consumeBeforeValid,
		consumeAfterValid:  left.consumeAfterValid || right.consumeAfterValid,
	}
}

func (transfer protocolRecursiveEffectTransfer) effects(
	targetKind string,
	targetSlot,
	resultSlot int,
) []ProtocolSummaryEffectFact {
	effects := make([]ProtocolSummaryEffectFact, 0, 7)
	if transfer.escape {
		effects = append(effects, newProtocolTargetSummaryEffect(protocolSummaryEffectEscape, targetKind, targetSlot))
	}
	if transfer.consumeBeforeValid {
		effects = append(effects, newProtocolTargetSummaryEffect(protocolSummaryEffectConsume, targetKind, targetSlot))
	}
	effects = appendProtocolRecursiveInvalidations(effects, transfer.preInvalidations, targetKind, targetSlot)
	if transfer.validation {
		effects = append(effects, newProtocolSummaryEffect(targetKind, targetSlot, resultSlot))
	}
	if transfer.consumeAfterValid {
		effects = append(effects, newProtocolTargetSummaryEffect(protocolSummaryEffectConsume, targetKind, targetSlot))
	}
	return appendProtocolRecursiveInvalidations(effects, transfer.postInvalidations, targetKind, targetSlot)
}

func appendProtocolRecursiveInvalidations(
	effects []ProtocolSummaryEffectFact,
	invalidations uint8,
	targetKind string,
	targetSlot int,
) []ProtocolSummaryEffectFact {
	if invalidations&protocolRecursiveInvalidatesMutation != 0 {
		effects = append(effects, newProtocolTargetSummaryEffect(protocolSummaryEffectMutate, targetKind, targetSlot))
	}
	if invalidations&protocolRecursiveInvalidatesReplacement != 0 {
		effects = append(effects, newProtocolTargetSummaryEffect(protocolSummaryEffectReplace, targetKind, targetSlot))
	}
	return effects
}

func protocolRecursiveResultSlot(node protocolRecursiveSummaryNode) (int, bool) {
	resultSlot := -1
	for _, route := range node.routes {
		if resultSlot < 0 {
			resultSlot = route.resultSlot
			continue
		}
		if resultSlot != route.resultSlot {
			return -1, false
		}
	}
	return resultSlot, resultSlot >= 0
}

func protocolCollectRecursiveSummaryNode(
	function *ssa.Function,
	slot calleeTargetSlot,
	nodes map[string]protocolRecursiveSummaryNode,
	visiting map[string]bool,
) {
	key := protocolRecursiveSummaryKey(function, slot)
	if visiting[key] {
		return
	}
	if _, exists := nodes[key]; exists {
		return
	}
	visiting[key] = true
	node := protocolScanRecursiveSummaryNode(function, slot)
	nodes[key] = node
	for _, dependency := range node.deps {
		protocolCollectRecursiveSummaryNode(dependency.function, dependency.slot, nodes, visiting)
	}
	delete(visiting, key)
}

func protocolScanRecursiveSummaryNode(function *ssa.Function, slot calleeTargetSlot) protocolRecursiveSummaryNode {
	node := protocolRecursiveSummaryNode{
		function: function,
		slot:     slot,
		deps:     make(map[string]protocolRecursiveSummaryNode),
	}
	target := protocolSSAValueForCalleeSlot(function, slot)
	if function == nil || target == nil || function.Signature == nil || function.Signature.Results() == nil {
		node.invalid = true
		return node
	}
	interner := newProtocolIdentityInterner()
	invocations := collectProtocolValidationInvocations(function, interner)
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			returned, ok := instruction.(*ssa.Return)
			if !ok {
				continue
			}
			node.returning = true
			classifiedErrorResult := false
			for resultSlot, result := range returned.Results {
				if resultSlot >= function.Signature.Results().Len() ||
					!isErrorType(function.Signature.Results().At(resultSlot).Type()) {
					continue
				}
				classifiedErrorResult = true
				direct, dependency, terminal, classified := protocolClassifyRecursiveSummaryReturn(
					function,
					target,
					result,
					invocations,
				)
				if !classified || terminal == nil {
					node.invalid = true
					continue
				}
				route := protocolRecursiveSummaryRoute{
					direct:     direct,
					dependency: dependency,
					terminal:   terminal,
					returned:   returned,
					resultSlot: resultSlot,
				}
				route.before, route.after = protocolRecursiveRouteEffects(function, target, route)
				node.routes = append(node.routes, route)
				if dependency.function != nil {
					node.deps[protocolRecursiveSummaryKey(dependency.function, dependency.slot)] = dependency
				}
			}
			if !classifiedErrorResult {
				node.invalid = true
			}
		}
	}
	return node
}

func protocolClassifyRecursiveSummaryReturn(
	function *ssa.Function,
	target ssa.Value,
	returned ssa.Value,
	invocations []protocolValidationInvocation,
) (bool, protocolRecursiveSummaryNode, ssa.CallInstruction, bool) {
	for _, invocation := range invocations {
		if protocolValueDerivedFrom(returned, invocation.Result, make(map[ssa.Value]bool)) &&
			protocolValueDerivedFrom(invocation.Receiver, target, make(map[ssa.Value]bool)) {
			return true, protocolRecursiveSummaryNode{}, invocation.Call, true
		}
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(ssa.CallInstruction)
			if !ok || call.Common() == nil || call.Common().StaticCallee() == nil {
				continue
			}
			errorResult := protocolSSAErrorResult(call)
			if errorResult == nil || !protocolValueDerivedFrom(returned, errorResult, make(map[ssa.Value]bool)) {
				continue
			}
			callee := call.Common().StaticCallee()
			for argumentIndex, argument := range call.Common().Args {
				if !protocolValueDerivedFrom(argument, target, make(map[ssa.Value]bool)) {
					continue
				}
				calleeSlot, ok := protocolCalleeSlotForSSAArgument(callee, argumentIndex)
				if !ok {
					continue
				}
				return false, protocolRecursiveSummaryNode{function: callee, slot: calleeSlot}, call, true
			}
		}
	}
	return false, protocolRecursiveSummaryNode{}, nil, false
}

func protocolRecursiveRouteEffects(
	function *ssa.Function,
	target ssa.Value,
	route protocolRecursiveSummaryRoute,
) ([]string, []string) {
	var before []string
	var after []string
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			if instruction == route.terminal {
				continue
			}
			effects := protocolRecursiveInstructionEffects(instruction, target)
			if len(effects) == 0 {
				continue
			}
			mayBeBefore := protocolInstructionMayPrecede(instruction, route.terminal) &&
				protocolInstructionMayPrecede(route.terminal, route.returned)
			mayBeAfter := (instruction == route.returned || protocolInstructionMayPrecede(route.terminal, instruction)) &&
				(instruction == route.returned || protocolInstructionMayPrecede(instruction, route.returned))
			if mayBeBefore {
				before = append(before, effects...)
			}
			if mayBeAfter {
				after = append(after, effects...)
			}
		}
	}
	return before, after
}

func protocolRecursiveInstructionEffects(instruction ssa.Instruction, target ssa.Value) []string {
	if instruction == nil || target == nil {
		return nil
	}
	var effects []string
	add := func(kind string) {
		if slices.Contains(effects, kind) {
			return
		}
		effects = append(effects, kind)
	}
	switch typed := instruction.(type) {
	case *ssa.DebugRef, *ssa.Jump, *ssa.Phi, *ssa.ChangeType,
		*ssa.ChangeInterface, *ssa.MakeInterface, *ssa.Convert,
		*ssa.FieldAddr, *ssa.IndexAddr, *ssa.Slice, *ssa.Extract:
		return nil
	case *ssa.Store:
		if protocolRecursiveValueMayDerive(typed.Addr, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectMutate)
		}
		if protocolRecursiveValueMayDerive(typed.Val, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectEscape)
		}
		return effects
	case *ssa.MapUpdate:
		if protocolRecursiveValueMayDerive(typed.Map, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectMutate)
		}
		if protocolRecursiveValueMayDerive(typed.Key, target, make(map[ssa.Value]bool)) ||
			protocolRecursiveValueMayDerive(typed.Value, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectEscape)
		}
		return effects
	case *ssa.Send:
		if protocolRecursiveValueMayDerive(typed.X, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectEscape)
		}
		return effects
	case *ssa.MakeClosure:
		if protocolClosureEscapes(typed) {
			for _, binding := range typed.Bindings {
				if protocolRecursiveValueMayDerive(binding, target, make(map[ssa.Value]bool)) {
					add(protocolSummaryEffectEscape)
				}
			}
		}
		return effects
	case *ssa.Return:
		for _, result := range typed.Results {
			if protocolRecursiveValueMayDerive(result, target, make(map[ssa.Value]bool)) {
				add(protocolSummaryEffectEscape)
			}
		}
		return effects
	}
	if call, ok := instruction.(ssa.CallInstruction); ok {
		common := call.Common()
		if common == nil {
			return nil
		}
		if receiver, validates := protocolValidateReceiver(common); validates &&
			protocolRecursiveValueMayDerive(receiver, target, make(map[ssa.Value]bool)) {
			return nil
		}
		for _, argument := range common.Args {
			if !protocolRecursiveValueMayDerive(argument, target, make(map[ssa.Value]bool)) {
				continue
			}
			if protocolValueMayCarryMutableIdentity(argument) {
				add(protocolSummaryEffectEscape)
			} else {
				add(protocolSummaryEffectConsume)
			}
		}
		return effects
	}
	for _, operand := range instruction.Operands(nil) {
		if operand != nil && *operand != nil &&
			protocolRecursiveValueMayDerive(*operand, target, make(map[ssa.Value]bool)) {
			add(protocolSummaryEffectConsume)
			break
		}
	}
	return effects
}

func protocolRecursiveValueMayDerive(candidate, target ssa.Value, seen map[ssa.Value]bool) bool {
	if candidate == nil || target == nil || seen[candidate] {
		return false
	}
	if candidate == target {
		return true
	}
	seen[candidate] = true
	switch typed := candidate.(type) {
	case *ssa.Phi:
		for _, edge := range typed.Edges {
			if protocolRecursiveValueMayDerive(edge, target, seen) {
				return true
			}
		}
	case *ssa.ChangeType:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.ChangeInterface:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.MakeInterface:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.Convert:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.FieldAddr:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.IndexAddr:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.Slice:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.UnOp:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.Field:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.Index:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.Lookup:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	case *ssa.TypeAssert:
		return protocolRecursiveValueMayDerive(typed.X, target, seen)
	}
	return false
}

func protocolInstructionMayPrecede(first, second ssa.Instruction) bool {
	if first == nil || second == nil || first == second || first.Block() == nil || second.Block() == nil {
		return false
	}
	if first.Block() == second.Block() {
		firstIndex := protocolInstructionIndex(first)
		secondIndex := protocolInstructionIndex(second)
		if firstIndex >= 0 && secondIndex >= 0 && firstIndex < secondIndex {
			return true
		}
		return protocolRecursiveBlockReaches(first.Block(), second.Block(), true)
	}
	return protocolRecursiveBlockReaches(first.Block(), second.Block(), false)
}

func protocolInstructionIndex(instruction ssa.Instruction) int {
	if instruction == nil || instruction.Block() == nil {
		return -1
	}
	for index, candidate := range instruction.Block().Instrs {
		if candidate == instruction {
			return index
		}
	}
	return -1
}

func protocolRecursiveBlockReaches(from, to *ssa.BasicBlock, requireEdge bool) bool {
	if from == nil || to == nil {
		return false
	}
	if from == to && !requireEdge {
		return true
	}
	queue := append([]*ssa.BasicBlock(nil), from.Succs...)
	seen := map[*ssa.BasicBlock]bool{from: true}
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == to {
			return true
		}
		if seen[block] {
			continue
		}
		seen[block] = true
		queue = append(queue, block.Succs...)
	}
	return false
}

func protocolSSAValueForCalleeSlot(function *ssa.Function, slot calleeTargetSlot) ssa.Value {
	if function == nil {
		return nil
	}
	parameterIndex := slot.argIndex
	if slot.kind == calleeTargetSlotReceiver {
		parameterIndex = 0
	} else if function.Signature != nil && function.Signature.Recv() != nil {
		parameterIndex++
	}
	if parameterIndex < 0 || parameterIndex >= len(function.Params) {
		return nil
	}
	return function.Params[parameterIndex]
}

func protocolCalleeSlotForSSAArgument(function *ssa.Function, argumentIndex int) (calleeTargetSlot, bool) {
	if function == nil || argumentIndex < 0 || argumentIndex >= len(function.Params) {
		return calleeTargetSlot{}, false
	}
	if function.Signature != nil && function.Signature.Recv() != nil {
		if argumentIndex == 0 {
			return calleeTargetSlot{kind: calleeTargetSlotReceiver}, true
		}
		argumentIndex--
	}
	return calleeTargetSlot{kind: calleeTargetSlotArg, argIndex: argumentIndex}, true
}

func protocolRecursiveSummaryKey(function *ssa.Function, slot calleeTargetSlot) string {
	return fmt.Sprintf("%s|%s", protocolProcedureKey(function), slot.cacheKey())
}
