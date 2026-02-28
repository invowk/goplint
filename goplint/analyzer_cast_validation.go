// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// inspectUnvalidatedCasts walks a function body to find type conversions from
// raw primitives to DDD Value Types where Validate() is not called on the
// result variable in the same function. Skips test files, ignored functions,
// constant-source casts, and auto-skip contexts (map keys, comparisons,
// fmt.* arguments).
func inspectUnvalidatedCasts(pass *analysis.Pass, fn *ast.FuncDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
	if fn.Body == nil {
		return
	}
	if shouldSkipFunc(fn) {
		return
	}

	// Build the qualified function name for exception matching.
	pkgName := packageName(pass.Pkg)
	funcName := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvName := receiverTypeName(fn.Recv.List[0].Type)
		if recvName != "" {
			funcName = recvName + "." + funcName
		}
	}
	qualFuncName := pkgName + "." + funcName

	// Phase 1: Build a parent map for auto-skip context detection.
	parentMap := buildParentMap(fn.Body)

	// Phase 2: Single walk collecting assigned casts, unassigned casts,
	// and validated variable names.
	type assignedCast struct {
		varName   string
		typeName  string
		pos       ast.Node
		castIndex int
	}
	type unassignedCast struct {
		typeName  string
		pos       ast.Node
		castIndex int
	}

	var assignedCasts []assignedCast
	var unassignedCasts []unassignedCast
	validatedVars := make(map[string]bool)
	castIndex := 0 // sequential counter for unique finding IDs per cast

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		// Skip closure bodies — they are separate validation scopes.
		// Analyzing them with the outer function's validatedVars map
		// creates false positives (closure validation suppresses outer)
		// and false negatives (outer validation suppresses closure).
		// FuncLit nodes are not FuncDecl, so they aren't visited by
		// the Preorder callback independently.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Detect Validate() calls: x.Validate()
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Validate" {
			if ident, ok := sel.X.(*ast.Ident); ok {
				validatedVars[ident.Name] = true
			}
			return true
		}

		// Check if this call is a type conversion.
		tv, ok := pass.TypesInfo.Types[call.Fun]
		if !ok || !tv.IsType() {
			return true
		}

		// Must have exactly one argument (type conversions always do).
		if len(call.Args) != 1 {
			return true
		}

		// Target type must have Validate() method — i.e., it's a DDD Value Type.
		targetType := tv.Type
		if !hasValidateMethod(targetType) {
			return true
		}

		// Source expression must be a raw primitive, not another named type.
		srcTV, srcOK := pass.TypesInfo.Types[call.Args[0]]
		if !srcOK {
			return true
		}

		// Skip constant expressions — developer can see the value.
		if srcTV.Value != nil {
			return true
		}

		// Skip casts from error-message expressions — the source is already
		// a formatted display string, not raw user input needing validation.
		// Patterns: DddType(err.Error()), DddType(fmt.Sprintf(...))
		if isErrorMessageExpr(pass, call.Args[0]) {
			return true
		}

		// Source must be a bare primitive type (string, int, etc.),
		// not another named type (cast between named types is safe).
		if !isRawPrimitive(srcTV.Type) {
			return true
		}

		// Resolve the target type name for diagnostics and exception matching.
		targetTypeName := qualifiedTypeName(targetType, pass.Pkg)

		// Determine if this cast is assigned to a variable.
		parent := parentMap[call]

		if assign, ok := parent.(*ast.AssignStmt); ok {
			// Find the variable name that receives this cast.
			for i, rhs := range assign.Rhs {
				if rhs != call {
					continue
				}
				if i < len(assign.Lhs) {
					if ident, ok := assign.Lhs[i].(*ast.Ident); ok && ident.Name != "_" {
						assignedCasts = append(assignedCasts, assignedCast{
							varName:   ident.Name,
							typeName:  targetTypeName,
							pos:       call,
							castIndex: castIndex,
						})
						castIndex++
						return true
					}
				}
			}
		}

		// Unassigned cast — check auto-skip contexts.
		if isAutoSkipContext(pass, call, parent, parentMap) {
			return true
		}

		unassignedCasts = append(unassignedCasts, unassignedCast{
			typeName:  targetTypeName,
			pos:       call,
			castIndex: castIndex,
		})
		castIndex++
		return true
	})

	// Phase 3: Report findings.
	// Assigned casts: report if variable is not in the validated set.
	for _, ac := range assignedCasts {
		if validatedVars[ac.varName] {
			continue
		}

		excKey := qualFuncName + ".cast-validation"
		if cfg.isExcepted(excKey) {
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", ac.typeName)
		findingID := StableFindingID(CategoryUnvalidatedCast, qualFuncName, ac.typeName, "assigned", strconv.Itoa(ac.castIndex))
		if bl.ContainsFinding(CategoryUnvalidatedCast, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, ac.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}

	// Unassigned casts: always report (no variable to track).
	for _, uc := range unassignedCasts {
		excKey := qualFuncName + ".cast-validation"
		if cfg.isExcepted(excKey) {
			continue
		}

		msg := fmt.Sprintf("type conversion to %s from non-constant without Validate() check", uc.typeName)
		findingID := StableFindingID(CategoryUnvalidatedCast, qualFuncName, uc.typeName, "unassigned", strconv.Itoa(uc.castIndex))
		if bl.ContainsFinding(CategoryUnvalidatedCast, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, uc.pos.Pos(), CategoryUnvalidatedCast, findingID, msg)
	}
}

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

	// Map index: m[DddType(s)]
	if idx, ok := parent.(*ast.IndexExpr); ok && idx.Index == call {
		return true
	}

	// Comparison: DddType(s) == x or x == DddType(s)
	if bin, ok := parent.(*ast.BinaryExpr); ok {
		if bin.Op.String() == "==" || bin.Op.String() == "!=" {
			return true
		}
	}

	// Switch tag: switch DddType(s) { case ...: } — semantically a comparison.
	if sw, ok := parent.(*ast.SwitchStmt); ok && sw.Tag == call {
		return true
	}

	// fmt.* function argument: the parent is a *ast.CallExpr targeting fmt.*
	if outerCall, ok := parent.(*ast.CallExpr); ok && outerCall != call {
		if isFmtCall(pass, outerCall) {
			return true
		}
	}

	// Chained Validate: DddType(x).Validate() — validated directly on cast result.
	// The parent of the type conversion CallExpr is the SelectorExpr that
	// forms the .Validate() method call.
	if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel.Name == "Validate" {
		return true
	}

	// Ancestor walk: check if any ancestor up to a statement boundary is
	// in an auto-skip context (e.g., fmt.Sprintf("...", Struct{Field: DddType(s)})).
	if isAutoSkipAncestor(pass, parent, parentMap) {
		return true
	}

	return false
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
		// Check if the grandparent is an fmt.* call.
		if outerCall, ok := grandparent.(*ast.CallExpr); ok {
			if isFmtCall(pass, outerCall) {
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

// isFmtCall reports whether the given call expression targets a function
// in the "fmt" package (e.g., fmt.Sprintf, fmt.Fprintf).
func isFmtCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	// Use the type checker to resolve the identifier to its package.
	obj := pass.TypesInfo.Uses[ident]
	if obj == nil {
		return false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == "fmt"
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

	// Pattern 1: x.Error() — error interface method.
	if sel.Sel.Name == "Error" && len(call.Args) == 0 {
		return true
	}

	// Pattern 2: fmt.Sprintf(...), fmt.Errorf(...), etc.
	if isFmtCall(pass, call) {
		return true
	}

	// Pattern 3: strconv.Itoa(...), strconv.FormatInt(...), etc.
	// All strconv functions return formatted strings, never raw user input.
	return isStrconvCall(pass, call)
}

// isStrconvCall reports whether the given call expression targets a function
// in the "strconv" package (e.g., strconv.Itoa, strconv.FormatInt).
func isStrconvCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
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
	return pkgName.Imported().Path() == "strconv"
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
