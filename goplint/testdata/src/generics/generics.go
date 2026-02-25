// SPDX-License-Identifier: MPL-2.0

package generics

// Container is a generic type.
type Container[T any] struct {
	Items []T // no diagnostic — T is a type parameter, not a primitive
}

// GetName returns a primitive string — should be flagged.
func (c Container[T]) GetName() string { return "" } // want `return value of generics\.Container\.GetName uses primitive type string`

// SetLabel takes a primitive string param — should be flagged.
func (c *Container[T]) SetLabel(label string) { // want `parameter "label" of generics\.Container\.SetLabel uses primitive type string`
	_ = label
}

// Pair is a generic type with two type params.
type Pair[K comparable, V any] struct {
	Key   K // no diagnostic — type parameter
	Value V // no diagnostic — type parameter
}

// GetKey returns a primitive — flagged.
func (p Pair[K, V]) Description() string { return "" } // want `return value of generics\.Pair\.Description uses primitive type string`
