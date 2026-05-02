// SPDX-License-Identifier: MPL-2.0

// Package goplint implements a go/analysis analyzer that detects
// bare primitive type usage in struct fields, function parameters, and
// return types. It is designed to enforce DDD Value Type conventions
// where named types (e.g., type CommandName string) should be used
// instead of raw string, int, etc.
//
// The analyzer supports an exception mechanism via TOML config file
// and inline //goplint:ignore (or //plint:ignore) directives for
// intentional primitive usage at exec/OS boundaries, display-only fields,
// etc. Fields can also be marked //goplint:internal to exclude them from
// functional options completeness checks.
//
// Additional modes:
//   - --audit-exceptions: report exception patterns that matched zero locations
//   - --check-validate: report named non-struct types missing Validate() method
//   - --check-stringer: report named non-struct types missing String() method
//   - --check-constructors: report exported structs missing NewXxx() constructor
//   - --check-nonzero: report struct fields using nonzero types as non-pointer
package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
)

// Diagnostic category constants for structured JSON output.
// These appear in the "category" field of analysis.Diagnostic
// when using -json mode, enabling agents to filter by finding type.
const (
	CategoryPrimitive                     = "primitive"
	CategoryMissingValidate               = "missing-validate"
	CategoryMissingStringer               = "missing-stringer"
	CategoryMissingConstructor            = "missing-constructor"
	CategoryWrongConstructorSig           = "wrong-constructor-sig"
	CategoryMissingFuncOptions            = "missing-func-options"
	CategoryMissingImmutability           = "missing-immutability"
	CategoryWrongValidateSig              = "wrong-validate-sig"
	CategoryWrongStringerSig              = "wrong-stringer-sig"
	CategoryMissingStructValidate         = "missing-struct-validate"
	CategoryWrongStructValidateSig        = "wrong-struct-validate-sig"
	CategoryUnvalidatedCast               = "unvalidated-cast"
	CategoryUnvalidatedCastInconclusive   = "unvalidated-cast-inconclusive"
	CategoryUnusedValidateResult          = "unused-validate-result"
	CategoryUnusedConstructorError        = "unused-constructor-error"
	CategoryMissingConstructorValidate    = "missing-constructor-validate"
	CategoryMissingConstructorValidateInc = "missing-constructor-validate-inconclusive"
	CategoryIncompleteValidateDelegation  = "incomplete-validate-delegation"
	CategoryNonZeroValueField             = "nonzero-value-field"
	CategoryStaleException                = "stale-exception"
	CategoryWrongFuncOptionType           = "wrong-func-option-type"
	CategoryOverdueReview                 = "overdue-review"
	CategoryEnumCueMissingGo              = "enum-cue-missing-go"
	CategoryEnumCueExtraGo                = "enum-cue-extra-go"
	CategoryUnknownDirective              = "unknown-directive"
	CategoryUseBeforeValidateSameBlock    = "use-before-validate-same-block"
	CategoryUseBeforeValidateCrossBlock   = "use-before-validate-cross-block"
	CategoryUseBeforeValidateInconclusive = "use-before-validate-inconclusive"
	CategorySuggestValidateAll            = "suggest-validate-all"
	CategoryMissingConstructorErrorReturn = "missing-constructor-error-return"
	CategoryRedundantConversion           = "redundant-conversion"
	CategoryMissingStructValidateFields   = "missing-struct-validate-fields"
	CategoryUnvalidatedBoundaryRequest    = "unvalidated-boundary-request"
	CategoryCrossPlatformPath             = "cross-platform-path"
	CategoryPathmatrixDivergent           = "pathmatrix-divergent-pass-relative"
)

// NewAnalyzer constructs an analyzer with isolated flag state.
func NewAnalyzer() *analysis.Analyzer {
	return newAnalyzerWithState(&flagState{})
}

func newAnalyzerWithState(state *flagState) *analysis.Analyzer {
	resetFlagStateDefaults(state)
	analyzer := &analysis.Analyzer{
		Name:     "goplint",
		Doc:      "reports bare primitive types where DDD Value Types should be used",
		URL:      "https://github.com/invowk/invowk/tools/goplint",
		Requires: []*analysis.Analyzer{inspect.Analyzer},
		FactTypes: []analysis.Fact{
			(*NonZeroFact)(nil),
			(*ValidatesTypeFact)(nil),
			(*CueFedPathFact)(nil),
		},
	}
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		return runWithState(pass, state)
	}
	bindAnalyzerFlags(analyzer, state)
	return analyzer
}

func shouldReportOverdueReviewFinding(state *flagState, findingID string) bool {
	if state == nil {
		return true
	}
	state.overdueReviewMu.Lock()
	defer state.overdueReviewMu.Unlock()
	if state.overdueReviewSeen == nil {
		state.overdueReviewSeen = make(map[string]bool)
	}
	if state.overdueReviewSeen[findingID] {
		return false
	}
	state.overdueReviewSeen[findingID] = true
	return true
}

// namedTypeInfo records a non-struct named type definition for
// Validate/Stringer checking.
type namedTypeInfo struct {
	name     string    // unqualified type name (e.g., "CommandName")
	pos      token.Pos // position for diagnostics
	exported bool      // whether the type name is exported
}

// exportedStructInfo records an exported struct type for constructor checking
// and structural analysis (immutability, functional options).
type exportedStructInfo struct {
	name    string            // unqualified type name (e.g., "Config")
	pos     token.Pos         // position for diagnostics
	typeKey string            // package-qualified type identity key
	fields  []structFieldMeta // field metadata, populated when structural checks are active
	mutable bool              // has //goplint:mutable directive (immutability exemption)
}

// structFieldMeta records a struct field's name and visibility for
// immutability and functional options analysis.
type structFieldMeta struct {
	name     string    // field name
	exported bool      // whether the field name is exported
	internal bool      // field has //plint:internal directive (excluded from func-options)
	pos      token.Pos // position for diagnostics
}

// constructorFuncInfo records details about a NewXxx constructor function
// for signature validation, functional options detection, and immutability.
type constructorFuncInfo struct {
	pos                    token.Pos // position for diagnostics
	returnTypeName         string    // resolved first non-error return type name (e.g., "Config")
	returnTypeKey          string    // package-qualified type identity key for first non-error return
	returnsInterface       bool      // first non-error return is an interface (skip sig check)
	returnsError           bool      // last return type is error (e.g., func() (*Foo, error))
	paramCount             int       // parameter count excluding trailing variadic option
	hasVariadicOpt         bool      // last param is ...OptionType (func taking *TargetStruct)
	variadicOptionTypeName string    // variadic option type name (e.g., "ConfigOption")
	variadicOptionTarget   string    // variadic option target struct name (e.g., "Config")
}

// methodInfo records a method's signature details for signature verification
// in --check-validate and --check-stringer modes. A non-nil entry in the
// methodSeen map means the method exists; the fields enable checking whether
// the method has the expected signature (not just the expected name).
type methodInfo struct {
	paramCount  int    // number of parameters (excluding receiver)
	resultTypes string // comma-separated result type names (e.g., "bool,[]error")
}

// collectNamedTypes extracts non-struct named type definitions from a
// GenDecl. These are the DDD Value Types themselves (type Foo string)
// that should have Validate() and String() methods.
//
// Skips type aliases (type X = Y) since they inherit methods from the
// aliased type.
func collectNamedTypes(pass *analysis.Pass, node *ast.GenDecl, out *[]namedTypeInfo) {
	if node.Tok != token.TYPE {
		return
	}

	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		// Skip type aliases — they inherit methods from the target type.
		if ts.Assign.IsValid() {
			continue
		}

		// Skip struct types — they use composite Validate() delegation.
		if _, isStruct := ts.Type.(*ast.StructType); isStruct {
			continue
		}

		// Skip interface types.
		if _, isIface := ts.Type.(*ast.InterfaceType); isIface {
			continue
		}

		// Only check types backed by primitives (string, int, etc.)
		// to avoid flagging types like func aliases or channel types.
		typeObj := pass.TypesInfo.TypeOf(ts.Type)
		if typeObj == nil {
			continue
		}
		if !isPrimitiveUnderlying(typeObj) {
			continue
		}

		*out = append(*out, namedTypeInfo{
			name:     ts.Name.Name,
			pos:      ts.Name.Pos(),
			exported: ts.Name.IsExported(),
		})
	}
}

// collectExportedStructs extracts exported struct type definitions.
func collectExportedStructs(pass *analysis.Pass, node *ast.GenDecl, out *[]exportedStructInfo) {
	if node.Tok != token.TYPE {
		return
	}

	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		if _, isStruct := ts.Type.(*ast.StructType); !isStruct {
			continue
		}

		// Only exported structs.
		if !ts.Name.IsExported() {
			continue
		}

		typeKey := ""
		if obj := pass.TypesInfo.Defs[ts.Name]; obj != nil {
			typeKey = typeIdentityKey(obj.Type())
		}

		*out = append(*out, exportedStructInfo{
			name:    ts.Name.Name,
			pos:     ts.Name.Pos(),
			typeKey: typeKey,
		})
	}
}

// trackMethods records method signatures keyed by receiver type for the
// Validate/Stringer checks and error type detection in --check-constructors.
// The pass parameter is used to resolve method signatures via TypesInfo.
func trackMethods(pass *analysis.Pass, fn *ast.FuncDecl, seen map[string]*methodInfo) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return
	}
	recvName := receiverTypeName(fn.Recv.List[0].Type)
	if recvName == "" {
		return
	}

	info := &methodInfo{}

	// Resolve the method's type signature via the type checker for
	// accurate parameter and result type information.
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj != nil {
		if sig, ok := obj.Type().(*types.Signature); ok {
			info.paramCount = sig.Params().Len()
			info.resultTypes = formatResultTypes(sig.Results())
		}
	}

	seen[recvName+"."+fn.Name.Name] = info
}

// formatResultTypes produces a comma-separated string of result type names
// from a method signature's result tuple. Used for signature matching in
// --check-validate and --check-stringer (e.g., "error" or "string").
func formatResultTypes(results *types.Tuple) string {
	if results == nil || results.Len() == 0 {
		return ""
	}
	parts := make([]string, results.Len())
	for i := range results.Len() {
		parts[i] = types.TypeString(results.At(i).Type(), nil)
	}
	return strings.Join(parts, ",")
}

// expectedValidateSig is the expected signature for Validate methods:
// zero parameters, returning error.
const expectedValidateSig = "error"

// expectedStringerSig is the expected signature for String methods:
// zero parameters, returning string.
const expectedStringerSig = "string"

// collectConstantOnlyTypes scans a GenDecl for type definitions annotated
// with //goplint:constant-only. These types have Validate() but are only
// instantiated from compile-time constants, so constructors returning them
// are exempt from --check-constructor-validates.
func collectConstantOnlyTypes(pkgPath string, node *ast.GenDecl, out map[string]bool) {
	if node.Tok != token.TYPE {
		return
	}
	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if hasDirectiveKey(node.Doc, ts.Doc, "constant-only") {
			out[pkgPath+"."+ts.Name.Name] = true
		}
	}
}

// reportMissingValidate reports named non-struct types that lack a
// Validate() method or have one with the wrong signature. For unexported
// types, also checks for validate() (lowercase), matching the project
// convention.
func reportMissingValidate(pass *analysis.Pass, namedTypes []namedTypeInfo, methods map[string]*methodInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)
	for _, t := range namedTypes {
		qualName := fmt.Sprintf("%s.%s", pkgName, t.name)

		// Determine which method name to check (exported vs unexported).
		methodKey := t.name + ".Validate"
		if !t.exported && methods[t.name+".validate"] != nil {
			methodKey = t.name + ".validate"
		}

		mi := methods[methodKey]
		if mi != nil {
			// Method exists — verify its signature matches the contract.
			if mi.paramCount != 0 || mi.resultTypes != expectedValidateSig {
				if cfg.isExcepted(qualName + ".Validate") {
					continue
				}
				msg := fmt.Sprintf("named type %s has Validate() but wrong signature (want func() error)", qualName)
				findingID := StableFindingID(CategoryWrongValidateSig, qualName, "Validate")
				if bl.ContainsFinding(CategoryWrongValidateSig, findingID, msg) {
					continue
				}
				reportDiagnostic(pass, t.pos, CategoryWrongValidateSig, findingID, msg)
			}
			continue
		}

		if cfg.isExcepted(qualName + ".Validate") {
			continue
		}

		msg := fmt.Sprintf("named type %s has no Validate() method", qualName)
		findingID := StableFindingID(CategoryMissingValidate, qualName, "Validate")
		if bl.ContainsFinding(CategoryMissingValidate, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, t.pos, CategoryMissingValidate, findingID, msg)
	}
}

// reportMissingStringer reports named non-struct types that lack a
// String() method or have one with the wrong signature. The String()
// method name is always capitalized regardless of type visibility
// (it implements fmt.Stringer).
func reportMissingStringer(pass *analysis.Pass, namedTypes []namedTypeInfo, methods map[string]*methodInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)
	for _, t := range namedTypes {
		qualName := fmt.Sprintf("%s.%s", pkgName, t.name)

		// Determine which method name to check (exported vs unexported).
		methodKey := t.name + ".String"
		if !t.exported && methods[t.name+".string"] != nil {
			methodKey = t.name + ".string"
		}

		mi := methods[methodKey]
		if mi != nil {
			// Method exists — verify its signature matches the contract.
			if mi.paramCount != 0 || mi.resultTypes != expectedStringerSig {
				if cfg.isExcepted(qualName + ".String") {
					continue
				}
				msg := fmt.Sprintf("named type %s has String() but wrong signature (want func() string)", qualName)
				findingID := StableFindingID(CategoryWrongStringerSig, qualName, "String")
				if bl.ContainsFinding(CategoryWrongStringerSig, findingID, msg) {
					continue
				}
				reportDiagnostic(pass, t.pos, CategoryWrongStringerSig, findingID, msg)
			}
			continue
		}

		if cfg.isExcepted(qualName + ".String") {
			continue
		}

		msg := fmt.Sprintf("named type %s has no String() method", qualName)
		findingID := StableFindingID(CategoryMissingStringer, qualName, "String")
		if bl.ContainsFinding(CategoryMissingStringer, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, t.pos, CategoryMissingStringer, findingID, msg)
	}
}

// reportMissingConstructors reports exported struct types that lack a
// NewXxx() constructor function in the same package. Constructor lookup
// uses prefix matching: any function starting with "New" + structName
// whose first non-error return type resolves to the struct satisfies
// the check (e.g., NewMetadataFromSource satisfies Metadata).
// Error types are skipped: structs whose name ends with "Error" or that
// implement the error interface (have an Error() string method).
func reportMissingConstructors(pass *analysis.Pass, structs []exportedStructInfo, ctors map[string]*constructorFuncInfo, methods map[string]*methodInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)
	for _, s := range structs {
		if findConstructorForStruct(s, ctors) != nil {
			continue
		}

		// Skip error types — they are typically constructed via struct
		// literals, not constructor functions.
		if strings.HasSuffix(s.name, "Error") || methods[s.name+".Error"] != nil {
			continue
		}

		qualName := fmt.Sprintf("%s.%s", pkgName, s.name)
		if cfg.isExcepted(qualName + ".constructor") {
			continue
		}

		ctorName := "New" + s.name
		msg := fmt.Sprintf("exported struct %s has no %s() constructor", qualName, ctorName)
		findingID := StableFindingID(CategoryMissingConstructor, qualName, ctorName)
		if bl.ContainsFinding(CategoryMissingConstructor, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, s.pos, CategoryMissingConstructor, findingID, msg)
	}
}

// findConstructorForStruct searches the constructor map for a function
// matching the struct by prefix. Returns the best matching constructor
// or nil. A constructor matches if its name starts with "New" + structName
// and its return type resolves to structName (or it returns an interface).
// This handles variant constructors like NewMetadataFromSource for Metadata.
//
// Selection is deterministic: exact match ("New" + structName) is preferred,
// then the lexicographically first prefix match. This avoids non-deterministic
// results from map iteration order when multiple variant constructors exist.
func findConstructorForStruct(structInfo exportedStructInfo, ctors map[string]*constructorFuncInfo) *constructorFuncInfo {
	prefix := "New" + structInfo.name
	exactName := prefix

	var bestName string
	var bestInfo *constructorFuncInfo

	for name, info := range ctors {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		typeMatch := info.returnTypeName == structInfo.name
		if structInfo.typeKey != "" && info.returnTypeKey != "" {
			typeMatch = sameStructTypeIdentity(structInfo.typeKey, info.returnTypeKey)
		}
		if !typeMatch && !info.returnsInterface {
			continue
		}
		// Exact match always wins.
		if name == exactName {
			return info
		}
		// Among prefix matches, pick the lexicographically first for determinism.
		if bestInfo == nil || name < bestName {
			bestName = name
			bestInfo = info
		}
	}
	return bestInfo
}

// sameStructTypeIdentity matches constructor return type keys to exported struct
// keys, including generic instantiations (e.g. pkg.Box[T] vs pkg.Box).
func sameStructTypeIdentity(structTypeKey, returnTypeKey string) bool {
	if structTypeKey == "" || returnTypeKey == "" {
		return false
	}
	if structTypeKey == returnTypeKey {
		return true
	}
	structBase := structTypeKey
	if idx := strings.Index(structBase, "["); idx >= 0 {
		structBase = structBase[:idx]
	}
	returnBase := returnTypeKey
	if idx := strings.Index(returnBase, "["); idx >= 0 {
		returnBase = returnBase[:idx]
	}
	return structBase != "" && structBase == returnBase
}

// reportStaleExceptionsInline reports stale exceptions via pass.Reportf.
// Since go/analysis runs per-package, this reports exceptions that matched
// zero locations within the current package only. For cross-package auditing,
// pipe the output through sort -u.
func reportStaleExceptionsInline(pass *analysis.Pass, cfg *ExceptionConfig) {
	stale := cfg.staleExceptions()
	if len(stale) == 0 || len(pass.Files) == 0 {
		return
	}

	pos := pass.Files[0].Package

	for _, idx := range stale {
		exc := cfg.Exceptions[idx]
		msg := fmt.Sprintf(
			"stale exception: pattern %q matched no diagnostics (reason: %s)",
			exc.Pattern, exc.Reason)
		findingID := StableFindingID(CategoryStaleException, exc.Pattern)
		reportDiagnosticWithMeta(pass, pos, CategoryStaleException, findingID, msg, map[string]string{
			"pattern": exc.Pattern,
		})
	}
}

// reportOverdueExceptions reports exceptions with review_after dates that
// have passed. Findings are deduplicated by stable finding ID across package
// passes in the current process.
func reportOverdueExceptions(pass *analysis.Pass, cfg *ExceptionConfig, state *flagState) {
	if len(pass.Files) == 0 {
		return
	}

	now := time.Now()
	pos := pass.Files[0].Package

	for _, exc := range cfg.Exceptions {
		if exc.ReviewAfter == "" {
			continue
		}
		reviewDate, err := time.Parse("2006-01-02", exc.ReviewAfter)
		if err != nil {
			msg := fmt.Sprintf(
				"exception pattern %q has invalid review_after date %q: %v",
				exc.Pattern, exc.ReviewAfter, err)
			findingID := StableFindingID(CategoryOverdueReview, exc.Pattern, "invalid-date")
			if !shouldReportOverdueReviewFinding(state, findingID) {
				continue
			}
			reportDiagnostic(pass, pos, CategoryOverdueReview, findingID, msg)
			continue
		}
		if now.After(reviewDate) {
			msg := fmt.Sprintf(
				"exception pattern %q is past its review date %s",
				exc.Pattern, exc.ReviewAfter)
			if exc.BlockedBy != "" {
				msg += fmt.Sprintf(" (blocked by: %s)", exc.BlockedBy)
			}
			findingID := StableFindingID(CategoryOverdueReview, exc.Pattern)
			if !shouldReportOverdueReviewFinding(state, findingID) {
				continue
			}
			reportDiagnostic(pass, pos, CategoryOverdueReview, findingID, msg)
		}
	}
}
