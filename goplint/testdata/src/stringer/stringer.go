// SPDX-License-Identifier: MPL-2.0

package stringer

// CommandName has both IsValid and String — no diagnostic.
type CommandName string

func (c CommandName) IsValid() (bool, []error) { return c != "", nil }
func (c CommandName) String() string            { return string(c) }

// MissingStringer has IsValid but no String.
type MissingStringer string // want `named type stringer\.MissingStringer has no String\(\) method`

func (m MissingStringer) IsValid() (bool, []error) { return m != "", nil }

// MissingBoth has neither IsValid nor String.
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
