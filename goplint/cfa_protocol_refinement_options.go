// SPDX-License-Identifier: MPL-2.0

package goplint

import "time"

// cfgProtocolRefinementOptions contains only resource budgets for the
// mandatory checked SSA refinement pass.
type cfgProtocolRefinementOptions struct {
	RefinementMaxIterations int
	FeasibilityMaxQueries   int
	FeasibilityTimeout      time.Duration
	controlFactory          func(time.Duration) protocolAnalysisControl
	feasibilityBackend      cfgFeasibilityBackend
}

func (o cfgProtocolRefinementOptions) Enabled() bool {
	return true
}

func (o cfgProtocolRefinementOptions) AllowsSafeResult() bool {
	return o.Enabled()
}

func (o cfgProtocolRefinementOptions) IterationBudget() int {
	if o.RefinementMaxIterations > 0 {
		return o.RefinementMaxIterations
	}
	return defaultCFGRefinementMaxIterations
}

func (o cfgProtocolRefinementOptions) QueryBudget() int {
	if o.FeasibilityMaxQueries > 0 {
		return o.FeasibilityMaxQueries
	}
	return defaultCFGFeasibilityMaxQueries
}

func newCFGProtocolRefinementOptions(rc runConfig) cfgProtocolRefinementOptions {
	timeout := rc.cfgFeasibilityTimeoutMS
	if timeout <= 0 {
		timeout = defaultCFGFeasibilityTimeoutMS
	}
	return cfgProtocolRefinementOptions{
		RefinementMaxIterations: rc.cfgRefinementMaxIterations,
		FeasibilityMaxQueries:   rc.cfgFeasibilityMaxQueries,
		FeasibilityTimeout:      time.Duration(timeout) * time.Millisecond,
		controlFactory:          rc.protocolControlFactory,
		feasibilityBackend:      rc.protocolFeasibilityBackend,
	}
}
