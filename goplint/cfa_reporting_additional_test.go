// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestReportInconclusiveFindingIsBlocking(t *testing.T) {
	t.Parallel()

	const (
		category = CategoryUnvalidatedCastInconclusive
		finding  = "gpl3_test"
		message  = "inconclusive"
	)

	var diags []analysis.Diagnostic
	pass := &analysis.Pass{Report: func(diag analysis.Diagnostic) { diags = append(diags, diag) }}
	reportInconclusiveFindingWithMetaIfNotBaselined(
		pass, nil, token.Pos(1), category, finding, message,
		map[string]string{"cfg_inconclusive_reason": "state-budget"},
	)
	if len(diags) != 1 {
		t.Fatalf("expected one blocking inconclusive diagnostic, got %d", len(diags))
	}
	if got := FindingMetaFromDiagnosticURL(diags[0].URL, "cfg_outcome_status"); got != cfgRefinementStatusInconclusive {
		t.Fatalf("cfg_outcome_status meta = %q, want %q", got, cfgRefinementStatusInconclusive)
	}
}

func TestReportInconclusiveFindingBypassesMatchingBaseline(t *testing.T) {
	t.Parallel()

	const findingID = "gpl3_exact_baseline_match"
	categories := []string{
		CategoryUnvalidatedCastInconclusive,
		CategoryMissingConstructorValidateInc,
		CategoryUseBeforeValidateInconclusive,
		CategoryUnvalidatedBoundaryRequest,
	}
	for _, category := range categories {
		t.Run(category, func(t *testing.T) {
			t.Parallel()

			baseline := &BaselineConfig{lookupByID: map[string]map[string]bool{
				category: {findingID: true},
			}}
			var diagnostics []analysis.Diagnostic
			pass := &analysis.Pass{Report: func(diagnostic analysis.Diagnostic) {
				diagnostics = append(diagnostics, diagnostic)
			}}
			reportInconclusiveFindingWithMetaIfNotBaselined(
				pass,
				baseline,
				token.Pos(1),
				category,
				findingID,
				"inconclusive",
				nil,
			)
			if len(diagnostics) != 1 {
				t.Fatalf("matching baseline suppressed %q diagnostic", category)
			}
		})
	}
}

func TestCFGOutcomeMetaWithWitness(t *testing.T) {
	t.Parallel()

	meta := cfgOutcomeMetaWithWitness(
		5,
		pathOutcomeReasonStateBudget,
		[]int32{0, 1, 2, 3},
		2,
	)
	if got := meta["cfg_witness_kind"]; got != "cfg-path" {
		t.Fatalf("cfg_witness_kind = %q, want %q", got, "cfg-path")
	}
	if got := meta["cfg_witness_blocks"]; got != "0,1" {
		t.Fatalf("cfg_witness_blocks = %q, want %q", got, "0,1")
	}
	if got := meta["cfg_witness_edges"]; got != "0->1" {
		t.Fatalf("cfg_witness_edges = %q, want %q", got, "0->1")
	}
	if got := meta["cfg_witness_truncation_cause"]; got != "max-steps" {
		t.Fatalf("cfg_witness_truncation_cause = %q, want %q", got, "max-steps")
	}
}

func TestAddCFGWitnessCallChainMeta(t *testing.T) {
	t.Parallel()

	meta := map[string]string{"cfg_outcome_status": cfgRefinementStatusInconclusive}
	addCFGWitnessCallChainMeta(
		meta,
		[]string{"pkg.Func", "pkg.helper", "pkg.deep"},
		2,
	)
	if got := meta["cfg_witness_call_chain"]; got != "pkg.Func -> pkg.helper" {
		t.Fatalf("cfg_witness_call_chain = %q, want %q", got, "pkg.Func -> pkg.helper")
	}
	if got := meta["cfg_witness_truncation_cause"]; got != "max-steps" {
		t.Fatalf("cfg_witness_truncation_cause = %q, want %q", got, "max-steps")
	}
}
