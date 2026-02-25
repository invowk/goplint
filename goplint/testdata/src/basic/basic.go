// SPDX-License-Identifier: MPL-2.0

package basic

// GoodName is a DDD Value Type wrapping string.
type GoodName string

// GoodCount is a DDD Value Type wrapping int.
type GoodCount int

// BadStruct has fields using bare primitive types.
type BadStruct struct {
	Name string // want `struct field basic\.BadStruct\.Name uses primitive type string`
	Age  int    // want `struct field basic\.BadStruct\.Age uses primitive type int`
}

// GoodStruct uses named types — should not be flagged.
type GoodStruct struct {
	Name  GoodName
	Count GoodCount
}

// MixedStruct has both good and bad fields.
type MixedStruct struct {
	Good GoodName
	Bad  string // want `struct field basic\.MixedStruct\.Bad uses primitive type string`
}

// BoolStruct uses bool — exempt by design decision.
type BoolStruct struct {
	Verbose     bool
	Interactive bool
}

// ErrorStruct uses error — not a primitive, never flagged.
type ErrorStruct struct {
	Err error
}
