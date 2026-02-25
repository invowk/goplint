// SPDX-License-Identifier: MPL-2.0

package returns

// MultiReturn has multiple primitive returns.
func MultiReturn() (int, string) { // want `return value of returns\.MultiReturn uses primitive type int` `return value of returns\.MultiReturn uses primitive type string`
	return 0, ""
}

// MapReturn returns a map of primitives.
func MapReturn() map[string]string { // want `return value of returns\.MapReturn uses primitive type string \(in map key and value\)`
	return nil
}

// HeteroMapReturn returns a map with different primitive key and value types.
func HeteroMapReturn() map[string]int { // want `return value of returns\.HeteroMapReturn uses primitive type string \(in map key\), int \(in map value\)`
	return nil
}

// BoolReturn returns bool — exempt.
func BoolReturn() bool {
	return false
}

// GoodName is a DDD Value Type.
type GoodName string

// GoodReturn uses named type — not flagged.
func GoodReturn() GoodName {
	return ""
}

// MethodReturn is a type with a method that returns a primitive.
type MethodReturn struct{}

func (m MethodReturn) GetName() string { // want `return value of returns\.MethodReturn\.GetName uses primitive type string`
	return ""
}

// PointerReceiverReturn uses a pointer receiver.
func (m *MethodReturn) GetID() int { // want `return value of returns\.MethodReturn\.GetID uses primitive type int`
	return 0
}

// String implements fmt.Stringer — return type is exempt.
func (m MethodReturn) String() string {
	return ""
}

// Error implements the error interface — return type is exempt.
type MyError struct{}

func (e *MyError) Error() string {
	return ""
}

// MarshalJSON implements json.Marshaler — return types are exempt.
func (m MethodReturn) MarshalJSON() ([]byte, error) {
	return nil, nil
}

// MarshalBinary implements encoding.BinaryMarshaler — return types are exempt.
func (m MethodReturn) MarshalBinary() ([]byte, error) {
	return nil, nil
}

// MarshalJSON with wrong signature is not exempt.
func (m MethodReturn) MarshalJSONWrong() (string, error) { // want `return value of returns\.MethodReturn\.MarshalJSONWrong uses primitive type string`
	return "", nil
}
