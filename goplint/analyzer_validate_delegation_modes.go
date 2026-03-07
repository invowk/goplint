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

// collectValidatableFieldKeys collects field names (or embedded type names)
// whose types have Validate() or validatable elements (slice/map with
// Validate() element type), skipping fields with //goplint:no-delegate.
func collectValidatableFieldKeys(pass *analysis.Pass, st *ast.StructType) []string {
	var keys []string
	for _, field := range st.Fields.List {
		fieldType := pass.TypesInfo.TypeOf(field.Type)
		if fieldType == nil {
			continue
		}
		if !hasValidateMethod(fieldType) && !hasValidatableElements(fieldType) {
			continue
		}
		if hasNoDelegateDirective(field.Doc, field.Comment) {
			continue
		}
		if len(field.Names) > 0 {
			for _, name := range field.Names {
				keys = append(keys, name.Name)
			}
		} else {
			// Anonymous embedded field — use the type name as the key.
			if embName := embeddedFieldTypeName(field.Type); embName != "" {
				keys = append(keys, embName)
			}
		}
	}
	return keys
}

// reportIncompleteDelegation checks which validatable fields are delegated in
// the struct's Validate() method and reports any that are missing.
func reportIncompleteDelegation(
	pass *analysis.Pass,
	structName string,
	structPos token.Pos,
	validatableKeys []string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	pkgName := packageName(pass.Pkg)
	qualName := fmt.Sprintf("%s.%s", pkgName, structName)
	calledFields := findDelegatedFields(pass, structName)
	for _, fieldName := range validatableKeys {
		if calledFields[fieldName] {
			continue
		}

		excKey := fmt.Sprintf("%s.%s.validate-delegation", qualName, fieldName)
		if cfg.isExcepted(excKey) {
			continue
		}

		msg := fmt.Sprintf(
			"%s.Validate() does not delegate to field %s which has Validate()",
			qualName, fieldName)
		findingID := PackageScopedFindingID(pass, CategoryIncompleteValidateDelegation, qualName, fieldName)
		if bl.ContainsFinding(CategoryIncompleteValidateDelegation, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, structPos, CategoryIncompleteValidateDelegation, findingID, msg)
	}
}

// inspectValidateDelegationAll is the universal version of
// inspectValidateDelegation. It checks ALL structs (not just those with
// //goplint:validate-all) for two conditions:
//
//  1. Struct has validatable fields but no Validate() method -> reports
//     missing-struct-validate-fields.
//  2. Struct has Validate() and validatable fields but does not delegate
//     to all of them -> reports incomplete-validate-delegation.
//
// Error-type structs (name ending in "Error" or implementing the error
// interface) are excluded, consistent with --check-constructors and
// --check-struct-validate.
func inspectValidateDelegationAll(pass *analysis.Pass, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)

	// Build the set of type names with Validate() in this package.
	validatableTypes := buildValidatableStructs(pass)

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
				structName := ts.Name.Name

				// Skip structs with //goplint:validate-all — those are handled
				// by the opt-in --check-validate-delegation mode. This avoids
				// duplicate findings when both modes are active.
				if hasValidateAllDirective(gd.Doc, ts.Doc) {
					continue
				}

				// Skip error types — they typically don't need Validate().
				if isErrorStructByAST(pass, structName) {
					continue
				}

				validatableKeys := collectValidatableFieldKeys(pass, st)
				if len(validatableKeys) == 0 {
					continue
				}

				qualName := fmt.Sprintf("%s.%s", pkgName, structName)

				// Sub-case 1: struct has validatable fields but no Validate() at all.
				if !validatableTypes[structName] {
					excKey := qualName + ".struct-validate-fields"
					if cfg.isExcepted(excKey) {
						continue
					}

					msg := fmt.Sprintf(
						"struct %s has %d validatable field(s) but no Validate() method",
						qualName, len(validatableKeys))
					findingID := PackageScopedFindingID(pass, CategoryMissingStructValidateFields, qualName)
					if bl.ContainsFinding(CategoryMissingStructValidateFields, findingID, msg) {
						continue
					}

					reportDiagnostic(pass, ts.Name.Pos(), CategoryMissingStructValidateFields, findingID, msg)
					continue
				}

				// Sub-case 2: struct has Validate() — check delegation completeness.
				reportIncompleteDelegation(pass, structName, ts.Name.Pos(), validatableKeys, cfg, bl)
			}
		}
	}
}

// isErrorStructByAST reports whether the given struct name represents an error
// type. Uses both naming convention (suffix "Error") and method set checking
// (has an Error() string method) via the type checker. Consistent with the
// error-type exclusion in reportMissingConstructors and reportMissingStructValidate.
func isErrorStructByAST(pass *analysis.Pass, structName string) bool {
	if strings.HasSuffix(structName, "Error") {
		return true
	}
	obj := pass.Pkg.Scope().Lookup(structName)
	if obj == nil {
		return false
	}
	// Unalias before Named assertion — required for Go 1.22+ type aliases.
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok {
		return false
	}
	// Check pointer receiver method set (superset of value receiver).
	mset := types.NewMethodSet(types.NewPointer(named))
	for method := range mset.Methods() {
		if method.Obj().Name() != "Error" {
			continue
		}
		sig, sigOK := method.Obj().Type().(*types.Signature)
		if !sigOK {
			continue
		}
		if sig.Params().Len() == 0 && sig.Results().Len() == 1 {
			if isStringType(sig.Results().At(0).Type()) {
				return true
			}
		}
	}
	return false
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
