// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

type (
	protocolValidationInvocation struct {
		Call               ssa.CallInstruction
		Receiver           ssa.Value
		Result             ssa.Value
		OnSuccessfulReturn bool
		Relation           protocolValidationRelation
		CallSite           string
		SummaryProvenance  string
		ReceiverExpr       ast.Expr
	}

	protocolValidationEdgeEffect struct {
		From       *ssa.BasicBlock
		To         *ssa.BasicBlock
		Effect     protocolConditionalEffect
		Result     protocolErrorResult
		Invocation protocolValidationInvocation
	}
)

func collectProtocolValidationInvocations(
	fn *ssa.Function,
	interner *protocolIdentityInterner,
) []protocolValidationInvocation {
	if fn == nil || interner == nil {
		return nil
	}
	invocations := make([]protocolValidationInvocation, 0)
	for _, block := range fn.Blocks {
		for _, instruction := range block.Instrs {
			call, ok := instruction.(ssa.CallInstruction)
			if !ok {
				continue
			}
			receiver, ok := protocolValidateReceiver(call.Common())
			if !ok {
				continue
			}
			result, ok := call.(ssa.Value)
			if !ok {
				continue
			}
			relation := protocolValidationRelation{
				ReceiverIdentity: interner.internValue(receiver),
				ErrorIdentity:    interner.internValue(result),
			}
			invocations = append(invocations, protocolValidationInvocation{
				Call:     call,
				Receiver: receiver,
				Result:   result,
				Relation: relation,
			})
		}
	}
	return invocations
}

func collectProtocolValidationEdgeEffects(
	fn *ssa.Function,
	invocations []protocolValidationInvocation,
) []protocolValidationEdgeEffect {
	if fn == nil || len(invocations) == 0 {
		return nil
	}
	invocationByResult := make(map[ssa.Value]protocolValidationInvocation, len(invocations))
	for _, invocation := range invocations {
		invocationByResult[invocation.Result] = invocation
	}

	effects := make([]protocolValidationEdgeEffect, 0)
	for _, block := range fn.Blocks {
		if len(block.Instrs) == 0 || len(block.Succs) != 2 {
			continue
		}
		branch, ok := block.Instrs[len(block.Instrs)-1].(*ssa.If)
		if !ok {
			continue
		}
		for result, invocation := range invocationByResult {
			trueResult, falseResult, matches := protocolNilCondition(branch.Cond, result)
			if !matches {
				continue
			}
			effect := protocolConditionalEffect{
				Kind:     protocolEffectValidate,
				Relation: invocation.Relation,
			}
			effects = append(effects,
				protocolValidationEdgeEffect{
					From: block, To: block.Succs[0], Effect: effect, Result: trueResult, Invocation: invocation,
				},
				protocolValidationEdgeEffect{
					From: block, To: block.Succs[1], Effect: effect, Result: falseResult, Invocation: invocation,
				},
			)
		}
	}
	return effects
}

func protocolValidateReceiver(common *ssa.CallCommon) (ssa.Value, bool) {
	if common == nil {
		return nil, false
	}
	if common.IsInvoke() {
		if !protocolValidateSignature(common.Method) || common.Value == nil {
			return nil, false
		}
		return common.Value, true
	}
	callee := common.StaticCallee()
	if receiver, ok := protocolBoundValidateReceiver(common, callee); ok {
		return receiver, true
	}
	if callee == nil || callee.Signature == nil || callee.Signature.Recv() == nil || len(common.Args) == 0 {
		return nil, false
	}
	object, ok := callee.Object().(*types.Func)
	if !ok || object.Name() != validateMethodName || !protocolValidateSignature(object) {
		return nil, false
	}
	return common.Args[0], true
}

func protocolValidateSignature(fn *types.Func) bool {
	if fn == nil || fn.Name() != validateMethodName || !fn.Exported() {
		return false
	}
	signature, ok := fn.Type().(*types.Signature)
	return ok && protocolValidateCallSignature(signature)
}

func protocolValidateCallSignature(signature *types.Signature) bool {
	if signature == nil || signature.Params().Len() != 0 || signature.Results().Len() != 1 {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	return types.Identical(signature.Results().At(0).Type(), errorType)
}

func protocolBoundValidateReceiver(common *ssa.CallCommon, callee *ssa.Function) (ssa.Value, bool) {
	if common == nil || callee == nil || callee.Signature == nil ||
		!strings.HasSuffix(callee.Name(), validateMethodName+"$bound") ||
		!protocolValidateCallSignature(callee.Signature) {
		return nil, false
	}
	closure, ok := common.Value.(*ssa.MakeClosure)
	if !ok || len(closure.Bindings) != 1 {
		return nil, false
	}
	return closure.Bindings[0], true
}

func protocolNilCondition(condition, result ssa.Value) (protocolErrorResult, protocolErrorResult, bool) {
	if negation, ok := condition.(*ssa.UnOp); ok && negation.Op == token.NOT {
		trueResult, falseResult, matches := protocolNilCondition(negation.X, result)
		return falseResult, trueResult, matches
	}
	comparison, ok := condition.(*ssa.BinOp)
	if !ok || (comparison.Op != token.EQL && comparison.Op != token.NEQ) {
		return protocolErrorResultUnknown, protocolErrorResultUnknown, false
	}
	if !protocolComparesValueWithNil(comparison.X, comparison.Y, result) &&
		!protocolComparesValueWithNil(comparison.Y, comparison.X, result) {
		return protocolErrorResultUnknown, protocolErrorResultUnknown, false
	}
	if comparison.Op == token.EQL {
		return protocolErrorResultNil, protocolErrorResultNonNil, true
	}
	return protocolErrorResultNonNil, protocolErrorResultNil, true
}

func protocolComparesValueWithNil(candidate, nilCandidate, result ssa.Value) bool {
	if candidate != result {
		return false
	}
	constant, ok := nilCandidate.(*ssa.Const)
	return ok && constant.IsNil()
}

func protocolSSAAvailabilityForDecl(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	declaration *ast.FuncDecl,
) ssaAvailability {
	if pass == nil || declaration == nil || declaration.Name == nil || declaration.Body == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	object, _ := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	return resolveSSAFunction(ssaRes, object).Availability
}

func protocolSSAAvailabilityForClosure(ssaRes *ssaResult, literal *ast.FuncLit) ssaAvailability {
	if literal == nil || literal.Body == nil {
		return ssaAvailability{Status: ssaAvailabilityMissingClosure}
	}
	return resolveSSAClosure(ssaRes, literal.Pos()).Availability
}

func collectSynchronousClosureValidationCalls(calls closureVarCallSet) methodValueValidateCallSet {
	validated := make(methodValueValidateCallSet)
	for call, literal := range calls {
		if call == nil || literal == nil || literal.Body == nil || len(literal.Body.List) != 1 {
			continue
		}
		returned, ok := literal.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returned.Results) != 1 {
			continue
		}
		validationCall, ok := stripParens(returned.Results[0]).(*ast.CallExpr)
		if !ok {
			continue
		}
		selector, ok := stripParens(validationCall.Fun).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != validateMethodName {
			continue
		}
		validated[call] = methodValueValidationCall{receiver: selector.X}
	}
	if len(validated) == 0 {
		return nil
	}
	return validated
}

func protocolInvocationHasCheckedSuccess(
	fn *ssa.Function,
	invocation protocolValidationInvocation,
	effects []protocolValidationEdgeEffect,
) bool {
	if protocolInvocationIsDirectlyReturned(invocation) {
		return true
	}
	for resultSlot := range fn.Signature.Results().Len() {
		if protocolFunctionAlwaysReturnsValueFromSlot(fn, invocation.Result, resultSlot) {
			return true
		}
	}
	var nilBlocks []*ssa.BasicBlock
	var nonNilBlocks []*ssa.BasicBlock
	conventionalFailureGuard := true
	invertedReturningGuard := true
	for _, effect := range effects {
		if effect.Effect.Relation != invocation.Relation {
			continue
		}
		if effect.From == nil || len(effect.From.Succs) != 2 {
			return false
		}
		switch effect.Result {
		case protocolErrorResultNil:
			conventionalFailureGuard = conventionalFailureGuard && effect.To == effect.From.Succs[1]
			invertedReturningGuard = invertedReturningGuard && effect.To == effect.From.Succs[0]
			nilBlocks = append(nilBlocks, effect.To)
		case protocolErrorResultNonNil:
			conventionalFailureGuard = conventionalFailureGuard && effect.To == effect.From.Succs[0]
			invertedReturningGuard = invertedReturningGuard && effect.To == effect.From.Succs[1]
			nonNilBlocks = append(nonNilBlocks, effect.To)
		case protocolErrorResultUnknown:
			return false
		}
	}
	if len(nilBlocks) == 0 || len(nonNilBlocks) == 0 {
		return false
	}
	if !conventionalFailureGuard &&
		(!invertedReturningGuard || !protocolRegionOnlyReturnsValidationResult(nonNilBlocks, invocation.Result)) {
		return false
	}
	guardBlock := invocation.Call.Block()
	nilReachable := protocolReachableSSABlocksBeforeGuardReentry(nilBlocks, guardBlock)
	for block := range protocolReachableSSABlocksBeforeGuardReentry(nonNilBlocks, guardBlock) {
		if nilReachable[block] {
			return false
		}
	}
	return true
}

func protocolInvocationIsDirectlyReturned(invocation protocolValidationInvocation) bool {
	if invocation.Call == nil || invocation.Result == nil || invocation.Call.Block() == nil {
		return false
	}
	for _, instruction := range invocation.Call.Block().Instrs {
		returned, ok := instruction.(*ssa.Return)
		if !ok {
			continue
		}
		for _, result := range returned.Results {
			if protocolValueDerivedFrom(result, invocation.Result, make(map[ssa.Value]bool)) {
				return true
			}
		}
	}
	return false
}

func protocolReachableSSABlocksBeforeGuardReentry(
	starts []*ssa.BasicBlock,
	guard *ssa.BasicBlock,
) map[*ssa.BasicBlock]bool {
	reachable := make(map[*ssa.BasicBlock]bool)
	queue := append([]*ssa.BasicBlock(nil), starts...)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || reachable[block] {
			continue
		}
		// A successor that dominates the validation guard is a loop re-entry,
		// not a continuation of the current iteration. Do not let a failure
		// edge reaching the next invocation overlap this iteration's nil edge.
		if guard != nil && block != guard && block.Dominates(guard) {
			continue
		}
		reachable[block] = true
		queue = append(queue, block.Succs...)
	}
	return reachable
}

func protocolRegionOnlyReturnsValidationResult(starts []*ssa.BasicBlock, validationResult ssa.Value) bool {
	if validationResult == nil {
		return false
	}
	seen := make(map[*ssa.BasicBlock]bool)
	queue := append([]*ssa.BasicBlock(nil), starts...)
	foundReturn := false
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || seen[block] {
			continue
		}
		seen[block] = true
		for _, instruction := range block.Instrs {
			switch typed := instruction.(type) {
			case *ssa.DebugRef, *ssa.Jump, *ssa.If:
				continue
			case *ssa.Return:
				returnsValidationResult := false
				for _, result := range typed.Results {
					if protocolValueDerivedFrom(result, validationResult, make(map[ssa.Value]bool)) {
						returnsValidationResult = true
						continue
					}
					constant, ok := result.(*ssa.Const)
					if !ok || !constant.IsNil() {
						return false
					}
				}
				if !returnsValidationResult {
					return false
				}
				foundReturn = true
			default:
				value, ok := instruction.(ssa.Value)
				if !ok || !protocolValueDerivedFrom(value, validationResult, make(map[ssa.Value]bool)) {
					return false
				}
			}
		}
		queue = append(queue, block.Succs...)
	}
	return foundReturn
}
