// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"go/token"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type cfgRefinementOverride struct {
	MaxStates           int
	DischargedWitnesses map[string]bool
	AllowSafe           bool
	ResolveTargets      bool
}

type cfgRefinementWitnessIdentity struct {
	Category      string
	FindingID     string
	OriginAnchors map[string]string
	SyntheticPath []int32
}

type cfgRefinementWitnessIdentityFunc func(interprocPathResult) cfgRefinementWitnessIdentity

func (o cfgRefinementOverride) hasConcreteAction() bool {
	return o.MaxStates > 0 ||
		len(o.DischargedWitnesses) > 0 ||
		o.ResolveTargets
}

type cfgRefinementRequest struct {
	Pass            *analysis.Pass
	Position        token.Pos
	CFG             *gocfg.CFG
	Result          interprocPathResult
	Category        string
	FindingID       string
	CallChain       []string
	OriginAnchors   map[string]string
	SyntheticPath   []int32
	Control         protocolAnalysisControl
	Rerun           func(cfgRefinementOverride) interprocPathResult
	WitnessIdentity cfgRefinementWitnessIdentityFunc
}

type cfgRefinementController struct {
	options         cfgProtocolRefinementOptions
	cache           *cfgRefinementCache
	deadlineFactory func(time.Duration) cfgFeasibilityDeadline
	backend         cfgFeasibilityBackend
}

func newCFGRefinementController(options cfgProtocolRefinementOptions) cfgRefinementController {
	deadlineFactory := options.controlFactory
	if deadlineFactory == nil {
		deadlineFactory = newWallClockFeasibilityDeadline
	}
	backend := options.feasibilityBackend
	if backend == nil {
		backend = cfgSSAConstraintsFeasibilityBackend{}
	}
	return cfgRefinementController{
		options:         options,
		cache:           newCFGRefinementCache(),
		deadlineFactory: deadlineFactory,
		backend:         backend,
	}
}

func (c cfgRefinementController) Refine(request cfgRefinementRequest) interprocPathResult {
	result := request.Result
	if !c.options.Enabled() {
		return result
	}
	if result.Class == interprocOutcomeSafe {
		// protocol refinement refines candidate violating witnesses. When IFDS already
		// proved the slice safe, there is no witness to discharge or trace.
		return result
	}

	record := buildCFGWitnessRecordForRefinement(request, result)
	if replayReason := refinementWitnessReplayRejection(result); replayReason != "" {
		result.Class = interprocOutcomeInconclusive
		result.Reason = pathOutcomeReasonFeasibilityUnknown
		result.Refinement = cfgProtocolRefinementResult{
			Enabled:           true,
			FeasibilityEngine: cfgSSAConstraintsEngine,
			FeasibilityResult: cfgFeasibilityResultUnknown,
			FeasibilityReason: cfgFeasibilityReasonEvidenceRejected,
			RefinementStatus:  cfgRefinementStatusInconclusive,
			RefinementTrigger: record.TriggerReason,
			WitnessHash:       record.WitnessHash,
		}
		result.WitnessRecord = record
		return result
	}
	deadline := request.Control
	if deadline == nil {
		deadline = c.newDeadline()
	}
	queryBudget := c.options.QueryBudget()
	if queryBudget <= 0 {
		queryBudget = defaultCFGFeasibilityMaxQueries
	}
	iterations := 0
	queries := 0
	discharged := map[string]bool{}
	decision := c.checkFeasibilityWithDeadline(c.backend, request.Pass, request.Position, request.CFG, record, deadline)
	verdict, reason := decision.Result, decision.Reason
	dischargeHash := refinementDischargeHash(result, record, decision)
	if feasibilityDeadlineReached(deadline) {
		verdict = cfgFeasibilityResultUnknown
		reason = cfgFeasibilityReasonTimeout
		dischargeHash = ""
	}
	queries++

	for iterations < c.options.IterationBudget() && queries < queryBudget {
		if !shouldAttemptRefinement(result, verdict) {
			break
		}
		if deadline != nil && deadline.Expired() {
			verdict = cfgFeasibilityResultUnknown
			reason = cfgFeasibilityReasonTimeout
			break
		}
		if verdict == cfgFeasibilityResultUNSAT {
			if feasibilityDeadlineReached(deadline) {
				verdict = cfgFeasibilityResultUnknown
				reason = cfgFeasibilityReasonTimeout
				break
			}
			discharged[dischargeHash] = true
		}
		override := cfgRefinementOverride{
			MaxStates:           refinementMaxStatesForTrigger(result.Reason),
			DischargedWitnesses: discharged,
			AllowSafe:           c.options.AllowsSafeResult(),
			ResolveTargets:      result.Reason == pathOutcomeReasonUnresolvedTarget,
		}
		if !override.hasConcreteAction() {
			break
		}
		if dischargeHash == "" || !c.cache.record(dischargeHash) {
			break
		}
		next := request.Rerun(override)
		iterations++
		if deadline != nil && deadline.Expired() {
			result = next
			verdict = cfgFeasibilityResultUnknown
			reason = cfgFeasibilityReasonTimeout
			break
		}
		if next.Class == interprocOutcomeSafe {
			if verdict == cfgFeasibilityResultUnknown {
				break
			}
			if feasibilityDeadlineReached(deadline) {
				result = next
				verdict = cfgFeasibilityResultUnknown
				reason = cfgFeasibilityReasonTimeout
				break
			}
			next.Refinement = cfgProtocolRefinementResult{
				Enabled:              true,
				FeasibilityEngine:    cfgSSAConstraintsEngine,
				FeasibilityResult:    verdict,
				FeasibilityReason:    reason,
				RefinementStatus:     cfgRefinementStatusDischargedInfeasible,
				RefinementIterations: iterations,
				RefinementTrigger:    record.TriggerReason,
				WitnessHash:          record.WitnessHash,
				SSASubjects:          decision.Subjects,
				Evidence:             decision.Evidence,
				EvidenceDigest:       decision.EvidenceDigest,
			}
			next.WitnessRecord = record
			return next
		}
		result = next
		record = buildCFGWitnessRecordForRefinement(request, result)
		decision = c.checkFeasibilityWithDeadline(c.backend, request.Pass, request.Position, request.CFG, record, deadline)
		verdict, reason = decision.Result, decision.Reason
		dischargeHash = refinementDischargeHash(result, record, decision)
		if feasibilityDeadlineReached(deadline) {
			verdict = cfgFeasibilityResultUnknown
			reason = cfgFeasibilityReasonTimeout
			dischargeHash = ""
		}
		queries++
	}
	if shouldAttemptRefinement(result, verdict) {
		result.Class = interprocOutcomeInconclusive
		result.Reason = pathOutcomeReasonFeasibilityUnknown
		verdict = cfgFeasibilityResultUnknown
		switch {
		case queries >= queryBudget:
			reason = cfgFeasibilityReasonQueryLimit
		case iterations >= c.options.IterationBudget():
			reason = cfgFeasibilityReasonIterationLimit
		}
	} else if verdict == cfgFeasibilityResultUnknown {
		result.Class = interprocOutcomeInconclusive
		result.Reason = pathOutcomeReasonFeasibilityUnknown
	}

	status := cfgRefinementStatusViolation
	switch result.Class {
	case interprocOutcomeSafe:
		status = cfgRefinementStatusDischargedInfeasible
	case interprocOutcomeUnsafe:
		status = cfgRefinementStatusViolation
	case interprocOutcomeInconclusive:
		status = cfgRefinementStatusInconclusive
	}
	result.Refinement = cfgProtocolRefinementResult{
		Enabled:              true,
		FeasibilityEngine:    cfgSSAConstraintsEngine,
		FeasibilityResult:    verdict,
		FeasibilityReason:    reason,
		RefinementStatus:     status,
		RefinementIterations: iterations,
		RefinementTrigger:    record.TriggerReason,
		WitnessHash:          record.WitnessHash,
		SSASubjects:          decision.Subjects,
		Evidence:             decision.Evidence,
		EvidenceDigest:       decision.EvidenceDigest,
	}
	result.WitnessRecord = record
	return result
}

func buildCFGWitnessRecordForRefinement(
	request cfgRefinementRequest,
	result interprocPathResult,
) cfgWitnessRecord {
	identity := cfgRefinementWitnessIdentity{
		Category:      request.Category,
		FindingID:     request.FindingID,
		OriginAnchors: request.OriginAnchors,
		SyntheticPath: request.SyntheticPath,
	}
	if request.WitnessIdentity != nil {
		identity = request.WitnessIdentity(result)
	}
	return buildCFGWitnessRecord(
		identity.Category,
		identity.FindingID,
		identity.OriginAnchors,
		result,
		request.CallChain,
		identity.SyntheticPath,
	)
}

func refinementWitnessReplayRejection(result interprocPathResult) string {
	if len(result.WitnessEdges) == 0 {
		return ""
	}
	if result.witnessGraph == nil {
		return interprocWitnessReplayMissingNode
	}
	return validateInterprocWitnessReplay(*result.witnessGraph, result)
}

func refinementDischargeHash(
	result interprocPathResult,
	record cfgWitnessRecord,
	decision cfgFeasibilityDecision,
) string {
	if decision.Result != cfgFeasibilityResultUNSAT {
		return ""
	}
	baseHash := record.WitnessHash
	if result.WitnessHash != "" {
		baseHash = result.WitnessHash
	}
	evidenceDigest := decision.EvidenceDigest
	formulaDigest := decision.FormulaDigest
	if baseHash == "" || evidenceDigest == "" || formulaDigest == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		baseHash,
		formulaDigest,
		evidenceDigest,
	}, "|")))
	return baseHash + "|cfgd1_" + hex.EncodeToString(digest[:])
}

func shouldAttemptRefinement(result interprocPathResult, verdict string) bool {
	return result.Class != interprocOutcomeSafe && verdict == cfgFeasibilityResultUNSAT
}

func (c cfgRefinementController) checkFeasibility(
	backend cfgFeasibilityBackend,
	pass *analysis.Pass,
	pos token.Pos,
	cfg *gocfg.CFG,
	record cfgWitnessRecord,
) cfgFeasibilityDecision {
	return c.checkFeasibilityWithDeadline(backend, pass, pos, cfg, record, c.newDeadline())
}

func (c cfgRefinementController) newDeadline() cfgFeasibilityDeadline {
	if c.deadlineFactory == nil {
		return nil
	}
	return c.deadlineFactory(c.options.FeasibilityTimeout)
}

func (c cfgRefinementController) checkFeasibilityWithDeadline(
	backend cfgFeasibilityBackend,
	pass *analysis.Pass,
	pos token.Pos,
	cfg *gocfg.CFG,
	record cfgWitnessRecord,
	deadline cfgFeasibilityDeadline,
) cfgFeasibilityDecision {
	if backend == nil {
		return cfgFeasibilityDecision{}
	}
	query := cfgFeasibilityQuery{
		Pass:     pass,
		CFG:      cfg,
		Witness:  record,
		Timeout:  c.options.FeasibilityTimeout,
		Position: pos,
	}
	query.Deadline = deadline
	if feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	decision := backend.Check(query)
	if feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	// Test-only corruptions are injected here, after production evidence is
	// built but before the independent production checker admits an UNSAT
	// discharge. Normal analyzer controls do not implement the injection seam.
	decision.Evidence = injectProtocolWitnessEvidence(deadline, decision.Evidence)
	decision = injectProtocolRefinementEvidence(deadline, decision)
	if decision.Result == cfgFeasibilityResultUNSAT {
		accepted, expired, formulaDigest, evidenceDigest := checkSSAConstraintEvidenceWithDigests(query, decision.Evidence)
		if expired || feasibilityDeadlineExpired(query) {
			return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
		}
		if !accepted {
			return cfgFeasibilityDecision{
				Result:   cfgFeasibilityResultUnknown,
				Reason:   cfgFeasibilityReasonEvidenceRejected,
				Evidence: decision.Evidence,
				Subjects: decision.Subjects,
			}
		}
		decision.FormulaDigest = formulaDigest
		decision.EvidenceDigest = evidenceDigest
	}
	return decision
}

func refinementMaxStatesForTrigger(reason pathOutcomeReason) int {
	switch reason {
	case pathOutcomeReasonStateBudget:
		return defaultCFGMaxStates
	default:
		return 0
	}
}
