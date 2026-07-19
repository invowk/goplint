// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

type corruptingCFGFeasibilityBackend struct{}

func (corruptingCFGFeasibilityBackend) Check(query cfgFeasibilityQuery) cfgFeasibilityDecision {
	decision := (cfgSSAConstraintsFeasibilityBackend{}).Check(query)
	if decision.Result == cfgFeasibilityResultUNSAT {
		decision.Evidence = corruptSSAConstraintEvidence(decision.Evidence)
	}
	return decision
}

func TestRealAnalyzerRejectsCorruptRefinementEvidence(t *testing.T) {
	t.Parallel()

	record := runRealAnalyzerRefinementReason(
		t,
		"cfa_refinement_evidence_corrupt",
		cfgFeasibilityReasonEvidenceRejected,
		0,
		0,
		corruptingCFGFeasibilityBackend{},
	)
	if record.Meta["cfg_refinement_evidence_digest"] != "" {
		t.Fatalf("corrupt evidence produced accepted digest %q", record.Meta["cfg_refinement_evidence_digest"])
	}
}

func TestRealAnalyzerReportsRefinementQueryLimit(t *testing.T) {
	t.Parallel()

	record := runRealAnalyzerRefinementReason(
		t,
		"cfa_refinement_query_limit",
		cfgFeasibilityReasonQueryLimit,
		1,
		0,
		nil,
	)
	if record.Meta["cfg_refinement_iterations"] != "0" {
		t.Fatalf("query-limit iterations = %q, want 0", record.Meta["cfg_refinement_iterations"])
	}
}

func TestRealAnalyzerReportsRefinementIterationLimit(t *testing.T) {
	t.Parallel()

	record := runRealAnalyzerRefinementReason(
		t,
		"cfa_refinement_iteration_limit",
		cfgFeasibilityReasonIterationLimit,
		4,
		1,
		nil,
	)
	if record.Meta["cfg_refinement_iterations"] != "1" {
		t.Fatalf("iteration-limit iterations = %q, want 1", record.Meta["cfg_refinement_iterations"])
	}
}

func runRealAnalyzerRefinementReason(
	t *testing.T,
	fixture string,
	wantReason string,
	queryBudget int,
	iterationBudget int,
	backend cfgFeasibilityBackend,
) FindingStreamRecord {
	t.Helper()

	h := newAnalyzerHarness()
	resetFlags(t, h)
	setFlag(t, h.Analyzer, "check-cast-validation", "true")
	if queryBudget > 0 {
		setFlag(t, h.Analyzer, "protocol-feasibility-max-queries", strconv.Itoa(queryBudget))
	}
	if iterationBudget > 0 {
		setFlag(t, h.Analyzer, "protocol-refinement-max-iterations", strconv.Itoa(iterationBudget))
	}
	h.state.protocolFeasibilityBackend = backend
	findingsPath := filepath.Join(t.TempDir(), "findings.jsonl")
	setFlag(t, h.Analyzer, "emit-findings-jsonl", findingsPath)

	runAnalysisTest(t, analysistest.TestData(), h.Analyzer, fixture)

	data, err := os.ReadFile(findingsPath)
	if err != nil {
		t.Fatalf("read findings stream: %v", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		var record FindingStreamRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode findings stream record: %v", err)
		}
		if record.Category != CategoryUnvalidatedCastInconclusive ||
			record.Meta["cfg_feasibility_reason"] != wantReason {
			continue
		}
		if record.Meta["cfg_inconclusive_reason"] != string(pathOutcomeReasonFeasibilityUnknown) ||
			record.Meta["cfg_feasibility_result"] != cfgFeasibilityResultUnknown ||
			record.Meta["cfg_refinement_status"] != cfgRefinementStatusInconclusive {
			t.Fatalf("refinement metadata = %+v, want blocking feasibility-unknown", record.Meta)
		}
		return record
	}
	t.Fatalf("findings stream has no real-analyzer refinement reason %q", wantReason)
	return FindingStreamRecord{}
}
