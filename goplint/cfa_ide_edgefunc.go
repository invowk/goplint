// SPDX-License-Identifier: MPL-2.0

package goplint

const (
	ideEdgeFuncIdentity                ideEdgeFuncTag = "identity"
	ideEdgeFuncValidate                ideEdgeFuncTag = "needs-to-validated"
	ideEdgeFuncValidationFailed        ideEdgeFuncTag = "needs-to-validation-failed"
	ideEdgeFuncInvalidate              ideEdgeFuncTag = "validated-to-needs"
	ideEdgeFuncEscape                  ideEdgeFuncTag = "needs-to-escaped-before-validate"
	ideEdgeFuncConsume                 ideEdgeFuncTag = "needs-to-consumed-before-validate"
	ideEdgeFuncDeferredErrorNil        ideEdgeFuncTag = "deferred-error-nil"
	ideEdgeFuncDeferredErrorValidation ideEdgeFuncTag = "deferred-error-validation"
	ideEdgeFuncDeferredErrorOther      ideEdgeFuncTag = "deferred-error-other"
	ideEdgeFuncDeferredErrorUnknown    ideEdgeFuncTag = "deferred-error-unknown"
)

var (
	ideStateNeedsValidate = newProtocolRequiredState()
	ideStateValidated     = protocolAbstractState{
		Validation: protocolValidationProven,
		Result:     protocolErrorResultNil,
	}
	ideStateValidationFailed = protocolAbstractState{
		Validation: protocolValidationRequired,
		Result:     protocolErrorResultNonNil,
	}
	ideStateEscapedBeforeValidate = protocolAbstractState{
		Validation: protocolValidationRequired,
		Hazards:    protocolHazardEscaped,
	}
	ideStateConsumedBeforeValidate = protocolAbstractState{
		Validation: protocolValidationRequired,
		Hazards:    protocolHazardConsumed,
	}
)

type ideEdgeFuncTag string

type ideEdgeFunc struct {
	tag ideEdgeFuncTag
}

func newIDEEdgeFunc(tag ideEdgeFuncTag) ideEdgeFunc {
	switch tag {
	case ideEdgeFuncValidate, ideEdgeFuncValidationFailed, ideEdgeFuncInvalidate, ideEdgeFuncEscape, ideEdgeFuncConsume,
		ideEdgeFuncDeferredErrorNil, ideEdgeFuncDeferredErrorValidation, ideEdgeFuncDeferredErrorOther,
		ideEdgeFuncDeferredErrorUnknown:
		return ideEdgeFunc{tag: tag}
	case ideEdgeFuncIdentity:
		return ideEdgeFunc{tag: ideEdgeFuncIdentity}
	default:
		return ideEdgeFunc{tag: ideEdgeFuncIdentity}
	}
}

func (f ideEdgeFunc) Tag() ideEdgeFuncTag {
	if f.tag == "" {
		return ideEdgeFuncIdentity
	}
	return f.tag
}

func (f ideEdgeFunc) Apply(state protocolAbstractState) protocolAbstractState {
	switch f.Tag() {
	case ideEdgeFuncValidate:
		return state.apply(
			protocolConditionalEffect{Kind: protocolEffectValidate},
			protocolErrorResultNil,
			0,
		)
	case ideEdgeFuncValidationFailed:
		return state.apply(
			protocolConditionalEffect{Kind: protocolEffectValidate},
			protocolErrorResultNonNil,
			0,
		)
	case ideEdgeFuncInvalidate:
		state.Validation = protocolValidationRequired
		state.Result = protocolErrorResultUnknown
		if state.DeferredError == protocolDeferredErrorValidation {
			state.DeferredError = protocolDeferredErrorOther
		}
		return state
	case ideEdgeFuncEscape:
		return state.apply(protocolConditionalEffect{Kind: protocolEffectEscape}, protocolErrorResultUnknown, 0)
	case ideEdgeFuncConsume:
		return state.apply(protocolConditionalEffect{Kind: protocolEffectConsume}, protocolErrorResultUnknown, 0)
	case ideEdgeFuncDeferredErrorNil:
		state.DeferredError = protocolDeferredErrorNil
		return state
	case ideEdgeFuncDeferredErrorValidation:
		state.DeferredError = protocolDeferredErrorValidation
		return state
	case ideEdgeFuncDeferredErrorOther:
		state.DeferredError = protocolDeferredErrorOther
		return state
	case ideEdgeFuncDeferredErrorUnknown:
		state.DeferredError = protocolDeferredErrorUnknown
		state.Identity = protocolIdentityUnknown
		return state
	case ideEdgeFuncIdentity:
		return state
	default:
		return state
	}
}

func composeIDEEdgeFuncs(first, second ideEdgeFunc) ideEdgeFunc {
	applied := second.Apply(first.Apply(newProtocolRequiredState()))
	return edgeFuncForState(applied)
}

func joinIDEEdgeFuncs(left, right ideEdgeFunc) ideEdgeFunc {
	joined := left.Apply(newProtocolRequiredState()).join(right.Apply(newProtocolRequiredState()))
	return edgeFuncForState(joined)
}

func edgeFuncForState(state protocolAbstractState) ideEdgeFunc {
	switch {
	case state.consumedBeforeValidation():
		return newIDEEdgeFunc(ideEdgeFuncConsume)
	case state.escapedBeforeValidation():
		return newIDEEdgeFunc(ideEdgeFuncEscape)
	case state.validationProven():
		return newIDEEdgeFunc(ideEdgeFuncValidate)
	case state.Result == protocolErrorResultNonNil:
		return newIDEEdgeFunc(ideEdgeFuncValidationFailed)
	default:
		return newIDEEdgeFunc(ideEdgeFuncIdentity)
	}
}

func joinIDEStates(left, right protocolAbstractState) protocolAbstractState {
	return left.join(right)
}
