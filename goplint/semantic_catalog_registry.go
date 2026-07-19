// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"strings"
)

const (
	semanticKindStructural    semanticKind = "structural"
	semanticKindProtocol      semanticKind = "protocol"
	semanticKindCrossArtifact semanticKind = "cross-artifact"

	oracleStrategyBoundaryFixture oracleStrategy = "boundary-fixture"
	oracleStrategyLayeredProtocol oracleStrategy = "layered-protocol"
	oracleStrategyArtifactParity  oracleStrategy = "artifact-parity"

	semanticLayerRuleContract       semanticEvidenceLayer = "rule-contract"
	semanticLayerOwnerRoute         semanticEvidenceLayer = "owner-route"
	semanticLayerMustReport         semanticEvidenceLayer = "must-report"
	semanticLayerMustNotReport      semanticEvidenceLayer = "must-not-report"
	semanticLayerMustBeInconclusive semanticEvidenceLayer = "must-be-inconclusive"
	semanticLayerArtifactParity     semanticEvidenceLayer = "artifact-parity"
	semanticLayerProduction         semanticEvidenceLayer = "production-integration"
	semanticLayerGenerated          semanticEvidenceLayer = "generated"
	semanticLayerMetamorphic        semanticEvidenceLayer = "metamorphic"
	semanticLayerFuzz               semanticEvidenceLayer = "fuzz"
	semanticLayerMutation           semanticEvidenceLayer = "mutation"
	semanticLayerDeterminism        semanticEvidenceLayer = "determinism"

	ownerPrimitive             semanticOwnerKey = "primitive-detection"
	ownerTypeMethodContract    semanticOwnerKey = "type-method-contract"
	ownerConstructorShape      semanticOwnerKey = "constructor-shape"
	ownerProtocolCFA           semanticOwnerKey = "protocol-cfa"
	ownerValidateUsage         semanticOwnerKey = "validate-result-usage"
	ownerConstructorErrorUsage semanticOwnerKey = "constructor-error-usage"
	ownerConstructorValidation semanticOwnerKey = "constructor-validation"
	ownerValidateDelegation    semanticOwnerKey = "validate-delegation"
	ownerNonZero               semanticOwnerKey = "nonzero-field"
	ownerEnumCUESync           semanticOwnerKey = "enum-cue-sync"
	ownerRedundantConversion   semanticOwnerKey = "redundant-conversion"
	ownerBoundaryRequest       semanticOwnerKey = "boundary-request-validation"
	ownerCrossPlatformPath     semanticOwnerKey = "cross-platform-path"
	ownerPathmatrix            semanticOwnerKey = "pathmatrix-divergence"
	ownerTestHome              semanticOwnerKey = "test-home-environment"
	ownerWindowsPitfalls       semanticOwnerKey = "windows-pitfalls"
	ownerPathDomain            semanticOwnerKey = "path-domain"
	ownerDirective             semanticOwnerKey = "directive-validation"
	ownerExceptionGovernance   semanticOwnerKey = "exception-governance"

	semanticRoutePrimitive             semanticProductionRoute = "primitive-detection"
	semanticRouteTypeMethodContract    semanticProductionRoute = "type-method-contract"
	semanticRouteConstructorShape      semanticProductionRoute = "constructor-shape"
	semanticRouteProtocolCFA           semanticProductionRoute = "protocol-cfa"
	semanticRouteValidateUsage         semanticProductionRoute = "validate-result-usage"
	semanticRouteConstructorErrorUsage semanticProductionRoute = "constructor-error-usage"
	semanticRouteConstructorValidation semanticProductionRoute = "constructor-validation"
	semanticRouteValidateDelegation    semanticProductionRoute = "validate-delegation"
	semanticRouteNonZero               semanticProductionRoute = "nonzero-field"
	semanticRouteEnumCUESync           semanticProductionRoute = "enum-cue-sync"
	semanticRouteRedundantConversion   semanticProductionRoute = "redundant-conversion"
	semanticRouteBoundaryRequest       semanticProductionRoute = "boundary-request-validation"
	semanticRouteCrossPlatformPath     semanticProductionRoute = "cross-platform-path"
	semanticRoutePathmatrix            semanticProductionRoute = "pathmatrix-divergence"
	semanticRouteTestHome              semanticProductionRoute = "test-home-environment"
	semanticRouteWindowsPitfalls       semanticProductionRoute = "windows-pitfalls"
	semanticRoutePathDomain            semanticProductionRoute = "path-domain"
	semanticRouteDirective             semanticProductionRoute = "directive-validation"
	semanticRouteExceptionGovernance   semanticProductionRoute = "exception-governance"
)

type (
	semanticKind            string
	oracleStrategy          string
	semanticOwnerKey        string
	semanticEvidenceLayer   string
	semanticProductionRoute string

	semanticCategorySpec struct {
		Category       string
		Kind           semanticKind
		Owner          semanticOwnerKey
		OracleStrategy oracleStrategy
	}

	semanticOwnerRegistration struct {
		Key                   semanticOwnerKey
		Route                 semanticProductionRoute
		Traversal             semanticTraversalHandler
		PostTraversal         semanticPostTraversalHandler
		TraverseNonProduction bool
	}
)

func semanticCategoryRegistry() []semanticCategorySpec {
	registry := diagnosticCategoryRegistry()
	result := make([]semanticCategorySpec, 0, len(registry))
	for _, spec := range registry {
		result = append(result, semanticCategorySpec{
			Category:       spec.Name,
			Kind:           spec.SemanticKind,
			Owner:          spec.Owner,
			OracleStrategy: oracleStrategyForKind(spec.SemanticKind),
		})
	}
	return result
}

func semanticOwnerRegistry() []semanticOwnerRegistration {
	return []semanticOwnerRegistration{
		{Key: ownerDirective, Route: semanticRouteDirective, Traversal: routeDirectiveDiagnostics},
		{Key: ownerPrimitive, Route: semanticRoutePrimitive, Traversal: routePrimitiveDiagnostics},
		{Key: ownerTypeMethodContract, Route: semanticRouteTypeMethodContract, PostTraversal: routeTypeMethodContractDiagnostics},
		{Key: ownerConstructorShape, Route: semanticRouteConstructorShape, PostTraversal: routeConstructorShapeDiagnostics},
		{Key: ownerProtocolCFA, Route: semanticRouteProtocolCFA, PostTraversal: routeProtocolCFADiagnostics},
		{Key: ownerValidateUsage, Route: semanticRouteValidateUsage, Traversal: routeValidateUsageDiagnostics},
		{Key: ownerConstructorErrorUsage, Route: semanticRouteConstructorErrorUsage, Traversal: routeConstructorErrorUsageDiagnostics},
		{Key: ownerConstructorValidation, Route: semanticRouteConstructorValidation, PostTraversal: routeConstructorValidationDiagnostics},
		{Key: ownerValidateDelegation, Route: semanticRouteValidateDelegation, PostTraversal: routeValidateDelegationDiagnostics},
		{Key: ownerNonZero, Route: semanticRouteNonZero, PostTraversal: routeNonZeroDiagnostics},
		{Key: ownerEnumCUESync, Route: semanticRouteEnumCUESync, PostTraversal: routeEnumCUESyncDiagnostics},
		{Key: ownerRedundantConversion, Route: semanticRouteRedundantConversion, Traversal: routeRedundantConversionDiagnostics},
		{Key: ownerBoundaryRequest, Route: semanticRouteBoundaryRequest, Traversal: routeBoundaryRequestDiagnostics},
		{Key: ownerCrossPlatformPath, Route: semanticRouteCrossPlatformPath, Traversal: routeCrossPlatformPathDiagnostics},
		{Key: ownerPathmatrix, Route: semanticRoutePathmatrix, Traversal: routePathmatrixDiagnostics, TraverseNonProduction: true},
		{Key: ownerTestHome, Route: semanticRouteTestHome, Traversal: routeTestHomeDiagnostics, TraverseNonProduction: true},
		{Key: ownerWindowsPitfalls, Route: semanticRouteWindowsPitfalls, Traversal: routeWindowsPitfallDiagnostics},
		{Key: ownerPathDomain, Route: semanticRoutePathDomain, Traversal: routePathDomainDiagnostics},
		{Key: ownerExceptionGovernance, Route: semanticRouteExceptionGovernance, PostTraversal: routeExceptionGovernanceDiagnostics},
	}
}

func requiredOracleLayersForKind(kind semanticKind) []semanticEvidenceLayer {
	common := []semanticEvidenceLayer{
		semanticLayerRuleContract,
		semanticLayerOwnerRoute,
		semanticLayerMustReport,
		semanticLayerMustNotReport,
	}
	switch kind {
	case semanticKindStructural:
		return common
	case semanticKindCrossArtifact:
		return append(common, semanticLayerArtifactParity)
	case semanticKindProtocol:
		return append(common,
			semanticLayerMustBeInconclusive,
			semanticLayerProduction,
			semanticLayerGenerated,
			semanticLayerMetamorphic,
			semanticLayerFuzz,
			semanticLayerMutation,
			semanticLayerDeterminism,
		)
	default:
		return nil
	}
}

func oracleStrategyForKind(kind semanticKind) oracleStrategy {
	switch kind {
	case semanticKindStructural:
		return oracleStrategyBoundaryFixture
	case semanticKindProtocol:
		return oracleStrategyLayeredProtocol
	case semanticKindCrossArtifact:
		return oracleStrategyArtifactParity
	default:
		return ""
	}
}

func validateSemanticRegistries() error {
	return validateSemanticRegistryData(diagnosticCategoryRegistry(), semanticCategoryRegistry(), semanticOwnerRegistry())
}

func validateSemanticRegistryData(
	diagnosticSpecs []CategorySpec,
	semanticSpecs []semanticCategorySpec,
	ownerSpecs []semanticOwnerRegistration,
) error {
	owners := make(map[semanticOwnerKey]bool, len(ownerSpecs))
	routes := make(map[semanticProductionRoute]semanticOwnerKey, len(ownerSpecs))
	for idx, owner := range ownerSpecs {
		if strings.TrimSpace(string(owner.Key)) == "" {
			return fmt.Errorf("semantic owner[%d] has an empty key", idx)
		}
		if _, duplicate := owners[owner.Key]; duplicate {
			return fmt.Errorf("duplicate semantic owner key %q", owner.Key)
		}
		if strings.TrimSpace(string(owner.Route)) == "" {
			return fmt.Errorf("semantic owner %q has no production route", owner.Key)
		}
		if owner.Traversal == nil && owner.PostTraversal == nil {
			return fmt.Errorf("semantic owner %q has no executable production handler", owner.Key)
		}
		if prior, duplicate := routes[owner.Route]; duplicate {
			return fmt.Errorf("semantic production route %q is registered by both %q and %q", owner.Route, prior, owner.Key)
		}
		routes[owner.Route] = owner.Key
		owners[owner.Key] = false
	}

	diagnosticCategories := make(map[string]struct{}, len(diagnosticSpecs))
	for _, spec := range diagnosticSpecs {
		diagnosticCategories[spec.Name] = struct{}{}
	}
	semanticCategories := make(map[string]struct{}, len(semanticSpecs))
	for idx, spec := range semanticSpecs {
		if strings.TrimSpace(spec.Category) == "" {
			return fmt.Errorf("semantic category[%d] has an empty category", idx)
		}
		if _, duplicate := semanticCategories[spec.Category]; duplicate {
			return fmt.Errorf("duplicate semantic category %q", spec.Category)
		}
		if _, live := diagnosticCategories[spec.Category]; !live {
			return fmt.Errorf("stale semantic category %q is not registered", spec.Category)
		}
		if !validSemanticKind(spec.Kind) {
			return fmt.Errorf("semantic category %q has invalid kind %q", spec.Category, spec.Kind)
		}
		_, resolves := owners[spec.Owner]
		if !resolves {
			return fmt.Errorf("semantic category %q has unresolvable owner %q", spec.Category, spec.Owner)
		}
		if !validOracleForKind(spec.Kind, spec.OracleStrategy) {
			return fmt.Errorf("semantic category %q has oracle strategy %q incompatible with kind %q", spec.Category, spec.OracleStrategy, spec.Kind)
		}
		owners[spec.Owner] = true
		semanticCategories[spec.Category] = struct{}{}
	}
	for category := range diagnosticCategories {
		if _, owned := semanticCategories[category]; !owned {
			return fmt.Errorf("registered diagnostic category %q has no semantic ownership", category)
		}
	}
	for owner, used := range owners {
		if !used {
			return fmt.Errorf("stale semantic owner %q has no categories", owner)
		}
	}
	return nil
}

func validateSemanticProductionRouting() error {
	return validateSemanticRegistries()
}

func validSemanticKind(kind semanticKind) bool {
	switch kind {
	case semanticKindStructural, semanticKindProtocol, semanticKindCrossArtifact:
		return true
	default:
		return false
	}
}

func validOracleForKind(kind semanticKind, strategy oracleStrategy) bool {
	switch kind {
	case semanticKindStructural:
		return strategy == oracleStrategyBoundaryFixture
	case semanticKindProtocol:
		return strategy == oracleStrategyLayeredProtocol
	case semanticKindCrossArtifact:
		return strategy == oracleStrategyArtifactParity
	default:
		return false
	}
}

func semanticCategoryByName(category string) (semanticCategorySpec, error) {
	for _, spec := range semanticCategoryRegistry() {
		if spec.Category == category {
			return spec, nil
		}
	}
	return semanticCategorySpec{}, fmt.Errorf("semantic category %q is not registered", category)
}
