// SPDX-License-Identifier: MPL-2.0

package goplint

type ideValidationState string

const (
	ideStateNeedsValidate          ideValidationState = "needs-validate"
	ideStateValidated              ideValidationState = "validated"
	ideStateEscapedBeforeValidate  ideValidationState = "escaped-before-validate"
	ideStateConsumedBeforeValidate ideValidationState = "consumed-before-validate"
)

type ideEdgeFuncTag string

const (
	ideEdgeFuncIdentity ideEdgeFuncTag = "identity"
	ideEdgeFuncValidate ideEdgeFuncTag = "needs-to-validated"
	ideEdgeFuncEscape   ideEdgeFuncTag = "needs-to-escaped-before-validate"
	ideEdgeFuncConsume  ideEdgeFuncTag = "needs-to-consumed-before-validate"
)

type ideEdgeFunc struct {
	tag ideEdgeFuncTag
}

func newIDEEdgeFunc(tag ideEdgeFuncTag) ideEdgeFunc {
	switch tag {
	case ideEdgeFuncValidate, ideEdgeFuncEscape, ideEdgeFuncConsume:
		return ideEdgeFunc{tag: tag}
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

func (f ideEdgeFunc) Apply(state ideValidationState) ideValidationState {
	switch f.Tag() {
	case ideEdgeFuncValidate:
		if state == ideStateNeedsValidate {
			return ideStateValidated
		}
		return state
	case ideEdgeFuncEscape:
		if state == ideStateNeedsValidate {
			return ideStateEscapedBeforeValidate
		}
		return state
	case ideEdgeFuncConsume:
		if state == ideStateNeedsValidate {
			return ideStateConsumedBeforeValidate
		}
		return state
	default:
		return state
	}
}

func composeIDEEdgeFuncs(first, second ideEdgeFunc) ideEdgeFunc {
	// Compose over the canonical IFDS flow state.
	applied := second.Apply(first.Apply(ideStateNeedsValidate))
	return edgeFuncForState(applied)
}

func joinIDEEdgeFuncs(left, right ideEdgeFunc) ideEdgeFunc {
	joined := joinIDEStates(left.Apply(ideStateNeedsValidate), right.Apply(ideStateNeedsValidate))
	return edgeFuncForState(joined)
}

func edgeFuncForState(state ideValidationState) ideEdgeFunc {
	switch state {
	case ideStateValidated:
		return newIDEEdgeFunc(ideEdgeFuncValidate)
	case ideStateEscapedBeforeValidate:
		return newIDEEdgeFunc(ideEdgeFuncEscape)
	case ideStateConsumedBeforeValidate:
		return newIDEEdgeFunc(ideEdgeFuncConsume)
	default:
		return newIDEEdgeFunc(ideEdgeFuncIdentity)
	}
}

func joinIDEStates(left, right ideValidationState) ideValidationState {
	if ideStateSeverity(left) <= ideStateSeverity(right) {
		return left
	}
	return right
}

func ideStateSeverity(state ideValidationState) int {
	switch state {
	case ideStateConsumedBeforeValidate:
		return 0
	case ideStateEscapedBeforeValidate:
		return 1
	case ideStateNeedsValidate:
		return 2
	case ideStateValidated:
		return 3
	default:
		return 2
	}
}
