// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

type constructorSSAReturnObligation struct {
	Position   token.Pos
	ObjectKey  string
	Identity   protocolIdentity
	Pointer    bool
	Value      ssa.Value
	Resolution protocolAliasResolution
}

type constructorSSAIdentityModel struct {
	pass              *analysis.Pass
	function          *ssa.Function
	interner          *protocolIdentityInterner
	aliases           *protocolAliasAnalysis
	returnsByPosition map[token.Pos][]constructorSSAReturnObligation
	returnErrors      map[token.Pos]protocolErrorResult
	uncertainReturns  map[token.Pos]protocolAliasResolution
}

func constructorSSAReturnErrorResult(
	function *ssa.Function,
	returned *ssa.Return,
) protocolErrorResult {
	if function == nil || function.Signature == nil || function.Signature.Results() == nil || returned == nil {
		return protocolErrorResultUnknown
	}
	foundError := false
	result := protocolErrorResultNil
	for slot, value := range returned.Results {
		if slot >= function.Signature.Results().Len() ||
			!isErrorType(function.Signature.Results().At(slot).Type()) {
			continue
		}
		foundError = true
		relation := constructorSSAErrorResultAtReturn(function, returned, value)
		if relation == protocolErrorResultNonNil {
			return relation
		}
		if relation == protocolErrorResultUnknown {
			result = relation
		}
	}
	if !foundError {
		return protocolErrorResultNil
	}
	return result
}

func constructorSSAErrorResultAtReturn(
	function *ssa.Function,
	returned *ssa.Return,
	value ssa.Value,
) protocolErrorResult {
	if relation := constructorStaticSSAErrorResult(value, make(map[ssa.Value]bool)); relation != protocolErrorResultUnknown {
		return relation
	}
	if function == nil || returned == nil || returned.Block() == nil || value == nil {
		return protocolErrorResultUnknown
	}

	result := protocolErrorResultUnknown
	for _, block := range function.Blocks {
		if block == nil || len(block.Instrs) == 0 || len(block.Succs) != 2 ||
			block.Succs[0] == nil || block.Succs[1] == nil {
			continue
		}
		branch, ok := block.Instrs[len(block.Instrs)-1].(*ssa.If)
		if !ok {
			continue
		}
		trueResult, falseResult, matches := protocolNilCondition(branch.Cond, value)
		if !matches {
			continue
		}
		trueDominates := block.Succs[0].Dominates(returned.Block())
		falseDominates := block.Succs[1].Dominates(returned.Block())
		if trueDominates == falseDominates {
			continue
		}
		candidate := falseResult
		if trueDominates {
			candidate = trueResult
		}
		if result != protocolErrorResultUnknown && result != candidate {
			return protocolErrorResultUnknown
		}
		result = candidate
	}
	return result
}

func constructorStaticSSAErrorResult(
	value ssa.Value,
	seen map[ssa.Value]bool,
) protocolErrorResult {
	if value == nil || seen[value] {
		return protocolErrorResultUnknown
	}
	seen[value] = true
	switch typed := value.(type) {
	case *ssa.Const:
		if typed.IsNil() {
			return protocolErrorResultNil
		}
		return protocolErrorResultUnknown
	case *ssa.MakeInterface:
		return protocolErrorResultNonNil
	case *ssa.ChangeInterface:
		return constructorStaticSSAErrorResult(typed.X, seen)
	case *ssa.ChangeType:
		return constructorStaticSSAErrorResult(typed.X, seen)
	case *ssa.Phi:
		result := protocolErrorResultUnknown
		for _, edge := range typed.Edges {
			edgeResult := constructorStaticSSAErrorResult(edge, maps.Clone(seen))
			if edgeResult == protocolErrorResultUnknown {
				return protocolErrorResultUnknown
			}
			if result != protocolErrorResultUnknown && result != edgeResult {
				return protocolErrorResultUnknown
			}
			result = edgeResult
		}
		return result
	case *ssa.Call:
		return constructorKnownCallErrorResult(typed.Common())
	default:
		return protocolErrorResultUnknown
	}
}

func constructorKnownCallErrorResult(common *ssa.CallCommon) protocolErrorResult {
	if common == nil {
		return protocolErrorResultUnknown
	}
	callee := common.StaticCallee()
	if callee == nil {
		return protocolErrorResultUnknown
	}
	function, ok := callee.Object().(*types.Func)
	if !ok || function.Pkg() == nil {
		return protocolErrorResultUnknown
	}
	if function.Pkg().Path() == "errors" && function.Name() == "New" ||
		function.Pkg().Path() == "fmt" && function.Name() == "Errorf" {
		return protocolErrorResultNonNil
	}
	return protocolErrorResultUnknown
}

func buildConstructorSSAIdentityModel(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	declaration *ast.FuncDecl,
	resultSlot int,
) (constructorSSAIdentityModel, ssaAvailability) {
	if pass == nil || declaration == nil || declaration.Name == nil {
		return constructorSSAIdentityModel{}, ssaAvailability{Status: ssaAvailabilityMissingFunction}
	}
	functionObject, _ := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
	resolution := resolveSSAFunction(ssaRes, functionObject)
	if !resolution.Availability.ready() {
		return constructorSSAIdentityModel{}, resolution.Availability
	}
	function := resolution.Function
	if function == nil || function.Signature == nil || function.Signature.Results() == nil ||
		resultSlot < 0 || resultSlot >= function.Signature.Results().Len() {
		return constructorSSAIdentityModel{}, ssaAvailability{Status: ssaAvailabilityUnsupportedInstruction}
	}

	interner := newProtocolIdentityInterner()
	model := constructorSSAIdentityModel{
		pass:              pass,
		function:          function,
		interner:          interner,
		aliases:           analyzeProtocolAliases(function, interner),
		returnsByPosition: make(map[token.Pos][]constructorSSAReturnObligation),
		returnErrors:      make(map[token.Pos]protocolErrorResult),
		uncertainReturns:  make(map[token.Pos]protocolAliasResolution),
	}
	for _, block := range function.Blocks {
		for _, instruction := range block.Instrs {
			returned, ok := instruction.(*ssa.Return)
			if !ok || resultSlot >= len(returned.Results) {
				continue
			}
			errorResult := constructorSSAReturnErrorResult(function, returned)
			if previous, exists := model.returnErrors[returned.Pos()]; exists {
				errorResult = joinProtocolErrorResults(previous, errorResult)
			}
			model.returnErrors[returned.Pos()] = errorResult
			if errorResult == protocolErrorResultNonNil {
				continue
			}
			value := returned.Results[resultSlot]
			if constant, constantOK := value.(*ssa.Const); constantOK && constant.IsNil() {
				continue
			}
			identity, aliasResolution := model.aliases.resolveBefore(returned, value)
			if constant, ok := value.(*ssa.Const); ok && !constant.IsNil() {
				// SSA does not emit an instruction for immutable constants, so the
				// alias dataflow has no binding to resolve. The constant value itself
				// is nevertheless an exact identity for value-return obligations.
				identity = interner.internValue(constant)
				aliasResolution = protocolAliasMust
			}
			if loaded, ok := value.(*ssa.UnOp); ok && loaded.Op == token.MUL {
				if !ssaValueHasPointerType(value) {
					identity = interner.internValue(loaded.X)
					aliasResolution = protocolAliasMust
				} else if pointee, resolution := model.aliases.resolvePointeeBefore(returned, loaded.X); resolution != protocolAliasUnknown {
					identity, aliasResolution = pointee, resolution
				}
			}
			if aliasResolution != protocolAliasMust {
				model.uncertainReturns[returned.Pos()] = aliasResolution
				continue
			}
			descriptor, ok := interner.descriptor(identity)
			if !ok || descriptor.ObjectKey == "" {
				model.uncertainReturns[returned.Pos()] = protocolAliasUnknown
				continue
			}
			model.returnsByPosition[returned.Pos()] = append(
				model.returnsByPosition[returned.Pos()],
				constructorSSAReturnObligation{
					Position:   returned.Pos(),
					ObjectKey:  descriptor.ObjectKey,
					Identity:   identity,
					Pointer:    ssaValueHasPointerType(value),
					Value:      value,
					Resolution: protocolAliasMust,
				},
			)
		}
	}
	// SSA emits a synthetic no-position return for the shared defer epilogue.
	// Explicit source returns own the constructor obligations; deferred effects are
	// modeled at those exits by constructorDeferredPlanner.
	if _, syntheticUncertain := model.uncertainReturns[token.NoPos]; syntheticUncertain &&
		len(model.returnsByPosition) > 0 {
		delete(model.uncertainReturns, token.NoPos)
	}
	return model, resolution.Availability
}

func (model constructorSSAIdentityModel) targetForObject(objectKey string) (castTarget, bool) {
	if objectKey == "" || model.function == nil || model.aliases == nil {
		return castTarget{}, false
	}
	for _, obligations := range model.returnsByPosition {
		for _, obligation := range obligations {
			if obligation.ObjectKey != objectKey || obligation.Identity == 0 {
				continue
			}
			matcher := newSSAFlowAliasMatcherForIdentity(
				model.pass,
				model.function,
				model.aliases,
				obligation.Identity,
				obligation.Pointer,
				obligation.Value,
			)
			if matcher == nil {
				return castTarget{}, false
			}
			return castTarget{
				displayName: objectKey,
				targetKey:   objectKey,
				flowAliases: matcher,
			}, true
		}
	}
	return castTarget{}, false
}

func (model constructorSSAIdentityModel) returnPositionHasObject(position token.Pos, objectKey string) bool {
	for _, obligation := range model.returnsByPosition[position] {
		if obligation.ObjectKey == objectKey && obligation.Resolution == protocolAliasMust {
			return true
		}
	}
	return false
}

func (model constructorSSAIdentityModel) returnErrorResult(position token.Pos) protocolErrorResult {
	result, ok := model.returnErrors[position]
	if !ok {
		return protocolErrorResultUnknown
	}
	return result
}

func (model constructorSSAIdentityModel) delegatedCallForObject(
	objectKey string,
) (ssa.CallInstruction, int, bool) {
	for _, obligations := range model.returnsByPosition {
		for _, obligation := range obligations {
			if obligation.ObjectKey != objectKey || obligation.Value == nil {
				continue
			}
			switch value := obligation.Value.(type) {
			case *ssa.Extract:
				call, ok := value.Tuple.(ssa.CallInstruction)
				return call, value.Index, ok
			case ssa.CallInstruction:
				return value, 0, true
			}
		}
	}
	return nil, 0, false
}

func (model constructorSSAIdentityModel) returnObjectKeys() []string {
	unique := make(map[string]struct{})
	for _, obligations := range model.returnsByPosition {
		for _, obligation := range obligations {
			if obligation.ObjectKey != "" {
				unique[obligation.ObjectKey] = struct{}{}
			}
		}
	}
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
