// SPDX-License-Identifier: MPL-2.0

package edgecases

// ByteData has a []byte field — exempt (I/O boundary type).
type ByteData struct {
	Data []byte // no diagnostic — []byte exempt
}

// ByteParam has a []byte parameter — exempt.
func ByteParam(data []byte) {
	_ = data
}

// ChanField has a channel field — not a primitive, not flagged.
type ChanField struct {
	Ch chan int // no diagnostic
}

// FuncField has a func field — not a primitive, not flagged.
type FuncField struct {
	Fn func() string // no diagnostic
}

// StringAlias is a type alias — transparent, resolves to bare string.
type StringAlias = string

// AliasField uses a type alias — the alias is transparent, so the
// underlying string IS flagged.
type AliasField struct {
	Name StringAlias // want `struct field edgecases\.AliasField\.Name uses primitive type string`
}

// UnnamedParams has unnamed parameters — flagged as unnamed.
func UnnamedParams(string, int) {} // want `unnamed parameter of edgecases\.UnnamedParams uses primitive type string` `unnamed parameter of edgecases\.UnnamedParams uses primitive type int`

// VariadicFunc has variadic string params — internally []string, flagged.
func VariadicFunc(args ...string) { // want `parameter "args" of edgecases\.VariadicFunc uses primitive type \[\]string`
	_ = args
}

// GoStringerType has GoString() — return type exempt.
type GoStringerType struct{}

func (g GoStringerType) GoString() string { return "" } // no diagnostic

// MarshalerType has MarshalText() — return type exempt.
type MarshalerType struct{}

func (m MarshalerType) MarshalText() ([]byte, error) { return nil, nil } // no diagnostic

// StringFunc is a top-level function named String WITHOUT a receiver.
// It is NOT exempt because isInterfaceMethodReturn requires a receiver.
func StringFunc() string { return "" } // want `return value of edgecases\.StringFunc uses primitive type string`

// WrongStringerReturn has a String() method returning int — NOT fmt.Stringer.
// The int return MUST be flagged because the method doesn't match the interface.
type WrongStringerReturn struct{}

func (w WrongStringerReturn) String() int { return 0 } // want `return value of edgecases\.WrongStringerReturn\.String uses primitive type int`

// PointerPrimitive tests pointer-to-primitive types — still flagged.
type PointerPrimitive struct {
	OptionalName *string   // want `struct field edgecases\.PointerPrimitive\.OptionalName uses primitive type \*string`
	OptionalList *[]string // want `struct field edgecases\.PointerPrimitive\.OptionalList uses primitive type \*\[\]string`
}

// NestedSlice tests nested slice of slices — flagged as [][]string.
type NestedSlice struct {
	Rows [][]string // want `struct field edgecases\.NestedSlice\.Rows uses primitive type \[\]\[\]string`
}

// RenderFieldStruct tests //plint:render on struct fields — should suppress
// the finding on the annotated field, similar to //plint:ignore.
type RenderFieldStruct struct {
	Output string //plint:render -- display text
	Normal string // want `struct field edgecases\.RenderFieldStruct\.Normal uses primitive type string`
}
