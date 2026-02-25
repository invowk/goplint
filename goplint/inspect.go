// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// inspectStructFields checks all named struct types in the file for fields
// using bare primitive types. Findings present in the baseline are suppressed.
func inspectStructFields(pass *analysis.Pass, node *ast.GenDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
	if node.Tok != token.TYPE {
		return
	}

	pkgName := packageName(pass.Pkg)

	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			// Not a struct — could be a type definition like "type X string".
			// These are the DDD types themselves; skip.
			continue
		}

		structName := pkgName + "." + ts.Name.Name

		for _, field := range st.Fields.List {
			reportUnknownDirectives(pass, field.Doc, field.Comment)
			if hasIgnoreDirective(field.Doc, field.Comment) || hasRenderDirective(field.Doc, field.Comment) {
				continue
			}

			fieldType := pass.TypesInfo.TypeOf(field.Type)
			if fieldType == nil {
				continue
			}

			if !isPrimitive(fieldType) {
				continue
			}

			// For map types, produce a targeted message identifying which
			// part(s) of the map are primitive instead of showing the full
			// composite type.
			typeName := primitiveTypeName(fieldType)
			if detail, ok := primitiveMapDetail(fieldType); ok {
				typeName = detail
			}
			if cfg.isSkippedType(typeName) {
				continue
			}

			for _, name := range field.Names {
				qualName := fmt.Sprintf("%s.%s", structName, name.Name)
				if cfg.isExcepted(qualName) {
					continue
				}

				msg := fmt.Sprintf("struct field %s uses primitive type %s", qualName, typeName)
				findingID := StableFindingID(CategoryPrimitive, "struct-field", qualName, typeName)
				if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
					continue
				}

				reportDiagnostic(pass, name.Pos(), CategoryPrimitive, findingID, msg)
			}

			// Anonymous/embedded fields (no names)
			if len(field.Names) == 0 {
				qualName := fmt.Sprintf("%s.(embedded)", structName)
				if cfg.isExcepted(qualName) {
					continue
				}

				msg := fmt.Sprintf("struct field %s uses primitive type %s", qualName, typeName)
				findingID := StableFindingID(CategoryPrimitive, "struct-field", qualName, typeName)
				if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
					continue
				}

				reportDiagnostic(pass, field.Pos(), CategoryPrimitive, findingID, msg)
			}
		}
	}
}

// inspectFuncDecl checks function/method parameters and return types for
// bare primitive types. Findings present in the baseline are suppressed.
func inspectFuncDecl(pass *analysis.Pass, fn *ast.FuncDecl, cfg *ExceptionConfig, bl *BaselineConfig) {
	if shouldSkipFunc(fn) {
		return
	}

	reportUnknownDirectives(pass, fn.Doc, nil)
	if hasIgnoreDirective(fn.Doc, nil) {
		return
	}

	isRender := hasRenderDirective(fn.Doc, nil)

	pkgName := packageName(pass.Pkg)
	funcName := fn.Name.Name

	// If it's a method, prefix with the receiver type name.
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvName := receiverTypeName(fn.Recv.List[0].Type)
		if recvName != "" {
			funcName = recvName + "." + funcName
		}
	}

	// Prefix with package name for exception matching.
	funcName = pkgName + "." + funcName

	// Check parameters — always checked, even with //goplint:render.
	if fn.Type.Params != nil {
		inspectFieldList(pass, fn.Type.Params, funcName, "parameter", cfg, bl)
	}

	// Check return types — skip for render functions (display output),
	// and for well-known interface methods (String, Error, GoString,
	// MarshalText) whose return types are dictated by the interface contract.
	if fn.Type.Results != nil && !isRender && !isInterfaceMethodReturn(fn) {
		inspectReturnTypes(pass, fn.Type.Results, funcName, cfg, bl)
	}
}

// inspectFieldList checks a function's parameter list for primitive types.
// Findings present in the baseline are suppressed.
func inspectFieldList(pass *analysis.Pass, fields *ast.FieldList, funcName, kind string, cfg *ExceptionConfig, bl *BaselineConfig) {
	for fieldIndex, field := range fields.List {
		reportUnknownDirectives(pass, field.Doc, field.Comment)
		if hasIgnoreDirective(field.Doc, field.Comment) {
			continue
		}

		fieldType := pass.TypesInfo.TypeOf(field.Type)
		if fieldType == nil {
			continue
		}

		if !isPrimitive(fieldType) {
			continue
		}

		typeName := primitiveTypeName(fieldType)
		if detail, ok := primitiveMapDetail(fieldType); ok {
			typeName = detail
		}
		if cfg.isSkippedType(typeName) {
			continue
		}

		for _, name := range field.Names {
			qualName := fmt.Sprintf("%s.%s", funcName, name.Name)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("%s %q of %s uses primitive type %s", kind, name.Name, funcName, typeName)
			findingID := StableFindingID(CategoryPrimitive, kind, funcName, name.Name, typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, name.Pos(), CategoryPrimitive, findingID, msg)
		}

		// Unnamed parameters (e.g., func(string))
		if len(field.Names) == 0 {
			qualName := fmt.Sprintf("%s.(unnamed)", funcName)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("unnamed %s of %s uses primitive type %s", kind, funcName, typeName)
			findingID := StableFindingID(CategoryPrimitive, "unnamed-"+kind, funcName, strconv.Itoa(fieldIndex), typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, field.Pos(), CategoryPrimitive, findingID, msg)
		}
	}
}

// inspectReturnTypes checks a function's return types for primitive types.
// Findings present in the baseline are suppressed.
func inspectReturnTypes(pass *analysis.Pass, results *ast.FieldList, funcName string, cfg *ExceptionConfig, bl *BaselineConfig) {
	for i, field := range results.List {
		reportUnknownDirectives(pass, field.Doc, field.Comment)
		if hasIgnoreDirective(field.Doc, field.Comment) {
			continue
		}

		fieldType := pass.TypesInfo.TypeOf(field.Type)
		if fieldType == nil {
			continue
		}

		if !isPrimitive(fieldType) {
			continue
		}

		typeName := primitiveTypeName(fieldType)
		if detail, ok := primitiveMapDetail(fieldType); ok {
			typeName = detail
		}
		if cfg.isSkippedType(typeName) {
			continue
		}

		// Named return values
		for _, name := range field.Names {
			qualName := fmt.Sprintf("%s.return.%s", funcName, name.Name)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("return value %q of %s uses primitive type %s", name.Name, funcName, typeName)
			findingID := StableFindingID(CategoryPrimitive, "named-return", funcName, name.Name, typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, name.Pos(), CategoryPrimitive, findingID, msg)
		}

		// Unnamed return values
		if len(field.Names) == 0 {
			qualName := fmt.Sprintf("%s.return.%d", funcName, i)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("return value of %s uses primitive type %s", funcName, typeName)
			findingID := StableFindingID(CategoryPrimitive, "return", funcName, strconv.Itoa(i), typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, field.Pos(), CategoryPrimitive, findingID, msg)
		}
	}
}

// shouldSkipFunc returns true for functions that should not be analyzed:
// init, main, test functions, and generated code.
func shouldSkipFunc(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	switch {
	case name == "init" || name == "main":
		return true
	case strings.HasPrefix(name, "Test"):
		return true
	case strings.HasPrefix(name, "Benchmark"):
		return true
	case strings.HasPrefix(name, "Fuzz"):
		return true
	case strings.HasPrefix(name, "Example"):
		return true
	default:
		return false
	}
}

// isInterfaceMethodReturn returns true if the function is a method whose
// return type is dictated by a well-known interface contract and whose
// signature matches that contract. These methods MUST return specific types
// by the interface definition and cannot use named types.
//
// Skipped patterns (name + matching signature):
//   - String() string (fmt.Stringer) — 0 params, 1 result
//   - Error() string (error interface) — 0 params, 1 result
//   - GoString() string (fmt.GoStringer) — 0 params, 1 result
//   - MarshalText() ([]byte, error) (encoding.TextMarshaler) — 0 params, 2 results
//   - MarshalBinary() ([]byte, error) (encoding.BinaryMarshaler) — 0 params, 2 results
//   - MarshalJSON() ([]byte, error) (json.Marshaler) — 0 params, 2 results
func isInterfaceMethodReturn(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || fn.Type == nil {
		return false
	}

	params := fn.Type.Params
	results := fn.Type.Results

	// All recognized interface methods have zero parameters.
	if params != nil && countParams(params) != 0 {
		return false
	}

	switch fn.Name.Name {
	case "String", "Error", "GoString":
		// Expected: () string — zero params, one string result.
		if results == nil || len(results.List) != 1 {
			return false
		}
		// Verify the return type is the string identifier, not just any
		// single-result method. Without this, String() int would be
		// incorrectly treated as fmt.Stringer and its return suppressed.
		ident, ok := results.List[0].Type.(*ast.Ident)
		return ok && ident.Name == "string"
	case "MarshalText", "MarshalBinary", "MarshalJSON":
		// Expected: () ([]byte, error) — zero params, two results.
		return hasByteSliceErrorResults(results)
	default:
		return false
	}
}

// hasByteSliceErrorResults reports whether results is exactly ([]byte, error).
func hasByteSliceErrorResults(results *ast.FieldList) bool {
	if results == nil || len(results.List) != 2 {
		return false
	}
	// Verify first result is []byte (array type with nil len = slice).
	arr, ok := results.List[0].Type.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	elemIdent, ok := arr.Elt.(*ast.Ident)
	if !ok || elemIdent.Name != "byte" {
		return false
	}
	// Verify second result is error.
	errIdent, ok := results.List[1].Type.(*ast.Ident)
	return ok && errIdent.Name == "error"
}

// receiverTypeName extracts the type name from a method receiver expression.
// Handles both value receivers (T) and pointer receivers (*T).
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		// Recurse to handle both *T (simple pointer receiver) and
		// *T[P] (pointer to generic receiver).
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		// Generic type: T[P]
		if ident, ok := e.X.(*ast.Ident); ok {
			return ident.Name
		}
	case *ast.IndexListExpr:
		// Generic type: T[P1, P2]
		if ident, ok := e.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// knownDirectiveKeys lists all recognized directive keys for validation.
// Unknown keys in a //goplint: or //plint: comment trigger an
// unknown-directive warning.
var knownDirectiveKeys = map[string]bool{
	"ignore":   true,
	"internal": true,
	"render":   true,
}

// hasIgnoreDirective checks whether a field/func has an ignore directive.
// Recognized forms: //goplint:ignore, //plint:ignore,
// //nolint:goplint, and combined forms like //goplint:ignore,internal.
func hasIgnoreDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "ignore")
}

// hasInternalDirective checks whether a struct field has an internal
// directive, indicating the field is internal state that should not be
// initialized via functional options (excluded from WithXxx() checks).
// Recognized forms: //goplint:internal, //plint:internal, and combined
// forms like //goplint:ignore,internal.
func hasInternalDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "internal")
}

// hasRenderDirective checks whether a func/field has a render directive,
// indicating the return value is intentionally a bare string (rendered
// display text). On functions, this suppresses return-type findings only
// — parameters are still checked. On struct fields, it behaves like ignore.
func hasRenderDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "render")
}

// hasDirectiveKey checks whether the given directive key appears in any
// goplint/plint directive in the doc or line comment groups.
func hasDirectiveKey(doc *ast.CommentGroup, lineComment *ast.CommentGroup, key string) bool {
	for _, cg := range []*ast.CommentGroup{doc, lineComment} {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			text := strings.TrimSpace(c.Text)
			keys, _ := parseDirectiveKeys(text)
			for _, k := range keys {
				if k == key {
					return true
				}
			}
		}
	}
	return false
}

// reportUnknownDirectives emits an unknown-directive diagnostic for each
// unrecognized key in a goplint/plint directive comment. Called at
// every site where directives are checked, so typos like //goplint:ignorr
// are caught immediately.
//
// Intentionally does not check baseline — typo warnings must always be visible.
func reportUnknownDirectives(pass *analysis.Pass, doc *ast.CommentGroup, lineComment *ast.CommentGroup) {
	for _, cg := range []*ast.CommentGroup{doc, lineComment} {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			text := strings.TrimSpace(c.Text)
			_, unknown := parseDirectiveKeys(text)
			for _, u := range unknown {
				msg := fmt.Sprintf("unknown directive key %q in goplint directive", u)
				findingID := StableFindingID(CategoryUnknownDirective, "directive", u)
				reportDiagnostic(pass, c.Pos(), CategoryUnknownDirective, findingID, msg)
			}
		}
	}
}

// parseDirectiveKeys extracts directive keys from a goplint/plint
// comment. Returns known keys and unknown keys separately.
//
// Supported forms (single prefix, comma-separated keys):
//
//	//goplint:ignore            → (["ignore"], nil)
//	//plint:ignore              → (["ignore"], nil)
//	//goplint:ignore,internal   → (["ignore", "internal"], nil)
//	//plint:ignore,foo          → (["ignore"], ["foo"])
//	//nolint:goplint            → (["ignore"], nil)  — special case
//	// regular comment          → (nil, nil)
func parseDirectiveKeys(text string) (keys []string, unknown []string) {
	// Handle nolint:goplint as a special "ignore" directive.
	// This is a golangci-lint convention — always means "suppress all".
	if strings.Contains(text, "nolint:goplint") {
		return []string{"ignore"}, nil
	}

	// Look for goplint: or plint: prefix.
	var valueStr string
	for _, prefix := range []string{"goplint:", "plint:"} {
		idx := strings.Index(text, prefix)
		if idx < 0 {
			continue
		}
		valueStr = text[idx+len(prefix):]
		break
	}
	if valueStr == "" {
		return nil, nil
	}

	// Trim the optional "-- reason" suffix. The convention is to use
	// " -- " as the separator between directive keys and explanation text.
	if sepIdx := strings.Index(valueStr, " --"); sepIdx >= 0 {
		valueStr = valueStr[:sepIdx]
	}

	// Split by comma and classify each token.
	for part := range strings.SplitSeq(valueStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if knownDirectiveKeys[part] {
			keys = append(keys, part)
		} else {
			unknown = append(unknown, part)
		}
	}
	return keys, unknown
}

// isTestFile returns true if the filename ends with _test.go.
func isTestFile(pass *analysis.Pass, pos token.Pos) bool {
	file := pass.Fset.Position(pos).Filename
	return strings.HasSuffix(file, "_test.go")
}

// packageName extracts the last segment of a package path for qualified names.
func packageName(pkg *types.Package) string {
	if pkg == nil {
		return ""
	}
	path := pkg.Path()
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}
