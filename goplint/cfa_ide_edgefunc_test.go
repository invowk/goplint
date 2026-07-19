// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

// ideValidationState keeps legacy solver fixtures source-compatible while the
// production solver carries only protocolAbstractState.
type ideValidationState = protocolAbstractState

func TestIDEEdgeFuncApply(t *testing.T) {
	t.Parallel()

	if got := newIDEEdgeFunc(ideEdgeFuncValidate).Apply(ideStateNeedsValidate); got != ideStateValidated {
		t.Fatalf("validate edge func produced %v, want %v", got, ideStateValidated)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncValidationFailed).Apply(ideStateNeedsValidate); got != ideStateValidationFailed {
		t.Fatalf("non-nil validation edge func produced %v, want %v", got, ideStateValidationFailed)
	}
	wantFailedConsume := ideStateValidationFailed
	wantFailedConsume.Hazards |= protocolHazardConsumed
	if got := newIDEEdgeFunc(ideEdgeFuncConsume).Apply(ideStateValidationFailed); got != wantFailedConsume {
		t.Fatalf("consume after failed validation produced %v, want %v", got, wantFailedConsume)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncEscape).Apply(ideStateNeedsValidate); got != ideStateEscapedBeforeValidate {
		t.Fatalf("escape edge func produced %v, want %v", got, ideStateEscapedBeforeValidate)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncConsume).Apply(ideStateNeedsValidate); got != ideStateConsumedBeforeValidate {
		t.Fatalf("consume edge func produced %v, want %v", got, ideStateConsumedBeforeValidate)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncInvalidate).Apply(ideStateValidated); got != ideStateNeedsValidate {
		t.Fatalf("invalidate edge func produced %v, want %v", got, ideStateNeedsValidate)
	}
}

func TestComposeIDEEdgeFuncs(t *testing.T) {
	t.Parallel()

	composed := composeIDEEdgeFuncs(newIDEEdgeFunc(ideEdgeFuncValidate), newIDEEdgeFunc(ideEdgeFuncConsume))
	if got := composed.Apply(ideStateNeedsValidate); got != ideStateValidated {
		t.Fatalf("composed validate->consume produced %v, want %v", got, ideStateValidated)
	}
}

func TestJoinIDEStatesIsConservative(t *testing.T) {
	t.Parallel()

	if got := joinIDEStates(ideStateValidated, ideStateEscapedBeforeValidate); got != ideStateEscapedBeforeValidate {
		t.Fatalf("join(validated, escaped) = %v, want %v", got, ideStateEscapedBeforeValidate)
	}
	if got := joinIDEEdgeFuncs(newIDEEdgeFunc(ideEdgeFuncValidate), newIDEEdgeFunc(ideEdgeFuncConsume)); got.Tag() != ideEdgeFuncConsume {
		t.Fatalf("join(validate, consume) = %q, want %q", got.Tag(), ideEdgeFuncConsume)
	}
}
