// SPDX-License-Identifier: MPL-2.0

package goplint

// protocolEvidenceTestInjectionControl is an internal test seam for proving
// that corrupt protocol evidence enters before production checking and
// aggregation. Production controls never implement it, so normal analyzer
// execution preserves every value unchanged.
type protocolEvidenceTestInjectionControl interface {
	injectProtocolWitnessEvidence(cfgSSAConstraintEvidence) cfgSSAConstraintEvidence
	injectProtocolRefinementEvidence(cfgFeasibilityDecision) cfgFeasibilityDecision
	injectProtocolSummaryEvidence(interprocProcedureSummary) interprocProcedureSummary
	injectProtocolReasonEvidence(pathOutcomeReason) pathOutcomeReason
}

func injectProtocolWitnessEvidence(
	control protocolAnalysisControl,
	evidence cfgSSAConstraintEvidence,
) cfgSSAConstraintEvidence {
	injector, ok := control.(protocolEvidenceTestInjectionControl)
	if !ok {
		return evidence
	}
	return injector.injectProtocolWitnessEvidence(evidence)
}

func injectProtocolRefinementEvidence(
	control protocolAnalysisControl,
	decision cfgFeasibilityDecision,
) cfgFeasibilityDecision {
	injector, ok := control.(protocolEvidenceTestInjectionControl)
	if !ok {
		return decision
	}
	return injector.injectProtocolRefinementEvidence(decision)
}

func injectProtocolSummaryEvidence(
	control protocolAnalysisControl,
	summary interprocProcedureSummary,
) interprocProcedureSummary {
	injector, ok := control.(protocolEvidenceTestInjectionControl)
	if !ok {
		return summary
	}
	return injector.injectProtocolSummaryEvidence(summary)
}

func injectProtocolReasonEvidence(
	control protocolAnalysisControl,
	reason pathOutcomeReason,
) pathOutcomeReason {
	injector, ok := control.(protocolEvidenceTestInjectionControl)
	if !ok {
		return reason
	}
	return injector.injectProtocolReasonEvidence(reason)
}
