// SPDX-License-Identifier: MPL-2.0

package cfa_phasec_refinement

import "strings"

type CommandName string

func (c CommandName) Validate() error {
	return nil
}

func useCmd(_ CommandName) {}

func InfeasibleUnsafeCast(raw string) {
	name := CommandName(raw)
	if raw == "" {
		if raw != "" {
			useCmd(name)
			return
		}
	}
	_ = name.Validate()
}

func InfeasibleCrossBlockUse(raw string) error {
	name := CommandName(raw)
	if raw == "" {
		if raw != "" {
			useCmd(name)
			return nil
		}
	}
	return name.Validate()
}

func BudgetLiftUnsafe(raw string) {
	name := CommandName(raw)
	if len(raw) > 0 {
		_ = 1
	}
	_ = name
}

func UnsupportedPredicateStaysUnsafe(raw string) {
	name := CommandName(raw)
	if strings.HasPrefix(raw, "prod") {
		useCmd(name)
		return
	}
	_ = name.Validate()
}
