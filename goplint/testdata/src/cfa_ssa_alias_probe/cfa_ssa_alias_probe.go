// SPDX-License-Identifier: MPL-2.0

// Package cfa_ssa_alias_probe provides simple fixtures for SSA alias
// unit tests. Used by probe analyzers that inspect SSA output directly.
package cfa_ssa_alias_probe

import "strings"

type ProbeTarget string

func (p ProbeTarget) Validate() error { return nil }

func useProbe(ProbeTarget) {}

func ValidateBeforeUse(raw string) {
	x := ProbeTarget(raw)
	if err := x.Validate(); err != nil {
		return
	}
	useProbe(x)
}

type probeHolder struct {
	value ProbeTarget
}

func touch(*ProbeTarget)                 {}
func preservePointer(value *ProbeTarget) { _ = value }

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

// SamePhiAlias keeps both branch values tied to the cast identity.
func SamePhiAlias(raw string, choose bool) ProbeTarget {
	x := ProbeTarget(raw)
	y := x
	if choose {
		y = x
	} else {
		y = x
	}
	_ = y.Validate()
	return x
}

// AmbiguousPhiAlias joins the cast identity with a distinct object.
func AmbiguousPhiAlias(raw1, raw2 string, choose bool) ProbeTarget {
	x := ProbeTarget(raw1)
	other := ProbeTarget(raw2)
	y := x
	if choose {
		y = x
	} else {
		y = other
	}
	_ = y.Validate()
	return x
}

// IrrelevantPhiAlias joins two objects that never alias the tracked cast.
func IrrelevantPhiAlias(raw1, raw2, raw3 string, choose bool) ProbeTarget {
	x := ProbeTarget(raw1)
	left := ProbeTarget(raw2)
	right := ProbeTarget(raw3)
	y := left
	if choose {
		y = left
	} else {
		y = right
	}
	_ = y.Validate()
	_ = x.Validate()
	return x
}

// InterfaceAlias round-trips the cast identity through an interface.
func InterfaceAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	var boxed any = x
	y := boxed.(ProbeTarget)
	_ = y.Validate()
	return x
}

// PointerAlias stores the cast in a unique local cell and loads it through a pointer.
func PointerAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	pointer := &x
	_ = (*pointer).Validate()
	return x
}

// AmbiguousPointerAlias joins two local cells before loading through the pointer.
func AmbiguousPointerAlias(raw1, raw2 string, choose bool) ProbeTarget {
	x := ProbeTarget(raw1)
	other := ProbeTarget(raw2)
	pointer := &x
	if choose {
		pointer = &x
	} else {
		pointer = &other
	}
	_ = (*pointer).Validate()
	return x
}

// AddressAlias passes the address of the tracked local after validation.
func AddressAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	_ = x.Validate()
	touch(&x)
	return x
}

// AddressValidateAlias invokes Validate through an address-of receiver.
func AddressValidateAlias(raw string) ProbeTarget {
	x := ProbeTarget(raw)
	_ = (&x).Validate()
	return x
}

// RebasedSelectorAlias validates a field on a definitely different allocation.
func RebasedSelectorAlias(raw string) ProbeTarget {
	first := &probeHolder{}
	second := &probeHolder{}
	target := first
	target.value = ProbeTarget(raw)
	target = second
	_ = target.value.Validate()
	return first.value
}

// RebasedIndexAlias validates a static index on a definitely different slice.
func RebasedIndexAlias(raw string) ProbeTarget {
	first := []ProbeTarget{"first"}
	second := []ProbeTarget{"second"}
	target := first
	target[0] = ProbeTarget(raw)
	target = second
	_ = target[0].Validate()
	return first[0]
}

// AmbiguousPreserveAlias passes a may-alias pointer to a preserving helper.
func AmbiguousPreserveAlias(raw string, choose bool) ProbeTarget {
	x := ProbeTarget(raw)
	other := ProbeTarget("other")
	pointer := &x
	if choose {
		pointer = &x
	} else {
		pointer = &other
	}
	preservePointer(pointer)
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
