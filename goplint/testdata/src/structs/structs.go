// SPDX-License-Identifier: MPL-2.0

package structs

// SliceField has a slice of primitives.
type SliceField struct {
	Items []string // want `struct field structs\.SliceField\.Items uses primitive type \[\]string`
}

// MapField has a map with primitive keys and values.
type MapField struct {
	Data map[string]string // want `struct field structs\.MapField\.Data uses primitive type string \(in map key and value\)`
}

// PointerField has a pointer to a primitive.
type PointerField struct {
	Value *int // want `struct field structs\.PointerField\.Value uses primitive type \*int`
}

// FloatField has float64.
type FloatField struct {
	Score float64 // want `struct field structs\.FloatField\.Score uses primitive type float64`
}

// Uint16Field has uint16.
type Uint16Field struct {
	Port uint16 // want `struct field structs\.Uint16Field\.Port uses primitive type uint16`
}

// NamedSlice wraps a named type in a slice — should NOT be flagged.
type GoodID string

type NamedSlice struct {
	IDs []GoodID
}

// MapWithNamedKey has a named type as map key — should NOT be flagged
// for the key, but flagged for the primitive value type.
type MapWithNamedKey struct {
	Lookup map[GoodID]int // want `struct field structs\.MapWithNamedKey\.Lookup uses primitive type int \(in map value\)`
}

// HeterogeneousMap has a map with different primitive types for key and value.
type HeterogeneousMap struct {
	Data map[string]int // want `struct field structs\.HeterogeneousMap\.Data uses primitive type string \(in map key\), int \(in map value\)`
}

// MultipleFields on one line.
type MultipleFields struct {
	X, Y int // want `struct field structs\.MultipleFields\.X uses primitive type int` `struct field structs\.MultipleFields\.Y uses primitive type int`
}

// EmbeddedPrimitive has an anonymous/embedded primitive field.
type EmbeddedPrimitive struct {
	string // want `struct field structs\.EmbeddedPrimitive\.\(embedded\) uses primitive type string`
}
