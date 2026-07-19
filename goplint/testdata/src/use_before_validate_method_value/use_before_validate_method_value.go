// SPDX-License-Identifier: MPL-2.0

package use_before_validate_method_value

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

// MethodValueUseBeforeValidate remains unvalidated because the bound method's
// error result is ignored.
func MethodValueUseBeforeValidate(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	useCmd(x)
	validateFn := x.Validate
	_ = validateFn()
}

// MethodValueValidateBeforeUse remains unvalidated because its error is ignored.
func MethodValueValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueValidateBeforeUse uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	_ = validateFn()
	useCmd(x)
}

// MethodValueAliasValidateBeforeUse remains unvalidated through the alias
// because its error is ignored.
func MethodValueAliasValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueAliasValidateBeforeUse uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	alias := validateFn
	_ = alias()
	useCmd(x)
}

// MethodValueDeferredValidate remains unvalidated because the deferred
// method-value result is ignored.
func MethodValueDeferredValidate(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueDeferredValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	defer validateFn()
	useCmd(x)
}
