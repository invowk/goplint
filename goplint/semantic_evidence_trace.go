// SPDX-License-Identifier: MPL-2.0

package goplint

const (
	semanticEvidenceStageSourceExtraction = "source-extraction"
	semanticEvidenceStageReporting        = "reporting"

	semanticFeatureCastValidation        = "cast-validation"
	semanticFeatureConstructorValidation = "constructor-validation"
	semanticFeatureUseBeforeValidation   = "use-before-validation"
	semanticFeatureBoundaryRequest       = "boundary-request-validation"
)

type semanticEvidenceTraceEvent struct {
	CaseID    string
	FeatureID string
	Owner     semanticOwnerKey
	Route     semanticProductionRoute
	Stage     string
}

type semanticEvidenceObserver func(semanticEvidenceTraceEvent)

func observeSemanticEvidenceRoute(
	rc runConfig,
	featureID string,
	owner semanticOwnerKey,
	route semanticProductionRoute,
	stage string,
) {
	if rc.semanticEvidenceObserver == nil {
		return
	}
	rc.semanticEvidenceObserver(semanticEvidenceTraceEvent{
		FeatureID: featureID,
		Owner:     owner,
		Route:     route,
		Stage:     stage,
	})
}
