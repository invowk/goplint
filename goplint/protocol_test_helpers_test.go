// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

func containsValidateCall(node ast.Node, varName string, syncLits map[*ast.FuncLit]bool) bool {
	matcher := func(_ *analysis.Pass, receiver ast.Expr) bool {
		identifier, ok := stripParensAndStar(receiver).(*ast.Ident)
		return ok && identifier.Name == varName
	}
	return containsValidateOnReceiver(nil, node, matcher, syncLits, nil, nil)
}

func isVarUse(node ast.Node, varName string) bool {
	pass, target := bindTestTargetForNodes(varName, node)
	return isVarUseTarget(pass, node, target, nil, nil, nil)
}

func bindTestTargetForNodes(name string, nodes ...ast.Node) (*analysis.Pass, castTarget) {
	object := types.NewVar(0, nil, name, types.Typ[types.Int])
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	for _, node := range nodes {
		ast.Inspect(node, func(current ast.Node) bool {
			identifier, ok := current.(*ast.Ident)
			if ok && identifier.Name == name {
				info.Uses[identifier] = object
			}
			return true
		})
	}
	return &analysis.Pass{TypesInfo: info}, castTarget{
		displayName: name,
		targetKey:   objectKey(object),
	}
}
