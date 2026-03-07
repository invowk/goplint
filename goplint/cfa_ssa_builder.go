// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// ssaResult wraps the SSA build output for a single package.
type ssaResult struct {
	Pkg *ssa.Package
}

// buildSSAForPass builds SSA with GlobalDebug mode for the current package.
// Returns nil if SSA building fails (recovered panic from unsatisfied imports).
// This is called on-demand rather than as a prerequisite analyzer, avoiding
// the framework running SSA building for every transitive import.
func buildSSAForPass(pass *analysis.Pass) (res *ssaResult) {
	if len(pass.Files) == 0 {
		return nil
	}

	// Recover from SSA build panics (unsatisfied imports, etc.).
	defer func() {
		if r := recover(); r != nil {
			res = nil
		}
	}()

	prog := ssa.NewProgram(pass.Fset, ssa.GlobalDebug)

	// Create stub SSA packages for all transitively imported packages.
	created := make(map[*types.Package]bool)
	var createImports func(pkgs []*types.Package)
	createImports = func(pkgs []*types.Package) {
		for _, p := range pkgs {
			if created[p] {
				continue
			}
			created[p] = true
			prog.CreatePackage(p, nil, nil, true)
			createImports(p.Imports())
		}
	}
	createImports(pass.Pkg.Imports())

	ssaPkg := prog.CreatePackage(pass.Pkg, pass.Files, pass.TypesInfo, false)
	ssaPkg.Build()

	return &ssaResult{Pkg: ssaPkg}
}

// ssaFuncForTypesFunc resolves a *types.Func to its *ssa.Function in the
// built SSA package. Works for both top-level functions and methods.
func ssaFuncForTypesFunc(ssaRes *ssaResult, obj *types.Func) *ssa.Function {
	if ssaRes == nil || ssaRes.Pkg == nil || obj == nil {
		return nil
	}
	return ssaRes.Pkg.Prog.FuncValue(obj)
}
