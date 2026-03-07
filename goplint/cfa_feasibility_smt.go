// SPDX-License-Identifier: MPL-2.0

package goplint

import "context"

// cfgSMTFeasibilityBackend is a narrow satisfiability checker for extracted
// guard literals. It is intentionally conservative: unsupported predicates and
// timeouts always degrade to unknown.
type cfgSMTFeasibilityBackend struct{}

func (cfgSMTFeasibilityBackend) Check(query cfgFeasibilityQuery) (string, string) {
	timeout := query.Timeout
	if timeout <= 0 {
		return cfgFeasibilityResultUnknown, cfgFeasibilityReasonTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return cfgFeasibilityResultUnknown, cfgFeasibilityReasonTimeout
	default:
	}

	extraction := extractWitnessConstraints(query.Pass, query.CFG, query.Witness.CFGPath)
	if extraction.unsupported {
		return cfgFeasibilityResultUnknown, cfgFeasibilityReasonUnsupportedPredicate
	}
	if contradictionDetected(extraction.constraints) {
		return cfgFeasibilityResultUNSAT, cfgFeasibilityReasonNone
	}

	select {
	case <-ctx.Done():
		return cfgFeasibilityResultUnknown, cfgFeasibilityReasonTimeout
	default:
		return cfgFeasibilityResultSAT, cfgFeasibilityReasonNone
	}
}

func contradictionDetected(constraints []cfgPredicateConstraint) bool {
	type subjectState struct {
		equalValue string
		notEqual   map[string]bool
	}

	states := make(map[string]*subjectState, len(constraints))
	for _, constraint := range constraints {
		if constraint.subject == "" || constraint.value == "" {
			continue
		}
		state := states[constraint.subject]
		if state == nil {
			state = &subjectState{notEqual: make(map[string]bool)}
			states[constraint.subject] = state
		}
		switch constraint.op {
		case "eq":
			if state.notEqual[constraint.value] {
				return true
			}
			if state.equalValue != "" && state.equalValue != constraint.value {
				return true
			}
			state.equalValue = constraint.value
		case "neq":
			if state.equalValue != "" && state.equalValue == constraint.value {
				return true
			}
			state.notEqual[constraint.value] = true
		}
	}
	return false
}

func feasibilityBackendForEngine(engine string) cfgFeasibilityBackend {
	switch engine {
	case cfgFeasibilityEngineSMT:
		return cfgSMTFeasibilityBackend{}
	default:
		return nil
	}
}
