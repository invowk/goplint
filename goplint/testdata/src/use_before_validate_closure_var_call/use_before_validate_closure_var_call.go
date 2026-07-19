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

// DirectClosureVarUseBeforeValidate remains unvalidated because the eventual
// validation error is ignored.
func DirectClosureVarUseBeforeValidate(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DirectClosureVarUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	f := func() {
		useCmd(x)
	}
	f()
	_ = x.Validate()
}

// DeferredClosureVarValidateDoesNotSuppressUBV remains unvalidated because
// the deferred closure ignores the validation error.
func DeferredClosureVarValidateDoesNotSuppressUBV(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DeferredClosureVarValidateDoesNotSuppressUBV uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	f := func() {
		_ = x.Validate()
	}
	defer f()
	useCmd(x)
}

// DirectClosureVarValidateBeforeUse is still unvalidated because direct
// execution does not make the ignored error a checked success.
func DirectClosureVarValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_closure_var_call\.DirectClosureVarValidateBeforeUse uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	f := func() {
		_ = x.Validate()
	}
	f()
	useCmd(x)
}
