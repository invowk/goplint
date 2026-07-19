// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// protocolCaptureBinding records the lexical free-variable slot and the SSA
// value bound to that slot when a closure procedure is created.
type protocolCaptureBinding struct {
	Index   int
	FreeVar *ssa.FreeVar
	Binding ssa.Value
}

// protocolProcedure is the canonical source/SSA identity for an executable
// function body. Both declarations and literals are procedures; whether a
// literal is immediately invoked is deliberately irrelevant.
type protocolProcedure struct {
	Key          string
	Function     *ssa.Function
	Declaration  *ast.FuncDecl
	Literal      *ast.FuncLit
	Body         *ast.BlockStmt
	Captures     []protocolCaptureBinding
	CaptureExact bool
	Availability ssaAvailability
}

type protocolProcedureIndex struct {
	byFunction       map[*ssa.Function]protocolProcedure
	byKey            map[string]protocolProcedure
	byDecl           map[*ast.FuncDecl]protocolProcedure
	byLiteral        map[*ast.FuncLit]protocolProcedure
	ambiguousDecl    map[*ast.FuncDecl]bool
	ambiguousLiteral map[*ast.FuncLit]bool
}

func buildProtocolProcedureIndex(pass *analysis.Pass, ssaRes *ssaResult) protocolProcedureIndex {
	if ssaRes == nil {
		return buildProtocolProcedureIndexUncached(pass, nil)
	}
	ssaRes.procedureIndexOnce.Do(func() {
		ssaRes.procedureIndex = buildProtocolProcedureIndexUncached(pass, ssaRes)
	})
	return ssaRes.procedureIndex
}

func buildProtocolProcedureIndexUncached(pass *analysis.Pass, ssaRes *ssaResult) protocolProcedureIndex {
	index := protocolProcedureIndex{
		byFunction:       make(map[*ssa.Function]protocolProcedure),
		byKey:            make(map[string]protocolProcedure),
		byDecl:           make(map[*ast.FuncDecl]protocolProcedure),
		byLiteral:        make(map[*ast.FuncLit]protocolProcedure),
		ambiguousDecl:    make(map[*ast.FuncDecl]bool),
		ambiguousLiteral: make(map[*ast.FuncLit]bool),
	}
	if pass == nil {
		return index
	}

	declarations := make(map[*types.Func]*ast.FuncDecl)
	literals := make(map[token.Pos]*ast.FuncLit)
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.FuncDecl:
				if object, ok := pass.TypesInfo.Defs[typed.Name].(*types.Func); ok {
					declarations[object] = typed
				}
			case *ast.FuncLit:
				literals[typed.Pos()] = typed
			}
			return true
		})
	}
	if ssaRes == nil || !ssaRes.availability().ready() || ssaRes.Pkg == nil || ssaRes.Pkg.Prog == nil {
		index.addUnresolvedSourceProcedures(pass, ssaRes, declarations, literals)
		return index
	}

	functions := protocolPackageFunctions(ssaRes)
	captureCandidates := protocolProcedureCaptureCandidates(functions)
	for _, function := range functions {
		procedure := protocolProcedure{
			Function:     function,
			Availability: ssaAvailability{Status: ssaAvailabilityReady},
		}
		if object, ok := function.Object().(*types.Func); ok {
			procedure.Key = objectKey(object)
			procedure.Declaration = declarations[object]
			if procedure.Declaration != nil {
				procedure.Body = procedure.Declaration.Body
			}
		} else {
			procedure.Literal = literals[function.Pos()]
			if procedure.Literal != nil {
				procedure.Key = protocolSourceProcedureKey(pass, procedure.Literal.Pos(), "func-lit")
				procedure.Body = procedure.Literal.Body
			}
			procedure.Captures, procedure.CaptureExact = protocolProcedureCaptureBindings(
				function,
				captureCandidates[function],
			)
			if !procedure.CaptureExact {
				procedure.Availability = ssaAvailability{
					Status: ssaAvailabilityMissingClosure,
					Detail: "closure capture identity is ambiguous",
				}
			}
		}
		if procedure.Key == "" {
			procedure.Key = protocolProcedureKey(function)
		}
		if procedure.Body == nil || procedure.Key == "" {
			continue
		}
		index.byFunction[function] = procedure
		if _, exists := index.byKey[procedure.Key]; !exists {
			index.byKey[procedure.Key] = procedure
		}
		if procedure.Declaration != nil {
			if existing, exists := index.byDecl[procedure.Declaration]; exists && existing.Function != function {
				delete(index.byDecl, procedure.Declaration)
				index.ambiguousDecl[procedure.Declaration] = true
			} else if !index.ambiguousDecl[procedure.Declaration] {
				index.byDecl[procedure.Declaration] = procedure
			}
		}
		if procedure.Literal != nil {
			if existing, exists := index.byLiteral[procedure.Literal]; exists && existing.Function != function {
				delete(index.byLiteral, procedure.Literal)
				index.ambiguousLiteral[procedure.Literal] = true
			} else if !index.ambiguousLiteral[procedure.Literal] {
				index.byLiteral[procedure.Literal] = procedure
			}
		}
	}
	index.addUnresolvedSourceProcedures(pass, ssaRes, declarations, literals)
	return index
}

func (index *protocolProcedureIndex) addUnresolvedSourceProcedures(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	declarations map[*types.Func]*ast.FuncDecl,
	literals map[token.Pos]*ast.FuncLit,
) {
	if index == nil {
		return
	}
	for object, declaration := range declarations {
		if declaration == nil || declaration.Body == nil {
			continue
		}
		if _, exists := index.byDecl[declaration]; exists {
			continue
		}
		key := objectKey(object)
		availability := unresolvedProtocolProcedureAvailability(ssaRes, ssaAvailabilityMissingFunction)
		if key == "" || index.ambiguousDecl[declaration] {
			key = protocolSourceProcedureKey(pass, declaration.Pos(), "func-decl")
		}
		if index.ambiguousDecl[declaration] {
			key += ":ambiguous"
			availability.Detail = "function declaration maps to multiple SSA procedures"
		}
		procedure := protocolProcedure{
			Key: key, Declaration: declaration, Body: declaration.Body, Availability: availability,
		}
		index.byDecl[declaration] = procedure
		if _, exists := index.byKey[key]; !exists {
			index.byKey[key] = procedure
		}
	}
	for _, literal := range literals {
		if literal == nil || literal.Body == nil {
			continue
		}
		if _, exists := index.byLiteral[literal]; exists {
			continue
		}
		key := protocolSourceProcedureKey(pass, literal.Pos(), "func-lit")
		availability := unresolvedProtocolProcedureAvailability(ssaRes, ssaAvailabilityMissingClosure)
		if index.ambiguousLiteral[literal] {
			key += ":ambiguous"
			availability.Detail = "function literal maps to multiple SSA procedures"
		}
		procedure := protocolProcedure{
			Key: key, Literal: literal, Body: literal.Body, Availability: availability,
		}
		index.byLiteral[literal] = procedure
		if _, exists := index.byKey[key]; !exists {
			index.byKey[key] = procedure
		}
	}
}

func unresolvedProtocolProcedureAvailability(
	ssaRes *ssaResult,
	missingStatus ssaAvailabilityStatus,
) ssaAvailability {
	availability := ssaAvailability{Status: missingStatus}
	if packageAvailability := ssaRes.availability(); !packageAvailability.ready() {
		return packageAvailability
	}
	return availability
}

func protocolSourceProcedureKey(pass *analysis.Pass, position token.Pos, kind string) string {
	packagePath := "<unknown>"
	if pass != nil && pass.Pkg != nil {
		packagePath = pass.Pkg.Path()
	}
	return packagePath + "." + kind + "@" + semanticNodeKey(pass, position)
}

func (index protocolProcedureIndex) procedures() []protocolProcedure {
	procedures := make([]protocolProcedure, 0, len(index.byDecl)+len(index.byLiteral))
	seenBodies := make(map[*ast.BlockStmt]bool, cap(procedures))
	for _, procedure := range index.byDecl {
		if procedure.Body == nil || seenBodies[procedure.Body] {
			continue
		}
		seenBodies[procedure.Body] = true
		procedures = append(procedures, procedure)
	}
	for _, procedure := range index.byLiteral {
		if procedure.Body == nil || seenBodies[procedure.Body] {
			continue
		}
		seenBodies[procedure.Body] = true
		procedures = append(procedures, procedure)
	}
	sort.Slice(procedures, func(left, right int) bool {
		if procedures[left].Key != procedures[right].Key {
			return procedures[left].Key < procedures[right].Key
		}
		return procedures[left].Body.Pos() < procedures[right].Body.Pos()
	})
	return procedures
}

func protocolProcedureCaptureCandidates(
	functions []*ssa.Function,
) map[*ssa.Function][][]ssa.Value {
	result := make(map[*ssa.Function][][]ssa.Value)
	for _, function := range functions {
		if function == nil {
			continue
		}
		for _, block := range function.Blocks {
			if block == nil {
				continue
			}
			for _, instruction := range block.Instrs {
				created, ok := instruction.(*ssa.MakeClosure)
				if !ok {
					continue
				}
				closure, closureOK := created.Fn.(*ssa.Function)
				if !closureOK || len(created.Bindings) != len(closure.FreeVars) {
					continue
				}
				bindings := append([]ssa.Value(nil), created.Bindings...)
				result[closure] = append(result[closure], bindings)
			}
		}
	}
	return result
}

func protocolProcedureCaptureBindings(
	closure *ssa.Function,
	candidates [][]ssa.Value,
) ([]protocolCaptureBinding, bool) {
	if closure == nil {
		return nil, false
	}
	if len(closure.FreeVars) == 0 {
		return nil, true
	}
	var bindings []ssa.Value
	for _, candidate := range candidates {
		if bindings != nil && !sameSSABindings(bindings, candidate) {
			return nil, false
		}
		bindings = append(bindings[:0], candidate...)
	}
	if len(bindings) != len(closure.FreeVars) {
		return nil, false
	}
	result := make([]protocolCaptureBinding, len(bindings))
	for index := range bindings {
		result[index] = protocolCaptureBinding{
			Index:   index,
			FreeVar: closure.FreeVars[index],
			Binding: bindings[index],
		}
	}
	return result, true
}

func sameSSABindings(left, right []ssa.Value) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (index protocolProcedureIndex) resolveCall(call ssa.CallInstruction) (protocolProcedure, bool) {
	if call == nil || call.Common() == nil {
		return protocolProcedure{}, false
	}
	callee := call.Common().StaticCallee()
	if callee == nil {
		return protocolProcedure{}, false
	}
	procedure, ok := index.byFunction[callee]
	return procedure, ok
}
