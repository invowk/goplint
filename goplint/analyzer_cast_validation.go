// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// buildParentMap builds a mapping from each AST node to its parent node
// within the given root. Used by cast-validation to determine the syntactic
// context of a type conversion (assignment, map index, comparison, etc.).
// Uses a single ast.Walk traversal with a stack-based visitor for O(N)
// performance with minimal allocations.
func buildParentMap(root ast.Node) map[ast.Node]ast.Node {
	rec := &parentRecorder{parents: make(map[ast.Node]ast.Node)}
	ast.Walk(rec, root)
	return rec.parents
}

// parentRecorder is an ast.Visitor that tracks the parent of each AST
// node using an explicit stack. Each Visit call records the current
// stack top as the parent of n, then pushes n. The nil sentinel from
// ast.Walk pops the stack when leaving a node's subtree.
type parentRecorder struct {
	parents map[ast.Node]ast.Node
	stack   []ast.Node
}

func (r *parentRecorder) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		r.stack = r.stack[:len(r.stack)-1]
		return nil
	}
	if len(r.stack) > 0 {
		r.parents[n] = r.stack[len(r.stack)-1]
	}
	r.stack = append(r.stack, n)
	return r
}

// isAutoSkipContext reports whether a type conversion call expression is in
// a context where validation is unnecessary:
//   - Map index expression: m[DddType(s)] — invalid key returns zero/false
//   - Comparison operand: DddType(s) == expected — string equality works
//   - Switch tag expression: switch DddType(s) { ... } — equivalent to comparison
//   - fmt.* function argument: fmt.Sprintf("...", DddType(s)) — display-only
//   - Nested in composite literal inside fmt.* — display-only (ancestor check)
func isAutoSkipContext(pass *analysis.Pass, call *ast.CallExpr, parent ast.Node, parentMap map[ast.Node]ast.Node) bool {
	if parent == nil {
		return false
	}
	castNode, nonParenParent := parentAfterParens(call, parentMap)
	if nonParenParent != nil {
		parent = nonParenParent
	}

	// Map index: m[DddType(s)]
	if idx, ok := parent.(*ast.IndexExpr); ok && idx.Index == castNode && isMapIndexExpr(pass, idx) {
		return !isMapIndexWriteLHS(idx, parentMap)
	}

	// Comparison: DddType(s) == x or x == DddType(s)
	if bin, ok := parent.(*ast.BinaryExpr); ok {
		if bin.Op.String() == "==" || bin.Op.String() == "!=" {
			return true
		}
	}

	// Switch tag: switch DddType(s) { case ...: } — semantically a comparison.
	if sw, ok := parent.(*ast.SwitchStmt); ok && sw.Tag == castNode {
		return true
	}

	// fmt.*, log/slog, or strings comparison function argument: the parent
	// is a *ast.CallExpr targeting a display-only or comparison-only package.
	if outerCall, ok := parent.(*ast.CallExpr); ok && outerCall != call {
		if isAutoSkipCall(pass, outerCall) {
			return true
		}
	}

	// Chained Validate: DddType(x).Validate() — validated directly on cast result.
	// The parent of the type conversion CallExpr is the SelectorExpr that
	// forms the .Validate() method call.
	if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel.Name == "Validate" && selectorIsDirectCallTarget(sel, parentMap) {
		return true
	}

	// Ancestor walk: check if any ancestor up to a statement boundary is
	// in an auto-skip context (e.g., fmt.Sprintf("...", Struct{Field: DddType(s)})).
	if isAutoSkipAncestor(pass, parent, parentMap) {
		return true
	}

	return false
}

func selectorIsDirectCallTarget(sel *ast.SelectorExpr, parentMap map[ast.Node]ast.Node) bool {
	if sel == nil || parentMap == nil {
		return false
	}
	current := ast.Node(sel)
	for {
		parent := parentMap[current]
		paren, ok := parent.(*ast.ParenExpr)
		if ok && paren.X == current {
			current = paren
			continue
		}
		call, ok := parent.(*ast.CallExpr)
		if !ok {
			return false
		}
		return call.Fun == current
	}
}

// isAutoSkipAncestor walks up the parent chain (bounded) looking for an
// ancestor fmt.* call that makes the entire expression tree display-only.
// Stops at statement boundaries to prevent cross-statement false suppression.
func isAutoSkipAncestor(pass *analysis.Pass, start ast.Node, parentMap map[ast.Node]ast.Node) bool {
	const maxAncestorDepth = 5

	current := start
	for range maxAncestorDepth {
		grandparent, ok := parentMap[current]
		if !ok || grandparent == nil {
			break
		}
		// Stop at statement boundaries — a cast in one statement should
		// not be auto-skipped because a parent statement uses fmt.*.
		if isStatementNode(grandparent) {
			break
		}
		// Check if the grandparent is a display-only or comparison-only call.
		if outerCall, ok := grandparent.(*ast.CallExpr); ok {
			if isAutoSkipCall(pass, outerCall) {
				return true
			}
		}
		current = grandparent
	}
	return false
}

// isStatementNode reports whether n is a statement-level AST node that
// represents a boundary for ancestor auto-skip walking.
func isStatementNode(n ast.Node) bool {
	switch n.(type) {
	case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt,
		*ast.IfStmt, *ast.ForStmt, *ast.RangeStmt,
		*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt,
		*ast.GoStmt, *ast.DeferStmt, *ast.SendStmt,
		*ast.BlockStmt:
		return true
	default:
		return false
	}
}

func isMapIndexExpr(pass *analysis.Pass, idx *ast.IndexExpr) bool {
	if pass == nil || pass.TypesInfo == nil || idx == nil {
		return false
	}
	baseType := pass.TypesInfo.TypeOf(idx.X)
	if baseType == nil {
		return false
	}
	_, ok := types.Unalias(baseType).Underlying().(*types.Map)
	return ok
}

func isMapIndexWriteLHS(idx *ast.IndexExpr, parentMap map[ast.Node]ast.Node) bool {
	if idx == nil {
		return false
	}
	assign, ok := parentMap[idx].(*ast.AssignStmt)
	if !ok {
		return false
	}
	for _, lhs := range assign.Lhs {
		if stripParens(lhs) == idx {
			return true
		}
	}
	return false
}

// isPackageCall reports whether the given call expression targets a function
// in one of the specified import paths. Uses the type checker to resolve
// the package through any import alias, including dot-imported symbols.
func isPackageCall(pass *analysis.Pass, call *ast.CallExpr, importPaths ...string) bool {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return false
	}

	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return false
		}
		obj := pass.TypesInfo.Uses[ident]
		if obj == nil {
			return false
		}
		pkgName, ok := obj.(*types.PkgName)
		if !ok {
			return false
		}
		return slices.Contains(importPaths, pkgName.Imported().Path())
	case *ast.Ident:
		obj := pass.TypesInfo.Uses[fun]
		if obj == nil || obj.Pkg() == nil {
			return false
		}
		return slices.Contains(importPaths, obj.Pkg().Path())
	default:
		return false
	}
}

// isFmtCall reports whether the call targets the "fmt" package.
func isFmtCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageCall(pass, call, "fmt")
}

// isLogCall reports whether the call targets "log" or "log/slog" — display-only sinks.
func isLogCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageCall(pass, call, "log", "log/slog")
}

// isStrconvCall reports whether the call targets the "strconv" package.
func isStrconvCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageCall(pass, call, "strconv")
}

// isAutoSkipCall reports whether the call targets a display-only or
// comparison-only package function. Used by both isAutoSkipContext
// (direct parent) and isAutoSkipAncestor (grandparent walk) to avoid
// repeating the same disjunction.
func isAutoSkipCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isFmtCall(pass, call) ||
		isLogCall(pass, call) ||
		isStringsComparisonCall(pass, call) ||
		isBytesComparisonCall(pass, call) ||
		isSlicesComparisonCall(pass, call) ||
		isErrorsComparisonCall(pass, call)
}

type packageFuncMatcher func(name string) bool

// isPackageFuncMatch reports whether the call targets a function in the
// given import path that satisfies matcher.
func isPackageFuncMatch(pass *analysis.Pass, call *ast.CallExpr, importPath string, matcher packageFuncMatcher) bool {
	if !isPackageCall(pass, call, importPath) {
		return false
	}
	return matcher(packageCallFuncName(call))
}

func packageCallFuncName(call *ast.CallExpr) string {
	if call == nil {
		return ""
	}
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.Ident:
		return fun.Name
	default:
		return ""
	}
}

// isStringsComparisonCall reports whether the call targets one of the string
// comparison functions in the "strings" package that are semantically
// equivalent to equality/containment checks.
func isStringsComparisonCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "strings", isContainmentPredicateFunc)
}

// isSlicesComparisonCall reports whether the call targets one of the
// comparison/lookup functions in the "slices" package that are semantically
// equivalent to membership or position checks.
func isSlicesComparisonCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "slices", isSlicesComparisonFunc)
}

// isErrorsComparisonCall reports whether the call targets errors.Is or
// errors.As — type-matching comparison operations where the cast value
// is used for error identity/type matching, not as domain input.
func isErrorsComparisonCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "errors", isErrorsComparisonFunc)
}

// isBytesComparisonCall reports whether the call targets one of the
// comparison functions in the "bytes" package that are semantically
// equivalent to equality/containment checks on byte slices. Uses the
// same function name set as strings (Contains, HasPrefix, etc.).
func isBytesComparisonCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	return isPackageFuncMatch(pass, call, "bytes", isContainmentPredicateFunc)
}

func isContainmentPredicateFunc(name string) bool {
	switch name {
	case "Contains", "HasPrefix", "HasSuffix", "EqualFold":
		return true
	default:
		return false
	}
}

func isSlicesComparisonFunc(name string) bool {
	switch name {
	case "Contains", "ContainsFunc", "Index", "IndexFunc":
		return true
	default:
		return false
	}
}

func isErrorsComparisonFunc(name string) bool {
	switch name {
	case "As", "Is":
		return true
	default:
		return false
	}
}

// isErrorMessageExpr reports whether expr is a call that produces display
// text (error messages, formatted strings) where domain validation via
// Validate() would be meaningless.
//
// Recognized patterns:
//   - x.Error() — error interface method, returns formatted message
//   - fmt.Sprintf/Errorf/Sprint/... — fmt package formatting functions
//   - strconv.Itoa/FormatInt/... — numeric formatting functions
func isErrorMessageExpr(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Pattern 1: x.Error() on values implementing the built-in error interface.
	if sel.Sel.Name == "Error" && len(call.Args) == 0 && receiverImplementsError(pass, sel.X) {
		return true
	}

	// Pattern 2+3: display-only packages (fmt, strconv).
	return isFmtCall(pass, call) || isStrconvCall(pass, call)
}

func receiverImplementsError(pass *analysis.Pass, expr ast.Expr) bool {
	if pass == nil || pass.TypesInfo == nil || expr == nil {
		return false
	}
	receiverType := pass.TypesInfo.TypeOf(expr)
	if receiverType == nil {
		return false
	}
	errorType := types.Universe.Lookup("error").Type()
	if errorType == nil {
		return false
	}
	return types.Implements(receiverType, errorType.Underlying().(*types.Interface)) ||
		types.Implements(types.NewPointer(receiverType), errorType.Underlying().(*types.Interface))
}

// isRawPrimitive reports whether t is a bare primitive type (string, int, etc.)
// as opposed to a named type wrapping a primitive. This distinguishes
// DddType(someString) from DddType(otherNamedType) — the latter is a
// named-to-named cast that doesn't need validation.
func isRawPrimitive(t types.Type) bool {
	t = types.Unalias(t)

	switch t := t.(type) {
	case *types.Basic:
		return isPrimitiveBasic(t) || t.Kind() == types.Bool || t.Kind() == types.UntypedBool
	case *types.Named:
		// Named type → not a raw primitive.
		return false
	default:
		return false
	}
}

// qualifiedTypeName returns a human-readable qualified name for a type,
// using the package name for external types and unqualified for same-package.
func qualifiedTypeName(t types.Type, currentPkg *types.Package) string {
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		pkg := named.Obj().Pkg()
		if pkg == nil {
			return named.Obj().Name()
		}
		if pkg == currentPkg {
			return named.Obj().Name()
		}
		// Use the last segment of the package path.
		path := pkg.Path()
		if i := strings.LastIndex(path, "/"); i >= 0 {
			return path[i+1:] + "." + named.Obj().Name()
		}
		return pkg.Name() + "." + named.Obj().Name()
	}
	return types.TypeString(t, nil)
}
