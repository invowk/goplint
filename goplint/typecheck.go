// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// isPrimitive reports whether t is a bare primitive type that should be
// replaced with a DDD Value Type. Named types wrapping primitives (e.g.,
// type CommandName string) return false — they ARE the Value Types.
//
// bool is exempt per design decision: creating type Verbose bool has
// marginal DDD value.
func isPrimitive(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic:
		return isPrimitiveBasic(t)
	case *types.Pointer:
		return isPrimitive(t.Elem())
	case *types.Slice:
		// []byte is an I/O boundary type, not a domain type.
		// Unalias the element type first so type aliases to byte
		// (e.g., type B = byte) are correctly recognized.
		elem := types.Unalias(t.Elem())
		if basic, ok := elem.(*types.Basic); ok && basic.Kind() == types.Byte {
			return false
		}
		return isPrimitive(t.Elem())
	case *types.Map:
		return isPrimitive(t.Key()) || isPrimitive(t.Elem())
	case *types.Named:
		// Named types are DDD Value Types — never flagged.
		return false
	case *types.Alias:
		// Type aliases (type X = string) are transparent — check
		// the underlying type they resolve to.
		return isPrimitive(types.Unalias(t))
	default:
		// Interfaces, channels, funcs, structs, etc.
		return false
	}
}

// isPrimitiveBasic checks whether a basic type is one we flag.
// Bool and untyped bool are exempt.
func isPrimitiveBasic(t *types.Basic) bool {
	switch t.Kind() {
	case types.Bool, types.UntypedBool:
		return false
	case types.String, types.UntypedString:
		return true
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		return true
	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
		return true
	case types.Float32, types.Float64:
		return true
	case types.UntypedInt, types.UntypedFloat:
		return true
	default:
		return false
	}
}

// isPrimitiveUnderlying reports whether t resolves to a basic primitive type.
// Used by --check-validate and --check-stringer to restrict checks to types
// backed by string, int, etc. — skipping func types, channels, and other
// non-primitive underlying types that don't need Validate/String methods.
func isPrimitiveUnderlying(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic:
		return isPrimitiveBasic(t) || t.Kind() == types.Bool || t.Kind() == types.UntypedBool
	case *types.Named:
		return isPrimitiveUnderlying(t.Underlying())
	case *types.Alias:
		return isPrimitiveUnderlying(types.Unalias(t))
	default:
		return false
	}
}

// primitiveTypeName returns a human-readable name for the primitive type
// detected in a finding. For composite types (slices, maps, pointers),
// it shows the full composite form. Type aliases are resolved to their
// underlying type so the diagnostic shows the actual primitive.
func primitiveTypeName(t types.Type) string {
	return types.TypeString(types.Unalias(t), nil)
}

// resolveReturnTypeName resolves the first non-error return type of a
// function's result list. Returns the type name after dereferencing pointers
// (e.g., *Config → "Config"). Returns "" for void functions or when no
// non-error return type can be resolved. Interface returns are detected
// separately by returnsInterface().
func resolveReturnTypeName(pass *analysis.Pass, results *ast.FieldList) string {
	if results == nil || len(results.List) == 0 {
		return ""
	}

	for _, field := range results.List {
		resolved := pass.TypesInfo.TypeOf(field.Type)
		if resolved == nil {
			continue
		}

		// Skip error return types.
		if isErrorType(resolved) {
			continue
		}

		// Dereference pointer: *Config → Config.
		if ptr, ok := resolved.(*types.Pointer); ok {
			resolved = ptr.Elem()
		}

		// Resolve aliases.
		resolved = types.Unalias(resolved)

		// Extract name from named types.
		if named, ok := resolved.(*types.Named); ok {
			return named.Obj().Name()
		}

		// Bare primitive or other non-named type — return the type string
		// so the diagnostic can show what the constructor actually returns.
		return types.TypeString(resolved, nil)
	}

	return ""
}

// returnsInterface reports whether the first non-error return type of a
// function result list is an interface. Interface returns are valid factory
// patterns (e.g., NewFoo() returning io.Reader) and should not be flagged
// as wrong return types.
func returnsInterface(pass *analysis.Pass, results *ast.FieldList) bool {
	if results == nil || len(results.List) == 0 {
		return false
	}

	for _, field := range results.List {
		resolved := pass.TypesInfo.TypeOf(field.Type)
		if resolved == nil {
			continue
		}
		if isErrorType(resolved) {
			continue
		}
		if ptr, ok := resolved.(*types.Pointer); ok {
			resolved = ptr.Elem()
		}
		resolved = types.Unalias(resolved)
		if named, ok := resolved.(*types.Named); ok {
			_, isIface := named.Underlying().(*types.Interface)
			return isIface
		}
		return false
	}
	return false
}

// isErrorType reports whether t is the built-in error interface.
func isErrorType(t types.Type) bool {
	// The error type is a named interface in the universe scope.
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name() == "error" && named.Obj().Pkg() == nil
	}
	return false
}

// primitiveMapDetail decomposes a map type and returns a targeted diagnostic
// message identifying which part(s) of the map are primitive. For maps with
// both primitive key and value, it reports the underlying primitive type with
// "(in map key and value)". For mixed maps, it specifies which part is the
// problem. Returns ("", false) for non-map types or maps with no primitive
// parts.
func primitiveMapDetail(t types.Type) (string, bool) {
	m, ok := types.Unalias(t).(*types.Map)
	if !ok {
		return "", false
	}

	keyPrim := isPrimitive(m.Key())
	valPrim := isPrimitive(m.Elem())

	if !keyPrim && !valPrim {
		return "", false
	}

	switch {
	case keyPrim && valPrim:
		// Both parts are primitive — if same type, use singular form.
		keyName := primitiveTypeName(m.Key())
		valName := primitiveTypeName(m.Elem())
		if keyName == valName {
			return keyName + " (in map key and value)", true
		}
		// Different primitives — report both types so neither is lost.
		return keyName + " (in map key), " + valName + " (in map value)", true
	case keyPrim:
		return primitiveTypeName(m.Key()) + " (in map key)", true
	default:
		return primitiveTypeName(m.Elem()) + " (in map value)", true
	}
}

// hasValidateMethod reports whether t is a named type with a
// Validate() error method, indicating it is a DDD Value Type
// that should be validated after construction from raw primitives.
// Checks both value and pointer receiver method sets.
func hasValidateMethod(t types.Type) bool {
	t = types.Unalias(t)
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	// Check both value and pointer receiver method sets.
	for _, mset := range []*types.MethodSet{
		types.NewMethodSet(named),
		types.NewMethodSet(types.NewPointer(named)),
	} {
		for method := range mset.Methods() {
			if method.Obj().Name() != "Validate" {
				continue
			}
			sig, ok := method.Obj().Type().(*types.Signature)
			if !ok {
				continue
			}
			// Must have 0 params and 1 result: error.
			if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
				continue
			}
			if isErrorType(sig.Results().At(0).Type()) {
				return true
			}
		}
	}
	return false
}

// hasValidatableElements reports whether t is a slice, array, or map type
// whose element type has a Validate() error method. Used by validate-delegation
// to recognize range-loop delegation patterns like:
//
//	for _, r := range c.Requires { r.Validate() }
//	for _, v := range c.Items { v.Validate() }
//
// For maps, only the value type is checked — map keys are typically lookup
// identifiers rather than validatable domain values.
func hasValidatableElements(t types.Type) bool {
	t = types.Unalias(t)
	switch ct := t.(type) {
	case *types.Slice:
		return hasValidateMethod(ct.Elem())
	case *types.Array:
		return hasValidateMethod(ct.Elem())
	case *types.Map:
		return hasValidateMethod(ct.Elem())
	default:
		return false
	}
}

// isOptionFuncType checks whether t is a named type whose underlying type
// is a function signature taking exactly one pointer-to-struct parameter.
// This detects the functional options pattern: type XxxOption func(*Xxx).
// Returns the target struct name and true if the pattern matches.
func isOptionFuncType(t types.Type) (targetStructName string, ok bool) {
	// Must be a named type (type XxxOption func(*Xxx)).
	named, isNamed := t.(*types.Named)
	if !isNamed {
		return "", false
	}

	// Underlying must be a function signature.
	sig, isSig := named.Underlying().(*types.Signature)
	if !isSig {
		return "", false
	}

	// Must take exactly one parameter, no results, not variadic.
	if sig.Params().Len() != 1 || sig.Results().Len() != 0 || sig.Variadic() {
		return "", false
	}

	// The parameter must be a pointer to a named struct.
	param := sig.Params().At(0).Type()
	ptr, isPtr := param.(*types.Pointer)
	if !isPtr {
		return "", false
	}

	elem := types.Unalias(ptr.Elem())
	target, isTarget := elem.(*types.Named)
	if !isTarget {
		return "", false
	}

	// Verify the target is actually a struct.
	if _, isStruct := target.Underlying().(*types.Struct); !isStruct {
		return "", false
	}

	return target.Obj().Name(), true
}
