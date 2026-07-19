// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// ssaCallIsDefinitelyNoReturn resolves the function value reaching one exact
// call site. Dynamic calls are terminal only when every reaching SSA value is
// a soundly modeled no-return target. Missing or unsupported SSA stays
// returning so graph construction never prunes a realizable continuation.
func ssaCallIsDefinitelyNoReturn(pass *analysis.Pass, call *ast.CallExpr, result *ssaResult) bool {
	common := ssaCallCommonAtSourceCall(call, result)
	if common == nil || common.IsInvoke() {
		return false
	}
	noReturn, resolved := ssaFunctionValueIsNoReturn(pass, common.Value, make(map[ssa.Value]bool))
	return resolved && noReturn
}

func ssaCallCommonAtSourceCall(call *ast.CallExpr, result *ssaResult) *ssa.CallCommon {
	if call == nil || result == nil || !result.availability().ready() || result.Pkg == nil {
		return nil
	}
	var match *ssa.CallCommon
	visitSSAFunctions(result.Pkg, func(function *ssa.Function) bool {
		for _, block := range function.Blocks {
			for _, instruction := range block.Instrs {
				callInstruction, ok := instruction.(ssa.CallInstruction)
				if !ok || callInstruction.Common() == nil || callInstruction.Common().Pos() != call.Lparen {
					continue
				}
				match = callInstruction.Common()
				return false
			}
		}
		return true
	})
	return match
}

func visitSSAFunctions(pkg *ssa.Package, visit func(*ssa.Function) bool) {
	if pkg == nil || visit == nil {
		return
	}
	seen := make(map[*ssa.Function]bool)
	var walk func(*ssa.Function) bool
	walk = func(function *ssa.Function) bool {
		if function == nil || seen[function] {
			return true
		}
		seen[function] = true
		if !visit(function) {
			return false
		}
		for _, anonymous := range function.AnonFuncs {
			if !walk(anonymous) {
				return false
			}
		}
		return true
	}
	for _, member := range pkg.Members {
		function, ok := member.(*ssa.Function)
		if ok && !walk(function) {
			return
		}
	}
}

func ssaFunctionValueIsNoReturn(
	pass *analysis.Pass,
	value ssa.Value,
	visiting map[ssa.Value]bool,
) (bool, bool) {
	if value == nil || visiting[value] {
		return false, false
	}
	visiting[value] = true
	defer delete(visiting, value)

	switch candidate := value.(type) {
	case *ssa.Builtin:
		return candidate.Name() == "panic", true
	case *ssa.Function:
		function, _ := candidate.Object().(*types.Func)
		if function == nil {
			return false, true
		}
		return isKnownNoReturnFunc(function.Pkg(), function.Name()) ||
			importedFunctionIsTerminal(pass, function), true
	case *ssa.MakeClosure:
		return ssaFunctionValueIsNoReturn(pass, candidate.Fn, visiting)
	case *ssa.Phi:
		if len(candidate.Edges) == 0 {
			return false, false
		}
		for _, edge := range candidate.Edges {
			noReturn, resolved := ssaFunctionValueIsNoReturn(pass, edge, visiting)
			if !resolved {
				return false, false
			}
			if !noReturn {
				return false, true
			}
		}
		return true, true
	case *ssa.ChangeType:
		return ssaFunctionValueIsNoReturn(pass, candidate.X, visiting)
	case *ssa.Convert:
		return ssaFunctionValueIsNoReturn(pass, candidate.X, visiting)
	default:
		return false, false
	}
}
