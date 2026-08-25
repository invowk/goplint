// SPDX-License-Identifier: MPL-2.0

// Package generic_methods pins goplint's behavior on Go 1.27 generic methods
// (methods whose declaration carries its own type-parameter list, distinct
// from the receiver's type parameters). Coverage here is intentional: the
// analyzer's core subject is method/constructor shapes, so a new legal
// method shape entering the language must have pinned test behavior.
//
// The pinned outcome must be fail-closed. If the analyzer cannot soundly
// classify a construct here, the pinned expectation is inconclusive or
// reported — never silently safe.
package generic_methods

// Container is a generic value type. Its receiver-level type parameter T
// is transparent to primitive classification.
type Container[T any] struct {
	Items []T
}

// TransformItem exercises a Go 1.27 generic method (own type-parameter list U).
// Type-parameter positions (U, T) are transparent to primitive classification,
// so nothing here is flagged.
func (c Container[T]) TransformItem[U any](converter func(T) U) U {
	if len(c.Items) == 0 {
		var zero U
		return zero
	}
	return converter(c.Items[0])
}

// LabelAt mixes a bare primitive parameter (int) with a type-parameter return
// on a Go 1.27 generic method. The primitive parameter MUST be flagged; the
// type-parameter return position is transparent.
func (c Container[T]) LabelAt[U any](converter func(T) U, index int) U { // want `parameter "index" of generic_methods\.Container\.LabelAt uses primitive type int`
	return converter(c.Items[index])
}

// Name is a primitive-backed DDD Value Type that declares a Go 1.27 generic
// method on a non-generic receiver.
type Name string

// LabelledTransformTo pairs a type-parameter return with a bare primitive
// return. The `string` return position MUST be flagged; the type-parameter
// return position is transparent.
func (n Name) LabelledTransformTo[U any](fn func(string) U) (U, string) { // want `return value of generic_methods\.Name\.LabelledTransformTo uses primitive type string`
	return fn(string(n)), string(n)
}

// AppendItems is a Go 1.27 generic method that takes a bare slice-of-primitive
// parameter alongside a type-parameter slice. The `[]int` parameter MUST be
// flagged; the type-parameter slice position is transparent.
func (c *Container[T]) AppendItems[U any](more []T, tags []int) []T { // want `parameter "tags" of generic_methods\.Container\.AppendItems uses primitive type \[\]int`
	_ = tags
	return append(c.Items, more...)
}
