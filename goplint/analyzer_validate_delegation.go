// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// validateAllStruct records a struct annotated with //goplint:validate-all
// along with its fields that have Validate() methods.
type validateAllStruct struct {
	name            string    // type name (e.g., "Config")
	pos             token.Pos // position of the type declaration
	validatableKeys []string  // field names whose types have Validate()
}

// inspectValidateDelegation checks structs annotated with //goplint:validate-all
// for delegation completeness: every field whose type has Validate() should
// be called in the struct's own Validate() method.
func inspectValidateDelegation(pass *analysis.Pass, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)

	// Phase 1: Collect structs with //goplint:validate-all directive and
	// their validatable fields.
	var targets []validateAllStruct
	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				if !hasValidateAllDirective(gd.Doc, ts.Doc) {
					continue
				}

				// Collect fields whose types have Validate().
				var validatableKeys []string
				for _, field := range st.Fields.List {
					fieldType := pass.TypesInfo.TypeOf(field.Type)
					if fieldType == nil {
						continue
					}
					if !hasValidateMethod(fieldType) && !hasValidatableElements(fieldType) {
						continue
					}
					// Skip fields with //goplint:no-delegate directive.
					if hasNoDelegateDirective(field.Doc, field.Comment) {
						continue
					}
					if len(field.Names) > 0 {
						for _, name := range field.Names {
							validatableKeys = append(validatableKeys, name.Name)
						}
					} else {
						// Anonymous embedded field — use the type name as the key.
						// Accessed as receiver.TypeName in Go.
						if embName := embeddedFieldTypeName(field.Type); embName != "" {
							validatableKeys = append(validatableKeys, embName)
						}
					}
				}

				if len(validatableKeys) > 0 {
					targets = append(targets, validateAllStruct{
						name:            ts.Name.Name,
						pos:             ts.Name.Pos(),
						validatableKeys: validatableKeys,
					})
				}
			}
		}
	}

	if len(targets) == 0 {
		return
	}

	// Phase 2: For each target, find its Validate() method and check
	// which fields are delegated.
	for _, target := range targets {
		calledFields := findDelegatedFields(pass, target.name)
		for _, fieldName := range target.validatableKeys {
			if calledFields[fieldName] {
				continue
			}

			qualName := fmt.Sprintf("%s.%s", pkgName, target.name)
			excKey := fmt.Sprintf("%s.%s.validate-delegation", qualName, fieldName)
			if cfg.isExcepted(excKey) {
				continue
			}

			msg := fmt.Sprintf(
				"%s.Validate() does not delegate to field %s which has Validate()",
				qualName, fieldName)
			findingID := StableFindingID(CategoryIncompleteValidateDelegation, qualName, fieldName)
			if bl.ContainsFinding(CategoryIncompleteValidateDelegation, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, target.pos, CategoryIncompleteValidateDelegation, findingID, msg)
		}
	}
}

// findDelegatedFields searches the package for a Validate() method on the
// given type and returns a set of field names that appear in
// `receiver.Field.Validate()` call patterns. Also handles intermediate
// variable assignment: `field := receiver.Field; field.Validate()`.
func findDelegatedFields(pass *analysis.Pass, typeName string) map[string]bool {
	called := make(map[string]bool)

	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			recvName := receiverTypeName(fn.Recv.List[0].Type)
			if recvName != typeName || fn.Name.Name != "Validate" {
				continue
			}

			// Get the receiver variable name (e.g., "c" in "func (c *Config)")
			recvVarName := ""
			if len(fn.Recv.List[0].Names) > 0 {
				recvVarName = fn.Recv.List[0].Names[0].Name
			}

			// Pass 1: Direct receiver.Field.Validate() calls.
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Validate" {
					return true
				}
				innerSel, ok := sel.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if recvVarName != "" {
					if ident, ok := innerSel.X.(*ast.Ident); ok && ident.Name == recvVarName {
						called[innerSel.Sel.Name] = true
					}
				}
				return true
			})

			// Pass 2: Intermediate variable pattern:
			//   field := receiver.Field
			//   field.Validate()
			fieldAliases := make(map[string]string)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				assign, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, rhs := range assign.Rhs {
					sel, ok := rhs.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					if recvVarName != "" {
						if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == recvVarName {
							if i < len(assign.Lhs) {
								if lhsIdent, ok := assign.Lhs[i].(*ast.Ident); ok {
									fieldAliases[lhsIdent.Name] = sel.Sel.Name
								}
							}
						}
					}
				}
				return true
			})
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				callExpr, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selExpr, ok := callExpr.Fun.(*ast.SelectorExpr)
				if !ok || selExpr.Sel.Name != "Validate" {
					return true
				}
				ident, ok := selExpr.X.(*ast.Ident)
				if !ok {
					return true
				}
				if fieldName, ok := fieldAliases[ident.Name]; ok {
					called[fieldName] = true
				}
				return true
			})

			// Pass 3: Range loop delegation pattern:
			//   for _, r := range receiver.Field { r.Validate() }
			// Recognizes iteration over slice/array fields with
			// validatable element types.
			ast.Inspect(fn.Body, func(n ast.Node) bool { //nolint:dupl // distinct AST pattern
				rangeStmt, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				sel, ok := rangeStmt.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if recvVarName == "" {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || ident.Name != recvVarName {
					return true
				}
				fieldName := sel.Sel.Name

				// Get the range value variable name.
				valueVar := ""
				if rangeStmt.Value != nil {
					if vi, ok := rangeStmt.Value.(*ast.Ident); ok {
						valueVar = vi.Name
					}
				}
				if valueVar == "" {
					return true
				}

				// Check if the loop body calls valueVar.Validate().
				ast.Inspect(rangeStmt.Body, func(inner ast.Node) bool {
					call, ok := inner.(*ast.CallExpr)
					if !ok {
						return true
					}
					callSel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || callSel.Sel.Name != "Validate" {
						return true
					}
					if vi, ok := callSel.X.(*ast.Ident); ok && vi.Name == valueVar {
						called[fieldName] = true
					}
					return true
				})
				return true
			})
			// Pass 4: Helper method delegation pattern:
			//   func (c *Config) Validate() error { return c.validateFields() }
			// When Validate() calls a method on the same receiver, walk that
			// method's body for direct field delegations.
			if recvVarName != "" {
				findHelperMethodDelegations(pass, fn.Body, typeName, recvVarName, nil, 0, called)
			}
		}
	}

	return called
}

// maxHelperMethodDepth bounds recursion in multi-level helper method
// delegation tracking to prevent pathological cases.
const maxHelperMethodDepth = 3

// findHelperMethodDelegations finds receiver.helperMethod() calls in the
// given body, then recursively walks each helper method's body for direct
// field delegation patterns. Writes delegated field names directly into
// the out accumulator. Bounds recursion with a visited set and depth limit.
func findHelperMethodDelegations(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	typeName, recvVarName string,
	visited map[string]bool,
	depth int,
	out map[string]bool,
) {
	if depth >= maxHelperMethodDepth {
		return
	}
	if visited == nil {
		visited = make(map[string]bool)
	}

	// Collect receiver.helperMethod() calls.
	var helperNames []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || ident.Name != recvVarName {
			return true
		}
		// Skip "Validate" itself to avoid infinite recursion.
		if sel.Sel.Name != "Validate" {
			helperNames = append(helperNames, sel.Sel.Name)
		}
		return true
	})

	// For each helper, find its method body and check for delegations.
	for _, helperName := range helperNames {
		if visited[helperName] {
			continue
		}
		visited[helperName] = true

		helperBody, helperRecvVar := findMethodBody(pass, typeName, helperName)
		if helperBody == nil {
			continue
		}

		// Walk the helper for direct receiver.Field.Validate() patterns.
		ast.Inspect(helperBody, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Validate" {
				return true
			}
			innerSel, ok := sel.X.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if helperRecvVar != "" {
				if id, ok := innerSel.X.(*ast.Ident); ok && id.Name == helperRecvVar {
					out[innerSel.Sel.Name] = true
				}
			}
			return true
		})

		// Recurse: check if this helper calls further helpers that
		// contain field delegations.
		findHelperMethodDelegations(pass, helperBody, typeName, helperRecvVar, visited, depth+1, out)
	}
}

// findMethodBody searches the package for a method with the given receiver
// type and name. Returns the body and the receiver variable name.
func findMethodBody(pass *analysis.Pass, typeName, methodName string) (*ast.BlockStmt, string) {
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			recvName := receiverTypeName(fn.Recv.List[0].Type)
			if recvName != typeName || fn.Name.Name != methodName {
				continue
			}
			recvVar := ""
			if len(fn.Recv.List[0].Names) > 0 {
				recvVar = fn.Recv.List[0].Names[0].Name
			}
			return fn.Body, recvVar
		}
	}
	return nil, ""
}

// embeddedFieldTypeName extracts the type name from an anonymous embedded
// field's type expression. Returns the simple type name for same-package
// types (*ast.Ident), qualified name for imported types (*ast.SelectorExpr),
// and handles pointer embeds (*ast.StarExpr). Returns "" if unrecognized.
func embeddedFieldTypeName(expr ast.Expr) string {
	// Unwrap pointer: *Name → Name
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		// Imported type: pkg.Name — the field key is just Name.
		return e.Sel.Name
	}
	return ""
}

// hasValidateAllDirective checks whether a type declaration has the
// //goplint:validate-all directive.
func hasValidateAllDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) bool {
	return hasDirectiveKey(genDoc, nil, "validate-all") || hasDirectiveKey(specDoc, nil, "validate-all")
}

// inspectSuggestValidateAll reports structs that have Validate() and at least
// one field whose type also has Validate(), but are not annotated with
// //goplint:validate-all. This is an advisory mode to help identify candidates
// for the directive — it does not block CI.
func inspectSuggestValidateAll(
	pass *analysis.Pass,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	pkgName := packageName(pass.Pkg)

	// Build a set of type names with Validate() methods in this package.
	validatableTypes := make(map[string]bool)
	for _, obj := range pass.TypesInfo.Defs {
		if obj == nil {
			continue
		}
		named := resolveNamedType(obj.Type())
		if named == nil {
			continue
		}
		if hasValidateMethod(named) {
			validatableTypes[obj.Name()] = true
		}
	}

	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok || st.Fields == nil {
					continue
				}

				// Skip if already annotated.
				if hasValidateAllDirective(gd.Doc, ts.Doc) {
					continue
				}

				// Skip if the struct itself doesn't have Validate().
				if !validatableTypes[ts.Name.Name] {
					continue
				}

				// Count fields whose types have Validate().
				validatableFieldCount := 0
				for _, field := range st.Fields.List {
					fieldType := pass.TypesInfo.TypeOf(field.Type)
					if fieldType == nil {
						continue
					}
					named := resolveNamedType(fieldType)
					if named != nil && hasValidateMethod(named) {
						if len(field.Names) > 0 {
							validatableFieldCount += len(field.Names)
						} else {
							validatableFieldCount++
						}
					}
				}

				if validatableFieldCount == 0 {
					continue
				}

				qualName := fmt.Sprintf("%s.%s", pkgName, ts.Name.Name)
				excKey := qualName + ".suggest-validate-all"
				if cfg.isExcepted(excKey) {
					continue
				}

				msg := fmt.Sprintf(
					"struct %s has Validate() and %d validatable field(s) but no //goplint:validate-all directive",
					qualName, validatableFieldCount)
				findingID := StableFindingID(CategorySuggestValidateAll, qualName)
				if bl.ContainsFinding(CategorySuggestValidateAll, findingID, msg) {
					continue
				}

				reportDiagnostic(pass, ts.Name.Pos(), CategorySuggestValidateAll, findingID, msg)
			}
		}
	}
}

// resolveNamedType dereferences pointers and aliases to find the underlying
// *types.Named type. Returns nil if the type is not a named type.
func resolveNamedType(t types.Type) *types.Named {
	// Dereference pointers.
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	t = types.Unalias(t)
	named, _ := t.(*types.Named)
	return named
}
