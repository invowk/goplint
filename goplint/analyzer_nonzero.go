// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// NonZeroFact is an analysis.Fact exported for types annotated with
// //goplint:nonzero. When a type in package A has this directive, the
// fact is serialized and available to downstream packages that import A.
// This enables cross-package enforcement: struct fields in package B
// using a nonzero type from A as a value (non-pointer) field are flagged
// because the zero value is invalid — they should use *Type instead.
type NonZeroFact struct{}

// AFact implements the analysis.Fact interface marker method.
func (*NonZeroFact) AFact() {}

// String returns a human-readable representation for analysistest fact matching.
func (*NonZeroFact) String() string { return "nonzero" }

// inspectNonZero performs two phases of nonzero analysis:
//
// Phase 1 (export): Scans type declarations for //goplint:nonzero directives
// and exports NonZeroFact for each annotated type.
//
// Phase 2 (check): Scans struct fields. For each field whose type has a
// NonZeroFact and is NOT a pointer, reports a nonzero-value-field diagnostic.
func inspectNonZero(pass *analysis.Pass, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)

	// Phase 1: Export facts for types with //goplint:nonzero directive.
	for _, file := range pass.Files {
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
				// Skip type aliases — they inherit facts from the target.
				if ts.Assign.IsValid() {
					continue
				}
				if !hasNonZeroDirective(gd.Doc, ts.Doc) {
					continue
				}
				obj := pass.TypesInfo.Defs[ts.Name]
				if obj == nil {
					continue
				}
				pass.ExportObjectFact(obj, &NonZeroFact{})
			}
		}
	}

	// Phase 2: Check struct fields for nonzero types used as value fields.
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
				structQualName := pkgName + "." + ts.Name.Name
				checkStructFieldsNonZero(pass, st, structQualName, cfg, bl)
			}
		}
	}
}

// checkStructFieldsNonZero inspects each field of a struct for nonzero
// type violations. A violation occurs when a field's type has NonZeroFact
// but the field is not a pointer type.
func checkStructFieldsNonZero(
	pass *analysis.Pass,
	st *ast.StructType,
	structQualName string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	for _, field := range st.Fields.List {
		if hasIgnoreDirective(field.Doc, field.Comment) {
			continue
		}

		// Skip pointer fields — *Type is the correct way to use nonzero types
		// for optional fields.
		if _, isPtr := field.Type.(*ast.StarExpr); isPtr {
			continue
		}

		fieldType := pass.TypesInfo.TypeOf(field.Type)
		if fieldType == nil {
			continue
		}

		// Resolve the type object to check for NonZeroFact.
		typeObj := resolveTypeObject(fieldType)
		if typeObj == nil {
			continue
		}

		var fact NonZeroFact
		if !pass.ImportObjectFact(typeObj, &fact) {
			continue
		}

		// The field uses a nonzero type as a value — flag it.
		for _, name := range field.Names {
			qualName := fmt.Sprintf("%s.%s", structQualName, name.Name)
			excKey := qualName + ".nonzero"
			if cfg.isExcepted(excKey) {
				continue
			}

			typeName := typeObj.Name()
			msg := fmt.Sprintf("struct field %s uses nonzero type %s as value; use *%s for optional fields",
				qualName, typeName, typeName)
			findingID := StableFindingID(CategoryNonZeroValueField, qualName, typeName)
			if bl.ContainsFinding(CategoryNonZeroValueField, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, name.Pos(), CategoryNonZeroValueField, findingID, msg)
		}

		// Anonymous/embedded fields.
		if len(field.Names) == 0 {
			qualName := fmt.Sprintf("%s.(embedded)", structQualName)
			excKey := qualName + ".nonzero"
			if cfg.isExcepted(excKey) {
				continue
			}

			typeName := typeObj.Name()
			msg := fmt.Sprintf("struct field %s uses nonzero type %s as value; use *%s for optional fields",
				qualName, typeName, typeName)
			findingID := StableFindingID(CategoryNonZeroValueField, qualName, typeName)
			if bl.ContainsFinding(CategoryNonZeroValueField, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, field.Pos(), CategoryNonZeroValueField, findingID, msg)
		}
	}
}

// resolveTypeObject extracts the types.Object for a named type, resolving
// through aliases. Returns nil for non-named types (primitives, etc.).
func resolveTypeObject(t types.Type) types.Object {
	t = types.Unalias(t)
	named, ok := t.(*types.Named)
	if !ok {
		return nil
	}
	return named.Obj()
}

// hasNonZeroDirective checks whether a type declaration has the
// //goplint:nonzero (or //plint:nonzero) directive. Checks both the
// GenDecl-level doc (for single-spec type blocks) and the TypeSpec-level doc.
func hasNonZeroDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) bool {
	return hasDirectiveKey(genDoc, nil, "nonzero") || hasDirectiveKey(specDoc, nil, "nonzero")
}
