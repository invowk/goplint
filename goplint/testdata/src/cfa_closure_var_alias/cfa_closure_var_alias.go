// SPDX-License-Identifier: MPL-2.0

package cfa_closure_var_alias

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

// AliasClosureCallAnalyzed verifies closure-variable aliasing is tracked:
// g := f; g() should execute f's closure body in CFA analysis.
func AliasClosureCallAnalyzed(raw string) { // want `parameter "raw" of cfa_closure_var_alias\.AliasClosureCallAnalyzed uses primitive type string`
	f := func() {
		x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
		useCmd(x)
	}
	g := f
	g()
}

// AliasClosureValidated verifies alias calls are also recognized when the
// closure validates before use.
func AliasClosureValidated(raw string) { // want `parameter "raw" of cfa_closure_var_alias\.AliasClosureValidated uses primitive type string`
	f := func() {
		x := CommandName(raw)
		if err := x.Validate(); err != nil {
			return
		}
		useCmd(x)
	}
	g := f
	g()
}
