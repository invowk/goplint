// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

type typeMatcher func(t types.Type) bool

func isKnownInterfaceContractMethod(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if pass == nil || pass.TypesInfo == nil || fn == nil || fn.Recv == nil {
		return false
	}
	obj := pass.TypesInfo.Defs[fn.Name]
	if obj == nil {
		return false
	}
	method, ok := obj.(*types.Func)
	if !ok {
		return false
	}
	sig, ok := method.Type().(*types.Signature)
	if !ok {
		return false
	}
	return matchesInterfaceContractSignature(method.Name(), sig)
}

func matchesInterfaceContractSignature(name string, sig *types.Signature) bool {
	switch name {
	case "String", "Error", "GoString":
		return signatureMatches(sig, nil, []typeMatcher{isStringType})
	case "Write", "Read":
		return signatureMatches(sig, []typeMatcher{isByteSliceType}, []typeMatcher{isIntType, isErrorType})
	case "ReadAt", "WriteAt":
		return signatureMatches(sig, []typeMatcher{isByteSliceType, isInt64Type}, []typeMatcher{isIntType, isErrorType})
	case "Seek":
		return signatureMatches(sig, []typeMatcher{isInt64Type, isIntType}, []typeMatcher{isInt64Type, isErrorType})
	case "WriteString":
		return signatureMatches(sig, []typeMatcher{isStringType}, []typeMatcher{isIntType, isErrorType})
	case "MarshalText", "MarshalBinary", "MarshalJSON":
		return signatureMatches(sig, nil, []typeMatcher{isByteSliceType, isErrorType})
	case "UnmarshalText", "UnmarshalBinary", "UnmarshalJSON":
		return signatureMatches(sig, []typeMatcher{isByteSliceType}, []typeMatcher{isErrorType})
	case "AppendText", "AppendBinary":
		return signatureMatches(sig, []typeMatcher{isByteSliceType}, []typeMatcher{isByteSliceType, isErrorType})
	case "WriteTo":
		return signatureMatches(sig, []typeMatcher{namedTypeMatcher("io", "Writer")}, []typeMatcher{isInt64Type, isErrorType})
	case "ReadFrom":
		return signatureMatches(sig, []typeMatcher{namedTypeMatcher("io", "Reader")}, []typeMatcher{isInt64Type, isErrorType})
	case "ReadByte":
		return signatureMatches(sig, nil, []typeMatcher{isByteType, isErrorType})
	case "WriteByte":
		return signatureMatches(sig, []typeMatcher{isByteType}, []typeMatcher{isErrorType})
	case "ReadRune":
		return signatureMatches(sig, nil, []typeMatcher{isRuneType, isIntType, isErrorType})
	case "Format":
		return signatureMatches(sig, []typeMatcher{namedTypeMatcher("fmt", "State"), isRuneType}, nil)
	case "Scan":
		return signatureMatches(sig, []typeMatcher{namedTypeMatcher("fmt", "ScanState"), isRuneType}, []typeMatcher{isErrorType})
	default:
		return false
	}
}

func signatureMatches(sig *types.Signature, params []typeMatcher, results []typeMatcher) bool {
	if sig == nil {
		return false
	}
	if !tupleMatches(sig.Params(), params) {
		return false
	}
	return tupleMatches(sig.Results(), results)
}

func tupleMatches(tuple *types.Tuple, matchers []typeMatcher) bool {
	expected := len(matchers)
	if tuple == nil {
		return expected == 0
	}
	if tuple.Len() != expected {
		return false
	}
	for i := range expected {
		if !matchers[i](tuple.At(i).Type()) {
			return false
		}
	}
	return true
}

func isStringType(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	return ok && basic.Kind() == types.String
}

func isIntType(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	return ok && basic.Kind() == types.Int
}

func isInt64Type(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	return ok && basic.Kind() == types.Int64
}

func isByteType(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == types.Byte || basic.Kind() == types.Uint8
}

func isRuneType(t types.Type) bool {
	basic, ok := types.Unalias(t).(*types.Basic)
	if !ok {
		return false
	}
	return basic.Kind() == types.Rune || basic.Kind() == types.Int32
}

func isByteSliceType(t types.Type) bool {
	slice, ok := types.Unalias(t).(*types.Slice)
	if !ok {
		return false
	}
	return isByteType(slice.Elem())
}

func namedTypeMatcher(pkgPath, name string) typeMatcher {
	return func(t types.Type) bool {
		t = types.Unalias(t)
		named, ok := t.(*types.Named)
		if !ok || named.Obj() == nil {
			return false
		}
		pkg := named.Obj().Pkg()
		if pkg == nil {
			return false
		}
		return pkg.Path() == pkgPath && named.Obj().Name() == name
	}
}
