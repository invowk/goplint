// SPDX-License-Identifier: MPL-2.0

package structvalidate

import "fmt"

// --- Has both constructor and Validate — no struct-isvalid diagnostic ---

// GoodStruct has a constructor and a correct Validate() method.
type GoodStruct struct {
	name string // want `struct field structvalidate\.GoodStruct\.name uses primitive type string`
}

func NewGoodStruct(name string) *GoodStruct { return &GoodStruct{name: name} } // want `parameter "name" of structvalidate\.NewGoodStruct uses primitive type string`

func (g *GoodStruct) Validate() error {
	if g.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// --- Has constructor but missing Validate — flagged ---

// MissingValidate has a constructor but no Validate method.
type MissingValidate struct { // want `struct structvalidate\.MissingValidate has constructor but no Validate\(\) method`
	data string // want `struct field structvalidate\.MissingValidate\.data uses primitive type string`
}

func NewMissingValidate(data string) *MissingValidate { return &MissingValidate{data: data} } // want `parameter "data" of structvalidate\.NewMissingValidate uses primitive type string`

// --- No constructor — not checked ---

// NoConstructor has no NewNoConstructor() function, so it's skipped.
type NoConstructor struct {
	value int // want `struct field structvalidate\.NoConstructor\.value uses primitive type int`
}

// --- Error type (by name suffix) — excluded ---

// ValidationError is an error type by name suffix — not flagged.
type ValidationError struct {
	Detail string // want `struct field structvalidate\.ValidationError\.Detail uses primitive type string`
}

func NewValidationError(detail string) *ValidationError { return &ValidationError{Detail: detail} } // want `parameter "detail" of structvalidate\.NewValidationError uses primitive type string`

func (e *ValidationError) Error() string { return e.Detail }

// --- Error type (by method, no suffix) — excluded ---

// BadRequest implements error via Error() method — not flagged.
type BadRequest struct {
	Reason string // want `struct field structvalidate\.BadRequest\.Reason uses primitive type string`
}

func NewBadRequest(reason string) *BadRequest { return &BadRequest{Reason: reason} } // want `parameter "reason" of structvalidate\.NewBadRequest uses primitive type string`

func (b *BadRequest) Error() string { return b.Reason }

// --- Has constructor but Validate with wrong signature — wrong-sig diagnostic ---

// WrongSigStruct has Validate() but with wrong return type.
type WrongSigStruct struct { // want `struct structvalidate\.WrongSigStruct has Validate\(\) but wrong signature`
	id int // want `struct field structvalidate\.WrongSigStruct\.id uses primitive type int`
}

func NewWrongSigStruct(id int) *WrongSigStruct { return &WrongSigStruct{id: id} } // want `parameter "id" of structvalidate\.NewWrongSigStruct uses primitive type int`

func (w *WrongSigStruct) Validate() bool { return w.id > 0 }
