// SPDX-License-Identifier: MPL-2.0

package castvalidation_method_value

import "fmt"

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

func useCmd(_ CommandName) {}

func alwaysNil() error { return nil }

type validateHolder struct {
	Validate func() error
}

// MethodValueValidate should not be flagged: calling a bound method value
// invokes Validate() on x before use.
func MethodValueValidate(raw string) { // want `parameter "raw" of castvalidation_method_value\.MethodValueValidate uses primitive type string`
	x := CommandName(raw)
	validateFn := x.Validate
	if err := validateFn(); err != nil {
		return
	}
	useCmd(x)
}

// MethodValueAliasValidate should also not be flagged when the method value
// is copied through an alias variable before invocation.
func MethodValueAliasValidate(raw string) { // want `parameter "raw" of castvalidation_method_value\.MethodValueAliasValidate uses primitive type string`
	x := CommandName(raw)
	validateFn := x.Validate
	alias := validateFn
	if err := alias(); err != nil {
		return
	}
	useCmd(x)
}

// MethodValueStoredNotCalled should be flagged because binding a method value
// alone does not validate the cast.
func MethodValueStoredNotCalled(raw string) { // want `parameter "raw" of castvalidation_method_value\.MethodValueStoredNotCalled uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	_ = validateFn
	useCmd(x)
}

// MethodValueConditionalCall should be flagged because Validate() only runs on
// one branch and therefore does not cover all return paths.
func MethodValueConditionalCall(raw string, strict bool) { // want `parameter "raw" of castvalidation_method_value\.MethodValueConditionalCall uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	if strict {
		if err := validateFn(); err != nil {
			return
		}
	}
	useCmd(x)
}

// MethodValueReboundToNonValidate should be flagged: rebinding to a non-Validate
// function must invalidate earlier method-value binding assumptions.
func MethodValueReboundToNonValidate(raw string) { // want `parameter "raw" of castvalidation_method_value\.MethodValueReboundToNonValidate uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	validateFn := x.Validate
	noop := alwaysNil
	validateFn = noop
	_ = validateFn()
	useCmd(x)
}

// MethodExpressionValidate should not be flagged: method expressions pass the
// receiver as the first argument (CommandName.Validate(x)).
func MethodExpressionValidate(raw string) { // want `parameter "raw" of castvalidation_method_value\.MethodExpressionValidate uses primitive type string`
	x := CommandName(raw)
	validateFn := CommandName.Validate
	if err := validateFn(x); err != nil {
		return
	}
	useCmd(x)
}

// SelectorStoredMethodValue should be flagged: storing CommandName(raw).Validate
// captures a method value but does not invoke it.
func SelectorStoredMethodValue(raw string) { // want `parameter "raw" of castvalidation_method_value\.SelectorStoredMethodValue uses primitive type string`
	h := validateHolder{}
	h.Validate = CommandName(raw).Validate // want `type conversion to CommandName from non-constant without Validate\(\) check`
	_ = h.Validate
}

// SelectorMethodValueValidate should not be flagged: h.Validate() invokes the
// method value bound to x.Validate.
func SelectorMethodValueValidate(raw string) { // want `parameter "raw" of castvalidation_method_value\.SelectorMethodValueValidate uses primitive type string`
	x := CommandName(raw)
	h := validateHolder{}
	h.Validate = x.Validate
	if err := h.Validate(); err != nil {
		return
	}
	useCmd(x)
}

// SelectorMethodValueRebound should be flagged: rebinding h.Validate to a
// non-Validate function must invalidate previous assumptions.
func SelectorMethodValueRebound(raw string) { // want `parameter "raw" of castvalidation_method_value\.SelectorMethodValueRebound uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	h := validateHolder{}
	h.Validate = x.Validate
	h.Validate = alwaysNil
	_ = h.Validate()
	useCmd(x)
}
