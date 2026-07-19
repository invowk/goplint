// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"slices"
)

const cfgSSAConstraintEvidenceVersion = 1

// cfgSSAConstraintEvidence is a replayable certificate. Every DNF alternative
// must name a pair of atoms that independently proves that alternative
// contradictory before an UNSAT result can discharge a witness.
type cfgSSAConstraintEvidence struct {
	FormatVersion  int
	ConstantFalse  bool
	WitnessPath    []int32
	Subjects       []string
	Contradictions []cfgSSAConstraintContradiction
}

type cfgSSAConstraintContradiction struct {
	Alternative int
	Left        cfgPredicateConstraint
	Right       cfgPredicateConstraint
}

func cfgSSAConstraintEvidenceDigest(evidence cfgSSAConstraintEvidence) string {
	digest, _ := cfgSSAConstraintEvidenceDigestWithControl(evidence, nil)
	return digest
}

func cfgSSAConstraintEvidenceDigestWithControl(
	evidence cfgSSAConstraintEvidence,
	control protocolAnalysisControl,
) (string, bool) {
	if feasibilityDeadlineReached(control) {
		return "", true
	}
	if evidence.FormatVersion == 0 {
		return "", false
	}
	type digestContradiction struct {
		Alternative int    `json:"alternative"`
		Left        string `json:"left"`
		Right       string `json:"right"`
	}
	type digestEvidence struct {
		FormatVersion  int                   `json:"format_version"`
		ConstantFalse  bool                  `json:"constant_false"`
		WitnessPath    []int32               `json:"witness_path"`
		Subjects       []string              `json:"subjects"`
		Contradictions []digestContradiction `json:"contradictions"`
	}
	preimage := digestEvidence{
		FormatVersion: evidence.FormatVersion,
		ConstantFalse: evidence.ConstantFalse,
		WitnessPath:   cloneCFGPath(evidence.WitnessPath),
		Subjects:      append([]string(nil), evidence.Subjects...),
	}
	slices.Sort(preimage.Subjects)
	for _, contradiction := range evidence.Contradictions {
		if feasibilityDeadlineReached(control) {
			return "", true
		}
		preimage.Contradictions = append(preimage.Contradictions, digestContradiction{
			Alternative: contradiction.Alternative,
			Left:        cfgConstraintKey(contradiction.Left),
			Right:       cfgConstraintKey(contradiction.Right),
		})
	}
	encoded, err := json.Marshal(preimage)
	if err != nil {
		return "", false
	}
	if feasibilityDeadlineReached(control) {
		return "", true
	}
	digest := sha256.Sum256(encoded)
	return "cfge2_" + hex.EncodeToString(digest[:]), feasibilityDeadlineReached(control)
}

func cfgSSAConstraintFormulaDigest(formula cfgSSAConstraintFormula) string {
	digest, _ := cfgSSAConstraintFormulaDigestWithControl(formula, nil)
	return digest
}

func cfgSSAConstraintFormulaDigestWithControl(
	formula cfgSSAConstraintFormula,
	control protocolAnalysisControl,
) (string, bool) {
	if feasibilityDeadlineReached(control) {
		return "", true
	}
	if formula.normalizeWithControl(control) {
		return "", true
	}
	alternatives := make([][]string, 0, len(formula.alternatives))
	for _, alternative := range formula.alternatives {
		if feasibilityDeadlineReached(control) {
			return "", true
		}
		constraints := make([]string, 0, len(alternative))
		for _, constraint := range alternative {
			if feasibilityDeadlineReached(control) {
				return "", true
			}
			constraints = append(constraints, cfgConstraintKey(constraint))
		}
		alternatives = append(alternatives, constraints)
	}
	preimage := struct {
		Alternatives [][]string `json:"alternatives"`
		Unsupported  bool       `json:"unsupported"`
	}{
		Alternatives: alternatives,
		Unsupported:  formula.unsupported,
	}
	encoded, err := json.Marshal(preimage)
	if err != nil {
		return "", false
	}
	if feasibilityDeadlineReached(control) {
		return "", true
	}
	digest := sha256.Sum256(encoded)
	return "cfgf2_" + hex.EncodeToString(digest[:]), feasibilityDeadlineReached(control)
}

// cfgSSAConstraintsFeasibilityBackend decides the documented finite
// SSA-versioned comparison fragment. Unsupported atoms are conservatively
// over-approximated as true: a checked contradiction may still prove the
// over-approximation UNSAT, while every non-UNSAT result remains unknown.
type cfgSSAConstraintsFeasibilityBackend struct{}

func (cfgSSAConstraintsFeasibilityBackend) Check(query cfgFeasibilityQuery) cfgFeasibilityDecision {
	if query.Timeout <= 0 {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	if query.Deadline == nil {
		query.Deadline = newWallClockFeasibilityDeadline(query.Timeout)
	}
	if feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}

	formula, expired := extractSSAConstraintsForWitnessRecordWithControl(
		query.Pass,
		query.CFG,
		query.Witness,
		query.Deadline,
	)
	if expired || feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	subjects, expired := ssaConstraintSubjectsWithDeadline(formula, query.Deadline)
	if expired {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	evidence, unsat, expired := buildSSAConstraintEvidenceWithDeadline(formula, query.Deadline)
	if expired {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	if feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	if unsat {
		evidence.WitnessPath = cloneCFGPath(query.Witness.CFGPath)
		evidence.Subjects = append([]string(nil), subjects...)
		formulaDigest, digestExpired := cfgSSAConstraintFormulaDigestWithControl(formula, query.Deadline)
		if digestExpired {
			return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
		}
		evidenceDigest, digestExpired := cfgSSAConstraintEvidenceDigestWithControl(evidence, query.Deadline)
		if digestExpired || feasibilityDeadlineExpired(query) {
			return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
		}
		return cfgFeasibilityDecision{
			Result:         cfgFeasibilityResultUNSAT,
			Evidence:       evidence,
			Subjects:       subjects,
			FormulaDigest:  formulaDigest,
			EvidenceDigest: evidenceDigest,
		}
	}
	if formula.unsupported {
		return cfgFeasibilityDecision{
			Result:   cfgFeasibilityResultUnknown,
			Reason:   cfgFeasibilityReasonUnsupportedPredicate,
			Subjects: subjects,
		}
	}

	if feasibilityDeadlineExpired(query) {
		return cfgFeasibilityDecision{Result: cfgFeasibilityResultUnknown, Reason: cfgFeasibilityReasonTimeout}
	}
	return cfgFeasibilityDecision{Result: cfgFeasibilityResultSAT, Subjects: subjects}
}

func ssaConstraintSubjects(formula cfgSSAConstraintFormula) []string {
	result, _ := ssaConstraintSubjectsWithDeadline(formula, nil)
	return result
}

func ssaConstraintSubjectsWithDeadline(
	formula cfgSSAConstraintFormula,
	deadline cfgFeasibilityDeadline,
) ([]string, bool) {
	seen := make(map[string]bool)
	for _, alternative := range formula.alternatives {
		if feasibilityDeadlineReached(deadline) {
			return nil, true
		}
		for _, constraint := range alternative {
			if feasibilityDeadlineReached(deadline) {
				return nil, true
			}
			if constraint.subject != "" {
				seen[constraint.subject] = true
			}
		}
	}
	result := make([]string, 0, len(seen))
	for subject := range seen {
		result = append(result, subject)
	}
	slices.Sort(result)
	return result, feasibilityDeadlineReached(deadline)
}

func buildSSAConstraintEvidence(formula cfgSSAConstraintFormula) (cfgSSAConstraintEvidence, bool) {
	evidence, unsat, _ := buildSSAConstraintEvidenceWithDeadline(formula, nil)
	return evidence, unsat
}

func buildSSAConstraintEvidenceWithDeadline(
	formula cfgSSAConstraintFormula,
	deadline cfgFeasibilityDeadline,
) (cfgSSAConstraintEvidence, bool, bool) {
	if feasibilityDeadlineReached(deadline) {
		return cfgSSAConstraintEvidence{}, false, true
	}
	evidence := cfgSSAConstraintEvidence{FormatVersion: cfgSSAConstraintEvidenceVersion}
	if len(formula.alternatives) == 0 {
		if feasibilityDeadlineReached(deadline) {
			return cfgSSAConstraintEvidence{}, false, true
		}
		evidence.ConstantFalse = true
		return evidence, true, false
	}
	for alternativeIndex, alternative := range formula.alternatives {
		if feasibilityDeadlineReached(deadline) {
			return cfgSSAConstraintEvidence{}, false, true
		}
		left, right, found, expired := findSSAConstraintContradictionWithDeadline(alternative, deadline)
		if expired {
			return cfgSSAConstraintEvidence{}, false, true
		}
		if !found {
			return cfgSSAConstraintEvidence{}, false, false
		}
		evidence.Contradictions = append(evidence.Contradictions, cfgSSAConstraintContradiction{
			Alternative: alternativeIndex,
			Left:        left,
			Right:       right,
		})
	}
	if feasibilityDeadlineReached(deadline) {
		return cfgSSAConstraintEvidence{}, false, true
	}
	return evidence, true, false
}

func findSSAConstraintContradictionWithDeadline(
	constraints []cfgPredicateConstraint,
	deadline cfgFeasibilityDeadline,
) (cfgPredicateConstraint, cfgPredicateConstraint, bool, bool) {
	for leftIndex, left := range constraints {
		if feasibilityDeadlineReached(deadline) {
			return cfgPredicateConstraint{}, cfgPredicateConstraint{}, false, true
		}
		for _, right := range constraints[leftIndex+1:] {
			if feasibilityDeadlineReached(deadline) {
				return cfgPredicateConstraint{}, cfgPredicateConstraint{}, false, true
			}
			if ssaConstraintsContradict(left, right) {
				return left, right, true, false
			}
		}
	}
	return cfgPredicateConstraint{}, cfgPredicateConstraint{}, false, false
}

func ssaConstraintsContradict(left, right cfgPredicateConstraint) bool {
	if left.subject == "" || left.subject != right.subject || left.value == "" || right.value == "" {
		return false
	}
	if left.op == "eq" && right.op == "eq" {
		return left.value != right.value
	}
	if left.op == "eq" {
		return cfgEqualityContradicts(left.value, right)
	}
	if right.op == "eq" {
		return cfgEqualityContradicts(right.value, left)
	}
	if left.op == "neq" || right.op == "neq" {
		return false
	}
	return cfgOrderedConstraintsContradict(left, right)
}

func cfgEqualityContradicts(value string, constraint cfgPredicateConstraint) bool {
	if constraint.op == "neq" {
		return value == constraint.value
	}
	comparison, ok := compareCFGNumericConstants(value, constraint.value)
	if !ok {
		return false
	}
	switch constraint.op {
	case "lt":
		return comparison >= 0
	case "le":
		return comparison > 0
	case "gt":
		return comparison <= 0
	case "ge":
		return comparison < 0
	default:
		return false
	}
}

func cfgOrderedConstraintsContradict(left, right cfgPredicateConstraint) bool {
	lower, upper, ok := cfgOrderedBounds(left, right)
	if !ok {
		lower, upper, ok = cfgOrderedBounds(right, left)
	}
	if !ok {
		return false
	}
	comparison, comparable := compareCFGNumericConstants(lower.value, upper.value)
	if !comparable {
		return false
	}
	return comparison > 0 || (comparison == 0 && (lower.op == "gt" || upper.op == "lt"))
}

func cfgOrderedBounds(
	lowerCandidate cfgPredicateConstraint,
	upperCandidate cfgPredicateConstraint,
) (cfgPredicateConstraint, cfgPredicateConstraint, bool) {
	if (lowerCandidate.op != "gt" && lowerCandidate.op != "ge") ||
		(upperCandidate.op != "lt" && upperCandidate.op != "le") {
		return cfgPredicateConstraint{}, cfgPredicateConstraint{}, false
	}
	return lowerCandidate, upperCandidate, true
}

func compareCFGNumericConstants(left, right string) (int, bool) {
	leftValue, leftOK := new(big.Rat).SetString(left)
	rightValue, rightOK := new(big.Rat).SetString(right)
	if !leftOK || !rightOK {
		return 0, false
	}
	return leftValue.Cmp(rightValue), true
}

func checkSSAConstraintEvidence(query cfgFeasibilityQuery, evidence cfgSSAConstraintEvidence) bool {
	accepted, _, _, _ := checkSSAConstraintEvidenceWithDigests(query, evidence)
	return accepted
}

func checkSSAConstraintEvidenceWithDigests(
	query cfgFeasibilityQuery,
	evidence cfgSSAConstraintEvidence,
) (bool, bool, string, string) {
	if query.Deadline == nil && query.Timeout > 0 {
		query.Deadline = newWallClockFeasibilityDeadline(query.Timeout)
	}
	expired := feasibilityDeadlineExpired(query)
	if expired || evidence.FormatVersion != cfgSSAConstraintEvidenceVersion {
		return false, expired, "", ""
	}
	formula, expired := extractSSAConstraintsForWitnessRecordWithControl(
		query.Pass,
		query.CFG,
		query.Witness,
		query.Deadline,
	)
	if expired || feasibilityDeadlineExpired(query) {
		return false, true, "", ""
	}
	accepted, expired := checkSSAConstraintFormulaEvidenceWithDeadline(
		formula,
		query.Witness.CFGPath,
		evidence,
		query.Deadline,
	)
	if expired || !accepted {
		return false, expired, "", ""
	}
	formulaDigest, expired := cfgSSAConstraintFormulaDigestWithControl(formula, query.Deadline)
	if expired {
		return false, true, "", ""
	}
	evidenceDigest, expired := cfgSSAConstraintEvidenceDigestWithControl(evidence, query.Deadline)
	if expired || feasibilityDeadlineExpired(query) {
		return false, true, "", ""
	}
	return true, false, formulaDigest, evidenceDigest
}

func checkSSAConstraintFormulaEvidence(
	formula cfgSSAConstraintFormula,
	witnessPath []int32,
	evidence cfgSSAConstraintEvidence,
) bool {
	accepted, _ := checkSSAConstraintFormulaEvidenceWithDeadline(formula, witnessPath, evidence, nil)
	return accepted
}

func checkSSAConstraintFormulaEvidenceWithDeadline(
	formula cfgSSAConstraintFormula,
	witnessPath []int32,
	evidence cfgSSAConstraintEvidence,
	deadline cfgFeasibilityDeadline,
) (bool, bool) {
	if feasibilityDeadlineReached(deadline) {
		return false, true
	}
	if evidence.FormatVersion != cfgSSAConstraintEvidenceVersion {
		return false, false
	}
	subjects, expired := ssaConstraintSubjectsWithDeadline(formula, deadline)
	if expired {
		return false, true
	}
	if !slices.Equal(evidence.WitnessPath, witnessPath) || !slices.Equal(evidence.Subjects, subjects) {
		return false, false
	}
	if len(formula.alternatives) == 0 {
		accepted := evidence.ConstantFalse && len(evidence.Contradictions) == 0
		expired := feasibilityDeadlineReached(deadline)
		return accepted && !expired, expired
	}
	if evidence.ConstantFalse || len(formula.alternatives) != len(evidence.Contradictions) {
		return false, false
	}
	for alternativeIndex, alternative := range formula.alternatives {
		if feasibilityDeadlineReached(deadline) {
			return false, true
		}
		certificate := evidence.Contradictions[alternativeIndex]
		if certificate.Alternative != alternativeIndex ||
			!constraintPresentWithDeadline(alternative, certificate.Left, deadline) ||
			!constraintPresentWithDeadline(alternative, certificate.Right, deadline) ||
			!ssaConstraintsContradict(certificate.Left, certificate.Right) {
			if feasibilityDeadlineReached(deadline) {
				return false, true
			}
			return false, false
		}
	}
	if feasibilityDeadlineReached(deadline) {
		return false, true
	}
	return true, false
}

func constraintPresentWithDeadline(
	constraints []cfgPredicateConstraint,
	target cfgPredicateConstraint,
	deadline cfgFeasibilityDeadline,
) bool {
	for _, constraint := range constraints {
		if feasibilityDeadlineReached(deadline) {
			return false
		}
		if constraint == target {
			return true
		}
	}
	return false
}
