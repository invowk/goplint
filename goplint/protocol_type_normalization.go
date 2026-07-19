// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type (
	protocolCallResult struct {
		Slot int
		Type types.Type
	}

	protocolNormalizedCall struct {
		Function      *types.Func
		Signature     *types.Signature
		Results       []protocolCallResult
		TrailingError bool
	}
)

func normalizeProtocolCall(pass *analysis.Pass, call *ast.CallExpr) (protocolNormalizedCall, bool) {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return protocolNormalizedCall{}, false
	}
	object, ok := protocolCalledFunction(pass.TypesInfo, call.Fun)
	if !ok {
		return protocolNormalizedCall{}, false
	}
	callType := pass.TypesInfo.TypeOf(call.Fun)
	if callType == nil {
		return protocolNormalizedCall{}, false
	}
	signature, ok := types.Unalias(callType).(*types.Signature)
	if !ok {
		return protocolNormalizedCall{}, false
	}
	normalized := protocolNormalizedCall{
		Function:  object,
		Signature: signature,
		Results:   make([]protocolCallResult, 0, signature.Results().Len()),
	}
	for slot := range signature.Results().Len() {
		normalized.Results = append(normalized.Results, protocolCallResult{Slot: slot, Type: signature.Results().At(slot).Type()})
	}
	if len(normalized.Results) > 0 {
		normalized.TrailingError = isErrorType(normalized.Results[len(normalized.Results)-1].Type)
	}
	return normalized, true
}

func protocolCalledFunction(info *types.Info, expression ast.Expr) (*types.Func, bool) {
	if info == nil {
		return nil, false
	}
	switch typed := expression.(type) {
	case *ast.IndexExpr:
		return protocolCalledFunction(info, typed.X)
	case *ast.IndexListExpr:
		return protocolCalledFunction(info, typed.X)
	case *ast.ParenExpr:
		return protocolCalledFunction(info, typed.X)
	case *ast.SelectorExpr:
		object, ok := info.ObjectOf(typed.Sel).(*types.Func)
		return object, ok
	case *ast.Ident:
		object, ok := info.ObjectOf(typed).(*types.Func)
		return object, ok
	default:
		return nil, false
	}
}

func resolveProtocolValidateMethod(t types.Type) (*types.Func, bool) {
	if t == nil {
		return nil, false
	}
	t = types.Unalias(t)
	methodSets := []*types.MethodSet{types.NewMethodSet(t)}
	if named, ok := t.(*types.Named); ok {
		methodSets = append(methodSets, types.NewMethodSet(types.NewPointer(named)))
	}
	seen := make(map[*types.Func]bool)
	for _, methodSet := range methodSets {
		for selection := range methodSet.Methods() {
			method, ok := selection.Obj().(*types.Func)
			if !ok || seen[method] {
				continue
			}
			seen[method] = true
			if protocolValidateSignature(method) {
				return method, true
			}
		}
	}
	return nil, false
}
