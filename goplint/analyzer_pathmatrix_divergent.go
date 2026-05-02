// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// inspectPathmatrixDivergent flags the test-side counterpart of the
// cross-platform-path bug class: pathmatrix.PassRelative used on a
// platform-divergent input constant without a matching OnWindows
// override on the surrounding Expectations literal.
//
// Background: pathmatrix.PassRelative encodes a single host-relative
// expectation across every platform. Three of the canonical seven-vector
// inputs — InputUNC ("\\server\share"), InputWindowsDriveAbs
// ("C:\absolute\path"), and InputWindowsRooted ("\absolute\path") —
// have absoluteness that diverges by platform. When a resolver's
// contract is "pass-through if filepath.IsAbs, else join", PassRelative
// is wrong on Windows for these three inputs because filepath.IsAbs
// returns true (UNC, WindowsDriveAbs) or false (WindowsRooted) per
// platform. The result is that the test passes on Linux/macOS but fails
// on Windows CI.
//
// The fix is one of:
//   - Use pathmatrix.PassHostNativeAbs(input), which delegates the
//     pass-through-vs-join decision to filepath.IsAbs at test runtime.
//   - Provide an OnWindows override for the field; the analyzer treats
//     a present override as proof the author considered Windows
//     behavior.
//   - //goplint:ignore on the call line for resolver shapes that
//     genuinely treat all three as relative on every platform.
//
// Detection: visits each FuncDecl, finds calls to
// internal/testutil/pathmatrix.PassRelative whose argument transitively
// references one of the three input constants. Suppresses when the call
// appears as a value for an Expectations field that is overridden on
// OnWindows in the same composite literal.
func inspectPathmatrixDivergent(pass *analysis.Pass, fn *ast.FuncDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
	if fn == nil || fn.Body == nil {
		return
	}
	if shouldSkipFunc(fn) {
		return
	}
	if hasIgnoreDirective(fn.Doc, nil) {
		return
	}

	funcQualName := qualFuncName(pass, fn)
	excKey := funcQualName + ".pathmatrix-divergent"
	if cfg != nil && cfg.isExcepted(excKey) {
		return
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if !isPathmatrixExpectationsType(pass, lit) {
			return true
		}
		overriddenWindowsFields := windowsOverrideFields(pass, lit)
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			fieldName := keyExprName(kv.Key)
			if !isDivergentExpectationField(fieldName) {
				continue
			}
			if overriddenWindowsFields[fieldName] {
				continue
			}
			passRelativeCall, found := findPassRelativeCall(pass, kv.Value)
			if !found {
				continue
			}
			constName, found := referencedDivergentInputConstant(pass, passRelativeCall.Args[0])
			if !found {
				continue
			}
			reportPathmatrixDivergentFinding(pass, passRelativeCall, funcQualName, fieldName, constName, bl)
		}
		return true
	})
}

// isPathmatrixExpectationsType reports whether lit is a composite literal
// of the pathmatrix.Expectations struct type. Used to scope the check to
// matrix call sites.
func isPathmatrixExpectationsType(pass *analysis.Pass, lit *ast.CompositeLit) bool {
	if pass == nil || pass.TypesInfo == nil || lit == nil || lit.Type == nil {
		return false
	}
	t := pass.TypesInfo.TypeOf(lit.Type)
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return isPathmatrixObject(obj) && obj.Name() == "Expectations"
}

// windowsOverrideFields returns the set of Expectations field names that
// have an explicit override in the composite literal's OnWindows entry.
// An override is treated as proof the author considered Windows
// divergence for that field.
func windowsOverrideFields(pass *analysis.Pass, lit *ast.CompositeLit) map[string]bool {
	overridden := make(map[string]bool)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if keyExprName(kv.Key) != "OnWindows" {
			continue
		}
		// Value may be a unary &-of-composite-literal or a function call
		// that returns *PlatformOverride. Walk down to the composite
		// literal payload, tolerating either shape.
		overlit := unwrapPlatformOverrideLiteral(kv.Value)
		if overlit == nil {
			continue
		}
		for _, oElt := range overlit.Elts {
			oKV, ok := oElt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			overridden[keyExprName(oKV.Key)] = true
		}
		// PlatformOverride uses pointer fields; assume any present field
		// counts as an override for analysis purposes.
		_ = pass
	}
	return overridden
}

// unwrapPlatformOverrideLiteral peels &PlatformOverride{...} or a
// helper-call returning *PlatformOverride down to the inner composite
// literal so windowsOverrideFields can read its keyed entries.
func unwrapPlatformOverrideLiteral(expr ast.Expr) *ast.CompositeLit {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		if lit, ok := e.X.(*ast.CompositeLit); ok {
			return lit
		}
	case *ast.CompositeLit:
		return e
	}
	return nil
}

// keyExprName extracts the field name from an Expectations literal key.
// Returns the empty string for non-identifier keys.
func keyExprName(expr ast.Expr) string {
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// isDivergentExpectationField reports whether fieldName is one of the
// three Expectations base-vector fields whose absoluteness diverges by
// platform.
func isDivergentExpectationField(fieldName string) bool {
	switch fieldName {
	case "WindowsDriveAbs", "WindowsRooted", "UNC":
		return true
	}
	return false
}

// findPassRelativeCall searches expr for a top-level call to
// pathmatrix.PassRelative(arg). Returns the call expression and true
// when matched.
func findPassRelativeCall(pass *analysis.Pass, expr ast.Expr) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "PassRelative" {
		return nil, false
	}
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	if !isPathmatrixObject(obj) {
		return nil, false
	}
	if len(call.Args) != 1 {
		return nil, false
	}
	return call, true
}

// referencedDivergentInputConstant reports whether expr transitively
// references one of the three platform-divergent pathmatrix input
// constants (InputUNC, InputWindowsDriveAbs, InputWindowsRooted), and
// returns the constant's name if so. The walk handles direct
// identifiers, binary-expression concatenation (e.g.
// `pathmatrix.InputUNC + ".sh"`), and parenthesized expressions.
func referencedDivergentInputConstant(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return "", false
	}
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		if e.Sel == nil {
			return "", false
		}
		obj := pass.TypesInfo.ObjectOf(e.Sel)
		if !isPathmatrixObject(obj) {
			return "", false
		}
		switch obj.Name() {
		case "InputUNC", "InputWindowsDriveAbs", "InputWindowsRooted":
			return obj.Name(), true
		}
	case *ast.BinaryExpr:
		if name, ok := referencedDivergentInputConstant(pass, e.X); ok {
			return name, true
		}
		if name, ok := referencedDivergentInputConstant(pass, e.Y); ok {
			return name, true
		}
	case *ast.ParenExpr:
		return referencedDivergentInputConstant(pass, e.X)
	}
	return "", false
}

// reportPathmatrixDivergentFinding emits the diagnostic at the
// PassRelative call site, honoring baseline suppression.
func reportPathmatrixDivergentFinding(
	pass *analysis.Pass,
	call *ast.CallExpr,
	funcQualName, fieldName, constName string,
	bl *BaselineConfig,
) {
	posKey := stablePosKey(pass, call.Pos())
	msg := fmt.Sprintf(
		"pathmatrix.PassRelative on platform-divergent vector %s in %s "+
			"(argument references %s); on Windows, filepath.IsAbs treats this "+
			"input differently than on Linux/macOS, so a single PassRelative "+
			"expectation will fail on the divergent platform. "+
			"Use pathmatrix.PassHostNativeAbs(input) (recommended), or set "+
			"an OnWindows override on the surrounding Expectations literal",
		fieldName, funcQualName, constName,
	)
	findingID := PackageScopedFindingID(pass, CategoryPathmatrixDivergent, funcQualName, fieldName, posKey)
	if bl != nil && bl.ContainsFinding(CategoryPathmatrixDivergent, findingID, msg) {
		return
	}
	reportDiagnostic(pass, call.Pos(), CategoryPathmatrixDivergent, findingID, msg)
}

// pathmatrixPkgName is the canonical package name. Symbols are matched
// by package name rather than full import path so test fixtures with a
// fixture-local stub `pathmatrix` package exercise the same code paths
// as the real package consumers.
const pathmatrixPkgName = "pathmatrix"

// isPathmatrixObject reports whether the given types.Object resolves to
// a symbol whose package is named `pathmatrix`. Used as the scope for
// PassRelative, Expectations, and Input* constant detection.
func isPathmatrixObject(obj types.Object) bool {
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Name() == pathmatrixPkgName
}
