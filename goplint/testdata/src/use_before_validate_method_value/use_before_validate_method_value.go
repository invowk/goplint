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

// MethodValueUseBeforeValidate should report UBV: x is consumed before the
// bound Validate method is called.
func MethodValueUseBeforeValidate(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueUseBeforeValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	useCmd(x)
	validateFn := x.Validate
	_ = validateFn()
}

// MethodValueValidateBeforeUse should not report UBV.
func MethodValueValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueValidateBeforeUse uses primitive type string`
	x := CommandName(raw)
	validateFn := x.Validate
	_ = validateFn()
	useCmd(x)
}

// MethodValueAliasValidateBeforeUse should not report UBV when invocation goes
// through an alias variable.
func MethodValueAliasValidateBeforeUse(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueAliasValidateBeforeUse uses primitive type string`
	x := CommandName(raw)
	validateFn := x.Validate
	alias := validateFn
	_ = alias()
	useCmd(x)
}

// MethodValueDeferredValidate should report UBV: deferred method-value
// invocation does not validate before use.
func MethodValueDeferredValidate(raw string) { // want `parameter "raw" of use_before_validate_method_value\.MethodValueDeferredValidate uses primitive type string`
	x := CommandName(raw) // want `variable x of type CommandName used before Validate\(\) in same block`
	validateFn := x.Validate
	defer validateFn()
	useCmd(x)
}
