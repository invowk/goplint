// SPDX-License-Identifier: MPL-2.0

package cfa_closure_var_rebind

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

func noValidate() {}

// RebindingTracksCallSite verifies closure-variable calls resolve bindings at
// each call site, not by the last assignment in the function.
func RebindingTracksCallSite(raw string) { // want `parameter "raw" of cfa_closure_var_rebind\.RebindingTracksCallSite uses primitive type string`
	f := func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	f()

	f = func() {
		y := CommandName(raw)
		if err := y.Validate(); err != nil {
			return
		}
		useCmd(y)
	}
	f()
}

// OuterCastValidatedViaClosureVar verifies direct closure-variable calls can
// satisfy outer-path validation checks.
func OuterCastValidatedViaClosureVar(raw string) { // want `parameter "raw" of cfa_closure_var_rebind\.OuterCastValidatedViaClosureVar uses primitive type string`
	x := CommandName(raw)
	f := func() {
		_ = x.Validate()
	}
	f()
	useCmd(x)
}

// RebindingToNonClosureInvalidates verifies that rebinding a closure variable to
// a non-closure function invalidates prior closure-based Validate tracking.
func RebindingToNonClosureInvalidates(raw string) { // want `parameter "raw" of cfa_closure_var_rebind\.RebindingToNonClosureInvalidates uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	f := func() {
		_ = x.Validate()
	}
	noOp := noValidate
	f = noOp
	f()
	useCmd(x)
}
