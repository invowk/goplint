// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestIDEEdgeFuncApply(t *testing.T) {
	t.Parallel()

	if got := newIDEEdgeFunc(ideEdgeFuncValidate).Apply(ideStateNeedsValidate); got != ideStateValidated {
		t.Fatalf("validate edge func produced %q, want %q", got, ideStateValidated)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncEscape).Apply(ideStateNeedsValidate); got != ideStateEscapedBeforeValidate {
		t.Fatalf("escape edge func produced %q, want %q", got, ideStateEscapedBeforeValidate)
	}
	if got := newIDEEdgeFunc(ideEdgeFuncConsume).Apply(ideStateNeedsValidate); got != ideStateConsumedBeforeValidate {
		t.Fatalf("consume edge func produced %q, want %q", got, ideStateConsumedBeforeValidate)
	}
}

func TestComposeIDEEdgeFuncs(t *testing.T) {
	t.Parallel()

	composed := composeIDEEdgeFuncs(newIDEEdgeFunc(ideEdgeFuncValidate), newIDEEdgeFunc(ideEdgeFuncConsume))
	if got := composed.Apply(ideStateNeedsValidate); got != ideStateValidated {
		t.Fatalf("composed validate->consume produced %q, want %q", got, ideStateValidated)
	}
}

func TestJoinIDEStatesIsConservative(t *testing.T) {
	t.Parallel()

	if got := joinIDEStates(ideStateValidated, ideStateEscapedBeforeValidate); got != ideStateEscapedBeforeValidate {
		t.Fatalf("join(validated, escaped) = %q, want %q", got, ideStateEscapedBeforeValidate)
	}
	if got := joinIDEEdgeFuncs(newIDEEdgeFunc(ideEdgeFuncValidate), newIDEEdgeFunc(ideEdgeFuncConsume)); got.Tag() != ideEdgeFuncConsume {
		t.Fatalf("join(validate, consume) = %q, want %q", got.Tag(), ideEdgeFuncConsume)
	}
}
