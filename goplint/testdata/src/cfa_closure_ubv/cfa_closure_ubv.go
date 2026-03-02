// SPDX-License-Identifier: MPL-2.0

package cfa_closure_ubv

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

func ClosureSameBlockUBV(raw string) { // want `parameter "raw" of cfa_closure_ubv\.ClosureSameBlockUBV uses primitive type string`
	go func() {
		x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
		useCmd(x)
		_ = x.Validate()
	}()
}

func ClosureCrossBlockUBV(raw string, ok bool) { // want `parameter "raw" of cfa_closure_ubv\.ClosureCrossBlockUBV uses primitive type string`
	go func() {
		x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) across blocks`
		if ok {
			useCmd(x)
		}
		_ = x.Validate()
		useCmd(x)
	}()
}

func ClosureUnassignedCast(raw string) { // want `parameter "raw" of cfa_closure_ubv\.ClosureUnassignedCast uses primitive type string`
	go func() {
		useCmd(CommandName(raw)) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	}()
}

func ClosureIgnoredCast(raw string) { // want `parameter "raw" of cfa_closure_ubv\.ClosureIgnoredCast uses primitive type string`
	go func() {
		useCmd(CommandName(raw)) //goplint:ignore -- intentional coverage for suppression branch
	}()
}
