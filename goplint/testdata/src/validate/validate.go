// SPDX-License-Identifier: MPL-2.0

package validate

import "fmt"

// CommandName has both Validate and String — no diagnostic.
type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command name")
	}
	return nil
}

func (c CommandName) String() string { return string(c) }

// RuntimeMode has Validate — no diagnostic for validate check.
type RuntimeMode string

func (r RuntimeMode) Validate() error {
	if r == "" {
		return fmt.Errorf("invalid runtime mode")
	}
	return nil
}

// MissingValidate has no Validate method — should be flagged.
type MissingValidate string // want `named type validate\.MissingValidate has no Validate\(\) method`

func (m MissingValidate) String() string { return string(m) }

// MissingBoth has neither Validate nor String.
type MissingBoth int // want `named type validate\.MissingBoth has no Validate\(\) method`

// BoolBacked is backed by bool — still needs Validate for enum semantics.
type BoolBacked bool // want `named type validate\.BoolBacked has no Validate\(\) method`

// unexportedWithValidate has lowercase validate() — should NOT be flagged.
type unexportedWithValidate string

func (u unexportedWithValidate) validate() error {
	if u == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

// unexportedMissing has no validate/Validate — should be flagged.
type unexportedMissing string // want `named type validate\.unexportedMissing has no Validate\(\) method`

// TypeAlias is a type alias — should NOT be flagged (inherits methods).
type TypeAlias = CommandName

// MyStruct is a struct — checked by primary mode, not by --check-validate.
type MyStruct struct {
	Name string // want `struct field validate\.MyStruct\.Name uses primitive type string`
}

// MyInterface should NOT be checked by --check-validate.
type MyInterface interface {
	DoSomething()
}

// WrongValidateSig has Validate() but with the wrong signature — should
// trigger wrong-validate-sig instead of missing-validate.
type WrongValidateSig string // want `named type validate\.WrongValidateSig has Validate\(\) but wrong signature`

func (w WrongValidateSig) Validate() bool { return w != "" }

// WrongValidateParams has Validate with a parameter — wrong signature.
type WrongValidateParams string // want `named type validate\.WrongValidateParams has Validate\(\) but wrong signature`

func (w WrongValidateParams) Validate(strict bool) error {
	if w == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}
