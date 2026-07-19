// SPDX-License-Identifier: MPL-2.0

package goplint

type protocolEffectSet uint16

const (
	protocolPossibleEffectValidate protocolEffectSet = 1 << iota
	protocolPossibleEffectMutate
	protocolPossibleEffectReplace
	protocolPossibleEffectEscape
	protocolPossibleEffectConsume
	protocolPossibleEffectTerminate
	protocolPossibleEffectConstraint
)

const protocolUnknownCallEffects = protocolPossibleEffectValidate |
	protocolPossibleEffectMutate |
	protocolPossibleEffectReplace |
	protocolPossibleEffectEscape |
	protocolPossibleEffectConsume |
	protocolPossibleEffectTerminate |
	protocolPossibleEffectConstraint

type protocolIdentityRelevance uint8

const (
	protocolIdentityUnrelated protocolIdentityRelevance = iota
	protocolIdentityMustAlias
	protocolIdentityMayAlias
	protocolIdentityUnknown
)

type protocolRelevanceQuery struct {
	ForwardReachable bool
	SinkReachable    bool
	Identity         protocolIdentityRelevance
	PossibleEffects  protocolEffectSet
}

func (query protocolRelevanceQuery) relevant(state protocolAbstractState) bool {
	if !query.ForwardReachable || !query.SinkReachable ||
		query.Identity == protocolIdentityUnrelated || query.PossibleEffects == 0 {
		return false
	}

	if !state.validationProven() {
		// A realized unvalidated/consumed/escaped sink is definite only when
		// the obligation's identity is definite. A may-alias or unknown identity
		// can change which object reaches the sink, so it remains blocking.
		// Effect uncertainty on a must-alias object is carried but cannot weaken
		// the definite unvalidated contribution.
		return query.Identity >= protocolIdentityMayAlias
	}
	relevantEffects := protocolPossibleEffectMutate |
		protocolPossibleEffectReplace |
		protocolPossibleEffectEscape |
		protocolPossibleEffectConsume |
		protocolPossibleEffectConstraint
	return query.PossibleEffects&relevantEffects != 0
}

func protocolEffectsForUncertainty(reason pathOutcomeReason) protocolEffectSet {
	switch reason {
	case pathOutcomeReasonConcurrentMutation:
		return protocolPossibleEffectMutate | protocolPossibleEffectReplace
	case pathOutcomeReasonEscapedHeapMutation:
		return protocolPossibleEffectMutate | protocolPossibleEffectReplace | protocolPossibleEffectEscape
	case pathOutcomeReasonAmbiguousIdentity:
		return protocolPossibleEffectMutate |
			protocolPossibleEffectReplace |
			protocolPossibleEffectEscape |
			protocolPossibleEffectConsume |
			protocolPossibleEffectConstraint
	case pathOutcomeReasonUnresolvedTarget, pathOutcomeReasonCallMapping, pathOutcomeReasonReflection, pathOutcomeReasonUnsafe:
		return protocolUnknownCallEffects
	default:
		return 0
	}
}

func protocolIdentityForUncertainty(reason pathOutcomeReason) protocolIdentityRelevance {
	if reason == pathOutcomeReasonAmbiguousIdentity {
		return protocolIdentityMayAlias
	}
	if protocolEffectsForUncertainty(reason) != 0 {
		return protocolIdentityMustAlias
	}
	return protocolIdentityUnrelated
}

func protocolIdentityAtUnresolvedSink(
	reason pathOutcomeReason,
	identity protocolIdentityRelevance,
) protocolIdentityRelevance {
	switch reason {
	case pathOutcomeReasonUnresolvedTarget, pathOutcomeReasonCallMapping, pathOutcomeReasonReflection, pathOutcomeReasonUnsafe:
		return joinProtocolIdentityRelevance(identity, protocolIdentityUnknown)
	default:
		return identity
	}
}

func joinProtocolIdentityRelevance(
	left protocolIdentityRelevance,
	right protocolIdentityRelevance,
) protocolIdentityRelevance {
	if left >= right {
		return left
	}
	return right
}
