// SPDX-License-Identifier: MPL-2.0

// Package cfa_ssa_alias_gate provides alias-sensitive CFA fixtures for the
// dedicated Phase D gate tests. It intentionally omits analysistest `want`
// comments because the gate compares diagnostics across alias modes directly.
package cfa_ssa_alias_gate

import "strings"

type AliasTarget string

func (a AliasTarget) Validate() error { return nil }
func (a AliasTarget) String() string  { return string(a) }

func useTarget(_ AliasTarget) {}

// NestedAliasValidated should stop reporting unvalidated-cast once alias mode
// recognizes that y validates the cast assigned to x.
func NestedAliasValidated(raw string) AliasTarget {
	x := AliasTarget(strings.TrimSpace(raw))
	y := x
	_ = y.Validate()
	return x
}

// ClosureAliasValidated exercises the closure-specific SSA lookup path.
func ClosureAliasValidated(raw string) AliasTarget {
	run := func() AliasTarget {
		x := AliasTarget(strings.TrimSpace(raw))
		y := x
		_ = y.Validate()
		return x
	}
	return run()
}

// AliasValidateSuppressesCast should stop reporting cast-validation once alias
// mode recognizes the Validate call on y.
func AliasValidateSuppressesCast(raw string) AliasTarget {
	x := AliasTarget(strings.TrimSpace(raw))
	y := x
	_ = y.Validate()
	useTarget(x)
	return x
}

// AliasUseBeforeValidate should start reporting UBV once alias mode recognizes
// that y aliases x at the use site.
func AliasUseBeforeValidate(raw string) AliasTarget {
	x := AliasTarget(strings.TrimSpace(raw))
	y := x
	useTarget(y)
	_ = x.Validate()
	return x
}
