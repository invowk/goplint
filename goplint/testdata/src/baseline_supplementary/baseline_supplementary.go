// SPDX-License-Identifier: MPL-2.0

package baseline_supplementary

// --- Missing Validate: one baselined, one new ---

// BaselinedType is in the baseline for missing-validate and missing-stringer.
type BaselinedType string

// NewType is NOT in the baseline for missing-validate — should be reported.
// Its missing-stringer is also new (not baselined).
type NewType string // want `named type baseline_supplementary\.NewType has no Validate\(\) method` `named type baseline_supplementary\.NewType has no String\(\) method`

// --- Missing Stringer: one baselined, one new ---

// BaselinedStringer is in the baseline for both missing-stringer and missing-validate.
type BaselinedStringer int

// NewStringer is NOT in the baseline for missing-stringer — reported.
// Its missing-validate is also new (not baselined).
type NewStringer int // want `named type baseline_supplementary\.NewStringer has no String\(\) method` `named type baseline_supplementary\.NewStringer has no Validate\(\) method`

// --- Missing Constructor: one baselined, one new ---

// BaselinedStruct is in the baseline for missing-constructor — suppressed.
type BaselinedStruct struct {
	v int // want `struct field baseline_supplementary\.BaselinedStruct\.v uses primitive type int`
}

// NewStruct is NOT in the baseline for missing-constructor — reported.
type NewStruct struct { // want `exported struct baseline_supplementary\.NewStruct has no NewNewStruct\(\) constructor`
	v int // want `struct field baseline_supplementary\.NewStruct\.v uses primitive type int`
}
