// SPDX-License-Identifier: MPL-2.0

package cfa_phasec_recursion_cycle

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func recursiveValidateA(x CommandName) {
	recursiveValidateB(x)
}

func recursiveValidateB(x CommandName) {
	recursiveValidateA(x)
}

func RecursiveCycleBeforeValidate(raw string) error {
	name := CommandName(raw)
	recursiveValidateA(name)
	return name.Validate()
}
