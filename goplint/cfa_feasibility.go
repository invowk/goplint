// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

const (
	cfgRefinementTriggerUnsafeCandidate = "unsafe-candidate"

	cfgFeasibilityResultSAT     = "sat"
	cfgFeasibilityResultUNSAT   = "unsat"
	cfgFeasibilityResultUnknown = "unknown"

	cfgFeasibilityReasonNone                 = ""
	cfgFeasibilityReasonUnsupportedPredicate = "unsupported-predicate"
	cfgFeasibilityReasonTimeout              = "timeout"
	cfgFeasibilityReasonSolverError          = "solver-error"
	cfgFeasibilityReasonEvidenceRejected     = "evidence-rejected"
	cfgFeasibilityReasonQueryLimit           = "query-limit"
	cfgFeasibilityReasonIterationLimit       = "iteration-limit"

	cfgRefinementStatusViolation            = "violation"
	cfgRefinementStatusInconclusive         = "inconclusive"
	cfgRefinementStatusDischargedInfeasible = "discharged-infeasible"
)

// cfgWitnessRecord is the canonical explanation substrate used by protocol refinement.
// It stays additive to the existing diagnostic metadata contract.
type cfgWitnessRecord struct {
	Category        string                 `json:"category,omitempty"`
	FindingID       string                 `json:"finding_id,omitempty"`
	OriginAnchors   map[string]string      `json:"origin_anchors,omitempty"`
	FactFamily      string                 `json:"fact_family,omitempty"`
	FactKey         string                 `json:"fact_key,omitempty"`
	EdgeTag         string                 `json:"edge_function_tag,omitempty"`
	FactOriginPath  []int32                `json:"fact_origin_path,omitempty"`
	CFGPath         []int32                `json:"cfg_path,omitempty"`
	WitnessEdges    []interprocWitnessEdge `json:"witness_edges,omitempty"`
	WitnessTerminal interprocNodeID        `json:"witness_terminal,omitzero"`
	CallChain       []string               `json:"call_chain,omitempty"`
	TriggerReason   string                 `json:"trigger_reason,omitempty"`
	WitnessHash     string                 `json:"witness_hash,omitempty"`
}

// cfgProtocolRefinementResult stores feasibility/refinement provenance for one evaluated
// witness. The metadata is surfaced on emitted diagnostics and trace records.
type cfgProtocolRefinementResult struct {
	Enabled              bool
	FeasibilityEngine    string
	FeasibilityResult    string
	FeasibilityReason    string
	RefinementStatus     string
	RefinementIterations int
	RefinementTrigger    string
	WitnessHash          string
	SSASubjects          []string
	Evidence             cfgSSAConstraintEvidence
	EvidenceDigest       string
}

type cfgFeasibilityQuery struct {
	Pass     *analysis.Pass
	CFG      *gocfg.CFG
	Witness  cfgWitnessRecord
	Timeout  time.Duration
	Position token.Pos
	Deadline cfgFeasibilityDeadline
}

// protocolAnalysisControl is the request-scoped cooperative cancellation
// contract shared by tabulation and refinement. Implementations may use a wall
// deadline or a deterministic operation budget; callers checkpoint it inside
// bounded loops and immediately before accepting proof results.
type protocolAnalysisControl interface {
	Expired() bool
}

type cfgFeasibilityDeadline = protocolAnalysisControl

type wallClockFeasibilityDeadline struct {
	deadline time.Time
}

func newWallClockFeasibilityDeadline(timeout time.Duration) cfgFeasibilityDeadline {
	return wallClockFeasibilityDeadline{deadline: time.Now().Add(timeout)}
}

func (deadline wallClockFeasibilityDeadline) Expired() bool {
	return deadline.deadline.IsZero() || !time.Now().Before(deadline.deadline)
}

func feasibilityDeadlineExpired(query cfgFeasibilityQuery) bool {
	if query.Deadline != nil {
		return query.Deadline.Expired()
	}
	return query.Timeout <= 0
}

func feasibilityDeadlineReached(deadline cfgFeasibilityDeadline) bool {
	return deadline != nil && deadline.Expired()
}

type cfgFeasibilityBackend interface {
	Check(query cfgFeasibilityQuery) cfgFeasibilityDecision
}

type cfgFeasibilityDecision struct {
	Result         string
	Reason         string
	Evidence       cfgSSAConstraintEvidence
	Subjects       []string
	FormulaDigest  string
	EvidenceDigest string
}

func buildCFGWitnessRecord(
	category string,
	findingID string,
	originAnchors map[string]string,
	result interprocPathResult,
	callChain []string,
	syntheticPath []int32,
) cfgWitnessRecord {
	record := cfgWitnessRecord{
		Category:        category,
		FindingID:       findingID,
		OriginAnchors:   copyFindingMeta(originAnchors),
		FactFamily:      string(result.FactFamily),
		FactKey:         result.FactKey,
		EdgeTag:         string(result.EdgeFunctionTag),
		FactOriginPath:  cloneCFGPath(syntheticPath),
		CFGPath:         effectiveWitnessPath(result.Witness, syntheticPath),
		WitnessEdges:    cloneInterprocWitnessEdges(result.WitnessEdges),
		WitnessTerminal: result.WitnessTerminal,
		CallChain:       cloneCallChain(callChain),
		TriggerReason:   refinementTriggerForResult(result),
	}
	record.WitnessHash = computeCFGWitnessHash(record)
	return record
}

func effectiveWitnessPath(path []int32, syntheticPath []int32) []int32 {
	if len(path) > 0 {
		return cloneCFGPath(path)
	}
	return cloneCFGPath(syntheticPath)
}

func cloneCallChain(callChain []string) []string {
	if len(callChain) == 0 {
		return nil
	}
	out := make([]string, len(callChain))
	copy(out, callChain)
	return out
}

func refinementTriggerForResult(result interprocPathResult) string {
	if result.Class == interprocOutcomeUnsafe {
		return cfgRefinementTriggerUnsafeCandidate
	}
	if result.Reason == pathOutcomeReasonNone {
		return cfgRefinementTriggerUnsafeCandidate
	}
	return string(result.Reason)
}

func computeCFGWitnessHash(record cfgWitnessRecord) string {
	preimage := struct {
		OriginAnchors   map[string]string      `json:"origin_anchors,omitempty"`
		FactFamily      string                 `json:"fact_family,omitempty"`
		FactKey         string                 `json:"fact_key,omitempty"`
		EdgeTag         string                 `json:"edge_function_tag,omitempty"`
		FactOriginPath  []int32                `json:"fact_origin_path,omitempty"`
		CFGPath         []int32                `json:"cfg_path,omitempty"`
		WitnessEdges    []interprocWitnessEdge `json:"witness_edges,omitempty"`
		WitnessTerminal interprocNodeID        `json:"witness_terminal,omitzero"`
		CallChain       []string               `json:"call_chain,omitempty"`
		TriggerReason   string                 `json:"trigger_reason,omitempty"`
	}{
		OriginAnchors:   normalizeFindingMeta(record.OriginAnchors),
		FactFamily:      record.FactFamily,
		FactKey:         record.FactKey,
		EdgeTag:         record.EdgeTag,
		FactOriginPath:  cloneCFGPath(record.FactOriginPath),
		CFGPath:         cloneCFGPath(record.CFGPath),
		WitnessEdges:    cloneInterprocWitnessEdges(record.WitnessEdges),
		WitnessTerminal: record.WitnessTerminal,
		CallChain:       cloneCallChain(record.CallChain),
		TriggerReason:   record.TriggerReason,
	}
	encoded, err := json.Marshal(preimage)
	if err != nil {
		sum := sha256.Sum256([]byte(record.TriggerReason + "|" + strings.Join(record.CallChain, "|")))
		return "cfgw1_" + hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(encoded)
	return "cfgw1_" + hex.EncodeToString(sum[:])
}

func normalizeFindingMeta(meta map[string]string) map[string]string {
	if len(meta) == 0 {
		return nil
	}
	keys := make([]string, 0, len(meta))
	for key, value := range meta {
		if key == "" || value == "" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = meta[key]
	}
	return out
}

func setInterprocWitnessHash(result *interprocPathResult, callChain []string, syntheticPath []int32) {
	if result == nil {
		return
	}
	path := effectiveWitnessPath(result.Witness, syntheticPath)
	result.WitnessEdges = qualifyInterprocWitnessFact(result.WitnessEdges, result.FactFamily, result.FactKey)
	result.WitnessHash = computeInterprocWitnessHash(*result, callChain, path)
}

func computeInterprocWitnessHash(result interprocPathResult, callChain []string, path []int32) string {
	if len(path) == 0 {
		return ""
	}
	record := cfgWitnessRecord{
		FactFamily:      string(result.FactFamily),
		FactKey:         result.FactKey,
		EdgeTag:         string(result.EdgeFunctionTag),
		CFGPath:         cloneCFGPath(path),
		WitnessEdges:    cloneInterprocWitnessEdges(result.WitnessEdges),
		WitnessTerminal: result.WitnessTerminal,
		CallChain:       cloneCallChain(callChain),
		TriggerReason:   refinementTriggerForResult(result),
	}
	return computeCFGWitnessHash(record)
}

func interprocPathResultForDischargeTrigger(
	factFamily ifdsFactFamily,
	factKey string,
	trigger string,
) interprocPathResult {
	result := interprocPathResult{
		FactFamily: factFamily,
		FactKey:    factKey,
	}
	if trigger == cfgRefinementTriggerUnsafeCandidate {
		result.Class = interprocOutcomeUnsafe
	} else {
		result.Class = interprocOutcomeInconclusive
		result.Reason = pathOutcomeReason(trigger)
	}
	result.EdgeFunctionTag = edgeTagFromPathResult(result)
	return result
}

func appendProtocolRefinementMeta(meta map[string]string, result interprocPathResult) map[string]string {
	if result.SSAAvailability.Status != "" {
		if meta == nil {
			meta = make(map[string]string)
		}
		meta["ssa_availability_status"] = string(result.SSAAvailability.Status)
		if result.SSAAvailability.Detail != "" {
			meta["ssa_availability_detail"] = result.SSAAvailability.Detail
		}
	}
	if result.Tabulation.PathEdges > 0 || result.Tabulation.Dependencies > 0 ||
		result.Tabulation.Summaries > 0 || result.Tabulation.SummaryReuses > 0 {
		if meta == nil {
			meta = make(map[string]string)
		}
		meta["cfg_tabulation_path_edges"] = strconvItoa(result.Tabulation.PathEdges)
		meta["cfg_tabulation_dependencies"] = strconvItoa(result.Tabulation.Dependencies)
		meta["cfg_tabulation_summaries"] = strconvItoa(result.Tabulation.Summaries)
		meta["cfg_tabulation_summary_reuses"] = strconvItoa(result.Tabulation.SummaryReuses)
	}
	if !result.Refinement.Enabled {
		return meta
	}
	if meta == nil {
		meta = make(map[string]string)
	}
	meta["cfg_feasibility_engine"] = result.Refinement.FeasibilityEngine
	if result.Refinement.FeasibilityResult != "" {
		meta["cfg_feasibility_result"] = result.Refinement.FeasibilityResult
	}
	if result.Refinement.FeasibilityReason != "" {
		meta["cfg_feasibility_reason"] = result.Refinement.FeasibilityReason
	}
	if result.Refinement.RefinementStatus != "" {
		meta["cfg_refinement_status"] = result.Refinement.RefinementStatus
	}
	meta["cfg_refinement_iterations"] = strconvItoa(result.Refinement.RefinementIterations)
	if result.Refinement.RefinementTrigger != "" {
		meta["cfg_refinement_trigger"] = result.Refinement.RefinementTrigger
	}
	if result.Refinement.WitnessHash != "" {
		meta["cfg_refinement_witness_hash"] = result.Refinement.WitnessHash
	}
	meta["cfg_refinement_ssa_subjects"] = "unavailable"
	if len(result.Refinement.SSASubjects) > 0 {
		meta["cfg_refinement_ssa_subjects"] = strings.Join(result.Refinement.SSASubjects, ",")
	}
	if result.Refinement.EvidenceDigest != "" {
		meta["cfg_refinement_evidence_digest"] = result.Refinement.EvidenceDigest
	}
	return meta
}

type cfgPredicateConstraint struct {
	subject string
	op      string
	value   string
}

func exprConstraintValue(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	if expr == nil {
		return "", false
	}
	switch node := stripParens(expr).(type) {
	case *ast.Ident:
		switch node.Name {
		case "nil":
			return "<nil>", true
		case "true", "false":
			return node.Name, true
		}
	case *ast.BasicLit:
		return node.Value, true
	}
	if pass == nil || pass.TypesInfo == nil {
		return "", false
	}
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil {
		return "", false
	}
	return tv.Value.ExactString(), true
}

func strconvItoa(v int) string {
	return strconv.Itoa(v)
}
