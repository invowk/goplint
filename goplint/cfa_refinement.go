// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"maps"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type cfgRefinementOverride struct {
	MaxStates           int
	MaxDepth            int
	DischargedWitnesses map[string]bool
	AllowSafe           bool
	ResolveTargets      bool
	RefineRecursion     bool
}

func (o cfgRefinementOverride) hasConcreteAction() bool {
	return o.MaxStates > 0 ||
		o.MaxDepth > 0 ||
		len(o.DischargedWitnesses) > 0 ||
		o.ResolveTargets ||
		o.RefineRecursion
}

type cfgRefinementRequest struct {
	Pass          *analysis.Pass
	Position      token.Pos
	CFG           *gocfg.CFG
	Result        interprocPathResult
	Category      string
	FindingID     string
	CallChain     []string
	OriginAnchors map[string]string
	SyntheticPath []int32
	Rerun         func(cfgRefinementOverride) interprocPathResult
}

type cfgRefinementController struct {
	options cfgPhaseCOptions
	cache   *cfgRefinementCache
}

func newCFGRefinementController(options cfgPhaseCOptions) cfgRefinementController {
	return cfgRefinementController{
		options: options,
		cache:   newCFGRefinementCache(),
	}
}

func (c cfgRefinementController) Refine(request cfgRefinementRequest) interprocPathResult {
	result := request.Result
	if !c.options.Enabled() {
		return result
	}
	if result.Class == interprocOutcomeSafe {
		// Phase C refines candidate violating witnesses. When IFDS already
		// proved the slice safe, there is no witness to discharge or trace.
		return result
	}

	record := buildCFGWitnessRecord(
		request.Category,
		request.FindingID,
		request.OriginAnchors,
		result,
		request.CallChain,
		request.SyntheticPath,
	)
	backend := feasibilityBackendForEngine(c.options.FeasibilityEngine)
	if backend == nil {
		return result
	}

	queryBudget := c.options.QueryBudget()
	if queryBudget <= 0 {
		queryBudget = defaultCFGFeasibilityMaxQueries
	}
	iterations := 0
	queries := 0
	discharged := map[string]bool{}
	dischargeHash := phaseCDischargeHash(result, record)
	verdict, reason := c.checkFeasibility(backend, request.Pass, request.Position, request.CFG, record)
	queries++

	for iterations < c.options.IterationBudget() && queries < queryBudget {
		if !shouldAttemptRefinement(result, verdict) {
			break
		}
		if verdict == cfgFeasibilityResultUNSAT {
			discharged[dischargeHash] = true
		}
		override := cfgRefinementOverride{
			MaxStates:           refinementMaxStatesForTrigger(result.Reason),
			MaxDepth:            refinementMaxDepthForTrigger(result.Reason),
			DischargedWitnesses: discharged,
			AllowSafe:           c.options.AllowsSafeResult(),
			ResolveTargets:      result.Reason == pathOutcomeReasonUnresolvedTarget,
			RefineRecursion:     result.Reason == pathOutcomeReasonRecursionCycle,
		}
		if !override.hasConcreteAction() {
			break
		}
		if dischargeHash == "" || !c.cache.record(dischargeHash) {
			break
		}
		next := request.Rerun(override)
		iterations++
		if next.Class == interprocOutcomeSafe {
			if verdict == cfgFeasibilityResultUnknown {
				break
			}
			next.PhaseC = cfgPhaseCResult{
				Enabled:              true,
				FeasibilityEngine:    c.options.FeasibilityEngine,
				FeasibilityResult:    verdict,
				FeasibilityReason:    reason,
				RefinementStatus:     cfgRefinementStatusProvenSafe,
				RefinementIterations: iterations,
				RefinementTrigger:    record.TriggerReason,
				WitnessHash:          record.WitnessHash,
			}
			next.WitnessRecord = record
			return next
		}
		result = next
		record = buildCFGWitnessRecord(
			request.Category,
			request.FindingID,
			request.OriginAnchors,
			result,
			request.CallChain,
			request.SyntheticPath,
		)
		dischargeHash = phaseCDischargeHash(result, record)
		verdict, reason = c.checkFeasibility(backend, request.Pass, request.Position, request.CFG, record)
		queries++
	}

	status := cfgRefinementStatusUnsafe
	switch result.Class {
	case interprocOutcomeSafe:
		status = cfgRefinementStatusProvenSafe
	case interprocOutcomeUnsafe:
		status = cfgRefinementStatusUnsafe
	case interprocOutcomeInconclusive:
		if iterations > 0 {
			status = cfgRefinementStatusInconclusiveRefined
		} else {
			status = cfgRefinementStatusInconclusiveRaw
		}
	}
	result.PhaseC = cfgPhaseCResult{
		Enabled:              true,
		FeasibilityEngine:    c.options.FeasibilityEngine,
		FeasibilityResult:    verdict,
		FeasibilityReason:    reason,
		RefinementStatus:     status,
		RefinementIterations: iterations,
		RefinementTrigger:    record.TriggerReason,
		WitnessHash:          record.WitnessHash,
	}
	result.WitnessRecord = record
	return result
}

func phaseCDischargeHash(result interprocPathResult, record cfgWitnessRecord) string {
	if result.WitnessHash != "" {
		return result.WitnessHash
	}
	return record.WitnessHash
}

func shouldAttemptRefinement(result interprocPathResult, verdict string) bool {
	if result.Class == interprocOutcomeUnsafe {
		return verdict == cfgFeasibilityResultUNSAT
	}
	if result.Class != interprocOutcomeInconclusive {
		return false
	}
	switch result.Reason {
	case pathOutcomeReasonStateBudget, pathOutcomeReasonDepthBudget, pathOutcomeReasonRecursionCycle, pathOutcomeReasonUnresolvedTarget:
		return true
	default:
		return false
	}
}

func (c cfgRefinementController) checkFeasibility(
	backend cfgFeasibilityBackend,
	pass *analysis.Pass,
	pos token.Pos,
	cfg *gocfg.CFG,
	record cfgWitnessRecord,
) (string, string) {
	if backend == nil {
		return "", cfgFeasibilityReasonNone
	}
	return backend.Check(cfgFeasibilityQuery{
		Pass:     pass,
		CFG:      cfg,
		Witness:  record,
		Timeout:  c.options.FeasibilityTimeout,
		Position: pos,
	})
}

func refinementMaxStatesForTrigger(reason pathOutcomeReason) int {
	switch reason {
	case pathOutcomeReasonStateBudget:
		return defaultCFGMaxStates
	default:
		return 0
	}
}

func refinementMaxDepthForTrigger(reason pathOutcomeReason) int {
	switch reason {
	case pathOutcomeReasonDepthBudget:
		return defaultCFGMaxDepth
	default:
		return 0
	}
}

const summaryStackOptionRecursionFallback = "__goplint:phasec:recursion-fallback"

func summaryStackWithRecursionFallback(base map[string]bool) map[string]bool {
	if len(base) == 0 {
		return map[string]bool{summaryStackOptionRecursionFallback: true}
	}
	out := make(map[string]bool, len(base)+1)
	maps.Copy(out, base)
	out[summaryStackOptionRecursionFallback] = true
	return out
}

func summaryStackHasRecursionFallback(stack map[string]bool) bool {
	return stack != nil && stack[summaryStackOptionRecursionFallback]
}

func mergeResolvedTargetRefinement(
	current interprocPathResult,
	fallback interprocPathResult,
) interprocPathResult {
	switch fallback.Class {
	case interprocOutcomeUnsafe:
		return fallback
	case interprocOutcomeSafe:
		return fallback
	case interprocOutcomeInconclusive:
		if fallback.Reason != pathOutcomeReasonUnresolvedTarget {
			return fallback
		}
	}
	return current
}
