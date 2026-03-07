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
	displayName string
	targetKey   string
	aliasKeys   ssaAliasSet // nil when SSA alias tracking is off
}

func newCastTargetFromName(name string) castTarget {
	return castTarget{
		displayName: name,
		targetKey:   "name:" + name,
	}
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
	return castTarget{
		displayName: display,
		targetKey:   key,
	}, true
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
		return true
	}
	// SSA alias set enrichment: check if expr's objectKey is a known alias.
	if len(t.aliasKeys) > 0 && key != "" {
		return t.aliasKeys[key]
	}
	return false
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
			return objectKey(obj)
		}
		return "name:" + e.Name
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
		index := canonicalIndexExprKey(e.Index)
		if index == "" {
			return ""
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

	key := exprStringKey(expr)
	if key == "" {
		return ""
	}
	return "expr:" + key
}

func objectKey(obj types.Object) string {
	if obj == nil {
		return ""
	}
	pkgPath := ""
	if pkg := obj.Pkg(); pkg != nil {
		pkgPath = pkg.Path()
	}
	return fmt.Sprintf("obj:%s:%s:%s:%d", objectKind(obj), pkgPath, obj.Name(), obj.Pos())
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

func canonicalIndexExprKey(expr ast.Expr) string {
	return exprStringKey(stripParens(expr))
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
