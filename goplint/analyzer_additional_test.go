// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestShouldReportOverdueReviewFinding(t *testing.T) {
	t.Parallel()

	if !shouldReportOverdueReviewFinding(nil, "finding-1") {
		t.Fatal("nil state should allow reporting")
	}

	state := &flagState{}
	if !shouldReportOverdueReviewFinding(state, "finding-1") {
		t.Fatal("first finding occurrence should report")
	}
	if shouldReportOverdueReviewFinding(state, "finding-1") {
		t.Fatal("duplicate finding occurrence should be suppressed")
	}
	if !shouldReportOverdueReviewFinding(state, "finding-2") {
		t.Fatal("different finding ID should report")
	}
	if state.overdueReviewSeen == nil {
		t.Fatal("overdueReviewSeen map should be initialized")
	}
}

func TestFlagState_CalleeSummaryCacheIsAnalyzerScoped(t *testing.T) {
	t.Parallel()

	stateA := &flagState{}
	stateB := &flagState{}
	resetFlagStateDefaults(stateA)
	resetFlagStateDefaults(stateB)

	stateA.calleeSummaryCache.Store("callee|arg:0", calleeSummaryEntry{ok: true})
	if _, ok := stateB.calleeSummaryCache.Load("callee|arg:0"); ok {
		t.Fatal("callee summary cache leaked between analyzer states")
	}

	resetFlagStateDefaults(stateA)
	if _, ok := stateA.calleeSummaryCache.Load("callee|arg:0"); ok {
		t.Fatal("resetFlagStateDefaults() did not clear callee summary cache")
	}
}
