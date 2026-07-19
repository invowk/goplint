// SPDX-License-Identifier: MPL-2.0

package goplint

// BaselinePolicy describes how a diagnostic category is treated by
// baseline generation and suppression.
type BaselinePolicy int

const (
	// BaselineSuppressible categories are included in baseline TOML and can
	// be suppressed by baseline matching.
	BaselineSuppressible BaselinePolicy = iota
	// BaselineAlwaysVisible categories are never baselined and should always
	// be reported when encountered.
	BaselineAlwaysVisible
	// BaselineAuditOnly categories are operational/config audit findings and
	// are excluded from baseline generation.
	BaselineAuditOnly
)

// CategorySpec is the canonical metadata for one diagnostic category.
type CategorySpec struct {
	Name                 string
	BaselinePolicy       BaselinePolicy
	SemanticKind         semanticKind
	Owner                semanticOwnerKey
	RequiredOracleLayers []semanticEvidenceLayer
	// BaselineLabel is the human-readable section label used when writing
	// baseline TOML. Only meaningful for BaselineSuppressible categories.
	BaselineLabel string
}

// diagnosticCategoryRegistry returns the canonical category list.
// Keep this list in sync with category constants and diagnostic emitters.
func diagnosticCategoryRegistry() []CategorySpec {
	return []CategorySpec{
		newStructuralCategory(CategoryPrimitive, BaselineSuppressible, "Bare primitive type usage", ownerPrimitive),
		newStructuralCategory(CategoryMissingValidate, BaselineSuppressible, "Named types missing Validate() method", ownerTypeMethodContract),
		newStructuralCategory(CategoryMissingStringer, BaselineSuppressible, "Named types missing String() method", ownerTypeMethodContract),
		newStructuralCategory(CategoryMissingConstructor, BaselineSuppressible, "Exported structs missing NewXxx() constructor", ownerConstructorShape),
		newStructuralCategory(CategoryWrongConstructorSig, BaselineSuppressible, "Constructors with wrong return type", ownerConstructorShape),
		newStructuralCategory(CategoryMissingFuncOptions, BaselineSuppressible, "Structs missing functional options pattern", ownerConstructorShape),
		newStructuralCategory(CategoryMissingImmutability, BaselineSuppressible, "Structs with constructor but exported mutable fields", ownerConstructorShape),
		newStructuralCategory(CategoryWrongValidateSig, BaselineSuppressible, "Named types with wrong Validate() signature", ownerTypeMethodContract),
		newStructuralCategory(CategoryWrongStringerSig, BaselineSuppressible, "Named types with wrong String() signature", ownerTypeMethodContract),
		newStructuralCategory(CategoryMissingStructValidate, BaselineSuppressible, "Structs with constructor but no Validate() method", ownerConstructorShape),
		newStructuralCategory(CategoryWrongStructValidateSig, BaselineSuppressible, "Structs with Validate() but wrong signature", ownerConstructorShape),
		newProtocolCategory(CategoryUnvalidatedCast, BaselineSuppressible, "Type conversions to DDD types without Validate() check", ownerProtocolCFA),
		newProtocolCategory(CategoryUnvalidatedCastInconclusive, BaselineAlwaysVisible, "", ownerProtocolCFA),
		newStructuralCategory(CategoryUnusedValidateResult, BaselineSuppressible, "Validate() calls with result completely discarded", ownerValidateUsage),
		newStructuralCategory(CategoryUnusedConstructorError, BaselineSuppressible, "Constructor calls with error return assigned to blank identifier", ownerConstructorErrorUsage),
		newProtocolCategory(CategoryMissingConstructorValidate, BaselineSuppressible, "Constructors returning validatable types without calling Validate()", ownerConstructorValidation),
		newProtocolCategory(CategoryMissingConstructorValidateInc, BaselineAlwaysVisible, "", ownerConstructorValidation),
		newStructuralCategory(CategoryIncompleteValidateDelegation, BaselineSuppressible, "Structs with validate-all missing field Validate() delegation", ownerValidateDelegation),
		newStructuralCategory(CategoryNonZeroValueField, BaselineSuppressible, "Struct fields using nonzero types as value (non-pointer)", ownerNonZero),
		newStructuralCategory(CategoryWrongFuncOptionType, BaselineSuppressible, "WithXxx option functions with parameter type mismatches", ownerConstructorShape),
		newCrossArtifactCategory(CategoryEnumCueMissingGo, BaselineSuppressible, "CUE disjunction members missing from Go Validate() switch", ownerEnumCUESync),
		newCrossArtifactCategory(CategoryEnumCueExtraGo, BaselineSuppressible, "Go Validate() switch cases not present in CUE disjunction", ownerEnumCUESync),
		newProtocolCategory(CategoryUseBeforeValidateSameBlock, BaselineSuppressible, "DDD Value Type values used before Validate() in same block", ownerProtocolCFA),
		newProtocolCategory(CategoryUseBeforeValidateCrossBlock, BaselineSuppressible, "DDD Value Type values used before Validate() across blocks", ownerProtocolCFA),
		newProtocolCategory(CategoryUseBeforeValidateInconclusive, BaselineAlwaysVisible, "", ownerProtocolCFA),
		newStructuralCategory(CategorySuggestValidateAll, BaselineSuppressible, "Structs suggesting //goplint:validate-all adoption", ownerValidateDelegation),
		newStructuralCategory(CategoryMissingConstructorErrorReturn, BaselineSuppressible, "Constructors returning validatable types without an error return", ownerConstructorShape),
		newStructuralCategory(CategoryRedundantConversion, BaselineSuppressible, "Redundant intermediate type conversions", ownerRedundantConversion),
		newStructuralCategory(CategoryMissingStructValidateFields, BaselineSuppressible, "Structs with validatable fields but no Validate() method", ownerValidateDelegation),
		newProtocolCategory(CategoryUnvalidatedBoundaryRequest, BaselineSuppressible, "Exported request/options boundaries using parameters before Validate()", ownerBoundaryRequest),
		newStructuralCategory(CategoryCrossPlatformPath, BaselineSuppressible, "filepath.IsAbs called on FromSlash result without strings.HasPrefix slash guard", ownerCrossPlatformPath),
		newStructuralCategory(CategoryPathmatrixDivergent, BaselineSuppressible, "pathmatrix.PassRelative on a platform-divergent vector without OnWindows override", ownerPathmatrix),
		newStructuralCategory(CategoryTestHomeEnvPlatform, BaselineAlwaysVisible, "", ownerTestHome),
		newStructuralCategory(CategoryMissingCommandWaitDelay, BaselineSuppressible, "exec.CommandContext used without setting Cmd.WaitDelay before process execution", ownerWindowsPitfalls),
		newStructuralCategory(CategoryCueFedPathNativeClean, BaselineSuppressible, "CUE-fed or repo-relative path validated through host-native filepath cleanup before slash normalization", ownerWindowsPitfalls),
		newStructuralCategory(CategoryPathBoundaryPrefix, BaselineSuppressible, "path containment check uses an unsafe string prefix without a path-segment boundary", ownerWindowsPitfalls),
		newStructuralCategory(CategoryVolumeMountHostToSlash, BaselineSuppressible, "container volume mount host path formatted without filepath.ToSlash before the colon", ownerWindowsPitfalls),
		newStructuralCategory(CategoryCobraCommandContext, BaselineSuppressible, "Cobra command handler uses context.Background instead of command context", ownerWindowsPitfalls),
		newStructuralCategory(CategoryPathDomainNativeFilepath, BaselineSuppressible, "path-domain annotated values sent through host-native filepath functions", ownerPathDomain),
		newStructuralCategory(CategoryUnknownDirective, BaselineAlwaysVisible, "", ownerDirective),
		newCrossArtifactCategory(CategoryStaleException, BaselineAuditOnly, "", ownerExceptionGovernance),
		newCrossArtifactCategory(CategoryOverdueReview, BaselineAuditOnly, "", ownerExceptionGovernance),
	}
}

func newStructuralCategory(name string, policy BaselinePolicy, label string, owner semanticOwnerKey) CategorySpec {
	return newCategorySpec(name, policy, label, semanticKindStructural, owner)
}

func newProtocolCategory(name string, policy BaselinePolicy, label string, owner semanticOwnerKey) CategorySpec {
	return newCategorySpec(name, policy, label, semanticKindProtocol, owner)
}

func newCrossArtifactCategory(name string, policy BaselinePolicy, label string, owner semanticOwnerKey) CategorySpec {
	return newCategorySpec(name, policy, label, semanticKindCrossArtifact, owner)
}

func newCategorySpec(name string, policy BaselinePolicy, label string, kind semanticKind, owner semanticOwnerKey) CategorySpec {
	return CategorySpec{
		Name:                 name,
		BaselinePolicy:       policy,
		BaselineLabel:        label,
		SemanticKind:         kind,
		Owner:                owner,
		RequiredOracleLayers: requiredOracleLayersForKind(kind),
	}
}

func diagnosticCategorySpec(name string) (CategorySpec, bool) {
	for _, spec := range diagnosticCategoryRegistry() {
		if spec.Name == name {
			return spec, true
		}
	}
	return CategorySpec{}, false
}

// IsKnownDiagnosticCategory reports whether category exists in the canonical
// category registry.
func IsKnownDiagnosticCategory(category string) bool {
	_, ok := diagnosticCategorySpec(category)
	return ok
}

// IsBaselineSuppressibleCategory reports whether category is included in
// baseline generation/suppression.
func IsBaselineSuppressibleCategory(category string) bool {
	spec, ok := diagnosticCategorySpec(category)
	return ok && spec.BaselinePolicy == BaselineSuppressible
}

// IsProtocolInconclusiveCategory reports whether category represents proof
// uncertainty that must remain visible outside every suppression surface.
func IsProtocolInconclusiveCategory(category string) bool {
	switch category {
	case CategoryUnvalidatedCastInconclusive,
		CategoryMissingConstructorValidateInc,
		CategoryUseBeforeValidateInconclusive:
		return true
	default:
		return false
	}
}

// BaselinedCategoryNames returns all categories that participate in baseline
// generation/suppression.
func BaselinedCategoryNames() []string {
	specs := suppressibleCategorySpecs()
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Name)
	}
	return out
}

// ProtocolSemanticCategoryNames returns the categories covered by
// path-sensitive protocol contract synchronization tests.
func ProtocolSemanticCategoryNames() []string {
	registry := diagnosticCategoryRegistry()
	out := make([]string, 0, len(registry))
	for _, spec := range registry {
		if spec.SemanticKind == semanticKindProtocol {
			out = append(out, spec.Name)
		}
	}
	return out
}

func suppressibleCategorySpecs() []CategorySpec {
	registry := diagnosticCategoryRegistry()
	out := make([]CategorySpec, 0, len(registry))
	for _, spec := range registry {
		if spec.BaselinePolicy == BaselineSuppressible {
			out = append(out, spec)
		}
	}
	return out
}
