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
//   - --check-isvalid: report named non-struct types missing IsValid() method
//   - --check-stringer: report named non-struct types missing String() method
//   - --check-constructors: report exported structs missing NewXxx() constructor
package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Diagnostic category constants for structured JSON output.
// These appear in the "category" field of analysis.Diagnostic
// when using -json mode, enabling agents to filter by finding type.
const (
	CategoryPrimitive             = "primitive"
	CategoryMissingIsValid        = "missing-isvalid"
	CategoryMissingStringer       = "missing-stringer"
	CategoryMissingConstructor    = "missing-constructor"
	CategoryWrongConstructorSig   = "wrong-constructor-sig"
	CategoryMissingFuncOptions    = "missing-func-options"
	CategoryMissingImmutability   = "missing-immutability"
	CategoryWrongIsValidSig       = "wrong-isvalid-sig"
	CategoryWrongStringerSig      = "wrong-stringer-sig"
	CategoryMissingStructIsValid  = "missing-struct-isvalid"
	CategoryWrongStructIsValidSig = "wrong-struct-isvalid-sig"
	CategoryStaleException        = "stale-exception"
	CategoryUnknownDirective      = "unknown-directive"
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
	checkIsValid        bool
	checkStringer       bool
	checkConstructors   bool
	checkConstructorSig bool
	checkFuncOptions    bool
	checkImmutability   bool
	checkStructIsValid  bool
)

// Analyzer is the goplint analysis pass. Use it with singlechecker
// or multichecker, or via go vet -vettool.
var Analyzer = &analysis.Analyzer{
	Name:     "goplint",
	Doc:      "reports bare primitive types where DDD Value Types should be used",
	URL:      "https://github.com/invowk/invowk/tools/goplint",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func init() {
	Analyzer.Flags.StringVar(&configPath, "config", "",
		"path to exceptions TOML config file")
	Analyzer.Flags.StringVar(&baselinePath, "baseline", "",
		"path to baseline TOML file (suppress known findings, report only new ones)")
	Analyzer.Flags.BoolVar(&auditExceptions, "audit-exceptions", false,
		"report exception patterns that matched zero locations (stale entries)")
	Analyzer.Flags.BoolVar(&checkIsValid, "check-isvalid", false,
		"report named non-struct types missing IsValid() (bool, []error) method")
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
	Analyzer.Flags.BoolVar(&checkStructIsValid, "check-struct-isvalid", false,
		"report exported struct types with constructors missing IsValid() (bool, []error) method")
	Analyzer.Flags.BoolVar(&checkAll, "check-all", false,
		"enable all DDD compliance checks (isvalid + stringer + constructors + structural)")
}

// runConfig holds the resolved flag values for a single run() invocation.
// Reading flag bindings into this struct at run() entry ensures run()
// never reads or mutates package-level state directly.
type runConfig struct {
	configPath          string
	baselinePath        string
	auditExceptions     bool
	checkAll            bool
	checkIsValid        bool
	checkStringer       bool
	checkConstructors   bool
	checkConstructorSig bool
	checkFuncOptions    bool
	checkImmutability   bool
	checkStructIsValid  bool
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
		checkIsValid:        checkIsValid,
		checkStringer:       checkStringer,
		checkConstructors:   checkConstructors,
		checkConstructorSig: checkConstructorSig,
		checkFuncOptions:    checkFuncOptions,
		checkImmutability:   checkImmutability,
		checkStructIsValid:  checkStructIsValid,
	}
	// Expand --check-all into individual supplementary checks.
	// Deliberately excludes --audit-exceptions which is a config
	// maintenance tool with per-package false positives.
	if rc.checkAll {
		rc.checkIsValid = true
		rc.checkStringer = true
		rc.checkConstructors = true
		rc.checkConstructorSig = true
		rc.checkFuncOptions = true
		rc.checkImmutability = true
		rc.checkStructIsValid = true
	}
	return rc
}

func run(pass *analysis.Pass) (interface{}, error) {
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
	needConstructors := rc.checkConstructors || rc.checkConstructorSig || rc.checkFuncOptions || rc.checkImmutability || rc.checkStructIsValid
	needStructFields := rc.checkFuncOptions || rc.checkImmutability
	needOptionTypes := rc.checkFuncOptions
	needWithFunctions := rc.checkFuncOptions

	// Collectors for the supplementary check modes. Built during the
	// same AST traversal as the primary primitive check and evaluated
	// after the traversal completes.
	var (
		namedTypes         []namedTypeInfo                 // non-struct named types (for isvalid/stringer)
		methodSeen         map[string]*methodInfo          // "TypeName.MethodName" → signature info
		exportedStructs    []exportedStructInfo            // exported struct types (for constructors + structural)
		constructorDetails map[string]*constructorFuncInfo // "NewTypeName" → details
		optionTypes        map[string]string               // optionTypeName → targetStructName
		withFunctions      map[string][]string             // targetStructName → ["WithXxx", ...]
	)

	// Method tracking serves IsValid/Stringer checks, error type detection
	// for the missing-constructor check (skip structs implementing error),
	// and struct IsValid() verification.
	needMethods := rc.checkIsValid || rc.checkStringer || rc.checkConstructors || rc.checkStructIsValid
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
		withFunctions = make(map[string][]string)
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
			if rc.checkIsValid || rc.checkStringer {
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

		case *ast.FuncDecl:
			// Primary mode: check func params and returns for primitives.
			inspectFuncDecl(pass, n, cfg, bl)

			// Supplementary: track methods for isvalid/stringer and error detection.
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
		}
	})

	// Post-traversal checks for supplementary modes.
	if rc.checkIsValid {
		reportMissingIsValid(pass, namedTypes, methodSeen, cfg, bl)
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
	if rc.checkStructIsValid {
		reportMissingStructIsValid(pass, exportedStructs, constructorDetails, methodSeen, cfg, bl)
	}

	if rc.auditExceptions {
		reportStaleExceptionsInline(pass, cfg)
	}

	return nil, nil
}

// namedTypeInfo records a non-struct named type definition for
// IsValid/Stringer checking.
type namedTypeInfo struct {
	name     string    // unqualified type name (e.g., "CommandName")
	pos      token.Pos // position for diagnostics
	exported bool      // whether the type name is exported
}

// exportedStructInfo records an exported struct type for constructor checking
// and structural analysis (immutability, functional options).
type exportedStructInfo struct {
	name   string            // unqualified type name (e.g., "Config")
	pos    token.Pos         // position for diagnostics
	fields []structFieldMeta // field metadata, populated when structural checks are active
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
// in --check-isvalid and --check-stringer modes. A non-nil entry in the
// methodSeen map means the method exists; the fields enable checking whether
// the method has the expected signature (not just the expected name).
type methodInfo struct {
	paramCount  int    // number of parameters (excluding receiver)
	resultTypes string // comma-separated result type names (e.g., "bool,[]error")
}

// collectNamedTypes extracts non-struct named type definitions from a
// GenDecl. These are the DDD Value Types themselves (type Foo string)
// that should have IsValid() and String() methods.
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

		// Skip struct types — they use composite IsValid() delegation.
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
// IsValid/Stringer checks and error type detection in --check-constructors.
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
// --check-isvalid and --check-stringer (e.g., "bool,[]error" or "string").
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

// expectedIsValidSig is the expected signature for IsValid methods:
// zero parameters, returning (bool, []error).
const expectedIsValidSig = "bool,[]error"

// expectedStringerSig is the expected signature for String methods:
// zero parameters, returning string.
const expectedStringerSig = "string"

// reportMissingIsValid reports named non-struct types that lack an
// IsValid() method or have one with the wrong signature. For unexported
// types, also checks for isValid() (lowercase), matching the project
// convention.
func reportMissingIsValid(pass *analysis.Pass, namedTypes []namedTypeInfo, methods map[string]*methodInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)
	for _, t := range namedTypes {
		qualName := fmt.Sprintf("%s.%s", pkgName, t.name)

		// Determine which method name to check (exported vs unexported).
		methodKey := t.name + ".IsValid"
		if !t.exported && methods[t.name+".isValid"] != nil {
			methodKey = t.name + ".isValid"
		}

		mi := methods[methodKey]
		if mi != nil {
			// Method exists — verify its signature matches the contract.
			if mi.paramCount != 0 || mi.resultTypes != expectedIsValidSig {
				if cfg.isExcepted(qualName + ".IsValid") {
					continue
				}
				msg := fmt.Sprintf("named type %s has IsValid() but wrong signature (want func() (bool, []error))", qualName)
				findingID := StableFindingID(CategoryWrongIsValidSig, qualName, "IsValid")
				if bl.ContainsFinding(CategoryWrongIsValidSig, findingID, msg) {
					continue
				}
				reportDiagnostic(pass, t.pos, CategoryWrongIsValidSig, findingID, msg)
			}
			continue
		}

		if cfg.isExcepted(qualName + ".IsValid") {
			continue
		}

		msg := fmt.Sprintf("named type %s has no IsValid() method", qualName)
		findingID := StableFindingID(CategoryMissingIsValid, qualName, "IsValid")
		if bl.ContainsFinding(CategoryMissingIsValid, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, t.pos, CategoryMissingIsValid, findingID, msg)
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

		mi := methods[t.name+".String"]
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
// matching the struct by prefix. Returns the first matching constructor
// or nil. A constructor matches if its name starts with "New" + structName
// and its return type resolves to structName (or it returns an interface).
// This handles variant constructors like NewMetadataFromSource for Metadata.
func findConstructorForStruct(structName string, ctors map[string]*constructorFuncInfo) *constructorFuncInfo {
	prefix := "New" + structName
	for name, info := range ctors {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if info.returnTypeName == structName || info.returnsInterface {
			return info
		}
	}
	return nil
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
