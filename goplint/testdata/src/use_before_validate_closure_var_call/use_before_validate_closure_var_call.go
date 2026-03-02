// SPDX-License-Identifier: MPL-2.0

package use_before_validate_closure_var_call

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

// DirectClosureVarUseBeforeValidate should report UBV because the direct
// closure-variable call uses x before x.Validate() is called.
func DirectClosureVarUseBeforeValidate(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DirectClosureVarUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	f := func() {
		useCmd(x)
	}
	f()
	_ = x.Validate()
}

// DeferredClosureVarValidateDoesNotSuppressUBV should report UBV because
// deferred validation happens after use in the same block.
func DeferredClosureVarValidateDoesNotSuppressUBV(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DeferredClosureVarValidateDoesNotSuppressUBV uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	f := func() {
		_ = x.Validate()
	}
	defer f()
	useCmd(x)
}

// DirectClosureVarValidateBeforeUse should not report UBV because direct
// closure-variable validation executes before use.
func DirectClosureVarValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DirectClosureVarValidateBeforeUse uses primitive type string`
	x := CommandName(raw)
	f := func() {
		_ = x.Validate()
	}
	f()
	useCmd(x)
}
