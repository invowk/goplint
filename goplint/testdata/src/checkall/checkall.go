// SPDX-License-Identifier: MPL-2.0

package checkall // want `stale exception: pattern "StalePattern.Field" matched no diagnostics`

import "fmt"

// Mode has both IsValid and String — no supplementary diagnostics.
type Mode string

func (m Mode) IsValid() (bool, []error) { return m != "", nil }
func (m Mode) String() string            { return string(m) }

// MissingAll has neither IsValid nor String — flagged by both checks.
type MissingAll string // want `named type checkall\.MissingAll has no IsValid\(\) method` `named type checkall\.MissingAll has no String\(\) method`

// Server is an exported struct with no constructor — flagged.
type Server struct { // want `exported struct checkall\.Server has no NewServer\(\) constructor`
	Addr string // want `struct field checkall\.Server\.Addr uses primitive type string`
}

// Client has a constructor — not flagged for missing constructor, but
// flagged for missing IsValid() by --check-struct-isvalid.
type Client struct { // want `struct checkall\.Client has constructor but no IsValid\(\) method`
	host Mode
}

// NewClient is the constructor for Client.
func NewClient() *Client { return &Client{} }

// WrongSig has a constructor returning the wrong type. Also flagged for
// missing constructor since the wrong-return constructor doesn't satisfy
// the prefix match (returnTypeName != structName).
type WrongSig struct { // want `exported struct checkall\.WrongSig has no NewWrongSig\(\) constructor`
	data string // want `struct field checkall\.WrongSig\.data uses primitive type string`
}

func NewWrongSig() *Client { return nil } // want `constructor NewWrongSig\(\) for checkall\.WrongSig returns Client, expected WrongSig`

// Mutable has a constructor but an exported field — immutability violation.
// Also flagged for missing IsValid().
type Mutable struct { // want `struct checkall\.Mutable has constructor but no IsValid\(\) method`
	Name string // want `struct field checkall\.Mutable\.Name uses primitive type string` `struct checkall\.Mutable has NewMutable\(\) constructor but field Name is exported`
}

func NewMutable() *Mutable { return &Mutable{} }

// ManyParams has a constructor with too many non-option params.
// Also flagged for missing IsValid().
type ManyParams struct { // want `struct checkall\.ManyParams has constructor but no IsValid\(\) method`
	a int // want `struct field checkall\.ManyParams\.a uses primitive type int`
	b int // want `struct field checkall\.ManyParams\.b uses primitive type int`
	c int // want `struct field checkall\.ManyParams\.c uses primitive type int`
	d int // want `struct field checkall\.ManyParams\.d uses primitive type int`
}

func NewManyParams(a, b, c, d int) *ManyParams { return &ManyParams{a: a, b: b, c: c, d: d} } // want `constructor NewManyParams\(\) for checkall\.ManyParams has 4 non-option parameters; consider using functional options` `parameter "a" of checkall\.NewManyParams uses primitive type int` `parameter "b" of checkall\.NewManyParams uses primitive type int` `parameter "c" of checkall\.NewManyParams uses primitive type int` `parameter "d" of checkall\.NewManyParams uses primitive type int`

// --- Interface return (constructor-sig improvement) ---

// InterfaceReturnAll has a constructor returning an interface — not flagged
// for wrong-sig, but still flagged for missing IsValid().
type InterfaceReturnAll struct { // want `struct checkall\.InterfaceReturnAll has constructor but no IsValid\(\) method`
	v string // want `struct field checkall\.InterfaceReturnAll\.v uses primitive type string`
}

func NewInterfaceReturnAll() fmt.Stringer { return nil }

// --- Error type exclusion (missing-constructor improvement) ---

// RequestError is an error type by name — not flagged for missing constructor.
type RequestError struct {
	Detail string // want `struct field checkall\.RequestError\.Detail uses primitive type string`
}

func (e *RequestError) Error() string { return e.Detail }

// --- Internal state (func-options improvement) ---

// WithInternalState has an option type and a //plint:internal field.
// Also flagged for missing IsValid().
type WithInternalState struct { // want `struct checkall\.WithInternalState has constructor but no IsValid\(\) method`
	port string // want `struct field checkall\.WithInternalState\.port uses primitive type string`
	//plint:internal -- computed state
	derived string // want `struct field checkall\.WithInternalState\.derived uses primitive type string`
}

// WithInternalStateOption is the functional option type.
type WithInternalStateOption func(*WithInternalState)

// WithPort satisfies the port field.
func WithPort(p string) WithInternalStateOption { return func(s *WithInternalState) { s.port = p } } // want `parameter "p" of checkall\.WithPort uses primitive type string`

// NewWithInternalState creates a WithInternalState with options.
func NewWithInternalState(opts ...WithInternalStateOption) *WithInternalState {
	s := &WithInternalState{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// No WithDerived expected — derived has //plint:internal.

// --- Wrong-signature diagnostics ---

// WrongSigType has IsValid() with wrong return and String() with wrong params.
type WrongSigType string // want `named type checkall\.WrongSigType has IsValid\(\) but wrong signature` `named type checkall\.WrongSigType has String\(\) but wrong signature`

func (w WrongSigType) IsValid() bool       { return w != "" }
func (w WrongSigType) String(x int) string { return "" } // want `parameter "x" of checkall\.WrongSigType\.String uses primitive type int` `return value of checkall\.WrongSigType\.String uses primitive type string`

// --- Variant constructor ---

// Metadata has a variant constructor (prefix match) — NOT flagged for missing
// constructor, but flagged for missing IsValid().
type Metadata struct { // want `struct checkall\.Metadata has constructor but no IsValid\(\) method`
	id string // want `struct field checkall\.Metadata\.id uses primitive type string`
}

func NewMetadataFromSource(id string) *Metadata { return &Metadata{id: id} } // want `parameter "id" of checkall\.NewMetadataFromSource uses primitive type string`
