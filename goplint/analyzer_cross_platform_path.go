// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// inspectCrossPlatformPath flags the canonical "FromSlash then IsAbs" Windows
// bug pattern. On Windows, filepath.FromSlash("/app") returns "\app", and
// filepath.IsAbs("\app") is false because Windows absolute paths require a
// drive letter or UNC prefix. The result is that the code silently treats
// container-absolute paths as relative on Windows, joining them with the
// invowkfile directory.
//
// The fix documented in .agents/rules/windows.md is to check
// strings.HasPrefix(input, "/") BEFORE the FromSlash conversion. This
// analyzer enforces the rule by flagging any:
//
//	filepath.IsAbs(filepath.FromSlash(x))             // single-line
//	nativePath := filepath.FromSlash(x)               // multi-line
//	if filepath.IsAbs(nativePath) { ... }
//
// pattern UNLESS strings.HasPrefix(x, "/") was called earlier in the same
// function on the same originating input variable. Direct calls like
// filepath.IsAbs(rawHostString) are NOT flagged — platform-native semantics
// are correct for true host paths.
//
// Suppression:
//   - //goplint:ignore on the function declaration.
//   - TOML key pkg.FuncName.cross-platform-path.
func inspectCrossPlatformPath(
	pass *analysis.Pass,
	fn *ast.FuncDecl,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
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
	excKey := funcQualName + ".cross-platform-path"
	if cfg != nil && cfg.isExcepted(excKey) {
		return
	}

	// fromSlashOriginal maps a variable assigned from filepath.FromSlash(x) to
	// the original input variable x. Cleared on reassignment from any other
	// source so a HasPrefix-guarded reassignment correctly drops the
	// FromSlash provenance.
	fromSlashOriginal := make(map[*types.Var]*types.Var)

	// guardedVars holds variables passed as the first argument to
	// strings.HasPrefix(<var>, "/"). The HasPrefix call must lexically precede
	// the IsAbs site for the suppression to apply, which AST.Inspect's
	// depth-first source-order traversal naturally enforces because we record
	// guards and check them in the same pass.
	guardedVars := make(map[*types.Var]bool)

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil {
			return false
		}

		switch node := n.(type) {
		case *ast.AssignStmt:
			recordFromSlashAssignments(pass, node, fromSlashOriginal)
		case *ast.CallExpr:
			if v := stringsHasPrefixSlashOperand(pass, node); v != nil {
				guardedVars[v] = true
			}
			if !isFilepathIsAbsCall(pass, node) || len(node.Args) != 1 {
				return true
			}
			origin := isAbsArgOriginVar(pass, node.Args[0], fromSlashOriginal)
			if origin == nil {
				// Not a FromSlash chain — host-path semantics are correct.
				return true
			}
			if guardedVars[origin] {
				return true
			}
			reportCrossPlatformPathFinding(pass, node, funcQualName, bl)
		}
		return true
	})
}

// recordFromSlashAssignments updates fromSlashOriginal for `x := filepath.FromSlash(y)`
// and `x = filepath.FromSlash(y)` statements, recording x → originVar(y). Any
// other assignment to a previously tracked variable removes it from the map,
// so a subsequent reassignment correctly drops the FromSlash provenance.
func recordFromSlashAssignments(pass *analysis.Pass, assign *ast.AssignStmt, fromSlashOriginal map[*types.Var]*types.Var) {
	if pass == nil || pass.TypesInfo == nil || assign == nil {
		return
	}
	if len(assign.Lhs) != len(assign.Rhs) {
		// Multi-return assignments (e.g., x, err := f()) cannot bind FromSlash;
		// leave the tracker untouched.
		return
	}
	for idx, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name == "_" {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(ident)
		v, ok := obj.(*types.Var)
		if !ok {
			continue
		}
		if originVar := fromSlashCallOriginVar(pass, assign.Rhs[idx]); originVar != nil {
			fromSlashOriginal[v] = originVar
			continue
		}
		// Reassignment from a non-FromSlash source clears the tracker.
		delete(fromSlashOriginal, v)
	}
}

// fromSlashCallOriginVar reports whether expr is a call to filepath.FromSlash;
// if so, it returns the *types.Var that the call's single argument
// resolves to (after unwrapping string(...) conversions), or nil if the
// argument is not a simple variable reference.
func fromSlashCallOriginVar(pass *analysis.Pass, expr ast.Expr) *types.Var {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !isCallToPathFilepathFunc(pass, call, "FromSlash") || len(call.Args) != 1 {
		return nil
	}
	return varRefThroughStringConversion(pass, call.Args[0])
}

// isAbsArgOriginVar walks back from an IsAbs argument to the originating input
// variable that flowed into a FromSlash call. Returns nil if the argument did
// not originate from FromSlash.
func isAbsArgOriginVar(pass *analysis.Pass, arg ast.Expr, fromSlashOriginal map[*types.Var]*types.Var) *types.Var {
	if arg == nil {
		return nil
	}
	// Direct case: filepath.IsAbs(filepath.FromSlash(x)).
	if origin := fromSlashCallOriginVar(pass, arg); origin != nil {
		return origin
	}
	// Indirect case: filepath.IsAbs(nativePath) where nativePath was assigned
	// earlier from filepath.FromSlash(x).
	v := varRefThroughStringConversion(pass, arg)
	if v == nil {
		return nil
	}
	return fromSlashOriginal[v]
}

// stringsHasPrefixSlashOperand returns the *types.Var passed as the first arg
// of a strings.HasPrefix(<var>, "/") call (after unwrapping string conversions),
// or nil if the call is not a "/"-prefix HasPrefix on a simple variable.
func stringsHasPrefixSlashOperand(pass *analysis.Pass, call *ast.CallExpr) *types.Var {
	if pass == nil || pass.TypesInfo == nil || call == nil || len(call.Args) != 2 {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != "HasPrefix" {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "strings" {
		return nil
	}
	if !isSlashStringConstant(pass, call.Args[1]) {
		return nil
	}
	return varRefThroughStringConversion(pass, call.Args[0])
}

// isSlashStringConstant reports whether expr is the constant string "/".
func isSlashStringConstant(pass *analysis.Pass, expr ast.Expr) bool {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return false
	}
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) == "/"
}

// varRefThroughStringConversion unwraps any chain of `string(...)` conversion
// calls around expr and returns the *types.Var the resulting identifier
// resolves to, or nil if the expression is not a simple variable reference.
func varRefThroughStringConversion(pass *analysis.Pass, expr ast.Expr) *types.Var {
	if pass == nil || pass.TypesInfo == nil {
		return nil
	}
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			break
		}
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			break
		}
		basic, ok := tv.Type.(*types.Basic)
		if !ok || basic.Kind() != types.String {
			break
		}
		expr = call.Args[0]
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	v, ok := obj.(*types.Var)
	if !ok {
		return nil
	}
	return v
}

// reportCrossPlatformPathFinding emits the cross-platform-path diagnostic at
// the IsAbs call site, honoring baseline suppression.
func reportCrossPlatformPathFinding(pass *analysis.Pass, call *ast.CallExpr, funcQualName string, bl *BaselineConfig) {
	posKey := stablePosKey(pass, call.Pos())
	msg := fmt.Sprintf(
		"filepath.IsAbs called on filepath.FromSlash result in %s; "+
			"on Windows, FromSlash(\"/foo\") = \"\\foo\" which IsAbs reports as not absolute. "+
			"Add strings.HasPrefix(input, \"/\") guard BEFORE FromSlash to preserve container-absolute paths "+
			"(see .agents/rules/windows.md)",
		funcQualName,
	)
	findingID := PackageScopedFindingID(pass, CategoryCrossPlatformPath, funcQualName, posKey)
	if bl != nil && bl.ContainsFinding(CategoryCrossPlatformPath, findingID, msg) {
		return
	}
	reportDiagnostic(pass, call.Pos(), CategoryCrossPlatformPath, findingID, msg)
}

// isFilepathIsAbsCall reports whether call is a call to path/filepath.IsAbs.
func isFilepathIsAbsCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isCallToPathFilepathFunc(pass, call, "IsAbs")
}

// isCallToPathFilepathFunc resolves the callee of call against types.Info and
// returns true when it matches path/filepath.<name>. Using the type checker
// (rather than syntactic matching) means an `import filepath "path/filepath"`
// or aliased import is handled correctly.
func isCallToPathFilepathFunc(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel == nil || sel.Sel.Name != name {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == "path/filepath"
}
