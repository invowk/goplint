// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/token"
	"go/types"
	"testing"
)

func TestPrimitiveMapDetail(t *testing.T) {
	t.Parallel()

	named := makeNamedType() // named string type (DDD Value Type)

	tests := []struct {
		name       string
		typ        types.Type
		wantDetail string
		wantOK     bool
	}{
		{
			name:       "map[string]string — both primitive same type",
			typ:        types.NewMap(types.Typ[types.String], types.Typ[types.String]),
			wantDetail: "string (in map key and value)",
			wantOK:     true,
		},
		{
			name:       "map[string]int — both primitive different types",
			typ:        types.NewMap(types.Typ[types.String], types.Typ[types.Int]),
			wantDetail: "string (in map key), int (in map value)",
			wantOK:     true,
		},
		{
			name:       "map[Named]int — only value primitive",
			typ:        types.NewMap(named, types.Typ[types.Int]),
			wantDetail: "int (in map value)",
			wantOK:     true,
		},
		{
			name:       "map[string]Named — only key primitive",
			typ:        types.NewMap(types.Typ[types.String], named),
			wantDetail: "string (in map key)",
			wantOK:     true,
		},
		{
			name:   "map[Named]Named — no primitives",
			typ:    types.NewMap(named, named),
			wantOK: false,
		},
		{
			name:   "non-map type (string)",
			typ:    types.Typ[types.String],
			wantOK: false,
		},
		{
			name:   "non-map type (slice)",
			typ:    types.NewSlice(types.Typ[types.String]),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			detail, ok := primitiveMapDetail(tt.typ)
			if ok != tt.wantOK {
				t.Errorf("primitiveMapDetail(%s) ok = %v, want %v", tt.typ, ok, tt.wantOK)
			}
			if detail != tt.wantDetail {
				t.Errorf("primitiveMapDetail(%s) detail = %q, want %q", tt.typ, detail, tt.wantDetail)
			}
		})
	}
}

func TestHasValidateMethod(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("test/pkg", "pkg")
	errType := types.Universe.Lookup("error").Type()

	// Helper: create a named type and add methods.
	makeTypeWithMethod := func(name string, underlying types.Type, methods ...*types.Func) *types.Named {
		tn := types.NewTypeName(token.NoPos, pkg, name, nil)
		named := types.NewNamed(tn, underlying, nil)
		for _, m := range methods {
			named.AddMethod(m)
		}
		return named
	}

	// Helper: create Validate() error function.
	makeValidateFunc := func(recv *types.Named) *types.Func {
		sig := types.NewSignatureType(
			types.NewVar(token.NoPos, pkg, "r", recv),
			nil, nil,
			nil, // no params
			types.NewTuple(
				types.NewVar(token.NoPos, pkg, "", errType),
			),
			false,
		)
		return types.NewFunc(token.NoPos, pkg, "Validate", sig)
	}

	// Type with correct Validate() error.
	goodType := makeTypeWithMethod("Good", types.Typ[types.String])
	goodType.AddMethod(makeValidateFunc(goodType))

	// Type with no methods.
	noMethodType := makeTypeWithMethod("NoMethod", types.Typ[types.String])

	// Type with wrong Validate signature: Validate() bool.
	wrongSigType := makeTypeWithMethod("WrongSig", types.Typ[types.String])
	wrongSigType.AddMethod(types.NewFunc(token.NoPos, pkg, "Validate",
		types.NewSignatureType(
			types.NewVar(token.NoPos, pkg, "r", wrongSigType),
			nil, nil,
			nil,
			types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.Bool])),
			false,
		),
	))

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		{name: "type with correct Validate", typ: goodType, want: true},
		{name: "type without Validate", typ: noMethodType, want: false},
		{name: "type with wrong signature", typ: wrongSigType, want: false},
		{name: "bare string", typ: types.Typ[types.String], want: false},
		{name: "alias of type with Validate", typ: makeAliasType(goodType), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := hasValidateMethod(tt.typ)
			if got != tt.want {
				t.Errorf("hasValidateMethod(%s) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestIsPrimitiveBasic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind types.BasicKind
		want bool
	}{
		// Exempt types
		{name: "Bool", kind: types.Bool, want: false},
		{name: "UntypedBool", kind: types.UntypedBool, want: false},

		// Flagged string types
		{name: "String", kind: types.String, want: true},
		{name: "UntypedString", kind: types.UntypedString, want: true},

		// Flagged int types
		{name: "Int", kind: types.Int, want: true},
		{name: "Int8", kind: types.Int8, want: true},
		{name: "Int16", kind: types.Int16, want: true},
		{name: "Int32", kind: types.Int32, want: true},
		{name: "Int64", kind: types.Int64, want: true},

		// Flagged uint types
		{name: "Uint", kind: types.Uint, want: true},
		{name: "Uint8", kind: types.Uint8, want: true},
		{name: "Uint16", kind: types.Uint16, want: true},
		{name: "Uint32", kind: types.Uint32, want: true},
		{name: "Uint64", kind: types.Uint64, want: true},

		// Flagged float types
		{name: "Float32", kind: types.Float32, want: true},
		{name: "Float64", kind: types.Float64, want: true},

		// Flagged untyped numeric
		{name: "UntypedInt", kind: types.UntypedInt, want: true},
		{name: "UntypedFloat", kind: types.UntypedFloat, want: true},

		// Aliases
		{name: "Byte (uint8 alias)", kind: types.Byte, want: true},
		{name: "Rune (int32 alias)", kind: types.Rune, want: true},

		// Default branch — not flagged
		{name: "Complex64", kind: types.Complex64, want: false},
		{name: "Complex128", kind: types.Complex128, want: false},
		{name: "UntypedNil", kind: types.UntypedNil, want: false},
		{name: "UntypedComplex", kind: types.UntypedComplex, want: false},
		{name: "Uintptr", kind: types.Uintptr, want: false},
		{name: "UnsafePointer", kind: types.UnsafePointer, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isPrimitiveBasic(types.Typ[tt.kind])
			if got != tt.want {
				t.Errorf("isPrimitiveBasic(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
}

// makeNamedType creates a *types.Named wrapping *types.Basic(String) for testing.
func makeNamedType() *types.Named {
	pkg := types.NewPackage("test/pkg", "pkg")
	typeName := types.NewTypeName(token.NoPos, pkg, "MyString", nil)
	named := types.NewNamed(typeName, types.Typ[types.String], nil)
	return named
}

// makeAliasType creates a *types.Alias wrapping the given type for testing.
func makeAliasType(rhs types.Type) *types.Alias {
	pkg := types.NewPackage("test/pkg", "pkg")
	aliasName := types.NewTypeName(token.NoPos, pkg, "MyAlias", nil)
	return types.NewAlias(aliasName, rhs)
}

func TestIsPrimitiveUnderlying(t *testing.T) {
	t.Parallel()

	namedString := makeNamedType() // wraps string

	// makeNamedBoolType creates a *types.Named wrapping bool.
	makeNamedBoolType := func() *types.Named {
		pkg := types.NewPackage("test/pkg", "pkg")
		tn := types.NewTypeName(token.NoPos, pkg, "MyBool", nil)
		return types.NewNamed(tn, types.Typ[types.Bool], nil)
	}

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		// Basic types — includes bool (differs from isPrimitive).
		{name: "basic string", typ: types.Typ[types.String], want: true},
		{name: "basic int", typ: types.Typ[types.Int], want: true},
		{name: "basic bool (included)", typ: types.Typ[types.Bool], want: true},
		{name: "basic float64", typ: types.Typ[types.Float64], want: true},
		{name: "untyped bool", typ: types.Typ[types.UntypedBool], want: true},

		// Named types — resolves through underlying.
		{name: "named wrapping string", typ: namedString, want: true},
		{name: "named wrapping bool", typ: makeNamedBoolType(), want: true},

		// Non-primitive underlying types.
		{name: "channel", typ: types.NewChan(types.SendRecv, types.Typ[types.Int]), want: false},
		{name: "func signature", typ: types.NewSignatureType(nil, nil, nil, nil, nil, false), want: false},
		{name: "interface", typ: types.NewInterfaceType(nil, nil), want: false},
		{name: "slice", typ: types.NewSlice(types.Typ[types.String]), want: false},

		// Aliases — transparent.
		{name: "alias of string", typ: makeAliasType(types.Typ[types.String]), want: true},
		{name: "alias of named string", typ: makeAliasType(namedString), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isPrimitiveUnderlying(tt.typ)
			if got != tt.want {
				t.Errorf("isPrimitiveUnderlying(%s) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestIsErrorType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		{
			name: "universe error type",
			typ: func() types.Type {
				// The built-in error is in the universe scope (Pkg() == nil).
				obj := types.Universe.Lookup("error")
				return obj.Type()
			}(),
			want: true,
		},
		{
			name: "named non-error type",
			typ:  makeNamedType(),
			want: false,
		},
		{
			name: "basic string",
			typ:  types.Typ[types.String],
			want: false,
		},
		{
			name: "user-defined error-named type",
			typ: func() types.Type {
				// A user type named "error" in a real package — not the built-in.
				pkg := types.NewPackage("test/pkg", "pkg")
				tn := types.NewTypeName(token.NoPos, pkg, "error", nil)
				return types.NewNamed(tn, types.Typ[types.String], nil)
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isErrorType(tt.typ)
			if got != tt.want {
				t.Errorf("isErrorType(%v) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}

func TestIsOptionFuncType(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("test/pkg", "pkg")

	// Helper to create a named struct type.
	makeStruct := func(name string) *types.Named {
		tn := types.NewTypeName(token.NoPos, pkg, name, nil)
		st := types.NewStruct(nil, nil)
		return types.NewNamed(tn, st, nil)
	}

	// Helper to create a named func type.
	makeNamedFunc := func(name string, sig *types.Signature) *types.Named {
		tn := types.NewTypeName(token.NoPos, pkg, name, nil)
		return types.NewNamed(tn, sig, nil)
	}

	targetStruct := makeStruct("Server")

	tests := []struct {
		name           string
		typ            types.Type
		wantTarget     string
		wantOK         bool
	}{
		{
			name: "valid option func(*Server)",
			typ: makeNamedFunc("ServerOption",
				types.NewSignatureType(nil, nil, nil,
					types.NewTuple(types.NewVar(token.NoPos, pkg, "s", types.NewPointer(targetStruct))),
					nil, false)),
			wantTarget: "Server",
			wantOK:     true,
		},
		{
			name:   "non-named type (bare signature)",
			typ:    types.NewSignatureType(nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, pkg, "s", types.NewPointer(targetStruct))), nil, false),
			wantOK: false,
		},
		{
			name: "wrong param count (0 params)",
			typ: makeNamedFunc("NoParamFunc",
				types.NewSignatureType(nil, nil, nil, nil, nil, false)),
			wantOK: false,
		},
		{
			name: "has results (should have none)",
			typ: makeNamedFunc("HasResults",
				types.NewSignatureType(nil, nil, nil,
					types.NewTuple(types.NewVar(token.NoPos, pkg, "s", types.NewPointer(targetStruct))),
					types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.String])),
					false)),
			wantOK: false,
		},
		{
			name: "variadic param",
			typ: makeNamedFunc("VariadicFunc",
				types.NewSignatureType(nil, nil, nil,
					types.NewTuple(types.NewVar(token.NoPos, pkg, "s", types.NewSlice(types.NewPointer(targetStruct)))),
					nil, true)),
			wantOK: false,
		},
		{
			name: "non-pointer param (Server, not *Server)",
			typ: makeNamedFunc("ValueFunc",
				types.NewSignatureType(nil, nil, nil,
					types.NewTuple(types.NewVar(token.NoPos, pkg, "s", targetStruct)),
					nil, false)),
			wantOK: false,
		},
		{
			name: "pointer to non-struct (named string type)",
			typ: makeNamedFunc("BadTarget",
				types.NewSignatureType(nil, nil, nil,
					types.NewTuple(types.NewVar(token.NoPos, pkg, "s", types.NewPointer(makeNamedType()))),
					nil, false)),
			wantOK: false,
		},
		{
			name: "named type but not func underlying",
			typ: func() types.Type {
				tn := types.NewTypeName(token.NoPos, pkg, "NotFunc", nil)
				return types.NewNamed(tn, types.Typ[types.String], nil)
			}(),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotTarget, gotOK := isOptionFuncType(tt.typ)
			if gotOK != tt.wantOK {
				t.Errorf("isOptionFuncType() ok = %v, want %v", gotOK, tt.wantOK)
			}
			if gotTarget != tt.wantTarget {
				t.Errorf("isOptionFuncType() target = %q, want %q", gotTarget, tt.wantTarget)
			}
		})
	}
}

func TestFormatResultTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results *types.Tuple
		want    string
	}{
		{
			name:    "nil tuple",
			results: nil,
			want:    "",
		},
		{
			name:    "empty tuple",
			results: types.NewTuple(),
			want:    "",
		},
		{
			name: "single result (string)",
			results: types.NewTuple(
				types.NewVar(token.NoPos, nil, "", types.Typ[types.String]),
			),
			want: "string",
		},
		{
			name: "two results (bool, []error)",
			results: types.NewTuple(
				types.NewVar(token.NoPos, nil, "", types.Typ[types.Bool]),
				types.NewVar(token.NoPos, nil, "", types.NewSlice(
					types.Universe.Lookup("error").Type(),
				)),
			),
			want: "bool,[]error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatResultTypes(tt.results)
			if got != tt.want {
				t.Errorf("formatResultTypes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsPrimitive(t *testing.T) {
	t.Parallel()

	named := makeNamedType()

	tests := []struct {
		name string
		typ  types.Type
		want bool
	}{
		// Basic types
		{name: "bare string", typ: types.Typ[types.String], want: true},
		{name: "bare int", typ: types.Typ[types.Int], want: true},
		{name: "bare bool (exempt)", typ: types.Typ[types.Bool], want: false},

		// Pointer types
		{name: "pointer to string", typ: types.NewPointer(types.Typ[types.String]), want: true},
		{name: "pointer to bool", typ: types.NewPointer(types.Typ[types.Bool]), want: false},
		{name: "pointer to named", typ: types.NewPointer(named), want: false},

		// Slice types
		{name: "slice of string", typ: types.NewSlice(types.Typ[types.String]), want: true},
		{name: "slice of int", typ: types.NewSlice(types.Typ[types.Int]), want: true},
		{name: "slice of byte (exempt)", typ: types.NewSlice(types.Typ[types.Byte]), want: false},
		{name: "slice of named", typ: types.NewSlice(named), want: false},

		// Map types
		{name: "map[string]string", typ: types.NewMap(types.Typ[types.String], types.Typ[types.String]), want: true},
		{name: "map[named]string (value prim)", typ: types.NewMap(named, types.Typ[types.String]), want: true},
		{name: "map[string]named (key prim)", typ: types.NewMap(types.Typ[types.String], named), want: true},
		{name: "map[named]named", typ: types.NewMap(named, named), want: false},

		// Named type
		{name: "named type", typ: named, want: false},

		// Alias type — transparent, resolves to underlying
		{name: "alias of string", typ: makeAliasType(types.Typ[types.String]), want: true},
		{name: "alias of bool", typ: makeAliasType(types.Typ[types.Bool]), want: false},
		{name: "alias of named", typ: makeAliasType(named), want: false},

		// Default branch: channel, func, interface
		{name: "channel", typ: types.NewChan(types.SendRecv, types.Typ[types.Int]), want: false},
		{name: "signature (func type)", typ: types.NewSignatureType(nil, nil, nil, nil, nil, false), want: false},
		{name: "interface", typ: types.NewInterfaceType(nil, nil), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isPrimitive(tt.typ)
			if got != tt.want {
				t.Errorf("isPrimitive(%s) = %v, want %v", tt.typ, got, tt.want)
			}
		})
	}
}
