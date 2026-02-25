// SPDX-License-Identifier: MPL-2.0

package structisvalid

// --- Has both constructor and IsValid — no struct-isvalid diagnostic ---

// GoodStruct has a constructor and a correct IsValid() method.
type GoodStruct struct {
	name string // want `struct field structisvalid\.GoodStruct\.name uses primitive type string`
}

func NewGoodStruct(name string) *GoodStruct { return &GoodStruct{name: name} } // want `parameter "name" of structisvalid\.NewGoodStruct uses primitive type string`

func (g *GoodStruct) IsValid() (bool, []error) { return g.name != "", nil }

// --- Has constructor but missing IsValid — flagged ---

// MissingIsValid has a constructor but no IsValid method.
type MissingIsValid struct { // want `struct structisvalid\.MissingIsValid has constructor but no IsValid\(\) method`
	data string // want `struct field structisvalid\.MissingIsValid\.data uses primitive type string`
}

func NewMissingIsValid(data string) *MissingIsValid { return &MissingIsValid{data: data} } // want `parameter "data" of structisvalid\.NewMissingIsValid uses primitive type string`

// --- No constructor — not checked ---

// NoConstructor has no NewNoConstructor() function, so it's skipped.
type NoConstructor struct {
	value int // want `struct field structisvalid\.NoConstructor\.value uses primitive type int`
}

// --- Error type (by name suffix) — excluded ---

// ValidationError is an error type by name suffix — not flagged.
type ValidationError struct {
	Detail string // want `struct field structisvalid\.ValidationError\.Detail uses primitive type string`
}

func NewValidationError(detail string) *ValidationError { return &ValidationError{Detail: detail} } // want `parameter "detail" of structisvalid\.NewValidationError uses primitive type string`

func (e *ValidationError) Error() string { return e.Detail }

// --- Error type (by method, no suffix) — excluded ---

// BadRequest implements error via Error() method — not flagged.
type BadRequest struct {
	Reason string // want `struct field structisvalid\.BadRequest\.Reason uses primitive type string`
}

func NewBadRequest(reason string) *BadRequest { return &BadRequest{Reason: reason} } // want `parameter "reason" of structisvalid\.NewBadRequest uses primitive type string`

func (b *BadRequest) Error() string { return b.Reason }

// --- Has constructor but IsValid with wrong signature — wrong-sig diagnostic ---

// WrongSigStruct has IsValid() but with wrong return type.
type WrongSigStruct struct { // want `struct structisvalid\.WrongSigStruct has IsValid\(\) but wrong signature`
	id int // want `struct field structisvalid\.WrongSigStruct\.id uses primitive type int`
}

func NewWrongSigStruct(id int) *WrongSigStruct { return &WrongSigStruct{id: id} } // want `parameter "id" of structisvalid\.NewWrongSigStruct uses primitive type int`

func (w *WrongSigStruct) IsValid() bool { return w.id > 0 }
