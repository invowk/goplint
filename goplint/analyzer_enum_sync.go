// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"golang.org/x/tools/go/analysis"
)

// enumCueAnnotation records a type declaration annotated with
// //goplint:enum-cue=<cuePath>. The analyzer compares the CUE
// disjunction members against the Go Validate() switch case literals.
type enumCueAnnotation struct {
	typeName     string         // unqualified Go type name (e.g., "RuntimeMode")
	cuePath      string         // CUE path expression (e.g., "#RuntimeType")
	pos          token.Pos      // position of the type declaration for diagnostics
	validateBody *ast.BlockStmt // body of the Validate() method; nil if not found
}

// inspectEnumSync compares Go Validate() switch case literals against CUE
// schema disjunction members for all types annotated with
// //goplint:enum-cue=<cuePath>. Reports missing-go and extra-go mismatches.
//
// CUE schema files are discovered by searching the package directory for
// files matching *_schema.cue. If no schema file is found, diagnostics are
// emitted on annotated types so developers know the check could not run.
func inspectEnumSync(pass *analysis.Pass, cfg *ExceptionConfig, bl *BaselineConfig) {
	annotations := collectEnumCueAnnotations(pass)
	if len(annotations) == 0 {
		return
	}

	schemaBytes, schemaFilename, err := findPackageCUESchema(pass)
	if err != nil || schemaBytes == nil {
		pkgName := packageName(pass.Pkg)
		for _, ann := range annotations {
			qualName := pkgName + "." + ann.typeName
			msg := fmt.Sprintf(
				"type %s has //goplint:enum-cue directive but no *_schema.cue file found in package directory",
				qualName)
			findingID := StableFindingID(CategoryEnumCueMissingGo, qualName, "no-schema")
			reportDiagnostic(pass, ann.pos, CategoryEnumCueMissingGo, findingID, msg)
		}
		return
	}

	pkgName := packageName(pass.Pkg)

	for _, ann := range annotations {
		qualName := pkgName + "." + ann.typeName

		// Extract Go switch case literals from the Validate() body.
		var goCases []string
		if ann.validateBody != nil {
			goCases = extractSwitchCaseLiterals(pass, ann.validateBody)
		}
		goSet := make(map[string]bool, len(goCases))
		for _, v := range goCases {
			goSet[v] = true
		}

		// Extract CUE disjunction members from the schema.
		cueMembers, cueErr := extractCUEDisjunctionMembers(schemaBytes, schemaFilename, ann.cuePath)
		if cueErr != nil {
			msg := fmt.Sprintf(
				"type %s: failed to extract CUE disjunction from %s at path %q: %v",
				qualName, filepath.Base(schemaFilename), ann.cuePath, cueErr)
			findingID := StableFindingID(CategoryEnumCueMissingGo, qualName, ann.cuePath, "cue-error")
			reportDiagnostic(pass, ann.pos, CategoryEnumCueMissingGo, findingID, msg)
			continue
		}
		cueSet := make(map[string]bool, len(cueMembers))
		for _, v := range cueMembers {
			cueSet[v] = true
		}

		// Report CUE members absent from Go switch.
		for _, cueMember := range sortedKeys(cueSet) {
			if goSet[cueMember] {
				continue
			}
			excKey := fmt.Sprintf("%s.%s.enum-cue-missing-go", qualName, sanitizeForPattern(cueMember))
			if cfg.isExcepted(excKey) {
				continue
			}
			msg := fmt.Sprintf(
				"type %s: CUE member %q (at %s) is missing from Validate() switch cases",
				qualName, cueMember, ann.cuePath)
			findingID := StableFindingID(CategoryEnumCueMissingGo, qualName, ann.cuePath, cueMember)
			if bl.ContainsFinding(CategoryEnumCueMissingGo, findingID, msg) {
				continue
			}
			reportDiagnostic(pass, ann.pos, CategoryEnumCueMissingGo, findingID, msg)
		}

		// Report Go switch cases absent from CUE disjunction.
		for _, goCase := range sortedKeys(goSet) {
			if cueSet[goCase] {
				continue
			}
			excKey := fmt.Sprintf("%s.%s.enum-cue-extra-go", qualName, sanitizeForPattern(goCase))
			if cfg.isExcepted(excKey) {
				continue
			}
			msg := fmt.Sprintf(
				"type %s: Validate() switch case %q is not present in CUE disjunction at %s",
				qualName, goCase, ann.cuePath)
			findingID := StableFindingID(CategoryEnumCueExtraGo, qualName, ann.cuePath, goCase)
			if bl.ContainsFinding(CategoryEnumCueExtraGo, findingID, msg) {
				continue
			}
			reportDiagnostic(pass, ann.pos, CategoryEnumCueExtraGo, findingID, msg)
		}
	}
}

// collectEnumCueAnnotations scans all non-test files for type declarations
// annotated with //goplint:enum-cue=<cuePath>. For each annotated type,
// it also searches the package for the corresponding Validate() method body.
func collectEnumCueAnnotations(pass *analysis.Pass) []enumCueAnnotation {
	var result []enumCueAnnotation

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
				cuePath, found := directiveValue(
					[]*ast.CommentGroup{gd.Doc, ts.Doc}, "enum-cue")
				if !found || cuePath == "" {
					continue
				}
				validateBody, _ := findMethodBody(pass, ts.Name.Name, "Validate")
				result = append(result, enumCueAnnotation{
					typeName:     ts.Name.Name,
					cuePath:      cuePath,
					pos:          ts.Name.Pos(),
					validateBody: validateBody,
				})
			}
		}
	}
	return result
}

// extractSwitchCaseLiterals walks a function body and collects all string
// and int literal values from switch case clauses. Handles direct literals
// (case "native":), named constants (case RuntimeNative:), and conversion
// switches (switch string(v) { ... }).
func extractSwitchCaseLiterals(pass *analysis.Pass, body *ast.BlockStmt) []string {
	var literals []string
	seen := make(map[string]bool)

	ast.Inspect(body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		for _, stmt := range sw.Body.List {
			cc, ok := stmt.(*ast.CaseClause)
			if !ok || cc.List == nil {
				continue
			}
			for _, expr := range cc.List {
				lit := extractLiteralString(expr)
				if lit == "" {
					// Try resolving named constant identifiers
					// (e.g., case RuntimeNative: where RuntimeNative = "native").
					lit = resolveConstantValue(pass, expr)
				}
				if lit != "" && !seen[lit] {
					seen[lit] = true
					literals = append(literals, lit)
				}
			}
		}
		return true
	})
	return literals
}

// resolveConstantValue resolves an AST expression to a constant string or
// int value via the type checker. Handles both bare identifiers
// (case RuntimeNative:) and qualified selectors (case invowkfile.RuntimeNative:)
// that appear as named constants in switch case clauses.
// Returns "" if the expression is not a resolvable constant.
func resolveConstantValue(pass *analysis.Pass, expr ast.Expr) string {
	var obj types.Object
	switch e := expr.(type) {
	case *ast.Ident:
		obj = pass.TypesInfo.Uses[e]
	case *ast.SelectorExpr:
		obj = pass.TypesInfo.Uses[e.Sel]
	default:
		return ""
	}
	if obj == nil {
		return ""
	}
	constObj, ok := obj.(*types.Const)
	if !ok {
		return ""
	}
	val := constObj.Val()
	switch val.Kind() {
	case constant.String:
		return constant.StringVal(val)
	case constant.Int:
		return val.ExactString()
	default:
		return ""
	}
}

// extractLiteralString extracts a comparable string from a case expression.
// Handles string literals (unquoted) and int literals (raw decimal).
func extractLiteralString(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		switch e.Kind {
		case token.STRING:
			s, err := strconv.Unquote(e.Value)
			if err != nil {
				return e.Value
			}
			return s
		case token.INT:
			return e.Value
		}
	case *ast.ParenExpr:
		return extractLiteralString(e.X)
	}
	return ""
}

// findPackageCUESchema searches the package directory for files matching
// *_schema.cue and concatenates them. This handles packages with multiple
// schema files (e.g., types_schema.cue and config_schema.cue) where enums
// may be split across files. Returns the combined contents, the first
// filename (for diagnostics), and nil error. Returns (nil, "", nil) when
// no schema file exists.
func findPackageCUESchema(pass *analysis.Pass) ([]byte, string, error) {
	var pkgDir string
	for _, file := range pass.Files {
		if isTestFile(pass, file.Pos()) {
			continue
		}
		filename := pass.Fset.Position(file.Pos()).Filename
		if filename == "" {
			continue
		}
		pkgDir = filepath.Dir(filename)
		break
	}
	if pkgDir == "" {
		return nil, "", nil
	}

	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, "", fmt.Errorf("reading package directory %s: %w", pkgDir, err)
	}

	var schemaNames []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_schema.cue") {
			schemaNames = append(schemaNames, entry.Name())
		}
	}
	if len(schemaNames) == 0 {
		return nil, "", nil
	}
	sort.Strings(schemaNames)

	// Concatenate all schema files. CUE supports multiple definitions in
	// a single compilation unit, so concatenation is semantically valid
	// for top-level #Definition declarations.
	var combined []byte
	for _, name := range schemaNames {
		data, readErr := os.ReadFile(filepath.Join(pkgDir, name))
		if readErr != nil {
			return nil, "", fmt.Errorf("reading CUE schema %s: %w", name, readErr)
		}
		combined = append(combined, data...)
		combined = append(combined, '\n')
	}
	return combined, filepath.Join(pkgDir, schemaNames[0]), nil
}

// extractCUEDisjunctionMembers compiles the CUE schema, looks up the value
// at cuePath, and extracts all concrete string/int leaf members from the
// disjunction. Supports top-level definitions (#RuntimeType) and nested
// field paths (#Config.container_engine).
//
// For nested paths with optional fields (field?:), LookupPath may fail.
// In that case, the function splits the path and iterates fields with
// cue.Optional(true) to find optional members.
func extractCUEDisjunctionMembers(schemaBytes []byte, schemaFilename, cuePath string) ([]string, error) {
	ctx := cuecontext.New()

	schemaVal := ctx.CompileBytes(schemaBytes, cue.Filename(schemaFilename))
	if err := schemaVal.Err(); err != nil {
		return nil, fmt.Errorf("compiling CUE schema: %w", err)
	}

	target := schemaVal.LookupPath(cue.ParsePath(cuePath))
	if target.Err() != nil {
		// LookupPath failed — try navigating manually for optional fields.
		resolved, err := lookupWithOptional(schemaVal, cuePath)
		if err != nil {
			return nil, fmt.Errorf("CUE path %q not found: %w", cuePath, err)
		}
		target = resolved
	}

	var members []string
	walkDisjunction(target, &members)
	return members, nil
}

// lookupWithOptional navigates a CUE value by splitting the path on dots
// and using Fields(cue.Optional(true)) at each step. This handles optional
// CUE fields (field?:) that LookupPath cannot find directly.
func lookupWithOptional(root cue.Value, path string) (cue.Value, error) {
	parts := strings.Split(path, ".")
	current := root

	for _, part := range parts {
		// Try direct lookup first (works for definitions like #Foo).
		next := current.LookupPath(cue.ParsePath(part))
		if next.Err() == nil {
			current = next
			continue
		}

		// Fall back to iterating fields with optional flag.
		trimmed := strings.TrimSuffix(part, "?")
		found := false
		iter, _ := current.Fields(cue.Optional(true))
		for iter.Next() {
			label := iter.Selector().String()
			label = strings.TrimSuffix(label, "?")
			if label == trimmed {
				current = iter.Value()
				found = true
				break
			}
		}
		if !found {
			return cue.Value{}, fmt.Errorf("field %q not found (including optional)", part)
		}
	}
	return current, nil
}

// walkDisjunction recursively walks a CUE value's disjunction tree,
// collecting concrete string and int leaf values.
func walkDisjunction(v cue.Value, out *[]string) {
	op, args := v.Expr()
	if op == cue.OrOp && len(args) >= 2 {
		for _, arg := range args {
			walkDisjunction(arg, out)
		}
		return
	}

	if s, ok := cueValueToString(v); ok {
		*out = append(*out, s)
	}
}

// cueValueToString converts a concrete CUE value to a string for comparison
// with Go switch case literals. Returns the value and true for concrete
// string or int literals; returns "", false otherwise.
func cueValueToString(v cue.Value) (string, bool) {
	if s, err := v.String(); err == nil {
		return s, true
	}
	if i, err := v.Int64(); err == nil {
		return strconv.FormatInt(i, 10), true
	}
	return "", false
}

// sortedKeys returns the keys of a map[string]bool in sorted order
// for deterministic diagnostic output.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sanitizeForPattern replaces dots (the pattern segment separator) with
// underscores so CUE member values don't confuse the exception matcher.
func sanitizeForPattern(s string) string {
	return strings.ReplaceAll(s, ".", "_")
}
