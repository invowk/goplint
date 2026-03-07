// SPDX-License-Identifier: MPL-2.0

package goplint

import "time"

// cfgPhaseCOptions captures the Phase C feasibility/refinement control plane.
// The defaults keep Phase B behavior unchanged until the new path is
// explicitly enabled on the IFDS engine.
type cfgPhaseCOptions struct {
	FeasibilityEngine       string
	RefinementMode          string
	RefinementMaxIterations int
	FeasibilityMaxQueries   int
	FeasibilityTimeout      time.Duration
}

func (o cfgPhaseCOptions) Enabled() bool {
	return o.FeasibilityEngine != cfgFeasibilityEngineOff || o.RefinementMode != cfgRefinementModeOff
}

func (o cfgPhaseCOptions) AllowsSafeResult() bool {
	return o.Enabled()
}

func (o cfgPhaseCOptions) IterationBudget() int {
	switch o.RefinementMode {
	case cfgRefinementModeOnce:
		return 1
	case cfgRefinementModeCEGAR:
		if o.RefinementMaxIterations > 0 {
			return o.RefinementMaxIterations
		}
		return defaultCFGRefinementMaxIterations
	default:
		return 0
	}
}

func (o cfgPhaseCOptions) QueryBudget() int {
	if o.FeasibilityMaxQueries > 0 {
		return o.FeasibilityMaxQueries
	}
	return defaultCFGFeasibilityMaxQueries
}

func newCFGPhaseCOptions(rc runConfig) cfgPhaseCOptions {
	timeout := rc.cfgFeasibilityTimeoutMS
	if timeout <= 0 {
		timeout = defaultCFGFeasibilityTimeoutMS
	}
	return cfgPhaseCOptions{
		FeasibilityEngine:       rc.cfgFeasibilityEngine,
		RefinementMode:          rc.cfgRefinementMode,
		RefinementMaxIterations: rc.cfgRefinementMaxIterations,
		FeasibilityMaxQueries:   rc.cfgFeasibilityMaxQueries,
		FeasibilityTimeout:      time.Duration(timeout) * time.Millisecond,
	}
}
