// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// maxTransitiveDepth is the maximum call chain depth for transitive
// factory tracking in --check-constructor-validates. This bounds
// recursion in bodyCallsValidateTransitive() to prevent pathological
// cases while allowing realistic delegation chains (e.g.,
// NewFoo → buildBar → initBaz → baz.Validate()).
const maxTransitiveDepth = 5

// constructorValidateInfo records a constructor function and whether
// its body calls Validate() on the returned value.
type constructorValidateInfo struct {
	name           string    // function name (e.g., "NewConfig")
	pos            ast.Node  // position of the function declaration
	returnTypeName string    // resolved first non-error return type name
	callsValidate  bool      // body contains a .Validate() selector call
}

// inspectConstructorValidates checks whether NewXxx() constructors call
// Validate() on the type they construct. Constructors returning types with
// a Validate() method should call it before returning to enforce invariants.
//
// Types annotated with //goplint:constant-only are exempt — their values
// only come from compile-time constants, so runtime validation is unnecessary.
//
// This is a post-traversal check: it receives the constructorDetails map
// already populated by trackConstructorDetails, then walks the function
// bodies looking for .Validate() calls.
func inspectConstructorValidates(
	pass *analysis.Pass,
	ctors map[string]*constructorFuncInfo,
	constantOnlyTypes map[string]bool,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	pkgName := packageName(pass.Pkg)

	// Build a set of struct names that have Validate() methods.
	// We check this using the type checker to find method sets.
	validatableStructs := make(map[string]bool)
	for _, obj := range pass.TypesInfo.Defs {
		if obj == nil {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		if hasValidateMethod(named) {
			validatableStructs[obj.Name()] = true
		}
	}

	// Walk all files to find constructor function bodies.
	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}

			name := fn.Name.Name
			if !strings.HasPrefix(name, "New") || len(name) <= 3 {
				continue
			}

			ctorInfo, exists := ctors[name]
			if !exists {
				continue
			}

			// Skip constructors returning interfaces — they may delegate
			// validation to the concrete implementation.
			if ctorInfo.returnsInterface {
				continue
			}

			returnType := ctorInfo.returnTypeName
			if returnType == "" || !validatableStructs[returnType] {
				continue
			}

			// Skip types annotated with //goplint:constant-only — their
			// Validate() is intentionally unwired because all values come
			// from compile-time constants.
			if constantOnlyTypes[returnType] {
				continue
			}

			// Check if the constructor body calls .Validate() on the return type.
			// This is receiver-type-aware: cfg.Validate() on a Config param does
			// not satisfy the check when the constructor returns *Server.
			// Also checks transitively through private factory calls.
			if bodyCallsValidateOnType(pass, fn.Body, returnType) {
				continue
			}
			if bodyCallsValidateTransitive(pass, fn.Body, returnType, nil, 0) {
				continue
			}

			// Check for ignore directive on the function.
			if hasIgnoreDirective(fn.Doc, nil) {
				continue
			}

			qualName := fmt.Sprintf("%s.%s", pkgName, name)
			excKey := qualName + ".constructor-validate"
			if cfg.isExcepted(excKey) {
				continue
			}

			msg := fmt.Sprintf(
				"constructor %s returns %s.%s which has Validate() but never calls it",
				qualName, pkgName, returnType)
			findingID := StableFindingID(CategoryMissingConstructorValidate, qualName, returnType)
			if bl.ContainsFinding(CategoryMissingConstructorValidate, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, fn.Name.Pos(), CategoryMissingConstructorValidate, findingID, msg)
		}
	}
}

// bodyCallsValidateOnType walks a function body looking for a .Validate()
// selector call where the receiver's type matches the constructor's return type.
// This avoids the false-negative pattern where cfg.Validate() (on a Config
// parameter) satisfies the heuristic even though the returned Server is never
// validated.
//
// Accepted patterns:
//   - Direct: s := &Server{...}; s.Validate()
//   - Delegated: s, err := helperNewServer(); s.Validate()
//   - Any .Validate() call on an expression whose resolved type matches returnTypeName
func bodyCallsValidateOnType(pass *analysis.Pass, body *ast.BlockStmt, returnTypeName string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Validate" {
			return true
		}

		// Resolve the type of the receiver expression (the X in X.Validate()).
		receiverType := pass.TypesInfo.TypeOf(sel.X)
		if receiverType == nil {
			return true
		}

		// Dereference pointers: *Server → Server.
		if ptr, ok := receiverType.(*types.Pointer); ok {
			receiverType = ptr.Elem()
		}

		// Resolve aliases.
		receiverType = types.Unalias(receiverType)

		// Check if the receiver's type name matches the constructor's return type.
		if named, ok := receiverType.(*types.Named); ok {
			if named.Obj().Name() == returnTypeName {
				found = true
				return false
			}
		}

		return true
	})
	return found
}

// methodCallTarget identifies a method call on the constructor's return type
// for transitive validation tracking.
type methodCallTarget struct {
	typeName   string
	methodName string
}

// bodyCallsValidateTransitive checks if any private function or method called
// from body transitively calls Validate() on the given return type. Uses
// pass.TypesInfo to resolve callee identities. Bounds recursion depth
// to maxTransitiveDepth to prevent pathological cases. The visited map
// prevents cycles (re-visiting the same function/method); depth tracks the
// actual call chain depth independently.
//
// This function follows two kinds of callees:
//  1. Same-package bare function calls (e.g., helper()) — via *ast.Ident
//  2. Method calls on variables whose type matches returnTypeName
//     (e.g., s.Setup() where s is *Server) — via *ast.SelectorExpr
func bodyCallsValidateTransitive(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	returnTypeName string,
	visited map[string]bool,
	depth int,
) bool {
	if visited == nil {
		visited = make(map[string]bool)
	}

	// Bound recursion by call chain depth, not visit count.
	if depth >= maxTransitiveDepth {
		return false
	}

	// Collect bare function call identifiers AND method calls on the return type.
	var bareFuncCallees []string
	var methodCallees []methodCallTarget

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		switch fun := call.Fun.(type) {
		case *ast.Ident:
			// Same-package bare function calls.
			obj := pass.TypesInfo.Uses[fun]
			if obj == nil {
				return true
			}
			fn, ok := obj.(*types.Func)
			if !ok {
				return true
			}
			// Only follow same-package, non-method functions.
			if fn.Pkg() != pass.Pkg {
				return true
			}
			bareFuncCallees = append(bareFuncCallees, fun.Name)

		case *ast.SelectorExpr:
			// Method calls on variables whose type matches the return type.
			// Skip Validate() itself — already handled by bodyCallsValidateOnType.
			if fun.Sel.Name == "Validate" {
				return true
			}
			receiverType := pass.TypesInfo.TypeOf(fun.X)
			if receiverType == nil {
				return true
			}
			// Dereference pointers: *Server → Server.
			if ptr, ok := receiverType.(*types.Pointer); ok {
				receiverType = ptr.Elem()
			}
			receiverType = types.Unalias(receiverType)
			named, ok := receiverType.(*types.Named)
			if !ok {
				return true
			}
			if named.Obj().Name() == returnTypeName && named.Obj().Pkg() == pass.Pkg {
				methodCallees = append(methodCallees, methodCallTarget{
					typeName:   returnTypeName,
					methodName: fun.Sel.Name,
				})
			}
		}
		return true
	})

	// Follow bare function callees.
	for _, calleeName := range bareFuncCallees {
		if visited[calleeName] {
			continue
		}
		visited[calleeName] = true

		calleeBody := findFuncBody(pass, calleeName)
		if calleeBody == nil {
			continue
		}
		if bodyCallsValidateOnType(pass, calleeBody, returnTypeName) {
			return true
		}
		// Recurse into the callee's body.
		if bodyCallsValidateTransitive(pass, calleeBody, returnTypeName, visited, depth+1) {
			return true
		}
	}

	// Follow method callees on the return type.
	for _, mc := range methodCallees {
		visitKey := mc.typeName + "." + mc.methodName
		if visited[visitKey] {
			continue
		}
		visited[visitKey] = true

		methodBody, _ := findMethodBody(pass, mc.typeName, mc.methodName)
		if methodBody == nil {
			continue
		}
		if bodyCallsValidateOnType(pass, methodBody, returnTypeName) {
			return true
		}
		if bodyCallsValidateTransitive(pass, methodBody, returnTypeName, visited, depth+1) {
			return true
		}
	}

	return false
}

// findFuncBody searches the package for a non-method function with the given
// name and returns its body. Returns nil if not found.
func findFuncBody(pass *analysis.Pass, funcName string) *ast.BlockStmt {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Body == nil {
				continue
			}
			if fn.Name.Name == funcName {
				return fn.Body
			}
		}
	}
	return nil
}
