// SPDX-License-Identifier: MPL-2.0

package baseline

// GoodName is a DDD Value Type — should not be flagged.
type GoodName string

// BaselinedStruct has fields that are in the baseline (suppressed)
// and one field that is NOT in the baseline (should be reported).
type BaselinedStruct struct {
	Known   string // suppressed by baseline — no "want" annotation
	Unknown int    // want `struct field baseline\.BaselinedStruct\.Unknown uses primitive type int`
}

// BaselinedFunc has one param in the baseline (suppressed) and one not.
func BaselinedFunc(known string, unknown int) {} // want `parameter "unknown" of baseline\.BaselinedFunc uses primitive type int`
