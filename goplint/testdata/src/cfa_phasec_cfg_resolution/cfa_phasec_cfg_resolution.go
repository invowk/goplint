// SPDX-License-Identifier: MPL-2.0

package cfa_phasec_cfg_resolution

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

func validateFirst(x CommandName) {
	_ = x.Validate()
}

func ClosureHelperValidateBeforeUse(raw string) {
	func(local string) {
		name := CommandName(local)
		validateFirst(name)
		useCmd(name)
	}(raw)
}
