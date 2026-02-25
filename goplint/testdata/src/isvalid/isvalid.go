// SPDX-License-Identifier: MPL-2.0

package isvalid

// CommandName has both IsValid and String — no diagnostic.
type CommandName string

func (c CommandName) IsValid() (bool, []error) { return c != "", nil }
func (c CommandName) String() string            { return string(c) }

// RuntimeMode has IsValid — no diagnostic for isvalid check.
type RuntimeMode string

func (r RuntimeMode) IsValid() (bool, []error) { return r != "", nil }

// MissingIsValid has no IsValid method — should be flagged.
type MissingIsValid string // want `named type isvalid\.MissingIsValid has no IsValid\(\) method`

func (m MissingIsValid) String() string { return string(m) }

// MissingBoth has neither IsValid nor String.
type MissingBoth int // want `named type isvalid\.MissingBoth has no IsValid\(\) method`

// BoolBacked is backed by bool — still needs IsValid for enum semantics.
type BoolBacked bool // want `named type isvalid\.BoolBacked has no IsValid\(\) method`

// unexportedWithIsValid has lowercase isValid() — should NOT be flagged.
type unexportedWithIsValid string

func (u unexportedWithIsValid) isValid() (bool, []error) { return u != "", nil }

// unexportedMissing has no isValid/IsValid — should be flagged.
type unexportedMissing string // want `named type isvalid\.unexportedMissing has no IsValid\(\) method`

// TypeAlias is a type alias — should NOT be flagged (inherits methods).
type TypeAlias = CommandName

// MyStruct is a struct — checked by primary mode, not by --check-isvalid.
type MyStruct struct {
	Name string // want `struct field isvalid\.MyStruct\.Name uses primitive type string`
}

// MyInterface should NOT be checked by --check-isvalid.
type MyInterface interface {
	DoSomething()
}

// WrongIsValidSig has IsValid() but with the wrong signature — should
// trigger wrong-isvalid-sig instead of missing-isvalid.
type WrongIsValidSig string // want `named type isvalid\.WrongIsValidSig has IsValid\(\) but wrong signature`

func (w WrongIsValidSig) IsValid() bool { return w != "" }

// WrongIsValidParams has IsValid with a parameter — wrong signature.
type WrongIsValidParams string // want `named type isvalid\.WrongIsValidParams has IsValid\(\) but wrong signature`

func (w WrongIsValidParams) IsValid(strict bool) (bool, []error) { return w != "", nil }
