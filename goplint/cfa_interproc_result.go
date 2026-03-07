// SPDX-License-Identifier: MPL-2.0

package goplint

type interprocOutcomeClass string

const (
	interprocOutcomeSafe         interprocOutcomeClass = "safe"
	interprocOutcomeUnsafe       interprocOutcomeClass = "unsafe"
	interprocOutcomeInconclusive interprocOutcomeClass = "inconclusive"
)

type interprocPathResult struct {
	Class           interprocOutcomeClass
	Reason          pathOutcomeReason
	Witness         []int32
	WitnessHash     string
	FactFamily      ifdsFactFamily
	FactKey         string
	EdgeFunctionTag ideEdgeFuncTag
	WitnessRecord   cfgWitnessRecord
	PhaseC          cfgPhaseCResult
}

func interprocPathResultFromOutcome(
	outcome pathOutcome,
	reason pathOutcomeReason,
	witness []int32,
) interprocPathResult {
	result := interprocPathResult{
		Class:   interprocOutcomeClassFromPathOutcome(outcome),
		Reason:  reason,
		Witness: cloneCFGPath(witness),
	}
	if result.Class != interprocOutcomeInconclusive {
		result.Reason = pathOutcomeReasonNone
	}
	return result
}

func interprocOutcomeClassFromPathOutcome(outcome pathOutcome) interprocOutcomeClass {
	switch outcome {
	case pathOutcomeSafe:
		return interprocOutcomeSafe
	case pathOutcomeUnsafe:
		return interprocOutcomeUnsafe
	default:
		return interprocOutcomeInconclusive
	}
}

func (r interprocPathResult) toPathOutcome() pathOutcome {
	switch r.Class {
	case interprocOutcomeSafe:
		return pathOutcomeSafe
	case interprocOutcomeUnsafe:
		return pathOutcomeUnsafe
	default:
		return pathOutcomeInconclusive
	}
}
