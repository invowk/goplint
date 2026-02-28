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
	"golang.org/x/tools/go/ast/inspector"
)

// Diagnostic category constants for structured JSON output.
// These appear in the "category" field of analysis.Diagnostic
// when using -json mode, enabling agents to filter by finding type.
const (
	CategoryPrimitive              = "primitive"
	CategoryMissingValidate        = "missing-validate"
	CategoryMissingStringer        = "missing-stringer"
	CategoryMissingConstructor     = "missing-constructor"
	CategoryWrongConstructorSig    = "wrong-constructor-sig"
	CategoryMissingFuncOptions     = "missing-func-options"
	CategoryMissingImmutability    = "missing-immutability"
	CategoryWrongValidateSig       = "wrong-validate-sig"
	CategoryWrongStringerSig       = "wrong-stringer-sig"
	CategoryMissingStructValidate  = "missing-struct-validate"
	CategoryWrongStructValidateSig = "wrong-struct-validate-sig"
	CategoryUnvalidatedCast        = "unvalidated-cast"
	CategoryUnusedValidateResult   = "unused-validate-result"
	CategoryUnusedConstructorError = "unused-constructor-error"
	CategoryMissingConstructorValidate     = "missing-constructor-validate"
	CategoryIncompleteValidateDelegation = "incomplete-validate-delegation"
	CategoryNonZeroValueField          = "nonzero-value-field"
	CategoryStaleException             = "stale-exception"
	CategoryWrongFuncOptionType        = "wrong-func-option-type"
	CategoryOverdueReview              = "overdue-review"
	CategoryEnumCueMissingGo           = "enum-cue-missing-go"
	CategoryEnumCueExtraGo             = "enum-cue-extra-go"
	CategoryUnknownDirective           = "unknown-directive"
	CategorySuggestValidateAll         = "suggest-validate-all"
)

// Flag binding variables for the analyzer's flag set. These are populated
// by the go/analysis framework during flag parsing via BoolVar/StringVar.
// The run() function never reads or mutates these directly — it reads them
// once via newRunConfig() into a local struct.
var (
	configPath          string
	baselinePath        string
	auditExceptions     bool
	checkAll            bool
	checkValidate       bool
	checkStringer       bool
	checkConstructors   bool
	checkConstructorSig bool
	checkFuncOptions    bool
	checkImmutability   bool
	checkStructValidate bool
	checkCastValidation          bool
	checkValidateUsage           bool
	checkConstructorErrUsage     bool
	checkConstructorValidates    bool
	checkValidateDelegation      bool
	checkNonZero                 bool
	noCFA                        bool
	auditReviewDates             bool
	checkEnumSync                bool
	suggestValidateAll           bool
)

// Analyzer is the goplint analysis pass. Use it with singlechecker
// or multichecker, or via go vet -vettool.
var Analyzer = &analysis.Analyzer{
	Name:      "goplint",
	Doc:       "reports bare primitive types where DDD Value Types should be used",
	URL:       "https://github.com/invowk/invowk/tools/goplint",
	Run:       run,
	Requires:  []*analysis.Analyzer{inspect.Analyzer},
	FactTypes: []analysis.Fact{(*NonZeroFact)(nil)},
}

func init() {
	Analyzer.Flags.StringVar(&configPath, "config", "",
		"path to exceptions TOML config file")
	Analyzer.Flags.StringVar(&baselinePath, "baseline", "",
		"path to baseline TOML file (suppress known findings, report only new ones)")
	Analyzer.Flags.BoolVar(&auditExceptions, "audit-exceptions", false,
		"report exception patterns that matched zero locations (stale entries)")
	Analyzer.Flags.BoolVar(&checkValidate, "check-validate", false,
		"report named non-struct types missing Validate() error method")
	Analyzer.Flags.BoolVar(&checkStringer, "check-stringer", false,
		"report named non-struct types missing String() string method")
	Analyzer.Flags.BoolVar(&checkConstructors, "check-constructors", false,
		"report exported struct types missing NewXxx() constructor function")
	Analyzer.Flags.BoolVar(&checkConstructorSig, "check-constructor-sig", false,
		"report NewXxx() constructors whose return type doesn't match the struct")
	Analyzer.Flags.BoolVar(&checkFuncOptions, "check-func-options", false,
		"report structs that should use or complete the functional options pattern")
	Analyzer.Flags.BoolVar(&checkImmutability, "check-immutability", false,
		"report structs with constructors that have exported mutable fields")
	Analyzer.Flags.BoolVar(&checkStructValidate, "check-struct-validate", false,
		"report exported struct types with constructors missing Validate() error method")
	Analyzer.Flags.BoolVar(&checkCastValidation, "check-cast-validation", false,
		"report type conversions to DDD Value Types from non-constants without Validate() check")
	Analyzer.Flags.BoolVar(&checkValidateUsage, "check-validate-usage", false,
		"detect unused Validate() results")
	Analyzer.Flags.BoolVar(&checkConstructorErrUsage, "check-constructor-error-usage", false,
		"detect constructor calls with error return assigned to blank identifier")
	Analyzer.Flags.BoolVar(&checkConstructorValidates, "check-constructor-validates", false,
		"report NewXxx() constructors that return types with Validate() but never call it")
	Analyzer.Flags.BoolVar(&checkValidateDelegation, "check-validate-delegation", false,
		"report structs with //goplint:validate-all whose Validate() misses field delegations")
	Analyzer.Flags.BoolVar(&checkNonZero, "check-nonzero", false,
		"report struct fields using nonzero-annotated types as value (non-pointer) fields where they are semantically optional")
	Analyzer.Flags.BoolVar(&auditReviewDates, "audit-review-dates", false,
		"report exception patterns with review_after dates that have passed")
	Analyzer.Flags.BoolVar(&noCFA, "no-cfa", false,
		"disable control-flow analysis and use AST heuristic for cast-validation (CFA is enabled by default)")
	Analyzer.Flags.BoolVar(&checkEnumSync, "check-enum-sync", false,
		"report mismatches between Go Validate() switch cases and CUE schema disjunction members (requires //goplint:enum-cue= directive)")
	Analyzer.Flags.BoolVar(&suggestValidateAll, "suggest-validate-all", false,
		"report structs with Validate() + validatable fields but no //goplint:validate-all directive (advisory)")
	Analyzer.Flags.BoolVar(&checkAll, "check-all", false,
		"enable all DDD compliance checks (validate + stringer + constructors + structural + cast-validation + validate-usage + constructor-error-usage + constructor-validates + nonzero + CFA)")
}

// runConfig holds the resolved flag values for a single run() invocation.
// Reading flag bindings into this struct at run() entry ensures run()
// never reads or mutates package-level state directly.
type runConfig struct {
	configPath          string
	baselinePath        string
	auditExceptions     bool
	checkAll            bool
	checkValidate       bool
	checkStringer       bool
	checkConstructors   bool
	checkConstructorSig bool
	checkFuncOptions    bool
	checkImmutability   bool
	checkStructValidate bool
	checkCastValidation          bool
	checkValidateUsage           bool
	checkConstructorErrUsage     bool
	checkConstructorValidates    bool
	checkValidateDelegation      bool
	checkNonZero                 bool
	noCFA                        bool
	auditReviewDates             bool
	checkEnumSync                bool
	suggestValidateAll           bool
}

// newRunConfig reads the current flag binding values into a local config
// struct and applies the --check-all expansion. The expansion happens on
// the local struct, never mutating the package-level flag variables.
func newRunConfig() runConfig {
	rc := runConfig{
		configPath:          configPath,
		baselinePath:        baselinePath,
		auditExceptions:     auditExceptions,
		checkAll:            checkAll,
		checkValidate:       checkValidate,
		checkStringer:       checkStringer,
		checkConstructors:   checkConstructors,
		checkConstructorSig: checkConstructorSig,
		checkFuncOptions:    checkFuncOptions,
		checkImmutability:   checkImmutability,
		checkStructValidate: checkStructValidate,
		checkCastValidation:          checkCastValidation,
		checkValidateUsage:           checkValidateUsage,
		checkConstructorErrUsage:     checkConstructorErrUsage,
		checkConstructorValidates:    checkConstructorValidates,
		checkValidateDelegation:      checkValidateDelegation,
		checkNonZero:                 checkNonZero,
		noCFA:                        noCFA,
		auditReviewDates:             auditReviewDates,
		checkEnumSync:                checkEnumSync,
		suggestValidateAll:           suggestValidateAll,
	}
	// Expand --check-all into individual supplementary checks.
	// Deliberately excludes --audit-exceptions (config maintenance tool
	// with per-package false positives). CFA is enabled by default
	// (opt out via --no-cfa).
	if rc.checkAll {
		rc.checkValidate = true
		rc.checkStringer = true
		rc.checkConstructors = true
		rc.checkConstructorSig = true
		rc.checkFuncOptions = true
		rc.checkImmutability = true
		rc.checkStructValidate = true
		rc.checkCastValidation = true
		rc.checkValidateUsage = true
		rc.checkConstructorErrUsage = true
		rc.checkConstructorValidates = true
		rc.checkValidateDelegation = true
		rc.checkNonZero = true
	}
	return rc
}

func run(pass *analysis.Pass) (any, error) {
	rc := newRunConfig()

	cfg, err := loadConfig(rc.configPath)
	if err != nil {
		return nil, err
	}

	bl, err := loadBaseline(rc.baselinePath)
	if err != nil {
		return nil, err
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	// Determine which data needs to be collected based on active modes.
	needConstructors := rc.checkConstructors || rc.checkConstructorSig || rc.checkFuncOptions || rc.checkImmutability || rc.checkStructValidate || rc.checkConstructorValidates
	needStructFields := rc.checkFuncOptions || rc.checkImmutability
	needOptionTypes := rc.checkFuncOptions
	needWithFunctions := rc.checkFuncOptions

	// Collectors for the supplementary check modes. Built during the
	// same AST traversal as the primary primitive check and evaluated
	// after the traversal completes.
	var (
		namedTypes         []namedTypeInfo                 // non-struct named types (for validate/stringer)
		methodSeen         map[string]*methodInfo          // "TypeName.MethodName" → signature info
		exportedStructs    []exportedStructInfo            // exported struct types (for constructors + structural)
		constructorDetails map[string]*constructorFuncInfo // "NewTypeName" → details
		optionTypes        map[string]string               // optionTypeName → targetStructName
		withFunctions      map[string][]withFuncInfo        // targetStructName → [withFuncInfo, ...]
	)

	// constantOnlyTypes tracks type names annotated with //goplint:constant-only.
	// These types have Validate() but are only ever instantiated from
	// compile-time constants, so their constructors are intentionally
	// exempt from --check-constructor-validates.
	var constantOnlyTypes map[string]bool
	if rc.checkConstructorValidates {
		constantOnlyTypes = make(map[string]bool)
	}

	// Method tracking serves Validate/Stringer checks, error type detection
	// for the missing-constructor check (skip structs implementing error),
	// and struct Validate() verification.
	needMethods := rc.checkValidate || rc.checkStringer || rc.checkConstructors || rc.checkStructValidate
	if needMethods {
		methodSeen = make(map[string]*methodInfo)
	}
	if needConstructors {
		constructorDetails = make(map[string]*constructorFuncInfo)
	}
	if needOptionTypes {
		optionTypes = make(map[string]string)
	}
	if needWithFunctions {
		withFunctions = make(map[string][]withFuncInfo)
	}

	nodeFilter := []ast.Node{
		(*ast.GenDecl)(nil),
		(*ast.FuncDecl)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		// Skip test files entirely — test data legitimately uses primitives.
		if isTestFile(pass, n.Pos()) {
			return
		}

		// Skip files matching exclude_paths from config.
		filePath := pass.Fset.Position(n.Pos()).Filename
		if cfg.isExcludedPath(filePath) {
			return
		}

		switch n := n.(type) {
		case *ast.GenDecl:
			// Primary mode: check struct fields for primitives.
			inspectStructFields(pass, n, cfg, bl)

			// Supplementary: collect named types.
			if rc.checkValidate || rc.checkStringer {
				collectNamedTypes(pass, n, &namedTypes)
			}

			// Collect exported structs — use field-enriched version when
			// structural checks need field metadata.
			if needConstructors {
				if needStructFields {
					collectExportedStructsWithFields(pass, n, &exportedStructs)
				} else {
					collectExportedStructs(pass, n, &exportedStructs)
				}
			}

			// Structural: collect option type definitions.
			if needOptionTypes {
				collectOptionTypes(pass, n, optionTypes)
			}

			// Collect types with //goplint:constant-only directive.
			if constantOnlyTypes != nil {
				collectConstantOnlyTypes(n, constantOnlyTypes)
			}

		case *ast.FuncDecl:
			// Primary mode: check func params and returns for primitives.
			inspectFuncDecl(pass, n, cfg, bl)

			// Supplementary: track methods for validate/stringer and error detection.
			if needMethods {
				trackMethods(pass, n, methodSeen)
			}

			// Track constructors with return type and param details.
			if constructorDetails != nil {
				trackConstructorDetails(pass, n, constructorDetails)
			}

			// Structural: track WithXxx option functions.
			if needWithFunctions {
				trackWithFunctions(pass, n, optionTypes, withFunctions)
			}

			// Cast validation: detect unvalidated type conversions to DDD types.
			// CFA (default) uses path-reachability analysis; --no-cfa falls
			// back to AST name-based heuristic.
			if rc.checkCastValidation {
				if rc.noCFA {
					inspectUnvalidatedCasts(pass, n, cfg, bl)
				} else {
					inspectUnvalidatedCastsCFA(pass, n, cfg, bl)
				}
			}

			// Validate usage: detect discarded Validate() results.
			if rc.checkValidateUsage {
				inspectValidateUsage(pass, n, cfg, bl)
			}

			// Constructor error usage: detect blanked error returns.
			if rc.checkConstructorErrUsage {
				inspectConstructorErrorUsage(pass, n, cfg, bl)
			}
		}
	})

	// Post-traversal checks for supplementary modes.
	if rc.checkValidate {
		reportMissingValidate(pass, namedTypes, methodSeen, cfg, bl)
	}
	if rc.checkStringer {
		reportMissingStringer(pass, namedTypes, methodSeen, cfg, bl)
	}
	if rc.checkConstructors {
		reportMissingConstructors(pass, exportedStructs, constructorDetails, methodSeen, cfg, bl)
	}

	// Structural checks — all require constructorDetails.
	if rc.checkConstructorSig {
		reportWrongConstructorSig(pass, exportedStructs, constructorDetails, cfg, bl)
	}
	if rc.checkFuncOptions {
		reportMissingFuncOptions(pass, exportedStructs, constructorDetails, optionTypes, withFunctions, cfg, bl)
	}
	if rc.checkImmutability {
		reportMissingImmutability(pass, exportedStructs, constructorDetails, cfg, bl)
	}
	if rc.checkStructValidate {
		reportMissingStructValidate(pass, exportedStructs, constructorDetails, methodSeen, cfg, bl)
	}
	if rc.checkConstructorValidates {
		inspectConstructorValidates(pass, constructorDetails, constantOnlyTypes, cfg, bl)
	}

	// Validate delegation — opt-in via //goplint:validate-all.
	if rc.checkValidateDelegation {
		inspectValidateDelegation(pass, cfg, bl)
	}

	// Nonzero field checks — cross-package via analysis.Fact.
	if rc.checkNonZero {
		inspectNonZero(pass, cfg, bl)
	}

	if rc.auditExceptions {
		reportStaleExceptionsInline(pass, cfg)
	}

	if rc.auditReviewDates {
		reportOverdueExceptions(pass, cfg)
	}

	// Suggest validate-all: advisory mode for structs that may benefit from the directive.
	// NOT included in --check-all — this is purely advisory.
	if rc.suggestValidateAll {
		inspectSuggestValidateAll(pass, cfg, bl)
	}

	// Enum sync: compare Go Validate() switch cases against CUE disjunctions.
	if rc.checkEnumSync {
		inspectEnumSync(pass, cfg, bl)
	}

	return nil, nil
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
	returnsInterface       bool      // first non-error return is an interface (skip sig check)
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

		*out = append(*out, exportedStructInfo{
			name: ts.Name.Name,
			pos:  ts.Name.Pos(),
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
func collectConstantOnlyTypes(node *ast.GenDecl, out map[string]bool) {
	if node.Tok != token.TYPE {
		return
	}
	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if hasDirectiveKey(node.Doc, ts.Doc, "constant-only") {
			out[ts.Name.Name] = true
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
		if findConstructorForStruct(s.name, ctors) != nil {
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
func findConstructorForStruct(structName string, ctors map[string]*constructorFuncInfo) *constructorFuncInfo {
	prefix := "New" + structName
	exactName := prefix

	var bestName string
	var bestInfo *constructorFuncInfo

	for name, info := range ctors {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if info.returnTypeName != structName && !info.returnsInterface {
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
		reportDiagnostic(pass, pos, CategoryStaleException, findingID, msg)
	}
}

// reportOverdueExceptions reports exceptions with review_after dates that
// have passed. Only runs once per analysis (first package).
func reportOverdueExceptions(pass *analysis.Pass, cfg *ExceptionConfig) {
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
			reportDiagnostic(pass, pos, CategoryOverdueReview, findingID, msg)
		}
	}
}
