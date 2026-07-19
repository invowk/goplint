// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ssaAliasSet holds additional objectKeys that must-alias the primary cast
// target variable. When y := x and x = Cast(raw), validating y via
// y.Validate() discharges x's validation requirement.
type ssaAliasSet map[string]bool

// castTarget identifies the LHS target of a cast assignment.
// It stores a canonical key so equivalent selector forms (for example
// (*cfg).Name and cfg.Name) match consistently.
type castTarget struct {
	displayName      string
	targetKey        string
	typeKey          string
	staticType       types.Type
	dynamicIndexBase string
	aliasKeys        ssaAliasSet // nil when SSA alias tracking is off
	originExpr       ast.Expr
	flowAliases      *ssaFlowAliasMatcher
}

func castTargetFromExpr(pass *analysis.Pass, expr ast.Expr) (castTarget, bool) {
	key := targetKeyForExpr(pass, expr)
	if key == "" {
		return castTarget{}, false
	}
	display := exprStringKey(expr)
	if ident, ok := stripParensAndStar(expr).(*ast.Ident); ok {
		display = ident.Name
	}
	target := castTarget{
		displayName: display,
		targetKey:   key,
		staticType:  pass.TypesInfo.TypeOf(expr),
		originExpr:  stripParensAndStar(expr),
	}
	target.dynamicIndexBase = dynamicIndexBaseKey(pass, expr)
	return target, true
}

func (t castTarget) key() string {
	return t.targetKey
}

func (t castTarget) matchesExpr(pass *analysis.Pass, expr ast.Expr) bool {
	if t.targetKey == "" {
		return false
	}
	key := targetKeyForExpr(pass, expr)
	if key == t.targetKey {
		if t.flowAliases != nil {
			return t.flowAliases.matches(pass, expr)
		}
		return true
	}
	if !t.expressionTypeMayAlias(pass, expr) {
		return false
	}
	if t.flowAliases != nil {
		return t.flowAliases.matches(pass, expr)
	}
	// SSA alias set enrichment: check if expr's objectKey is a known alias.
	if len(t.aliasKeys) > 0 && key != "" {
		return t.aliasKeys[key]
	}
	return false
}

func (t castTarget) aliasResolution(pass *analysis.Pass, expr ast.Expr) protocolAliasResolution {
	if t.targetKey == "" || expr == nil {
		return protocolAliasUnknown
	}
	if targetKeyForExpr(pass, expr) == t.targetKey {
		if t.flowAliases != nil {
			return t.flowAliases.resolution(pass, expr)
		}
		return protocolAliasMust
	}
	if !t.expressionTypeMayAlias(pass, expr) {
		return protocolAliasUnknown
	}
	if t.dynamicIndexBase != "" && dynamicIndexCandidateMayAlias(pass, expr, t.dynamicIndexBase) {
		return protocolAliasAmbiguous
	}
	if t.flowAliases == nil {
		return protocolAliasUnknown
	}
	return t.flowAliases.resolution(pass, expr)
}

// expressionTypeMayAlias rejects flow-sensitive alias candidates whose static
// Go types cannot carry the tracked value. Interface conversions remain
// eligible whenever either type is assignable to the other, so values routed
// through any or another implemented interface are still analyzed.
func (t castTarget) expressionTypeMayAlias(pass *analysis.Pass, expr ast.Expr) bool {
	if pass == nil || pass.TypesInfo == nil || expr == nil || t.staticType == nil {
		return true
	}
	candidateType := pass.TypesInfo.TypeOf(expr)
	if candidateType == nil {
		return true
	}
	if typeIdentityKey(t.staticType) == typeIdentityKey(candidateType) {
		return true
	}
	if types.AssignableTo(t.staticType, candidateType) || types.AssignableTo(candidateType, t.staticType) {
		return true
	}
	trackedPointer := types.NewPointer(t.staticType)
	candidatePointer := types.NewPointer(candidateType)
	return types.AssignableTo(trackedPointer, candidateType) ||
		types.AssignableTo(candidateType, trackedPointer) ||
		types.AssignableTo(candidatePointer, t.staticType) ||
		types.AssignableTo(t.staticType, candidatePointer)
}

func objectForIdent(pass *analysis.Pass, ident *ast.Ident) types.Object {
	if pass == nil || pass.TypesInfo == nil || ident == nil {
		return nil
	}
	if obj := pass.TypesInfo.Uses[ident]; obj != nil {
		return obj
	}
	if obj := pass.TypesInfo.Defs[ident]; obj != nil {
		return obj
	}
	return nil
}

func targetKeyForExpr(pass *analysis.Pass, expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	expr = stripParensAndStar(expr)
	switch e := expr.(type) {
	case *ast.Ident:
		if e.Name == "_" {
			return ""
		}
		if obj := objectForIdent(pass, e); obj != nil {
			return objectKeyAt(pass, obj)
		}
		return ""
	case *ast.SelectorExpr:
		base := targetKeyForExpr(pass, e.X)
		if base == "" {
			return ""
		}
		return base + "." + e.Sel.Name
	case *ast.IndexExpr:
		base := targetKeyForExpr(pass, e.X)
		if base == "" {
			return ""
		}
		index, isStatic := canonicalStaticIndexExprKey(pass, e.Index)
		if !isStatic {
			return fmt.Sprintf("%s[dynamic@%s]", base, semanticNodeKey(pass, e.Pos()))
		}
		return base + "[" + index + "]"
	case *ast.IndexListExpr:
		base := targetKeyForExpr(pass, e.X)
		if base == "" {
			return ""
		}
		indexes := make([]string, 0, len(e.Indices))
		for _, idx := range e.Indices {
			key := canonicalIndexExprKey(idx)
			if key == "" {
				return ""
			}
			indexes = append(indexes, key)
		}
		return base + "[" + strings.Join(indexes, ",") + "]"
	}

	// Protocol identities must be anchored in go/types objects or canonical
	// SSA values. A formatted AST expression is not stable under harmless
	// syntax changes and can alias unrelated allocations, so unsupported
	// expressions deliberately remain unresolved.
	return ""
}

func objectKey(obj types.Object) string {
	if obj == nil {
		return ""
	}
	return semanticObjectKey(obj)
}

// objectKeyAt adds a source-local semantic declaration anchor when an object is
// not package-scoped. Package objects are already unique by full import path,
// kind, name, and (for methods) receiver. Local names may be shadowed, so their
// declaration node distinguishes the binding without admitting raw token.Pos
// or file-set ordering into protocol identities.
func objectKeyAt(pass *analysis.Pass, obj types.Object) string {
	key := objectKey(obj)
	if key == "" || pass == nil || obj == nil || !obj.Pos().IsValid() {
		return key
	}
	if pkg := obj.Pkg(); pkg != nil && obj.Parent() == pkg.Scope() {
		return key
	}
	return key + "|declaration|" + semanticNodeKey(pass, obj.Pos())
}

func objectKind(obj types.Object) string {
	switch obj.(type) {
	case *types.Var:
		return "var"
	case *types.Const:
		return "const"
	case *types.Func:
		return "func"
	case *types.TypeName:
		return "type"
	case *types.Label:
		return "label"
	case *types.PkgName:
		return "pkg"
	case *types.Builtin:
		return "builtin"
	case *types.Nil:
		return "nil"
	default:
		return "obj"
	}
}

func canonicalStaticIndexExprKey(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return "", false
	}
	value := pass.TypesInfo.Types[stripParens(expr)].Value
	if value == nil {
		return "", false
	}
	return value.ExactString(), true
}

func canonicalIndexExprKey(expr ast.Expr) string {
	return exprStringKey(stripParens(expr))
}

func dynamicIndexBaseKey(pass *analysis.Pass, expr ast.Expr) string {
	indexed, ok := stripParensAndStar(expr).(*ast.IndexExpr)
	if !ok {
		return ""
	}
	if _, isStatic := canonicalStaticIndexExprKey(pass, indexed.Index); isStatic {
		return ""
	}
	return targetKeyForExpr(pass, indexed.X)
}

func dynamicIndexCandidateMayAlias(pass *analysis.Pass, expr ast.Expr, baseKey string) bool {
	indexed, ok := stripParensAndStar(expr).(*ast.IndexExpr)
	if !ok {
		return false
	}
	return targetKeyForExpr(pass, indexed.X) == baseKey
}

func stripParens(expr ast.Expr) ast.Expr {
	for {
		paren, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = paren.X
	}
}

func stripParensAndStar(expr ast.Expr) ast.Expr {
	for {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.StarExpr:
			expr = e.X
		case *ast.UnaryExpr:
			// Receivers like (&x).Validate() should match casts assigned to x.
			if e.Op != token.AND {
				return expr
			}
			expr = e.X
		default:
			return expr
		}
	}
}

func exprStringKey(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	return types.ExprString(expr)
}
