// SPDX-License-Identifier: MPL-2.0

package checkall // want `stale exception: pattern "StalePattern.Field" matched no diagnostics`

import "fmt"

// Mode has both Validate and String — no supplementary diagnostics.
type Mode string

func (m Mode) Validate() error {
	if m == "" {
		return fmt.Errorf("invalid mode")
	}
	return nil
}

func (m Mode) String() string { return string(m) }

// MissingAll has neither Validate nor String — flagged by both checks.
type MissingAll string // want `named type checkall\.MissingAll has no Validate\(\) method` `named type checkall\.MissingAll has no String\(\) method`

// Server is an exported struct with no constructor — flagged.
type Server struct { // want `exported struct checkall\.Server has no NewServer\(\) constructor`
	Addr string // want `struct field checkall\.Server\.Addr uses primitive type string`
}

// Client has a constructor — not flagged for missing constructor, but
// flagged for missing Validate() by --check-struct-validate.
type Client struct { // want `struct checkall\.Client has constructor but no Validate\(\) method`
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
// Also flagged for missing Validate().
type Mutable struct { // want `struct checkall\.Mutable has constructor but no Validate\(\) method`
	Name string // want `struct field checkall\.Mutable\.Name uses primitive type string` `struct checkall\.Mutable has NewMutable\(\) constructor but field Name is exported`
}

func NewMutable() *Mutable { return &Mutable{} }

// ManyParams has a constructor with too many non-option params.
// Also flagged for missing Validate().
type ManyParams struct { // want `struct checkall\.ManyParams has constructor but no Validate\(\) method`
	a int // want `struct field checkall\.ManyParams\.a uses primitive type int`
	b int // want `struct field checkall\.ManyParams\.b uses primitive type int`
	c int // want `struct field checkall\.ManyParams\.c uses primitive type int`
	d int // want `struct field checkall\.ManyParams\.d uses primitive type int`
}

func NewManyParams(a, b, c, d int) *ManyParams { return &ManyParams{a: a, b: b, c: c, d: d} } // want `constructor NewManyParams\(\) for checkall\.ManyParams has 4 non-option parameters; consider using functional options` `parameter "a" of checkall\.NewManyParams uses primitive type int` `parameter "b" of checkall\.NewManyParams uses primitive type int` `parameter "c" of checkall\.NewManyParams uses primitive type int` `parameter "d" of checkall\.NewManyParams uses primitive type int`

// --- Interface return (constructor-sig improvement) ---

// InterfaceReturnAll has a constructor returning an interface — not flagged
// for wrong-sig, but still flagged for missing Validate().
type InterfaceReturnAll struct { // want `struct checkall\.InterfaceReturnAll has constructor but no Validate\(\) method`
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
// Also flagged for missing Validate().
type WithInternalState struct { // want `struct checkall\.WithInternalState has constructor but no Validate\(\) method`
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

// WrongSigType has Validate() with wrong return and String() with wrong params.
type WrongSigType string // want `named type checkall\.WrongSigType has Validate\(\) but wrong signature` `named type checkall\.WrongSigType has String\(\) but wrong signature`

func (w WrongSigType) Validate() bool      { return w != "" }
func (w WrongSigType) String(x int) string { return "" } // want `parameter "x" of checkall\.WrongSigType\.String uses primitive type int` `return value of checkall\.WrongSigType\.String uses primitive type string`

// --- Variant constructor ---

// Metadata has a variant constructor (prefix match) — NOT flagged for missing
// constructor, but flagged for missing Validate().
type Metadata struct { // want `struct checkall\.Metadata has constructor but no Validate\(\) method`
	id string // want `struct field checkall\.Metadata\.id uses primitive type string`
}

func NewMetadataFromSource(id string) *Metadata { return &Metadata{id: id} } // want `parameter "id" of checkall\.NewMetadataFromSource uses primitive type string`

// --- Unvalidated cast (--check-cast-validation) ---

// castWithoutValidate performs a DDD type cast without calling Validate().
func castWithoutValidate(s string) Mode { // want `parameter "s" of checkall\.castWithoutValidate uses primitive type string`
	return Mode(s) // want `type conversion to Mode from non-constant without Validate\(\) check`
}

// --- Unused validate result (--check-validate-usage) ---

// discardValidateResult calls Validate() as a bare statement.
func discardValidateResult(m Mode) {
	m.Validate() // want `Validate\(\) result discarded`
}

// --- Unused constructor error (--check-constructor-error-usage) ---

// ValidatedThing has a validating constructor.
type ValidatedThing struct {
	val Mode
}

func (v *ValidatedThing) Validate() error { return v.val.Validate() }

func NewValidatedThing(s string) (*ValidatedThing, error) { // want `parameter "s" of checkall\.NewValidatedThing uses primitive type string`
	vt := &ValidatedThing{val: Mode(s)} // want `type conversion to Mode from non-constant without Validate\(\) check`
	if err := vt.Validate(); err != nil {
		return nil, err
	}
	return vt, nil
}

// discardCtorError blanks the error from a constructor.
func discardCtorError() {
	_, _ = NewValidatedThing("test") // want `constructor NewValidatedThing error return assigned to blank identifier`
}

// --- Missing constructor validate (--check-constructor-validates) ---

// Widget has Validate(), but NewWidget doesn't call it.
type Widget struct {
	label string // want `struct field checkall\.Widget\.label uses primitive type string`
}

func (w *Widget) Validate() error {
	if w.label == "" {
		return fmt.Errorf("empty label")
	}
	return nil
}

func NewWidget(label string) (*Widget, error) { // want `parameter "label" of checkall\.NewWidget uses primitive type string` `constructor checkall\.NewWidget returns checkall\.Widget which has Validate\(\) but never calls it`
	return &Widget{label: label}, nil
}

// --- Incomplete validate delegation (--check-validate-delegation) ---

//goplint:validate-all
//
// Composite has validate-all but doesn't delegate to its Mode field.
type Composite struct { // want `exported struct checkall\.Composite has no NewComposite\(\) constructor` `checkall\.Composite\.Validate\(\) does not delegate to field Name which has Validate\(\)`
	Name Mode
	Tag  MissingAll
}

func (c Composite) Validate() error {
	return nil
}

// --- Nonzero value field (--check-nonzero) ---

//goplint:nonzero
//
// NonZeroID must not be zero-valued.
type NonZeroID int // want NonZeroID:"nonzero"

func (n NonZeroID) Validate() error {
	if n == 0 {
		return fmt.Errorf("zero ID")
	}
	return nil
}

func (n NonZeroID) String() string { return fmt.Sprintf("%d", int(n)) }

// Holder uses NonZeroID as a value field (should be *NonZeroID).
type Holder struct { // want `exported struct checkall\.Holder has no NewHolder\(\) constructor`
	ID NonZeroID // want `struct field checkall\.Holder\.ID uses nonzero type NonZeroID as value`
}

// --- Constant-only type (--check-constructor-validates exemption) ---

//goplint:constant-only
type Priority string

func (p Priority) Validate() error {
	if p == "" {
		return fmt.Errorf("empty priority")
	}
	return nil
}

func (p Priority) String() string { return string(p) }

// NewPriority does NOT call Validate() — exempt because Priority is constant-only.
func NewPriority(s string) (*Priority, error) { // want `parameter "s" of checkall\.NewPriority uses primitive type string`
	p := Priority(s) // want `type conversion to Priority from non-constant without Validate\(\) check`
	return &p, nil
}
