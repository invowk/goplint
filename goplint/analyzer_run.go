// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"go/ast"
	"strings"
	"sync"

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
	if err := validateSemanticProductionRouting(); err != nil {
		return nil, fmt.Errorf("validating semantic production routing: %w", err)
	}
	rc := newRunConfigForState(state)
	if err := validateRunConfig(rc); err != nil {
		return nil, err
	}
	restoreReporter := installDiagnosticReporter(pass, rc.emitFindingsPath)
	defer restoreReporter()

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
	var protocolSSA *ssaResult
	if needsProtocolSummaryFacts(rc) {
		protocolSSA = state.ssaBuilder(pass)
		exportProtocolSummaryFacts(pass, protocolSSA)
	}
	if !cfg.ShouldAnalyzePackage(pass.Pkg.Path()) {
		runFactExportOnly(pass)
		return nil, nil
	}

	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return nil, errors.New("inspect analyzer result has unexpected type")
	}
	needs := deriveRunNeeds(rc)
	collectors := newRunCollectors(rc, needs)
	ssaRes := protocolSSA
	if rc.checkCastValidation && ssaRes == nil {
		// Canonical validation effects and alias identities share one SSA view
		// for every function and closure in the package.
		ssaRes = state.ssaBuilder(pass)
	}

	if err := runTraversal(pass, insp, rc, cfg, bl, needs, &collectors, ssaRes, &state.calleeSummaryCache); err != nil {
		return nil, err
	}
	if err := runPostTraversalChecks(
		pass,
		state,
		rc,
		cfg,
		bl,
		&collectors,
		ssaRes,
		&state.calleeSummaryCache,
	); err != nil {
		return nil, err
	}

	return nil, nil
}

func needsProtocolSummaryFacts(rc runConfig) bool {
	return rc.checkCastValidation || rc.checkUseBeforeValidate ||
		rc.checkConstructorValidates || rc.checkBoundaryRequest
}

func validateRunConfig(rc runConfig) error {
	if rc.configPathExplicit && strings.TrimSpace(rc.configPath) == "" {
		return errors.New("flag --config was provided with an empty path")
	}
	if rc.baselinePathExplicit && strings.TrimSpace(rc.baselinePath) == "" {
		return errors.New("flag --baseline was provided with an empty path")
	}
	cfgMaxStates := rc.cfgMaxStates
	if cfgMaxStates == 0 {
		cfgMaxStates = defaultCFGMaxStates
	}
	if cfgMaxStates <= 0 {
		return fmt.Errorf("flag --cfg-max-states must be > 0 (got %d)", rc.cfgMaxStates)
	}
	cfgWitnessMaxSteps := rc.cfgWitnessMaxSteps
	if cfgWitnessMaxSteps == 0 {
		cfgWitnessMaxSteps = defaultCFGWitnessMaxSteps
	}
	if cfgWitnessMaxSteps <= 0 {
		return fmt.Errorf("flag --cfg-witness-max-steps must be > 0 (got %d)", rc.cfgWitnessMaxSteps)
	}
	cfgRefinementMaxIterations := rc.cfgRefinementMaxIterations
	if cfgRefinementMaxIterations == 0 {
		cfgRefinementMaxIterations = defaultCFGRefinementMaxIterations
	}
	if cfgRefinementMaxIterations <= 0 {
		return fmt.Errorf(
			"flag --protocol-refinement-max-iterations must be > 0 (got %d)",
			rc.cfgRefinementMaxIterations,
		)
	}
	cfgFeasibilityMaxQueries := rc.cfgFeasibilityMaxQueries
	if cfgFeasibilityMaxQueries == 0 {
		cfgFeasibilityMaxQueries = defaultCFGFeasibilityMaxQueries
	}
	if cfgFeasibilityMaxQueries <= 0 {
		return fmt.Errorf(
			"flag --protocol-feasibility-max-queries must be > 0 (got %d)",
			rc.cfgFeasibilityMaxQueries,
		)
	}
	cfgFeasibilityTimeoutMS := rc.cfgFeasibilityTimeoutMS
	if cfgFeasibilityTimeoutMS == 0 {
		cfgFeasibilityTimeoutMS = defaultCFGFeasibilityTimeoutMS
	}
	if cfgFeasibilityTimeoutMS <= 0 {
		return fmt.Errorf(
			"flag --protocol-feasibility-timeout-ms must be > 0 (got %d)",
			rc.cfgFeasibilityTimeoutMS,
		)
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
// directive facts without emitting any diagnostics. ProtocolSummaryFact is
// exported separately from SSA before the package filter is evaluated.
// Called for packages excluded by include_packages — their type information
// is still needed by downstream packages for constructor-validates and
// nonzero field checking.
func runFactExportOnly(pass *analysis.Pass) {
	insp, ok := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !ok {
		return
	}

	nodeFilter := []ast.Node{(*ast.GenDecl)(nil)}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		if n, ok := n.(*ast.GenDecl); ok {
			// Export NonZeroFact for types with //goplint:nonzero directive.
			exportNonZeroFacts(pass, n)
			// Export CueFedPathFact for types with //goplint:cue-fed-path directive.
			exportCueFedPathFacts(pass, n)
			// Export PathDomainFact for types with //goplint:path-domain directives.
			exportPathDomainFacts(pass, n)
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
	calleeSummaryCache *sync.Map,
) error {
	var traverseErr error
	nodeFilter := []ast.Node{
		(*ast.File)(nil),
		(*ast.GenDecl)(nil),
		(*ast.FuncDecl)(nil),
	}

	insp.Preorder(nodeFilter, func(n ast.Node) {
		if traverseErr != nil {
			return
		}

		filePath := pass.Fset.Position(n.Pos()).Filename
		productionNode := !isTestFile(pass, n.Pos()) && !cfg.isExcludedPath(filePath)
		context := &semanticTraversalContext{
			pass:               pass,
			node:               n,
			rc:                 rc,
			cfg:                cfg,
			bl:                 bl,
			ssaRes:             ssaRes,
			calleeSummaryCache: calleeSummaryCache,
			productionNode:     productionNode,
		}
		if !productionNode {
			traverseErr = runSemanticTraversalRoutes(context)
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

		}
		traverseErr = runSemanticTraversalRoutes(context)
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
	ssaRes *ssaResult,
	calleeSummaryCache *sync.Map,
) error {
	return runSemanticPostTraversalRoutes(&semanticPostTraversalContext{
		pass:               pass,
		state:              state,
		rc:                 rc,
		cfg:                cfg,
		bl:                 bl,
		collectors:         collectors,
		ssaRes:             ssaRes,
		calleeSummaryCache: calleeSummaryCache,
	})
}
