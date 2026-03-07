// SPDX-License-Identifier: MPL-2.0

// Package cfa_ssa_alias_probe provides simple fixtures for SSA alias
// unit tests. Used by probe analyzers that inspect SSA output directly.
package cfa_ssa_alias_probe

import "strings"

type ProbeTarget string

func (p ProbeTarget) Validate() error { return nil }

// CopyAlias: y should alias x (both point to same SSA value).
func CopyAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	y := x
	_ = y.Validate()
	return x
}

// ReassignedAlias: y := x; y = other should exclude y from alias set.
func ReassignedAlias(raw1, raw2 string) ProbeTarget {
	x := ProbeTarget(raw1)
	y := x
	y = ProbeTarget(raw2)
	_ = y.Validate()
	return x
}

// NestedCallAlias keeps the cast wrapped around a helper call so the alias
// matcher must distinguish the cast result from inner SSA call values.
func NestedCallAlias(raw string) ProbeTarget {
	x := ProbeTarget(strings.TrimSpace(raw))
	y := x
	_ = y.Validate()
	return x
}

// OverflowAlias creates more aliases than the conservative tracking budget
// allows, so computeMustAliasKeys should return nil.
func OverflowAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	a01 := x
	a02 := a01
	a03 := a02
	a04 := a03
	a05 := a04
	a06 := a05
	a07 := a06
	a08 := a07
	a09 := a08
	a10 := a09
	a11 := a10
	a12 := a11
	a13 := a12
	a14 := a13
	a15 := a14
	a16 := a15
	a17 := a16
	a18 := a17
	a19 := a18
	a20 := a19
	a21 := a20
	a22 := a21
	a23 := a22
	a24 := a23
	a25 := a24
	a26 := a25
	a27 := a26
	a28 := a27
	a29 := a28
	a30 := a29
	a31 := a30
	a32 := a31
	a33 := a32
	_ = a33.Validate()
	return x
}
