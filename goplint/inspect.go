// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
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
			typeName, ok := primitiveFindingTypeName(fieldType, cfg)
			if !ok {
				continue
			}

			for _, name := range field.Names {
				qualName := fmt.Sprintf("%s.%s", structName, name.Name)
				if cfg.isExcepted(qualName) {
					continue
				}

				msg := fmt.Sprintf("struct field %s uses primitive type %s", qualName, typeName)
				findingID := PackageScopedFindingID(pass, CategoryPrimitive, "struct-field", qualName, typeName)
				if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
					continue
				}

				reportDiagnostic(pass, name.Pos(), CategoryPrimitive, findingID, msg)
			}

			// Anonymous/embedded fields (no names)
			if len(field.Names) == 0 {
				qualName := structName + ".(embedded)"
				if cfg.isExcepted(qualName) {
					continue
				}

				msg := fmt.Sprintf("struct field %s uses primitive type %s", qualName, typeName)
				findingID := PackageScopedFindingID(pass, CategoryPrimitive, "struct-field", qualName, typeName)
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
	contractMethod := isKnownInterfaceContractMethod(pass, fn)

	// Check parameters — always checked, even with //goplint:render.
	if fn.Type.Params != nil && !contractMethod {
		inspectFieldList(pass, fn.Type.Params, funcName, "parameter", cfg, bl)
	}

	// Check return types — skip for render functions (display output),
	// and for well-known interface methods (String, Error, GoString,
	// MarshalText) whose return types are dictated by the interface contract.
	if fn.Type.Results != nil && !isRender && !isInterfaceMethodReturn(fn) && !contractMethod {
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

		typeName, ok := primitiveFindingTypeName(fieldType, cfg)
		if !ok {
			continue
		}

		for _, name := range field.Names {
			qualName := fmt.Sprintf("%s.%s", funcName, name.Name)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("%s %q of %s uses primitive type %s", kind, name.Name, funcName, typeName)
			findingID := PackageScopedFindingID(pass, CategoryPrimitive, kind, funcName, name.Name, typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, name.Pos(), CategoryPrimitive, findingID, msg)
		}

		// Unnamed parameters (e.g., func(string))
		if len(field.Names) == 0 {
			qualName := funcName + ".(unnamed)"
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("unnamed %s of %s uses primitive type %s", kind, funcName, typeName)
			findingID := PackageScopedFindingID(pass, CategoryPrimitive, "unnamed-"+kind, funcName, strconv.Itoa(fieldIndex), typeName)
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

		typeName, ok := primitiveFindingTypeName(fieldType, cfg)
		if !ok {
			continue
		}

		// Named return values
		for _, name := range field.Names {
			qualName := fmt.Sprintf("%s.return.%s", funcName, name.Name)
			if cfg.isExcepted(qualName) {
				continue
			}

			msg := fmt.Sprintf("return value %q of %s uses primitive type %s", name.Name, funcName, typeName)
			findingID := PackageScopedFindingID(pass, CategoryPrimitive, "named-return", funcName, name.Name, typeName)
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
			findingID := PackageScopedFindingID(pass, CategoryPrimitive, "return", funcName, strconv.Itoa(i), typeName)
			if bl.ContainsFinding(CategoryPrimitive, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, field.Pos(), CategoryPrimitive, findingID, msg)
		}
	}
}

// shouldSkipFunc returns true for functions that should not be analyzed.
// Test/benchmark/fuzz/example functions are skipped at file traversal level
// (_test.go files), so function-name prefix checks are intentionally avoided.
func shouldSkipFunc(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	switch {
	case name == "init" || name == "main":
		return true
	default:
		return false
	}
}

func primitiveFindingTypeName(fieldType types.Type, cfg *ExceptionConfig) (string, bool) {
	if detail, ok := primitiveMapDetailWithSkip(fieldType, cfg); ok {
		return detail, true
	}
	if _, isMap := types.Unalias(fieldType).(*types.Map); isMap {
		return "", false
	}
	typeName := primitiveTypeName(fieldType)
	if cfg != nil && cfg.isSkippedType(typeName) {
		return "", false
	}
	return typeName, true
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

func isKnownDirectiveKey(key string) bool {
	switch key {
	case "ignore",
		"internal",
		"render",
		"nonzero",
		"validate-all",
		"constant-only",
		"mutable",
		"no-delegate",
		"enum-cue",
		"validates-type",
		"trusted-boundary",
		"cue-fed-path",
		"path-domain":
		return true
	default:
		return false
	}
}

// hasIgnoreDirective checks whether a field/func has an ignore directive.
// Recognized forms: `//goplint:ignore`, `//plint:ignore`,
// `//nolint:goplint`, and combined forms like `//goplint:ignore,internal`.
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

// hasMutableDirective checks whether a struct type has a mutable directive,
// indicating the struct is intentionally mutable despite having a constructor.
// Suppresses all immutability findings for the struct's exported fields.
// Checked at GenDecl and TypeSpec level (same pattern as validate-all).
func hasMutableDirective(genDoc *ast.CommentGroup, specDoc *ast.CommentGroup) bool {
	return hasDirectiveKey(genDoc, nil, "mutable") || hasDirectiveKey(specDoc, nil, "mutable")
}

// hasNoDelegateDirective checks whether a struct field has a no-delegate
// directive, indicating the field should be excluded from validate-all
// delegation checking even though its type has a Validate() method.
func hasNoDelegateDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "no-delegate")
}

// hasTrustedBoundaryDirective checks whether a function is an intentional
// boundary exception because its caller has already validated the request.
func hasTrustedBoundaryDirective(doc *ast.CommentGroup, lineComment *ast.CommentGroup) bool {
	return hasDirectiveKey(doc, lineComment, "trusted-boundary")
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
			if slices.Contains(keys, key) {
				return true
			}
		}
	}
	return false
}

// directiveValue extracts the value from a parametric directive of the form
// //goplint:key=value (e.g., //goplint:enum-cue=#RuntimeType).
// Returns the value after "=" and true when the directive key is present
// with a value; returns "", false otherwise.
func directiveValue(cgs []*ast.CommentGroup, key string) (string, bool) {
	for _, cg := range cgs {
		if cg == nil {
			continue
		}
		for _, c := range cg.List {
			content := strings.TrimPrefix(strings.TrimSpace(c.Text), "//")
			content = strings.TrimSpace(content)

			var valueStr string
			for _, prefix := range []string{"goplint:", "plint:"} {
				if strings.HasPrefix(content, prefix) {
					valueStr = content[len(prefix):]
					break
				}
			}
			if valueStr == "" {
				continue
			}

			if sepIdx := strings.Index(valueStr, " --"); sepIdx >= 0 {
				valueStr = valueStr[:sepIdx]
			}

			for part := range strings.SplitSeq(valueStr, ",") {
				part = strings.TrimSpace(part)
				keyPart, val, hasEq := strings.Cut(part, "=")
				if !hasEq {
					continue
				}
				if keyPart == key {
					return val, true
				}
			}
		}
	}
	return "", false
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
// The directive prefix must appear at the start of the comment content
// (after // and optional whitespace). Mentions of directive names in
// prose comments (e.g., "// see plint:ignore for docs") are not treated
// as directives.
//
// Supported forms (single prefix, comma-separated keys):
//
//	//goplint:ignore            → (["ignore"], nil)
//	//plint:ignore              → (["ignore"], nil)
//	//goplint:ignore,internal   → (["ignore", "internal"], nil)
//	//plint:ignore,foo          → (["ignore"], ["foo"])
//	//nolint:goplint            → (["ignore"], nil)  — special case
//	// regular comment          → (nil, nil)
//	// see plint:ignore for docs → (nil, nil)  — prose, not directive
func parseDirectiveKeys(text string) (keys []string, unknown []string) {
	// Strip the comment marker and leading whitespace to get the
	// meaningful content. This normalizes "//goplint:..." and
	// "// goplint:..." to "goplint:..." for prefix matching.
	content := strings.TrimPrefix(text, "//")
	content = strings.TrimSpace(content)

	// Handle nolint:goplint as a special "ignore" directive.
	// Match the token exactly in the nolint linter list, so near-misses like
	// "nolint:goplintfoo" are not treated as valid suppressions.
	if rest, ok := strings.CutPrefix(content, "nolint:"); ok {
		if sepIdx := strings.Index(rest, " --"); sepIdx >= 0 {
			rest = rest[:sepIdx]
		}
		for linter := range strings.SplitSeq(rest, ",") {
			if strings.TrimSpace(linter) == "goplint" {
				return []string{"ignore"}, nil
			}
		}
		return nil, nil
	}

	// Look for goplint: or plint: prefix at the start of content.
	// Using HasPrefix ensures prose references like
	// "see plint:ignore for docs" don't trigger the directive.
	var valueStr string
	for _, prefix := range []string{"goplint:", "plint:"} {
		if strings.HasPrefix(content, prefix) {
			valueStr = content[len(prefix):]
			break
		}
	}
	if valueStr == "" {
		return nil, nil
	}

	// Trim the optional "-- reason" suffix. The convention is to use
	// " -- " as the separator between directive keys and explanation text.
	if sepIdx := strings.Index(valueStr, " --"); sepIdx >= 0 {
		valueStr = valueStr[:sepIdx]
	}

	// Split by comma and classify each token. For parametric directives
	// like enum-cue=#RuntimeType, strip the =value suffix before key lookup.
	for part := range strings.SplitSeq(valueStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		keyPart, _, _ := strings.Cut(part, "=")
		if isKnownDirectiveKey(keyPart) {
			keys = append(keys, keyPart)
		} else {
			unknown = append(unknown, part)
		}
	}
	return keys, unknown
}

// hasIgnoreAtPos checks if any comment in the file associated with pos
// contains a //goplint:ignore or //plint:ignore directive on the same line
// or the line immediately before the given position. This enables per-statement
// suppression in CFA mode where struct-level doc comments are not available.
func hasIgnoreAtPos(pass *analysis.Pass, pos token.Pos) bool {
	posPos := pass.Fset.Position(pos)
	posLine := posPos.Line
	filename := posPos.Filename
	stmtStartLine := findEnclosingStmtStartLine(pass, pos)

	for _, file := range pass.Files {
		if pass.Fset.Position(file.Pos()).Filename != filename {
			continue
		}
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				commentPos := pass.Fset.Position(c.Slash)
				commentLine := commentPos.Line

				if commentLine != posLine && commentLine != stmtStartLine && commentLine != stmtStartLine-1 {
					continue
				}
				// For previous-line comments, only accept standalone directives.
				// Trailing comments from the previous statement must not suppress
				// the next statement.
				if commentLine == stmtStartLine-1 && isTrailingComment(pass, file, c) {
					continue
				}
				keys, _ := parseDirectiveKeys(strings.TrimSpace(c.Text))
				if slices.Contains(keys, "ignore") {
					return true
				}
			}
		}
		break
	}
	return false
}

func findEnclosingStmtStartLine(pass *analysis.Pass, pos token.Pos) int {
	if pass == nil || pass.Fset == nil {
		return 0
	}
	target := pass.Fset.Position(pos)
	if target.Line == 0 || target.Filename == "" {
		return 0
	}

	bestLine := target.Line
	bestSpan := target.Offset + 1

	for _, file := range pass.Files {
		filePos := pass.Fset.Position(file.Pos())
		if filePos.Filename != target.Filename {
			continue
		}

		ast.Inspect(file, func(n ast.Node) bool {
			stmt, ok := n.(ast.Stmt)
			if !ok {
				return true
			}
			stmtPos := pass.Fset.Position(stmt.Pos())
			stmtEnd := pass.Fset.Position(stmt.End())
			if stmtPos.Filename != target.Filename || stmtEnd.Filename != target.Filename {
				return true
			}
			if stmtPos.Offset > target.Offset || target.Offset >= stmtEnd.Offset {
				return true
			}

			span := stmtEnd.Offset - stmtPos.Offset
			if span < bestSpan {
				bestSpan = span
				bestLine = stmtPos.Line
			}
			return true
		})
		break
	}

	return bestLine
}

func isTrailingComment(pass *analysis.Pass, file *ast.File, comment *ast.Comment) bool {
	if pass == nil || pass.Fset == nil || file == nil || comment == nil {
		return false
	}
	commentPos := pass.Fset.Position(comment.Slash)
	if commentPos.Line == 0 || commentPos.Filename == "" {
		return false
	}

	isTrailing := false
	ast.Inspect(file, func(n ast.Node) bool {
		if isTrailing {
			return false
		}
		stmt, ok := n.(ast.Stmt)
		if !ok {
			return true
		}
		stmtEnd := pass.Fset.Position(stmt.End())
		if stmtEnd.Filename != commentPos.Filename || stmtEnd.Line != commentPos.Line {
			return true
		}
		if stmtEnd.Offset <= commentPos.Offset {
			isTrailing = true
			return false
		}
		return true
	})

	return isTrailing
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

// qualFuncName builds a package-qualified function/method name for exception
// matching and diagnostic messages. For methods, the format is
// "pkg.ReceiverType.MethodName"; for functions, "pkg.FuncName".
func qualFuncName(pass *analysis.Pass, fn *ast.FuncDecl) string {
	pkgName := packageName(pass.Pkg)
	funcName := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		recvName := receiverTypeName(fn.Recv.List[0].Type)
		if recvName != "" {
			funcName = recvName + "." + funcName
		}
	}
	return pkgName + "." + funcName
}
