// SPDX-License-Identifier: MPL-2.0

package generics_structural

// GoodContainer is a generic struct with a correct constructor — no flag.
type GoodContainer[T any] struct {
	items []T
}

func NewGoodContainer[T any]() *GoodContainer[T] { return &GoodContainer[T]{} }

// WrongReturn is a generic struct whose constructor returns a different type.
type WrongReturn[T any] struct {
	data []T
}

func NewWrongReturn[T any]() *GoodContainer[T] { return nil } // want `constructor NewWrongReturn\(\) for generics_structural\.WrongReturn returns GoodContainer, expected WrongReturn`

// MutableGeneric has a constructor but an exported field — immutability flag.
type MutableGeneric[T any] struct {
	Items []T // want `struct generics_structural\.MutableGeneric has NewMutableGeneric\(\) constructor but field Items is exported`
}

func NewMutableGeneric[T any]() *MutableGeneric[T] { return &MutableGeneric[T]{} }

// ImmutableGeneric has all unexported fields — no immutability flag.
type ImmutableGeneric[T any] struct {
	items []T
}

func NewImmutableGeneric[T any]() *ImmutableGeneric[T] { return &ImmutableGeneric[T]{} }

// NoCtorGeneric has no constructor — not checked by structural modes.
type NoCtorGeneric[T any] struct {
	Items []T
}
