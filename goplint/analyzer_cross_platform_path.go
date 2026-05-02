// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// inspectCrossPlatformPath flags two related cross-platform path bug patterns.
//
// V1 — FromSlash → IsAbs chain. On Windows, filepath.FromSlash("/app") returns
// "\app", and filepath.IsAbs("\app") is false because Windows absolute paths
// require a drive letter or UNC prefix. The code silently treats container-
// absolute paths as relative on Windows, joining them with the invowkfile dir.
//
// V2 — IsAbs called on a value whose type carries //goplint:cue-fed-path. The
// V1 chain is a proxy for "input is forward-slash CUE-fed convention". When
// callers skip FromSlash and call IsAbs directly on a CUE-fed-typed value,
// the V1 rule cannot detect the bug, but the underlying issue is identical —
// IsAbs("/foo") is false on Windows regardless of FromSlash. V2 closes that
// gap by detecting on type, not on call shape.
//
// Both rules suppress when strings.HasPrefix(input, "/") was called earlier
// in the same function on the same originating input variable. Direct calls
// like filepath.IsAbs(rawHostString) are NOT flagged — platform-native
// semantics are correct for true host paths. V2 is opt-in by directive,
// bounding false-positive risk.
//
// Suppression:
//   - //goplint:ignore on the function declaration.
//   - TOML key pkg.FuncName.cross-platform-path.
//
// Provenance through known-shape transformations is preserved for V2:
// string(...), strings.TrimSpace, filepath.FromSlash, filepath.Clean,
// path.Clean. Other transformations break provenance and produce a known
// false-negative documented in the package comment.
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

	// cueFedOriginType maps a variable to the named CUE-fed-path type whose
	// value flowed into it via a known-shape transformation chain. Cleared on
	// any reassignment that doesn't preserve provenance.
	cueFedOriginType := make(map[*types.Var]*types.TypeName)

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
			recordCueFedAssignments(pass, node, cueFedOriginType)
		case *ast.CallExpr:
			if v := stringsHasPrefixSlashOperand(pass, node); v != nil {
				guardedVars[v] = true
			}
			if !isFilepathIsAbsCall(pass, node) || len(node.Args) != 1 {
				return true
			}
			// V1 path: arg traces back to filepath.FromSlash(x).
			if origin := isAbsArgOriginVar(pass, node.Args[0], fromSlashOriginal); origin != nil {
				if !guardedVars[origin] {
					reportCrossPlatformPathFinding(pass, node, funcQualName, bl)
				}
				return true
			}
			// V2 path: arg's underlying type carries //goplint:cue-fed-path,
			// either directly or via tracked assignment provenance.
			if originVar, typeName := isAbsArgCueFedOrigin(pass, node.Args[0], cueFedOriginType); typeName != nil {
				if originVar != nil && guardedVars[originVar] {
					return true
				}
				reportCueFedPathFinding(pass, node, funcQualName, typeName.Name(), bl)
			}
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

// recordCueFedAssignments updates cueFedOriginType for assignments whose RHS
// expression resolves to a CUE-fed-path-typed origin via known-shape transformations.
// Any other assignment to a previously tracked variable removes it from the map.
func recordCueFedAssignments(pass *analysis.Pass, assign *ast.AssignStmt, cueFedOriginType map[*types.Var]*types.TypeName) {
	if pass == nil || pass.TypesInfo == nil || assign == nil {
		return
	}
	if len(assign.Lhs) != len(assign.Rhs) {
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
		if typeName := cueFedTypeOfExpr(pass, assign.Rhs[idx], cueFedOriginType); typeName != nil {
			cueFedOriginType[v] = typeName
			continue
		}
		// Reassignment from a non-cue-fed source clears the tracker.
		delete(cueFedOriginType, v)
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

// isAbsArgCueFedOrigin reports the CUE-fed-path-typed origin of an IsAbs
// argument when it can be traced. Returns the variable used as the IsAbs
// argument (if any, for HasPrefix-guard suppression) and the named type
// carrying CueFedPathFact.
func isAbsArgCueFedOrigin(
	pass *analysis.Pass,
	arg ast.Expr,
	cueFedOriginType map[*types.Var]*types.TypeName,
) (*types.Var, *types.TypeName) {
	if arg == nil {
		return nil, nil
	}
	// Direct case: filepath.IsAbs(string(workDir)) or filepath.IsAbs(myFsPath).
	if typeName := cueFedTypeOfExpr(pass, arg, cueFedOriginType); typeName != nil {
		// Try to extract the variable being checked, for suppression tracking.
		v := varRefThroughStringConversion(pass, arg)
		return v, typeName
	}
	// Indirect case: arg is a variable assigned earlier from a cue-fed origin.
	v := varRefThroughStringConversion(pass, arg)
	if v == nil {
		return nil, nil
	}
	if typeName, ok := cueFedOriginType[v]; ok {
		return v, typeName
	}
	return nil, nil
}

// cueFedTypeOfExpr returns the named type carrying CueFedPathFact that expr
// resolves to, traversing through string(...) conversions, strings.TrimSpace,
// filepath.FromSlash, filepath.Clean, and path.Clean. Returns nil when the
// expression doesn't trace to a CUE-fed-typed value.
func cueFedTypeOfExpr(
	pass *analysis.Pass,
	expr ast.Expr,
	cueFedOriginType map[*types.Var]*types.TypeName,
) *types.TypeName {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return nil
	}
	// Strip provenance-preserving wrappers: string(...) conversions and
	// known whitelisted single-arg calls that preserve absoluteness.
	for {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			break
		}
		// Type conversion `string(x)` — preserves provenance.
		if tv, ok := pass.TypesInfo.Types[call.Fun]; ok && tv.IsType() {
			if basic, ok := tv.Type.(*types.Basic); ok && basic.Kind() == types.String {
				expr = call.Args[0]
				continue
			}
		}
		// Whitelisted single-arg path/string transformations that preserve
		// absoluteness and "leading slash" semantics.
		if isProvenancePreservingCall(pass, call) {
			expr = call.Args[0]
			continue
		}
		break
	}
	// Look at the type of the resulting expression.
	exprType := pass.TypesInfo.TypeOf(expr)
	if exprType != nil {
		if typeName := cueFedTypeNameForType(pass, exprType); typeName != nil {
			return typeName
		}
	}
	// If the expression is an identifier to a tracked variable, fall back
	// to the assignment-provenance map.
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	v, ok := obj.(*types.Var)
	if !ok {
		return nil
	}
	if typeName, ok := cueFedOriginType[v]; ok {
		return typeName
	}
	return nil
}

// isProvenancePreservingCall reports whether the call is one of the known
// single-arg transformations whose argument's CUE-fed-path provenance is
// preserved through to the result for absoluteness purposes.
func isProvenancePreservingCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if pass == nil || pass.TypesInfo == nil || call == nil || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	switch obj.Pkg().Path() {
	case "strings":
		switch sel.Sel.Name {
		case "TrimSpace", "TrimLeft", "TrimRight", "Trim", "TrimPrefix", "TrimSuffix":
			return true
		}
	case "path/filepath":
		switch sel.Sel.Name {
		case "FromSlash", "ToSlash", "Clean":
			return true
		}
	case "path":
		if sel.Sel.Name == "Clean" {
			return true
		}
	}
	return false
}

// cueFedTypeNameForType returns the *types.TypeName for a Go type if its
// underlying named type carries CueFedPathFact, or nil otherwise. Handles
// type aliases and pointers.
func cueFedTypeNameForType(pass *analysis.Pass, t types.Type) *types.TypeName {
	if pass == nil || t == nil {
		return nil
	}
	t = types.Unalias(t)
	if ptr, ok := t.(*types.Pointer); ok {
		t = types.Unalias(ptr.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	obj := named.Obj()
	if obj == nil {
		return nil
	}
	var fact CueFedPathFact
	if !pass.ImportObjectFact(obj, &fact) {
		return nil
	}
	return obj
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

// reportCrossPlatformPathFinding emits the V1 cross-platform-path diagnostic
// (FromSlash → IsAbs chain) at the IsAbs call site, honoring baseline suppression.
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

// reportCueFedPathFinding emits the V2 cross-platform-path diagnostic
// (IsAbs on a CUE-fed-typed value without HasPrefix guard) at the IsAbs
// call site, honoring baseline suppression.
func reportCueFedPathFinding(pass *analysis.Pass, call *ast.CallExpr, funcQualName, typeName string, bl *BaselineConfig) {
	posKey := stablePosKey(pass, call.Pos())
	msg := fmt.Sprintf(
		"filepath.IsAbs called on %s value in %s without prior strings.HasPrefix(_, \"/\") guard; "+
			"%s holds CUE-fed forward-slash paths and IsAbs(\"/foo\") is false on Windows. "+
			"Add strings.HasPrefix(input, \"/\") guard BEFORE filepath.IsAbs to preserve container-absolute paths "+
			"(see .agents/rules/windows.md)",
		typeName, funcQualName, typeName,
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

// CueFedPathFact is exported for types annotated with //goplint:cue-fed-path.
// It marks types whose values hold CUE-fed forward-slash paths. When such a
// type is used in a filepath.IsAbs argument without a preceding
// strings.HasPrefix(input, "/") guard, the analyzer emits a cross-platform-path
// diagnostic — IsAbs("/foo") is false on Windows regardless of how the path
// reached the call site.
type CueFedPathFact struct{}

// AFact implements the analysis.Fact interface marker method.
func (*CueFedPathFact) AFact() {}

// String returns a human-readable representation for analysistest fact matching.
func (*CueFedPathFact) String() string { return "cue-fed-path" }

// exportCueFedPathFacts scans type declarations in a GenDecl for
// //goplint:cue-fed-path directives and exports CueFedPathFact for matching
// types. Called for ALL packages (even those filtered by include_packages)
// so cross-package consumers see facts for path-bearing types.
func exportCueFedPathFacts(pass *analysis.Pass, gd *ast.GenDecl) {
	if gd == nil || pass == nil || pass.TypesInfo == nil || gd.Tok != token.TYPE {
		return
	}
	for _, spec := range gd.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		// Type aliases inherit facts from the target.
		if ts.Assign.IsValid() {
			continue
		}
		if !hasCueFedPathDirective(gd.Doc, ts.Doc) {
			continue
		}
		obj := pass.TypesInfo.Defs[ts.Name]
		if obj == nil {
			continue
		}
		pass.ExportObjectFact(obj, &CueFedPathFact{})
	}
}

// hasCueFedPathDirective checks whether a type declaration carries the
// //goplint:cue-fed-path directive on either the GenDecl-level doc (for
// single-spec type blocks) or the TypeSpec-level doc.
func hasCueFedPathDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) bool {
	return hasDirectiveKey(genDoc, nil, "cue-fed-path") || hasDirectiveKey(specDoc, nil, "cue-fed-path")
}
