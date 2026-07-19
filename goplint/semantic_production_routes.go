// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"sync"

	"golang.org/x/tools/go/analysis"
)

type semanticTraversalHandler func(*semanticTraversalContext) error

type semanticPostTraversalHandler func(*semanticPostTraversalContext) error

type semanticTraversalContext struct {
	pass               *analysis.Pass
	node               ast.Node
	rc                 runConfig
	cfg                *ExceptionConfig
	bl                 *BaselineConfig
	ssaRes             *ssaResult
	calleeSummaryCache *sync.Map
	productionNode     bool
}

type semanticPostTraversalContext struct {
	pass               *analysis.Pass
	state              *flagState
	rc                 runConfig
	cfg                *ExceptionConfig
	bl                 *BaselineConfig
	collectors         *runCollectors
	ssaRes             *ssaResult
	calleeSummaryCache *sync.Map
}

func runSemanticTraversalRoutes(context *semanticTraversalContext) error {
	for _, owner := range semanticOwnerRegistry() {
		if owner.Traversal == nil {
			continue
		}
		if !context.productionNode && !owner.TraverseNonProduction {
			continue
		}
		if err := owner.Traversal(context); err != nil {
			return err
		}
	}
	return nil
}

func runSemanticPostTraversalRoutes(context *semanticPostTraversalContext) error {
	for _, owner := range semanticOwnerRegistry() {
		if owner.PostTraversal == nil {
			continue
		}
		if err := owner.PostTraversal(context); err != nil {
			return err
		}
	}
	return nil
}

func routeDirectiveDiagnostics(context *semanticTraversalContext) error {
	file, ok := context.node.(*ast.File)
	if ok {
		inspectDirectivesInFile(context.pass, file)
	}
	return nil
}

func routePrimitiveDiagnostics(context *semanticTraversalContext) error {
	switch node := context.node.(type) {
	case *ast.GenDecl:
		inspectStructFields(context.pass, node, context.cfg, context.bl)
	case *ast.FuncDecl:
		inspectFuncDecl(context.pass, node, context.cfg, context.bl)
	}
	return nil
}

func routeTypeMethodContractDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.checkValidate {
		reportMissingValidate(context.pass, context.collectors.namedTypes, context.collectors.methodSeen, context.cfg, context.bl)
	}
	if context.rc.checkStringer {
		reportMissingStringer(context.pass, context.collectors.namedTypes, context.collectors.methodSeen, context.cfg, context.bl)
	}
	return nil
}

func routeConstructorShapeDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.checkConstructors {
		reportMissingConstructors(context.pass, context.collectors.exportedStructs, context.collectors.constructorDetails, context.collectors.methodSeen, context.cfg, context.bl)
	}
	if context.rc.checkConstructorSig {
		reportWrongConstructorSig(context.pass, context.collectors.exportedStructs, context.collectors.constructorDetails, context.cfg, context.bl)
	}
	if context.rc.checkFuncOptions {
		reportMissingFuncOptions(context.pass, context.collectors.exportedStructs, context.collectors.constructorDetails, context.collectors.optionTypes, context.collectors.withFunctions, context.cfg, context.bl)
	}
	if context.rc.checkImmutability {
		reportMissingImmutability(context.pass, context.collectors.exportedStructs, context.collectors.constructorDetails, context.cfg, context.bl)
	}
	if context.rc.checkStructValidate {
		reportMissingStructValidate(context.pass, context.collectors.exportedStructs, context.collectors.constructorDetails, context.collectors.methodSeen, context.cfg, context.bl)
	}
	if context.rc.checkConstructorReturnError {
		inspectConstructorReturnError(context.pass, context.collectors.constructorDetails, context.collectors.constantOnlyTypes, context.cfg, context.bl)
	}
	return nil
}

func routeProtocolCFADiagnostics(context *semanticPostTraversalContext) error {
	if !context.rc.checkCastValidation {
		return nil
	}
	features := []string{semanticFeatureCastValidation}
	if context.rc.checkUseBeforeValidate {
		features = append(features, semanticFeatureUseBeforeValidation)
	}
	procedureIndex := buildProtocolProcedureIndex(context.pass, context.ssaRes)
	for _, procedure := range procedureIndex.procedures() {
		if !protocolProcedureIsProduction(context.pass, context.cfg, procedure) {
			continue
		}
		for _, featureID := range features {
			observeSemanticEvidenceRoute(
				context.rc,
				featureID,
				ownerProtocolCFA,
				semanticRouteProtocolCFA,
				semanticEvidenceStageSourceExtraction,
			)
		}
		var err error
		switch {
		case procedure.Declaration != nil:
			err = inspectUnvalidatedCastsCFA(
				context.pass,
				procedure.Declaration,
				context.cfg,
				context.bl,
				context.rc.checkUseBeforeValidate,
				context.rc.cfgMaxStates,
				context.rc.cfgWitnessMaxSteps,
				newCFGProtocolRefinementOptions(context.rc),
				context.ssaRes,
				context.calleeSummaryCache,
				procedure.Availability,
			)
		case procedure.Literal != nil:
			err = inspectClosureCastsCFA(
				context.pass,
				procedure.Literal,
				protocolLiteralEnclosingQualifier(context.pass, procedure.Literal),
				procedure.Key,
				context.cfg,
				context.bl,
				context.rc.checkUseBeforeValidate,
				context.rc.cfgMaxStates,
				context.rc.cfgWitnessMaxSteps,
				newCFGProtocolRefinementOptions(context.rc),
				context.ssaRes,
				context.calleeSummaryCache,
				procedure.Availability,
			)
		}
		if err != nil {
			return err
		}
		for _, featureID := range features {
			observeSemanticEvidenceRoute(
				context.rc,
				featureID,
				ownerProtocolCFA,
				semanticRouteProtocolCFA,
				semanticEvidenceStageReporting,
			)
		}
	}
	return nil
}

func protocolProcedureIsProduction(
	pass *analysis.Pass,
	cfg *ExceptionConfig,
	procedure protocolProcedure,
) bool {
	if pass == nil || procedure.Body == nil {
		return false
	}
	filename := pass.Fset.Position(procedure.Body.Pos()).Filename
	return !isTestFile(pass, procedure.Body.Pos()) && (cfg == nil || !cfg.isExcludedPath(filename))
}

func protocolLiteralEnclosingQualifier(pass *analysis.Pass, literal *ast.FuncLit) string {
	if pass == nil || literal == nil {
		return "<unknown>.init"
	}
	for _, file := range pass.Files {
		if literal.Pos() < file.Pos() || literal.End() > file.End() {
			continue
		}
		parents := buildParentMap(file)
		for current := parents[literal]; current != nil; current = parents[current] {
			if declaration, ok := current.(*ast.FuncDecl); ok {
				return qualFuncName(pass, declaration)
			}
		}
		break
	}
	return packageName(pass.Pkg) + ".init"
}

func routeValidateUsageDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkValidateUsage {
		inspectValidateUsage(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeConstructorErrorUsageDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkConstructorErrUsage {
		inspectConstructorErrorUsage(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeConstructorValidationDiagnostics(context *semanticPostTraversalContext) error {
	if !context.rc.checkConstructorValidates {
		return nil
	}
	observeSemanticEvidenceRoute(
		context.rc,
		semanticFeatureConstructorValidation,
		ownerConstructorValidation,
		semanticRouteConstructorValidation,
		semanticEvidenceStageSourceExtraction,
	)
	err := inspectConstructorValidates(
		context.pass,
		context.collectors.constructorDetails,
		context.collectors.constantOnlyTypes,
		context.cfg,
		context.bl,
		context.rc.cfgMaxStates,
		context.rc.cfgWitnessMaxSteps,
		newCFGProtocolRefinementOptions(context.rc),
		&context.state.calleeSummaryCache,
		context.ssaRes,
	)
	if err != nil {
		return err
	}
	observeSemanticEvidenceRoute(
		context.rc,
		semanticFeatureConstructorValidation,
		ownerConstructorValidation,
		semanticRouteConstructorValidation,
		semanticEvidenceStageReporting,
	)
	return nil
}

func routeValidateDelegationDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.checkValidateDelegation {
		inspectValidateDelegation(context.pass, context.cfg, context.bl)
		inspectValidateDelegationAll(context.pass, context.cfg, context.bl)
	}
	if context.rc.suggestValidateAll {
		inspectSuggestValidateAll(context.pass, context.cfg, context.bl)
	}
	return nil
}

func routeNonZeroDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.checkNonZero {
		inspectNonZero(context.pass, context.cfg, context.bl)
	}
	return nil
}

func routeEnumCUESyncDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.checkEnumSync {
		inspectEnumSync(context.pass, context.cfg, context.bl)
	}
	return nil
}

func routeRedundantConversionDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkRedundantConversion {
		inspectRedundantConversions(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeBoundaryRequestDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if !ok || !context.rc.checkBoundaryRequest {
		return nil
	}
	observeSemanticEvidenceRoute(
		context.rc,
		semanticFeatureBoundaryRequest,
		ownerBoundaryRequest,
		semanticRouteBoundaryRequest,
		semanticEvidenceStageSourceExtraction,
	)
	inspectBoundaryRequestValidation(
		context.pass,
		function,
		context.cfg,
		context.bl,
		context.rc.cfgMaxStates,
		newCFGProtocolRefinementOptions(context.rc),
		context.ssaRes,
		context.calleeSummaryCache,
	)
	observeSemanticEvidenceRoute(
		context.rc,
		semanticFeatureBoundaryRequest,
		ownerBoundaryRequest,
		semanticRouteBoundaryRequest,
		semanticEvidenceStageReporting,
	)
	return nil
}

func routeCrossPlatformPathDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkCrossPlatformPath {
		inspectCrossPlatformPath(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routePathmatrixDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkPathmatrixDivergent {
		inspectPathmatrixDivergent(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeTestHomeDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkTestHomeEnvPlatform {
		inspectTestHomeEnvPlatform(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeWindowsPitfallDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if !ok {
		return nil
	}
	if context.rc.checkCommandWaitDelay {
		inspectCommandWaitDelay(context.pass, function, context.cfg, context.bl)
	}
	if context.rc.checkCueFedPathNativeClean {
		inspectCueFedPathNativeClean(context.pass, function, context.cfg, context.bl)
	}
	if context.rc.checkPathBoundaryPrefix {
		inspectPathBoundaryPrefix(context.pass, function, context.cfg, context.bl)
	}
	if context.rc.checkVolumeMountHostToSlash {
		inspectVolumeMountHostToSlash(context.pass, function, context.cfg, context.bl)
	}
	if context.rc.checkCobraCommandContext {
		inspectCobraCommandContext(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routePathDomainDiagnostics(context *semanticTraversalContext) error {
	function, ok := context.node.(*ast.FuncDecl)
	if ok && context.rc.checkPathDomainNativeFilepath {
		inspectPathDomainNativeFilepath(context.pass, function, context.cfg, context.bl)
	}
	return nil
}

func routeExceptionGovernanceDiagnostics(context *semanticPostTraversalContext) error {
	if context.rc.auditReviewDates {
		reportOverdueExceptions(context.pass, context.cfg, context.state)
	}
	if context.rc.auditExceptions {
		reportStaleExceptionsInline(context.pass, context.cfg)
	}
	return nil
}
