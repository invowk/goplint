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
