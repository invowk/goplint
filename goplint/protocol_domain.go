// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
)

const (
	protocolValidationProven   protocolValidationState = 0
	protocolValidationRequired protocolValidationState = 1

	protocolHazardEscaped  protocolHazardSet = 1 << 0
	protocolHazardConsumed protocolHazardSet = 1 << 1

	protocolUncertaintyUnresolvedCall         protocolUncertaintySet = 1 << 1
	protocolUncertaintyAmbiguousIdentity      protocolUncertaintySet = 1 << 2
	protocolUncertaintyMissingSSA             protocolUncertaintySet = 1 << 5
	protocolUncertaintyStateBudget            protocolUncertaintySet = 1 << 6
	protocolUncertaintyWitnessBudget          protocolUncertaintySet = 1 << 7
	protocolUncertaintyTimeout                protocolUncertaintySet = 1 << 10
	protocolUncertaintyUnsupportedInstruction protocolUncertaintySet = 1 << 12
	protocolUncertaintyReflection             protocolUncertaintySet = 1 << 13
	protocolUncertaintyUnsafe                 protocolUncertaintySet = 1 << 14
	protocolUncertaintyConcurrentMutation     protocolUncertaintySet = 1 << 15
	protocolUncertaintyEscapedHeapMutation    protocolUncertaintySet = 1 << 16
	protocolUncertaintyRecursionCycle         protocolUncertaintySet = 1 << 17
	protocolUncertaintyFeasibilityUnknown     protocolUncertaintySet = 1 << 18
	protocolUncertaintyCallMapping            protocolUncertaintySet = 1 << 19

	protocolErrorResultUnknown protocolErrorResult = 0
	protocolErrorResultNil     protocolErrorResult = 1
	protocolErrorResultNonNil  protocolErrorResult = 2

	protocolDeferredErrorUnknown    protocolDeferredErrorRelation = 0
	protocolDeferredErrorNil        protocolDeferredErrorRelation = 1
	protocolDeferredErrorValidation protocolDeferredErrorRelation = 2
	protocolDeferredErrorOther      protocolDeferredErrorRelation = 3

	protocolEffectValidate protocolEffectKind = 0
	protocolEffectEscape   protocolEffectKind = 1
	protocolEffectConsume  protocolEffectKind = 2

	protocolPathSatisfied            protocolPathStatus = 0
	protocolPathViolation            protocolPathStatus = 1
	protocolPathUnresolved           protocolPathStatus = 2
	protocolPathDischargedInfeasible protocolPathStatus = 3

	protocolEvidenceViolation            protocolEvidenceKind = "violation"
	protocolEvidenceInconclusive         protocolEvidenceKind = "inconclusive"
	protocolEvidenceDischargedInfeasible protocolEvidenceKind = "discharged-infeasible"
)

type (
	protocolIdentity              uint32
	protocolValidationState       uint8
	protocolHazardSet             uint8
	protocolUncertaintySet        uint32
	protocolErrorResult           uint8
	protocolDeferredErrorRelation uint8
	protocolEffectKind            uint8
	protocolPathStatus            uint8
	protocolEvidenceKind          string
	protocolAbstractState         struct {
		Validation      protocolValidationState
		Hazards         protocolHazardSet
		Uncertainty     protocolUncertaintySet
		Result          protocolErrorResult
		DeferredError   protocolDeferredErrorRelation
		Identity        protocolIdentityRelevance
		PossibleEffects protocolEffectSet
	}

	protocolValidationRelation struct {
		ReceiverIdentity protocolIdentity
		ErrorIdentity    protocolIdentity
	}

	protocolConditionalEffect struct {
		Kind     protocolEffectKind
		Relation protocolValidationRelation
	}

	protocolDecision struct {
		Kind                protocolEvidenceKind
		EmitDiagnostic      bool
		DischargedWitnesses int
	}
)

func newProtocolRequiredState() protocolAbstractState {
	return protocolAbstractState{
		Validation: protocolValidationRequired,
		Result:     protocolErrorResultUnknown,
	}
}

func (s protocolAbstractState) lessEqual(other protocolAbstractState) bool {
	return s.Validation <= other.Validation &&
		s.Hazards&other.Hazards == s.Hazards &&
		s.Uncertainty&other.Uncertainty == s.Uncertainty &&
		protocolErrorResultLessEqual(s.Result, other.Result) &&
		protocolDeferredErrorLessEqual(s.DeferredError, other.DeferredError) &&
		s.Identity <= other.Identity &&
		s.PossibleEffects&other.PossibleEffects == s.PossibleEffects
}

func (s protocolAbstractState) join(other protocolAbstractState) protocolAbstractState {
	return protocolAbstractState{
		Validation:      max(s.Validation, other.Validation),
		Hazards:         s.Hazards | other.Hazards,
		Uncertainty:     s.Uncertainty | other.Uncertainty,
		Result:          joinProtocolErrorResults(s.Result, other.Result),
		DeferredError:   joinProtocolDeferredErrors(s.DeferredError, other.DeferredError),
		Identity:        joinProtocolIdentityRelevance(s.Identity, other.Identity),
		PossibleEffects: s.PossibleEffects | other.PossibleEffects,
	}
}

func (s protocolAbstractState) apply(effect protocolConditionalEffect, result protocolErrorResult, unknownReason protocolUncertaintySet) protocolAbstractState {
	switch effect.Kind {
	case protocolEffectValidate:
		switch result {
		case protocolErrorResultNil:
			s.Validation = protocolValidationProven
			s.Result = protocolErrorResultNil
		case protocolErrorResultNonNil:
			// A failing validation preserves the obligation.
			s.Result = protocolErrorResultNonNil
		case protocolErrorResultUnknown:
			s.Result = protocolErrorResultUnknown
			s.Uncertainty |= unknownReason
		}
	case protocolEffectEscape:
		if s.Validation == protocolValidationRequired {
			s.Hazards |= protocolHazardEscaped
		}
	case protocolEffectConsume:
		if s.Validation == protocolValidationRequired {
			s.Hazards |= protocolHazardConsumed
		}
	}
	return s
}

func (s protocolAbstractState) withUncertainty(reason pathOutcomeReason) protocolAbstractState {
	if reason == pathOutcomeReasonNone {
		return s
	}
	s.Uncertainty |= protocolUncertaintyForPathReason(reason)
	s.PossibleEffects |= protocolEffectsForUncertainty(reason)
	s.Identity = joinProtocolIdentityRelevance(s.Identity, protocolIdentityForUncertainty(reason))
	return s
}

func (s protocolAbstractState) key() string {
	return fmt.Sprintf(
		"validation=%d,hazards=%d,uncertainty=%d,result=%d,deferred-error=%d,identity=%d,effects=%d",
		s.Validation,
		s.Hazards,
		s.Uncertainty,
		s.Result,
		s.DeferredError,
		s.Identity,
		s.PossibleEffects,
	)
}

func (s protocolAbstractState) valid() bool {
	if s.Validation > protocolValidationRequired ||
		s.Hazards & ^(protocolHazardEscaped|protocolHazardConsumed) != 0 ||
		s.Result > protocolErrorResultNonNil ||
		s.DeferredError > protocolDeferredErrorOther ||
		s.Identity > protocolIdentityUnknown {
		return false
	}
	const allUncertainty = protocolUncertaintyUnresolvedCall |
		protocolUncertaintyAmbiguousIdentity |
		protocolUncertaintyMissingSSA |
		protocolUncertaintyStateBudget |
		protocolUncertaintyWitnessBudget |
		protocolUncertaintyTimeout |
		protocolUncertaintyUnsupportedInstruction |
		protocolUncertaintyReflection |
		protocolUncertaintyUnsafe |
		protocolUncertaintyConcurrentMutation |
		protocolUncertaintyEscapedHeapMutation |
		protocolUncertaintyRecursionCycle |
		protocolUncertaintyFeasibilityUnknown |
		protocolUncertaintyCallMapping
	return s.Uncertainty & ^allUncertainty == 0 &&
		s.PossibleEffects & ^protocolUnknownCallEffects == 0
}

func (s protocolAbstractState) validationProven() bool {
	return s.Validation == protocolValidationProven
}

func (s protocolAbstractState) validationRequired() bool {
	return s.Validation == protocolValidationRequired
}

func (s protocolAbstractState) escapedBeforeValidation() bool {
	return s.Hazards&protocolHazardEscaped != 0
}

func (s protocolAbstractState) consumedBeforeValidation() bool {
	return s.Hazards&protocolHazardConsumed != 0
}

func (s protocolAbstractState) pathOutcomeReason() pathOutcomeReason {
	for _, entry := range protocolPathReasonOrder() {
		if s.Uncertainty&entry.bit != 0 {
			return entry.reason
		}
	}
	return pathOutcomeReasonNone
}

func protocolErrorResultLessEqual(left, right protocolErrorResult) bool {
	return left == right || right == protocolErrorResultUnknown
}

func joinProtocolErrorResults(left, right protocolErrorResult) protocolErrorResult {
	if left == right {
		return left
	}
	return protocolErrorResultUnknown
}

func protocolDeferredErrorLessEqual(left, right protocolDeferredErrorRelation) bool {
	return left == right || right == protocolDeferredErrorUnknown
}

func joinProtocolDeferredErrors(
	left,
	right protocolDeferredErrorRelation,
) protocolDeferredErrorRelation {
	if left == right {
		return left
	}
	return protocolDeferredErrorUnknown
}

func protocolUncertaintyForPathReason(reason pathOutcomeReason) protocolUncertaintySet {
	for _, entry := range protocolPathReasonOrder() {
		if entry.reason == reason {
			return entry.bit
		}
	}
	return protocolUncertaintyFeasibilityUnknown
}

func protocolPathReasonOrder() []struct {
	bit    protocolUncertaintySet
	reason pathOutcomeReason
} {
	return []struct {
		bit    protocolUncertaintySet
		reason pathOutcomeReason
	}{
		{protocolUncertaintyTimeout, pathOutcomeReasonTimeout},
		{protocolUncertaintyStateBudget, pathOutcomeReasonStateBudget},
		{protocolUncertaintyWitnessBudget, pathOutcomeReasonWitnessBudget},
		{protocolUncertaintyRecursionCycle, pathOutcomeReasonRecursionCycle},
		{protocolUncertaintyFeasibilityUnknown, pathOutcomeReasonFeasibilityUnknown},
		{protocolUncertaintyMissingSSA, pathOutcomeReasonMissingSSA},
		{protocolUncertaintyUnsupportedInstruction, pathOutcomeReasonUnsupportedInstr},
		{protocolUncertaintyConcurrentMutation, pathOutcomeReasonConcurrentMutation},
		{protocolUncertaintyEscapedHeapMutation, pathOutcomeReasonEscapedHeapMutation},
		{protocolUncertaintyAmbiguousIdentity, pathOutcomeReasonAmbiguousIdentity},
		{protocolUncertaintyReflection, pathOutcomeReasonReflection},
		{protocolUncertaintyUnsafe, pathOutcomeReasonUnsafe},
		{protocolUncertaintyUnresolvedCall, pathOutcomeReasonUnresolvedTarget},
		{protocolUncertaintyCallMapping, pathOutcomeReasonCallMapping},
	}
}

func (s protocolAbstractState) pathStatus() protocolPathStatus {
	if s.Hazards != 0 {
		return protocolPathViolation
	}
	if s.Uncertainty != 0 {
		return protocolPathUnresolved
	}
	return protocolPathSatisfied
}

func aggregateProtocolPaths(statuses []protocolPathStatus) protocolDecision {
	decision := protocolDecision{}
	hasViolation := false
	hasUnresolved := false
	for _, status := range statuses {
		switch status {
		case protocolPathViolation:
			hasViolation = true
		case protocolPathUnresolved:
			hasUnresolved = true
		case protocolPathDischargedInfeasible:
			decision.DischargedWitnesses++
		case protocolPathSatisfied:
		}
	}
	if hasViolation {
		decision.Kind = protocolEvidenceViolation
		decision.EmitDiagnostic = true
		return decision
	}
	if hasUnresolved {
		decision.Kind = protocolEvidenceInconclusive
		decision.EmitDiagnostic = true
	}
	return decision
}
