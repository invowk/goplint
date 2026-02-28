// SPDX-License-Identifier: MPL-2.0

package stringer

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

// MissingStringer has Validate but no String.
type MissingStringer string // want `named type stringer\.MissingStringer has no String\(\) method`

func (m MissingStringer) Validate() error {
	if m == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

// MissingBoth has neither Validate nor String.
type MissingBoth int // want `named type stringer\.MissingBoth has no String\(\) method`

// PointerReceiver uses *T — should still be recognized.
type PointerReceiver string

func (p *PointerReceiver) String() string { return string(*p) }

// MyStruct is a struct — checked by primary mode, not by --check-stringer.
type MyStruct struct {
	Name string // want `struct field stringer\.MyStruct\.Name uses primitive type string`
}

// WrongStringSig has String() but returns int instead of string — should
// trigger wrong-stringer-sig instead of missing-stringer.
type WrongStringSig string // want `named type stringer\.WrongStringSig has String\(\) but wrong signature`

func (w WrongStringSig) String() int { return 0 } // want `return value of stringer\.WrongStringSig\.String uses primitive type int`

// WrongStringParams has String(x int) — wrong parameter count. Also
// flagged by primary mode for param and return since it no longer matches
// the interface method exemption.
type WrongStringParams string // want `named type stringer\.WrongStringParams has String\(\) but wrong signature`

func (w WrongStringParams) String(x int) string { return "" } // want `parameter "x" of stringer\.WrongStringParams\.String uses primitive type int` `return value of stringer\.WrongStringParams\.String uses primitive type string`

// --- Lowercase string() for unexported types ---

// unexportedWithStringer has string() (lowercase) — not flagged.
type unexportedWithStringer string

func (u unexportedWithStringer) Validate() error { return nil }
func (u unexportedWithStringer) string() string { return string(u) } // want `return value of stringer\.unexportedWithStringer\.string uses primitive type string`

// unexportedMissing has neither String() nor string() — flagged.
type unexportedMissing string // want `named type stringer\.unexportedMissing has no String\(\) method`

func (u unexportedMissing) Validate() error { return nil }

// unexportedWrongSig has string() returning int — wrong sig.
type unexportedWrongSig string // want `named type stringer\.unexportedWrongSig has String\(\) but wrong signature`

func (u unexportedWrongSig) Validate() error  { return nil }
func (u unexportedWrongSig) string() int      { return 0 } // want `return value of stringer\.unexportedWrongSig\.string uses primitive type int`
