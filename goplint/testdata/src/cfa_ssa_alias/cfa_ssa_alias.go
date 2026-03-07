// SPDX-License-Identifier: MPL-2.0

// Package cfa_ssa_alias tests SSA-based must-alias tracking for cast validation.
// With --cfg-alias-mode=ssa, validating a copy-alias of a cast target
// should discharge the cast's validation requirement.
package cfa_ssa_alias

// AliasTarget is a DDD Value Type requiring validation.
type AliasTarget string

func (a AliasTarget) Validate() error { return nil }
func (a AliasTarget) String() string  { return string(a) }

func useTarget(_ AliasTarget) {}

// CopyAliasValidated: y := x; y.Validate() should discharge x.
// With --cfg-alias-mode=ssa, this should NOT be flagged for cast validation.
func CopyAliasValidated(raw string) AliasTarget { // want `parameter "raw" .* uses primitive type string`
	x := AliasTarget(raw)
	y := x
	_ = y.Validate()
	return x
}

// MultiHopAlias: y := x; z := y; z.Validate() discharges x.
// With --cfg-alias-mode=ssa, this should NOT be flagged.
func MultiHopAlias(raw string) AliasTarget { // want `parameter "raw" .* uses primitive type string`
	x := AliasTarget(raw)
	y := x
	z := y
	_ = z.Validate()
	return x
}

// NoAlias: y is a different cast, not an alias of x.
// x should still be flagged.
func NoAlias(raw1, raw2 string) AliasTarget { // want `parameter "raw1" .* uses primitive type string` `parameter "raw2" .* uses primitive type string`
	x := AliasTarget(raw1) // want `type conversion to AliasTarget from non-constant without Validate`
	y := AliasTarget(raw2)
	_ = y.Validate()
	return x
}

// ReassignmentBreaksAlias: y := x; y = AliasTarget(raw2); y.Validate()
// does not discharge x because y was reassigned.
func ReassignmentBreaksAlias(raw1, raw2 string) AliasTarget { // want `parameter "raw1" .* uses primitive type string` `parameter "raw2" .* uses primitive type string`
	x := AliasTarget(raw1) // want `type conversion to AliasTarget from non-constant without Validate`
	y := x
	y = AliasTarget(raw2)
	_ = y.Validate()
	return x
}

// DirectValidation: standard case, x.Validate() called directly.
// Should NOT be flagged (baseline behavior).
func DirectValidation(raw string) AliasTarget { // want `parameter "raw" .* uses primitive type string`
	x := AliasTarget(raw)
	_ = x.Validate()
	return x
}

// PartialBranchAlias: alias is only validated on one branch.
// x should still be flagged.
func PartialBranchAlias(raw string, cond bool) AliasTarget { // want `parameter "raw" .* uses primitive type string`
	x := AliasTarget(raw) // want `type conversion to AliasTarget from non-constant without Validate`
	y := x
	if cond {
		_ = y.Validate()
	}
	return x
}
