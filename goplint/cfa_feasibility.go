// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/token"
	"go/types"
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

	cfgRefinementStatusUnsafe              = "unsafe"
	cfgRefinementStatusInconclusiveRefined = "inconclusive-refined"
	cfgRefinementStatusInconclusiveRaw     = "inconclusive-unrefined"
	cfgRefinementStatusProvenSafe          = "proven-safe"
)

// cfgWitnessRecord is the canonical explanation substrate used by Phase C.
// It stays additive to the existing diagnostic metadata contract.
type cfgWitnessRecord struct {
	Category      string            `json:"category,omitempty"`
	FindingID     string            `json:"finding_id,omitempty"`
	OriginAnchors map[string]string `json:"origin_anchors,omitempty"`
	FactFamily    string            `json:"fact_family,omitempty"`
	FactKey       string            `json:"fact_key,omitempty"`
	EdgeTag       string            `json:"edge_function_tag,omitempty"`
	CFGPath       []int32           `json:"cfg_path,omitempty"`
	CallChain     []string          `json:"call_chain,omitempty"`
	TriggerReason string            `json:"trigger_reason,omitempty"`
	WitnessHash   string            `json:"witness_hash,omitempty"`
}

// cfgPhaseCResult stores feasibility/refinement provenance for one evaluated
// witness. The metadata is surfaced on emitted diagnostics and trace records.
type cfgPhaseCResult struct {
	Enabled              bool
	FeasibilityEngine    string
	FeasibilityResult    string
	FeasibilityReason    string
	RefinementStatus     string
	RefinementIterations int
	RefinementTrigger    string
	WitnessHash          string
}

type cfgFeasibilityQuery struct {
	Pass     *analysis.Pass
	CFG      *gocfg.CFG
	Witness  cfgWitnessRecord
	Timeout  time.Duration
	Position token.Pos
}

type cfgFeasibilityBackend interface {
	Check(query cfgFeasibilityQuery) (string, string)
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
		Category:      category,
		FindingID:     findingID,
		OriginAnchors: copyFindingMeta(originAnchors),
		FactFamily:    string(result.FactFamily),
		FactKey:       result.FactKey,
		EdgeTag:       string(result.EdgeFunctionTag),
		CFGPath:       effectiveWitnessPath(result.Witness, syntheticPath),
		CallChain:     cloneCallChain(callChain),
		TriggerReason: refinementTriggerForResult(result),
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
		OriginAnchors map[string]string `json:"origin_anchors,omitempty"`
		FactFamily    string            `json:"fact_family,omitempty"`
		FactKey       string            `json:"fact_key,omitempty"`
		EdgeTag       string            `json:"edge_function_tag,omitempty"`
		CFGPath       []int32           `json:"cfg_path,omitempty"`
		CallChain     []string          `json:"call_chain,omitempty"`
		TriggerReason string            `json:"trigger_reason,omitempty"`
	}{
		OriginAnchors: normalizeFindingMeta(record.OriginAnchors),
		FactFamily:    record.FactFamily,
		FactKey:       record.FactKey,
		EdgeTag:       record.EdgeTag,
		CFGPath:       cloneCFGPath(record.CFGPath),
		CallChain:     cloneCallChain(record.CallChain),
		TriggerReason: record.TriggerReason,
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
	result.WitnessHash = computeInterprocWitnessHash(*result, callChain, path)
}

func computeInterprocWitnessHash(result interprocPathResult, callChain []string, path []int32) string {
	if len(path) == 0 {
		return ""
	}
	record := cfgWitnessRecord{
		FactFamily:    string(result.FactFamily),
		FactKey:       result.FactKey,
		EdgeTag:       string(result.EdgeFunctionTag),
		CFGPath:       cloneCFGPath(path),
		CallChain:     cloneCallChain(callChain),
		TriggerReason: refinementTriggerForResult(result),
	}
	return computeCFGWitnessHash(record)
}

func interprocPathResultForDischargeTrigger(
	factFamily ifdsFactFamily,
	factKey string,
	ubvMode string,
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
	result.EdgeFunctionTag = edgeTagFromPathResult(result, ubvMode)
	return result
}

func appendPhaseCMeta(meta map[string]string, result interprocPathResult) map[string]string {
	if !result.PhaseC.Enabled {
		return meta
	}
	if meta == nil {
		meta = make(map[string]string)
	}
	meta["cfg_feasibility_engine"] = result.PhaseC.FeasibilityEngine
	if result.PhaseC.FeasibilityResult != "" {
		meta["cfg_feasibility_result"] = result.PhaseC.FeasibilityResult
	}
	if result.PhaseC.FeasibilityReason != "" {
		meta["cfg_feasibility_reason"] = result.PhaseC.FeasibilityReason
	}
	if result.PhaseC.RefinementStatus != "" {
		meta["cfg_refinement_status"] = result.PhaseC.RefinementStatus
	}
	meta["cfg_refinement_iterations"] = strconvItoa(result.PhaseC.RefinementIterations)
	if result.PhaseC.RefinementTrigger != "" {
		meta["cfg_refinement_trigger"] = result.PhaseC.RefinementTrigger
	}
	if result.PhaseC.WitnessHash != "" {
		meta["cfg_refinement_witness_hash"] = result.PhaseC.WitnessHash
	}
	return meta
}

type cfgPredicateConstraint struct {
	subject string
	op      string
	value   string
}

type cfgConstraintExtraction struct {
	constraints []cfgPredicateConstraint
	unsupported bool
}

func extractWitnessConstraints(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	path []int32,
) cfgConstraintExtraction {
	if cfg == nil || len(path) < 2 {
		return cfgConstraintExtraction{}
	}
	blocksByIndex := make(map[int32]*gocfg.Block, len(cfg.Blocks))
	for _, block := range cfg.Blocks {
		if block == nil {
			continue
		}
		blocksByIndex[block.Index] = block
	}
	result := cfgConstraintExtraction{}
	for idx := 0; idx+1 < len(path); idx++ {
		from := blocksByIndex[path[idx]]
		to := blocksByIndex[path[idx+1]]
		if from == nil || to == nil || len(from.Succs) != 2 || len(from.Nodes) == 0 {
			continue
		}
		cond, ok := from.Nodes[len(from.Nodes)-1].(ast.Expr)
		if !ok || cond == nil {
			continue
		}
		polarity := -1
		if from.Succs[0] != nil && from.Succs[0].Index == to.Index {
			polarity = 1
		}
		if from.Succs[1] != nil && from.Succs[1].Index == to.Index {
			polarity = 0
		}
		if polarity == -1 {
			continue
		}
		constraints, supported := extractPredicateConstraints(pass, cond, polarity == 1)
		if !supported {
			result.unsupported = true
			return result
		}
		result.constraints = append(result.constraints, constraints...)
	}
	return result
}

func extractPredicateConstraints(pass *analysis.Pass, expr ast.Expr, truthy bool) ([]cfgPredicateConstraint, bool) {
	switch node := stripParens(expr).(type) {
	case *ast.Ident:
		switch node.Name {
		case "true":
			if truthy {
				return nil, true
			}
			return []cfgPredicateConstraint{{subject: "__const__", op: "eq", value: "false"}}, true
		case "false":
			if truthy {
				return []cfgPredicateConstraint{{subject: "__const__", op: "eq", value: "false"}}, true
			}
			return nil, true
		default:
			return nil, false
		}
	case *ast.UnaryExpr:
		if node.Op != token.NOT {
			return nil, false
		}
		return extractPredicateConstraints(pass, node.X, !truthy)
	case *ast.BinaryExpr:
		switch node.Op {
		case token.LAND:
			if !truthy {
				return nil, false
			}
			left, ok := extractPredicateConstraints(pass, node.X, true)
			if !ok {
				return nil, false
			}
			right, ok := extractPredicateConstraints(pass, node.Y, true)
			if !ok {
				return nil, false
			}
			return append(left, right...), true
		case token.LOR:
			if truthy {
				return nil, false
			}
			left, ok := extractPredicateConstraints(pass, node.X, false)
			if !ok {
				return nil, false
			}
			right, ok := extractPredicateConstraints(pass, node.Y, false)
			if !ok {
				return nil, false
			}
			return append(left, right...), true
		case token.EQL, token.NEQ:
			subject, value, ok := extractEqualityConstraintOperands(pass, node.X, node.Y)
			if !ok {
				subject, value, ok = extractEqualityConstraintOperands(pass, node.Y, node.X)
			}
			if !ok {
				return nil, false
			}
			op := "eq"
			if node.Op == token.NEQ {
				op = "neq"
			}
			if !truthy {
				if op == "eq" {
					op = "neq"
				} else {
					op = "eq"
				}
			}
			return []cfgPredicateConstraint{{subject: subject, op: op, value: value}}, true
		default:
			return nil, false
		}
	default:
		return nil, false
	}
}

func extractEqualityConstraintOperands(pass *analysis.Pass, subjectExpr ast.Expr, valueExpr ast.Expr) (string, string, bool) {
	subject := exprConstraintSubject(pass, subjectExpr)
	if subject == "" {
		return "", "", false
	}
	value, ok := exprConstraintValue(pass, valueExpr)
	if !ok {
		return "", "", false
	}
	return subject, value, true
}

func exprConstraintSubject(pass *analysis.Pass, expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	if key := targetKeyForExpr(pass, expr); key != "" {
		return key
	}
	switch node := stripParens(expr).(type) {
	case *ast.Ident:
		if pass != nil && pass.TypesInfo != nil {
			if obj := objectForIdent(pass, node); obj != nil {
				return objectKey(obj)
			}
		}
	case *ast.SelectorExpr:
		if pass != nil && pass.TypesInfo != nil {
			if obj := objectForIdent(pass, node.Sel); obj != nil {
				return objectKey(obj)
			}
		}
	}
	return types.ExprString(expr)
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
