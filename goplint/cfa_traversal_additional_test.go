// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestAdaptiveBlockVisitBudgetScalesByCFGSize(t *testing.T) {
	t.Parallel()

	_, cfg := parseFuncBody(t, `
package p

func f(v string) {
	x := v
	if len(v) > 0 {
		x = v + "a"
	}
	if len(v) > 1 {
		x = v + "b"
	}
	if len(v) > 2 {
		x = v + "c"
	}
	if len(v) > 3 {
		x = v + "d"
	}
	_ = x
}
`)
	requested := blockVisitBudget{maxStates: 32}
	effective := adaptiveBlockVisitBudget(cfg, requested)
	if effective.maxStates <= requested.maxStates {
		t.Fatalf("expected adaptive maxStates > %d, got %d", requested.maxStates, effective.maxStates)
	}
}
