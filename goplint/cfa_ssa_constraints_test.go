// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/types"
	"testing"
)

func TestCFGSSAComparisonSupportsNilableTypes(t *testing.T) {
	t.Parallel()

	nilable := map[string]types.Type{
		"channel":        types.NewChan(types.SendRecv, types.Typ[types.String]),
		"function":       types.NewSignatureType(nil, nil, nil, nil, nil, false),
		"interface":      types.NewInterfaceType(nil, nil).Complete(),
		"map":            types.NewMap(types.Typ[types.String], types.Typ[types.String]),
		"pointer":        types.NewPointer(types.Typ[types.String]),
		"slice":          types.NewSlice(types.Typ[types.String]),
		"unsafe pointer": types.Typ[types.UnsafePointer],
	}
	for name, typ := range nilable {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !cfgSSAComparisonSupported(typ, "<nil>") {
				t.Fatalf("nil comparison for %s = unsupported, want supported", typ)
			}
		})
	}

	nonNilable := map[string]types.Type{
		"bool":   types.Typ[types.Bool],
		"int":    types.Typ[types.Int],
		"string": types.Typ[types.String],
	}
	for name, typ := range nonNilable {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if cfgSSAComparisonSupported(typ, "<nil>") {
				t.Fatalf("nil comparison for %s = supported, want unsupported", typ)
			}
		})
	}
}
