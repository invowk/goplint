// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

type ssaAvailabilityStatus string

const (
	ssaAvailabilityReady                  ssaAvailabilityStatus = "ready"
	ssaAvailabilityBuildFailure           ssaAvailabilityStatus = "build-failure"
	ssaAvailabilityIncompleteDependencies ssaAvailabilityStatus = "incomplete-dependencies"
	ssaAvailabilityMissingFunction        ssaAvailabilityStatus = "missing-function"
	ssaAvailabilityMissingClosure         ssaAvailabilityStatus = "missing-closure"
	ssaAvailabilityUnsupportedInstruction ssaAvailabilityStatus = "unsupported-instruction"
)

type ssaAvailability struct {
	Status ssaAvailabilityStatus
	Detail string
}

func (a ssaAvailability) ready() bool {
	return a.Status == ssaAvailabilityReady
}

func (a ssaAvailability) pathOutcomeReason() pathOutcomeReason {
	if a.Status == ssaAvailabilityUnsupportedInstruction {
		return pathOutcomeReasonUnsupportedInstr
	}
	if a.ready() {
		return pathOutcomeReasonNone
	}
	return pathOutcomeReasonMissingSSA
}

// ssaResult wraps the typed SSA build output for a single package. A result is
// always returned, including when SSA is unavailable, so protocol adapters can
// preserve the exact failure class instead of treating every absence as nil.
type ssaResult struct {
	Pkg                *ssa.Package
	Availability       ssaAvailability
	functionResolver   func(*types.Func) *ssa.Function
	closureResolver    func(token.Pos) *ssa.Function
	functionSeeds      []*ssa.Function
	functionCensusOnce sync.Once
	functionCensus     []*ssa.Function
	procedureIndexOnce sync.Once
	procedureIndex     protocolProcedureIndex
	callEventIndexOnce sync.Once
	callEventIndex     protocolCallEventIndex
	validationOnce     sync.Once
	validationBase     protocolValidationProgram
	valueIndexMu       sync.RWMutex
	valueIndex         cfgSSAValueIndex
	valueIndexReady    bool
}

// protocolPackageFunctions returns the one canonical, immutable local-function
// census shared by every package-level protocol index. Walking
// ssautil.AllFunctions here would construct method sets and wrappers for the
// whole transitive program even though protocol analysis can only associate
// source bodies with the current package. Start from exact package declarations
// and members, then follow only local anonymous and statically reached
// functions.
func protocolPackageFunctions(ssaRes *ssaResult) []*ssa.Function {
	if ssaRes == nil || !ssaRes.availability().ready() || ssaRes.Pkg == nil || ssaRes.Pkg.Prog == nil {
		return nil
	}
	ssaRes.functionCensusOnce.Do(func() {
		functions := make([]*ssa.Function, 0, len(ssaRes.functionSeeds)+len(ssaRes.Pkg.Members))
		seen := make(map[*ssa.Function]bool)
		add := func(function *ssa.Function) {
			if function == nil || function.Pkg != ssaRes.Pkg || len(function.Blocks) == 0 || seen[function] {
				return
			}
			seen[function] = true
			functions = append(functions, function)
		}
		for _, function := range ssaRes.functionSeeds {
			add(function)
		}
		for _, member := range ssaRes.Pkg.Members {
			function, ok := member.(*ssa.Function)
			if ok {
				add(function)
			}
		}
		//nolint:intrange // The slice grows as newly discovered local callees are appended.
		for index := 0; index < len(functions); index++ {
			function := functions[index]
			for _, anonymous := range function.AnonFuncs {
				add(anonymous)
			}
			for _, block := range function.Blocks {
				for _, instruction := range block.Instrs {
					if call, ok := instruction.(ssa.CallInstruction); ok && call.Common() != nil {
						add(call.Common().StaticCallee())
					}
					if closure, ok := instruction.(*ssa.MakeClosure); ok {
						anonymous, _ := closure.Fn.(*ssa.Function)
						add(anonymous)
					}
				}
			}
		}
		sort.Slice(functions, func(left, right int) bool {
			return protocolProcedureKey(functions[left]) < protocolProcedureKey(functions[right])
		})
		ssaRes.functionCensus = functions
	})
	return ssaRes.functionCensus
}

type ssaFunctionResolution struct {
	Function     *ssa.Function
	Availability ssaAvailability
}

// buildSSAForPass builds SSA with GlobalDebug mode for the current package.
// Returns a typed unavailable result if SSA building fails or dependencies are incomplete.
// This is called on-demand rather than as a prerequisite analyzer, avoiding
// the framework running SSA building for every transitive import.
func buildSSAForPass(pass *analysis.Pass) (res *ssaResult) {
	if pass == nil || pass.Pkg == nil || len(pass.Files) == 0 || !pass.Pkg.Complete() {
		return &ssaResult{Availability: ssaAvailability{Status: ssaAvailabilityIncompleteDependencies}}
	}

	// Recover from SSA build panics (unsatisfied imports, etc.).
	defer func() {
		if r := recover(); r != nil {
			res = &ssaResult{Availability: ssaAvailability{
				Status: ssaAvailabilityBuildFailure,
				Detail: fmt.Sprintf("%T", r),
			}}
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

	return &ssaResult{
		Pkg:           ssaPkg,
		Availability:  ssaAvailability{Status: ssaAvailabilityReady},
		functionSeeds: protocolFunctionSeeds(pass, prog),
	}
}

func protocolFunctionSeeds(pass *analysis.Pass, program *ssa.Program) []*ssa.Function {
	if pass == nil || pass.TypesInfo == nil || program == nil {
		return nil
	}
	seeds := make([]*ssa.Function, 0)
	seen := make(map[*ssa.Function]bool)
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			functionDeclaration, ok := declaration.(*ast.FuncDecl)
			if !ok {
				continue
			}
			object, ok := pass.TypesInfo.Defs[functionDeclaration.Name].(*types.Func)
			if !ok {
				continue
			}
			function := program.FuncValue(object)
			if function == nil || seen[function] {
				continue
			}
			seen[function] = true
			seeds = append(seeds, function)
		}
	}
	return seeds
}

func resolveSSAFunction(ssaRes *ssaResult, obj *types.Func) ssaFunctionResolution {
	if ssaRes == nil {
		return ssaFunctionResolution{Availability: ssaAvailability{Status: ssaAvailabilityBuildFailure}}
	}
	if !ssaRes.Availability.ready() || ssaRes.Pkg == nil {
		return ssaFunctionResolution{Availability: ssaRes.Availability}
	}
	if obj == nil {
		return ssaFunctionResolution{Availability: ssaAvailability{Status: ssaAvailabilityMissingFunction}}
	}
	function := ssaRes.Pkg.Prog.FuncValue(obj)
	if ssaRes.functionResolver != nil {
		function = ssaRes.functionResolver(obj)
	}
	if function == nil {
		return ssaFunctionResolution{Availability: ssaAvailability{
			Status: ssaAvailabilityMissingFunction,
			Detail: objectKey(obj),
		}}
	}
	return ssaFunctionResolution{
		Function:     function,
		Availability: ssaAvailability{Status: ssaAvailabilityReady},
	}
}

func resolveSSAClosure(ssaRes *ssaResult, position token.Pos) ssaFunctionResolution {
	if ssaRes == nil {
		return ssaFunctionResolution{Availability: ssaAvailability{Status: ssaAvailabilityBuildFailure}}
	}
	if !ssaRes.Availability.ready() || ssaRes.Pkg == nil {
		return ssaFunctionResolution{Availability: ssaRes.Availability}
	}
	var function *ssa.Function
	for _, candidate := range protocolPackageFunctions(ssaRes) {
		if candidate.Object() == nil && candidate.Pos() == position {
			function = candidate
			break
		}
	}
	if ssaRes.closureResolver != nil {
		function = ssaRes.closureResolver(position)
	}
	if function == nil {
		return ssaFunctionResolution{Availability: ssaAvailability{
			Status: ssaAvailabilityMissingClosure,
			Detail: fmt.Sprintf("position:%d", position),
		}}
	}
	return ssaFunctionResolution{
		Function:     function,
		Availability: ssaAvailability{Status: ssaAvailabilityReady},
	}
}

func (r *ssaResult) availability() ssaAvailability {
	if r == nil {
		return ssaAvailability{Status: ssaAvailabilityBuildFailure}
	}
	if r.Availability.Status == "" {
		return ssaAvailability{Status: ssaAvailabilityBuildFailure}
	}
	return r.Availability
}

func unsupportedSSAInstruction(instruction ssa.Instruction) ssaAvailability {
	detail := "<nil>"
	if instruction != nil {
		detail = fmt.Sprintf("%T", instruction)
	}
	return ssaAvailability{Status: ssaAvailabilityUnsupportedInstruction, Detail: detail}
}

// ssaFuncForTypesFunc resolves a *types.Func to its *ssa.Function in the
// built SSA package. Works for both top-level functions and methods.
func ssaFuncForTypesFunc(ssaRes *ssaResult, obj *types.Func) *ssa.Function {
	return resolveSSAFunction(ssaRes, obj).Function
}
