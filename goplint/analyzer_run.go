// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

type runNeeds struct {
	needConstructors bool
	needStructFields bool
	needOptionTypes  bool
	needWithFuncs    bool
	needMethods      bool
}

func deriveRunNeeds(rc runConfig) runNeeds {
	return runNeeds{
		needConstructors: rc.checkConstructors ||
			rc.checkConstructorSig ||
			rc.checkFuncOptions ||
			rc.checkImmutability ||
			rc.checkStructValidate ||
			rc.checkConstructorValidates ||
			rc.checkConstructorReturnError,
		needStructFields: rc.checkFuncOptions || rc.checkImmutability,
		needOptionTypes:  rc.checkFuncOptions,
		needWithFuncs:    rc.checkFuncOptions,
		needMethods: rc.checkValidate ||
			rc.checkStringer ||
			rc.checkConstructors ||
			rc.checkStructValidate,
	}
}

type runCollectors struct {
	// non-struct named types (for validate/stringer)
	namedTypes []namedTypeInfo
	// "TypeName.MethodName" -> signature info
	methodSeen map[string]*methodInfo
	// exported struct types (for constructors + structural)
	exportedStructs []exportedStructInfo
	// "NewTypeName" -> details
	constructorDetails map[string]*constructorFuncInfo
	// optionTypeName -> targetStructName
	optionTypes map[string]string
	// targetStructName -> [withFuncInfo, ...]
	withFunctions map[string][]withFuncInfo
	// <pkg-path>.<TypeName> -> true
	constantOnlyTypes map[string]bool
}

func newRunCollectors(rc runConfig, needs runNeeds) runCollectors {
	collectors := runCollectors{}

	// constantOnlyTypes tracks fully-qualified type keys
	// (<pkg-path>.<TypeName>) annotated with //goplint:constant-only.
	// These types have Validate() but are only ever instantiated from
	// compile-time constants, so their constructors are intentionally
	// exempt from --check-constructor-validates and --check-constructor-return-error.
	if rc.checkConstructorValidates || rc.checkConstructorReturnError {
		collectors.constantOnlyTypes = make(map[string]bool)
	}
	if needs.needMethods {
		collectors.methodSeen = make(map[string]*methodInfo)
	}
	if needs.needConstructors {
		collectors.constructorDetails = make(map[string]*constructorFuncInfo)
	}
	if needs.needOptionTypes {
		collectors.optionTypes = make(map[string]string)
	}
	if needs.needWithFuncs {
		collectors.withFunctions = make(map[string][]withFuncInfo)
	}
	return collectors
}

func runWithState(pass *analysis.Pass, state *flagState) (any, error) {
	rc := newRunConfigForState(state)
	if err := validateRunConfig(rc); err != nil {
		return nil, err
	}

	cfg, bl, err := loadRunInputs(state, rc)
	if err != nil {
		return nil, err
	}

	includePackages, hasIncludeOverride, err := parseIncludePackagesOverride(rc)
	if err != nil {
		return nil, err
	}
	// Apply CLI --include-packages override when explicitly set.
	if hasIncludeOverride {
		cfg.Settings.IncludePackages = includePackages
	}

	// Package filter: if include_packages is configured and this package
	// doesn't match, run only fact-exporting traversal (for cross-package
	// resolution) and skip all diagnostic emission.
	if !cfg.ShouldAnalyzePackage(pass.Pkg.Path()) {
		runFactExportOnly(pass)
		return nil, nil
	}

	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	needs := deriveRunNeeds(rc)
	collectors := newRunCollectors(rc, needs)
	var ssaRes *ssaResult
	if rc.checkCastValidation && rc.cfgAliasMode == cfgAliasModeSSA {
		// Build SSA at most once per package analysis when Phase D alias
		// tracking is active so every function and closure shares the same view.
		ssaRes = buildSSAForPass(pass)
	}

	if err := runTraversal(pass, insp, rc, cfg, bl, needs, &collectors, ssaRes); err != nil {
		return nil, err
	}
	if err := runPostTraversalChecks(pass, state, rc, cfg, bl, &collectors); err != nil {
		return nil, err
	}

	return nil, nil
}

func validateRunConfig(rc runConfig) error {
	if rc.configPathExplicit && strings.TrimSpace(rc.configPath) == "" {
		return errors.New("flag --config was provided with an empty path")
	}
	if rc.baselinePathExplicit && strings.TrimSpace(rc.baselinePath) == "" {
		return errors.New("flag --baseline was provided with an empty path")
	}
	ubvMode := rc.ubvMode
	if ubvMode == "" {
		ubvMode = defaultUBVMode
	}
	switch ubvMode {
	case ubvModeOrder, ubvModeEscape:
	default:
		return fmt.Errorf("flag --ubv-mode must be %q or %q (got %q)", ubvModeOrder, ubvModeEscape, rc.ubvMode)
	}
	cfgBackend := rc.cfgBackend
	if cfgBackend == "" {
		cfgBackend = defaultCFGBackend
	}
	switch cfgBackend {
	case cfgBackendSSA, cfgBackendAST:
	default:
		return fmt.Errorf("flag --cfg-backend must be %q or %q (got %q)", cfgBackendSSA, cfgBackendAST, rc.cfgBackend)
	}
	cfgInterprocEngine := strings.TrimSpace(strings.ToLower(rc.cfgInterprocEngine))
	if cfgInterprocEngine == "" {
		cfgInterprocEngine = defaultCFGInterprocEngine
	}
	switch cfgInterprocEngine {
	case cfgInterprocEngineLegacy, cfgInterprocEngineIFDS, cfgInterprocEngineCompare:
	default:
		return fmt.Errorf(
			"flag --cfg-interproc-engine must be %q, %q, or %q (got %q)",
			cfgInterprocEngineLegacy,
			cfgInterprocEngineIFDS,
			cfgInterprocEngineCompare,
			rc.cfgInterprocEngine,
		)
	}
	cfgMaxStates := rc.cfgMaxStates
	if cfgMaxStates == 0 {
		cfgMaxStates = defaultCFGMaxStates
	}
	if cfgMaxStates <= 0 {
		return fmt.Errorf("flag --cfg-max-states must be > 0 (got %d)", rc.cfgMaxStates)
	}
	cfgMaxDepth := rc.cfgMaxDepth
	if cfgMaxDepth == 0 {
		cfgMaxDepth = defaultCFGMaxDepth
	}
	if cfgMaxDepth <= 0 {
		return fmt.Errorf("flag --cfg-max-depth must be > 0 (got %d)", rc.cfgMaxDepth)
	}
	cfgInconclusivePolicy := strings.TrimSpace(strings.ToLower(rc.cfgInconclusivePolicy))
	if cfgInconclusivePolicy == "" {
		cfgInconclusivePolicy = defaultCFGInconclusivePolicy
	}
	switch cfgInconclusivePolicy {
	case cfgInconclusivePolicyError, cfgInconclusivePolicyWarn, cfgInconclusivePolicyOff:
	default:
		return fmt.Errorf(
			"flag --cfg-inconclusive-policy must be %q, %q, or %q (got %q)",
			cfgInconclusivePolicyError,
			cfgInconclusivePolicyWarn,
			cfgInconclusivePolicyOff,
			rc.cfgInconclusivePolicy,
		)
	}
	cfgWitnessMaxSteps := rc.cfgWitnessMaxSteps
	if cfgWitnessMaxSteps == 0 {
		cfgWitnessMaxSteps = defaultCFGWitnessMaxSteps
	}
	if cfgWitnessMaxSteps <= 0 {
		return fmt.Errorf("flag --cfg-witness-max-steps must be > 0 (got %d)", rc.cfgWitnessMaxSteps)
	}
	cfgFeasibilityEngine := strings.TrimSpace(strings.ToLower(rc.cfgFeasibilityEngine))
	if cfgFeasibilityEngine == "" {
		cfgFeasibilityEngine = defaultCFGFeasibilityEngine
	}
	switch cfgFeasibilityEngine {
	case cfgFeasibilityEngineOff, cfgFeasibilityEngineSMT:
	default:
		return fmt.Errorf(
			"flag --cfg-feasibility-engine must be %q or %q (got %q)",
			cfgFeasibilityEngineOff,
			cfgFeasibilityEngineSMT,
			rc.cfgFeasibilityEngine,
		)
	}
	cfgRefinementMode := strings.TrimSpace(strings.ToLower(rc.cfgRefinementMode))
	if cfgRefinementMode == "" {
		cfgRefinementMode = defaultCFGRefinementMode
	}
	switch cfgRefinementMode {
	case cfgRefinementModeOff, cfgRefinementModeOnce, cfgRefinementModeCEGAR:
	default:
		return fmt.Errorf(
			"flag --cfg-refinement-mode must be %q, %q, or %q (got %q)",
			cfgRefinementModeOff,
			cfgRefinementModeOnce,
			cfgRefinementModeCEGAR,
			rc.cfgRefinementMode,
		)
	}
	cfgRefinementMaxIterations := rc.cfgRefinementMaxIterations
	if cfgRefinementMaxIterations == 0 {
		cfgRefinementMaxIterations = defaultCFGRefinementMaxIterations
	}
	if cfgRefinementMaxIterations <= 0 {
		return fmt.Errorf(
			"flag --cfg-refinement-max-iterations must be > 0 (got %d)",
			rc.cfgRefinementMaxIterations,
		)
	}
	cfgFeasibilityMaxQueries := rc.cfgFeasibilityMaxQueries
	if cfgFeasibilityMaxQueries == 0 {
		cfgFeasibilityMaxQueries = defaultCFGFeasibilityMaxQueries
	}
	if cfgFeasibilityMaxQueries <= 0 {
		return fmt.Errorf(
			"flag --cfg-feasibility-max-queries must be > 0 (got %d)",
			rc.cfgFeasibilityMaxQueries,
		)
	}
	cfgFeasibilityTimeoutMS := rc.cfgFeasibilityTimeoutMS
	if cfgFeasibilityTimeoutMS == 0 {
		cfgFeasibilityTimeoutMS = defaultCFGFeasibilityTimeoutMS
	}
	if cfgFeasibilityTimeoutMS <= 0 {
		return fmt.Errorf(
			"flag --cfg-feasibility-timeout-ms must be > 0 (got %d)",
			rc.cfgFeasibilityTimeoutMS,
		)
	}
	cfgAliasMode := strings.TrimSpace(strings.ToLower(rc.cfgAliasMode))
	if cfgAliasMode == "" {
		cfgAliasMode = defaultCFGAliasMode
	}
	switch cfgAliasMode {
	case cfgAliasModeOff, cfgAliasModeSSA:
	default:
		return fmt.Errorf(
			"flag --cfg-alias-mode must be %q or %q (got %q)",
			cfgAliasModeOff,
			cfgAliasModeSSA,
			rc.cfgAliasMode,
		)
	}
	phaseCEnabled := cfgFeasibilityEngine != cfgFeasibilityEngineOff || cfgRefinementMode != cfgRefinementModeOff
	if phaseCEnabled {
		if cfgInterprocEngine != cfgInterprocEngineIFDS {
			return fmt.Errorf(
				"phase c flags require --cfg-interproc-engine=%q (got %q)",
				cfgInterprocEngineIFDS,
				cfgInterprocEngine,
			)
		}
		if cfgFeasibilityEngine == cfgFeasibilityEngineOff || cfgRefinementMode == cfgRefinementModeOff {
			return errors.New("phase c requires either off/off or smt with once/cegar refinement")
		}
	}
	return nil
}

func parseIncludePackagesOverride(rc runConfig) ([]string, bool, error) {
	if !rc.includePackagesExplicit {
		return nil, false, nil
	}

	trimmed := strings.TrimSpace(rc.includePackages)
	if trimmed == "" {
		// Explicit empty value clears include_packages filtering.
		return []string{}, true, nil
	}

	parts := strings.Split(rc.includePackages, ",")
	out := make([]string, 0, len(parts))
	for i, part := range parts {
		prefix := strings.TrimSpace(part)
		if prefix == "" {
			return nil, false, fmt.Errorf("flag --include-packages contains an empty package prefix at position %d", i)
		}
		out = append(out, prefix)
	}
	return out, true, nil
}

func loadRunInputs(state *flagState, rc runConfig) (*ExceptionConfig, *BaselineConfig, error) {
	cfg, err := loadConfigCached(state, rc.configPath, rc.configPathExplicit)
	if err != nil {
		return nil, nil, err
	}

	bl, err := loadBaselineCached(state, rc.baselinePath, rc.baselinePathExplicit)
	if err != nil {
		return nil, nil, err
	}

	return cfg, bl, nil
}

// runFactExportOnly performs a minimal AST traversal to export cross-package
// facts (ValidatesTypeFact, NonZeroFact) without emitting any diagnostics.
// Called for packages excluded by include_packages — their type information
// is still needed by downstream packages for constructor-validates and
// nonzero field checking.
func runFactExportOnly(pass *analysis.Pass) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.GenDecl)(nil),
		(*ast.FuncDecl)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		switch n := n.(type) {
		case *ast.GenDecl:
			// Export NonZeroFact for types with //goplint:nonzero directive.
			exportNonZeroFacts(pass, n)
			// Export CueFedPathFact for types with //goplint:cue-fed-path directive.
			exportCueFedPathFacts(pass, n)
			// Export PathDomainFact for types with //goplint:path-domain directives.
			exportPathDomainFacts(pass, n)
		case *ast.FuncDecl:
			// Export ValidatesTypeFact for functions with //goplint:validates-type directive.
			exportValidatesTypeFacts(pass, n)
		}
	})
}

func runTraversal(
	pass *analysis.Pass,
	insp *inspector.Inspector,
	rc runConfig,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
	needs runNeeds,
	collectors *runCollectors,
	ssaRes *ssaResult,
) error {
	var traverseErr error
	nodeFilter := []ast.Node{
		(*ast.GenDecl)(nil),
		(*ast.FuncDecl)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		if traverseErr != nil {
			return
		}

		// Pathmatrix-divergent runs on test files (where pathmatrix is
		// used) and is dispatched BEFORE the general test-file skip
		// below. Production-code checks all skip _test.go files.
		if rc.checkPathmatrixDivergent {
			if fn, ok := n.(*ast.FuncDecl); ok {
				inspectPathmatrixDivergent(pass, fn, cfg, bl)
			}
		}

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
			// Export CueFedPathFact for types with //goplint:cue-fed-path
			// directive. Done in source order so subsequent IsAbs checks
			// in this package see the fact via pass.ImportObjectFact.
			exportCueFedPathFacts(pass, n)
			// Export PathDomainFact for path-domain annotated types.
			exportPathDomainFacts(pass, n)

			// Primary mode: check struct fields for primitives.
			inspectStructFields(pass, n, cfg, bl)

			// Supplementary: collect named types.
			if rc.checkValidate || rc.checkStringer {
				collectNamedTypes(pass, n, &collectors.namedTypes)
			}

			// Collect exported structs — use field-enriched version when
			// structural checks need field metadata.
			if needs.needConstructors {
				if needs.needStructFields {
					collectExportedStructsWithFields(pass, n, &collectors.exportedStructs)
				} else {
					collectExportedStructs(pass, n, &collectors.exportedStructs)
				}
			}

			// Structural: collect option type definitions.
			if needs.needOptionTypes {
				collectOptionTypes(pass, n, collectors.optionTypes)
			}

			// Collect types with //goplint:constant-only directive.
			if collectors.constantOnlyTypes != nil {
				collectConstantOnlyTypes(pass.Pkg.Path(), n, collectors.constantOnlyTypes)
			}

		case *ast.FuncDecl:
			// Always export validates-type facts for cross-package
			// constructor-validates tracking (analysis.Fact propagation).
			exportValidatesTypeFacts(pass, n)

			// Primary mode: check func params and returns for primitives.
			inspectFuncDecl(pass, n, cfg, bl)

			// Supplementary: track methods for validate/stringer and error detection.
			if needs.needMethods {
				trackMethods(pass, n, collectors.methodSeen)
			}

			// Track constructors with return type and param details.
			if collectors.constructorDetails != nil {
				trackConstructorDetails(pass, n, collectors.constructorDetails)
			}

			// Structural: track WithXxx option functions.
			if needs.needWithFuncs {
				trackWithFunctions(pass, n, collectors.optionTypes, collectors.withFunctions)
			}

			// Cast validation: detect unvalidated type conversions to DDD types.
			// Always uses CFA path-reachability analysis.
			if rc.checkCastValidation {
				if err := inspectUnvalidatedCastsCFA(
					pass,
					n,
					cfg,
					bl,
					rc.checkUseBeforeValidate,
					rc.ubvMode,
					rc.cfgBackend,
					rc.cfgInterprocEngine,
					rc.cfgMaxStates,
					rc.cfgMaxDepth,
					rc.cfgInconclusivePolicy,
					rc.cfgWitnessMaxSteps,
					newCFGPhaseCOptions(rc),
					rc.cfgAliasMode,
					ssaRes,
				); err != nil {
					traverseErr = err
					return
				}
			}

			// Redundant conversion: detect NamedType(basic(namedExpr)) chains.
			if rc.checkRedundantConversion {
				inspectRedundantConversions(pass, n, cfg, bl)
			}

			// Validate usage: detect discarded Validate() results.
			if rc.checkValidateUsage {
				inspectValidateUsage(pass, n, cfg, bl)
			}

			// Constructor error usage: detect blanked error returns.
			if rc.checkConstructorErrUsage {
				inspectConstructorErrorUsage(pass, n, cfg, bl)
			}

			if rc.checkBoundaryRequest {
				inspectBoundaryRequestValidation(pass, n, cfg, bl)
			}

			// Cross-platform path: detect filepath.IsAbs(filepath.FromSlash(x))
			// chains that miss Unix-style absolute paths on Windows.
			if rc.checkCrossPlatformPath {
				inspectCrossPlatformPath(pass, n, cfg, bl)
			}

			if rc.checkCommandWaitDelay {
				inspectCommandWaitDelay(pass, n, cfg, bl)
			}

			if rc.checkCueFedPathNativeClean {
				inspectCueFedPathNativeClean(pass, n, cfg, bl)
			}

			if rc.checkPathBoundaryPrefix {
				inspectPathBoundaryPrefix(pass, n, cfg, bl)
			}

			if rc.checkVolumeMountHostToSlash {
				inspectVolumeMountHostToSlash(pass, n, cfg, bl)
			}

			if rc.checkCobraCommandContext {
				inspectCobraCommandContext(pass, n, cfg, bl)
			}

			if rc.checkPathDomainNativeFilepath {
				inspectPathDomainNativeFilepath(pass, n, cfg, bl)
			}

		}
	})
	return traverseErr
}

func runPostTraversalChecks(
	pass *analysis.Pass,
	state *flagState,
	rc runConfig,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
	collectors *runCollectors,
) error {
	// Post-traversal checks for supplementary modes.
	if rc.checkValidate {
		reportMissingValidate(pass, collectors.namedTypes, collectors.methodSeen, cfg, bl)
	}
	if rc.checkStringer {
		reportMissingStringer(pass, collectors.namedTypes, collectors.methodSeen, cfg, bl)
	}
	if rc.checkConstructors {
		reportMissingConstructors(pass, collectors.exportedStructs, collectors.constructorDetails, collectors.methodSeen, cfg, bl)
	}

	// Structural checks — all require constructorDetails.
	if rc.checkConstructorSig {
		reportWrongConstructorSig(pass, collectors.exportedStructs, collectors.constructorDetails, cfg, bl)
	}
	if rc.checkFuncOptions {
		reportMissingFuncOptions(pass, collectors.exportedStructs, collectors.constructorDetails, collectors.optionTypes, collectors.withFunctions, cfg, bl)
	}
	if rc.checkImmutability {
		reportMissingImmutability(pass, collectors.exportedStructs, collectors.constructorDetails, cfg, bl)
	}
	if rc.checkStructValidate {
		reportMissingStructValidate(pass, collectors.exportedStructs, collectors.constructorDetails, collectors.methodSeen, cfg, bl)
	}
	if rc.checkConstructorValidates {
		if err := inspectConstructorValidates(
			pass,
			collectors.constructorDetails,
			collectors.constantOnlyTypes,
			cfg,
			bl,
			rc.cfgBackend,
			rc.cfgInterprocEngine,
			rc.cfgMaxStates,
			rc.cfgMaxDepth,
			rc.cfgInconclusivePolicy,
			rc.cfgWitnessMaxSteps,
			newCFGPhaseCOptions(rc),
		); err != nil {
			return err
		}
	}

	// Constructor return error — constructors for validatable types should return error.
	if rc.checkConstructorReturnError {
		inspectConstructorReturnError(pass, collectors.constructorDetails, collectors.constantOnlyTypes, cfg, bl)
	}

	// Validate delegation — always evaluates both directive-based and
	// universal delegation semantics.
	if rc.checkValidateDelegation {
		inspectValidateDelegation(pass, cfg, bl)
		inspectValidateDelegationAll(pass, cfg, bl)
	}

	// Nonzero field checks — cross-package via analysis.Fact.
	if rc.checkNonZero {
		inspectNonZero(pass, cfg, bl)
	}

	if rc.auditExceptions {
		reportStaleExceptionsInline(pass, cfg)
	}

	if rc.auditReviewDates {
		reportOverdueExceptions(pass, cfg, state)
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
	return nil
}
