// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

type protocolCallEventPhase string

const (
	protocolCallEventSync              protocolCallEventPhase = "sync"
	protocolCallEventGo                protocolCallEventPhase = "go"
	protocolCallEventDeferRegistration protocolCallEventPhase = "defer-registration"
)

type protocolCallSiteID struct {
	Procedure        string
	Position         token.Pos
	BlockIndex       int
	InstructionIndex int
	Phase            protocolCallEventPhase
}

func (id protocolCallSiteID) Key() string {
	return fmt.Sprintf("%s@%d|%d|%d|%s", id.Procedure, id.Position, id.BlockIndex, id.InstructionIndex, id.Phase)
}

type protocolCallEvent struct {
	ID          protocolCallSiteID
	Call        *ast.CallExpr
	Instruction ssa.CallInstruction
	Caller      *ssa.Function
	Callee      *ssa.Function
	Phase       protocolCallEventPhase
	Mapped      bool
	Builtin     string
}

type protocolCallEventIndex struct {
	byLparen    map[token.Pos][]protocolCallEvent
	byProcedure map[string][]protocolCallEvent
	astByLparen map[token.Pos]*ast.CallExpr
	phaseByCall map[*ast.CallExpr]protocolCallEventPhase
}

func buildProtocolCallEventIndex(pass *analysis.Pass, ssaRes *ssaResult) protocolCallEventIndex {
	if ssaRes == nil {
		return buildProtocolCallEventIndexUncached(pass, nil)
	}
	ssaRes.callEventIndexOnce.Do(func() {
		ssaRes.callEventIndex = buildProtocolCallEventIndexUncached(pass, ssaRes)
	})
	return ssaRes.callEventIndex
}

func buildProtocolCallEventIndexUncached(pass *analysis.Pass, ssaRes *ssaResult) protocolCallEventIndex {
	index := protocolCallEventIndex{
		byLparen:    make(map[token.Pos][]protocolCallEvent),
		byProcedure: make(map[string][]protocolCallEvent),
		astByLparen: make(map[token.Pos]*ast.CallExpr),
		phaseByCall: make(map[*ast.CallExpr]protocolCallEventPhase),
	}
	if pass != nil {
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CallExpr:
					if typed.Lparen.IsValid() {
						index.astByLparen[typed.Lparen] = typed
						if _, exists := index.phaseByCall[typed]; !exists {
							index.phaseByCall[typed] = protocolCallEventSync
						}
					}
				case *ast.GoStmt:
					if typed.Call != nil {
						index.phaseByCall[typed.Call] = protocolCallEventGo
					}
				case *ast.DeferStmt:
					if typed.Call != nil {
						index.phaseByCall[typed.Call] = protocolCallEventDeferRegistration
					}
				}
				return true
			})
		}
	}
	if ssaRes == nil || !ssaRes.availability().ready() || ssaRes.Pkg == nil || ssaRes.Pkg.Prog == nil {
		return index
	}

	functions := protocolPackageFunctions(ssaRes)
	for _, function := range functions {
		procedure := protocolProcedureKey(function)
		for blockIndex, block := range function.Blocks {
			for instructionIndex, instruction := range block.Instrs {
				call, ok := instruction.(ssa.CallInstruction)
				if !ok || call.Common() == nil {
					continue
				}
				position := call.Common().Pos()
				phase := protocolCallEventSync
				switch instruction.(type) {
				case *ssa.Go:
					phase = protocolCallEventGo
				case *ssa.Defer:
					phase = protocolCallEventDeferRegistration
				}
				event := protocolCallEvent{
					ID: protocolCallSiteID{
						Procedure:        procedure,
						Position:         position,
						BlockIndex:       blockIndex,
						InstructionIndex: instructionIndex,
						Phase:            phase,
					},
					Call:        index.astByLparen[position],
					Instruction: call,
					Caller:      function,
					Callee:      call.Common().StaticCallee(),
					Phase:       phase,
				}
				event.Mapped = event.Call != nil
				index.byLparen[position] = append(index.byLparen[position], event)
				index.byProcedure[procedure] = append(index.byProcedure[procedure], event)
			}
		}
	}
	for procedure := range index.byProcedure {
		sort.SliceStable(index.byProcedure[procedure], func(left, right int) bool {
			return protocolCallEventLess(index.byProcedure[procedure][left], index.byProcedure[procedure][right])
		})
	}
	return index
}

func protocolCallEventLess(left, right protocolCallEvent) bool {
	if left.ID.BlockIndex != right.ID.BlockIndex {
		return left.ID.BlockIndex < right.ID.BlockIndex
	}
	if left.ID.InstructionIndex != right.ID.InstructionIndex {
		return left.ID.InstructionIndex < right.ID.InstructionIndex
	}
	return left.ID.Key() < right.ID.Key()
}

func protocolSourceCallsInNode(node ast.Node) []*ast.CallExpr {
	if node == nil {
		return nil
	}
	calls := make([]*ast.CallExpr, 0)
	root := node
	ast.Inspect(node, func(candidate ast.Node) bool {
		if literal, ok := candidate.(*ast.FuncLit); ok && ast.Node(literal) != root {
			return false
		}
		if call, ok := candidate.(*ast.CallExpr); ok {
			calls = append(calls, call)
		}
		return true
	})
	return calls
}

// protocolOrderedCallsInNode is retained for source-only queries that do not
// establish execution order. Interprocedural graph construction must use
// protocolCallEventIndex.eventsForNode, whose order comes exclusively from SSA.
func protocolOrderedCallsInNode(node ast.Node) []*ast.CallExpr {
	calls := protocolSourceCallsInNode(node)
	sort.SliceStable(calls, func(left, right int) bool {
		leftContainsRight := calls[left].Pos() <= calls[right].Pos() && calls[left].End() >= calls[right].End()
		rightContainsLeft := calls[right].Pos() <= calls[left].Pos() && calls[right].End() >= calls[left].End()
		if leftContainsRight != rightContainsLeft {
			return rightContainsLeft
		}
		return calls[left].Pos() < calls[right].Pos()
	})
	return calls
}

func (index protocolCallEventIndex) eventsForNode(
	pass *analysis.Pass,
	procedure string,
	node ast.Node,
) []protocolCallEvent {
	return index.eventsForProcedureNode(pass, procedure, nil, node)
}

func (index protocolCallEventIndex) eventsForProcedureNode(
	pass *analysis.Pass,
	procedure string,
	caller *ssa.Function,
	node ast.Node,
) []protocolCallEvent {
	calls := protocolSourceCallsInNode(node)
	if len(calls) == 0 {
		return nil
	}
	relevantCalls := make([]*ast.CallExpr, 0, len(calls))
	for _, call := range calls {
		if pass != nil && pass.TypesInfo != nil {
			if value, ok := pass.TypesInfo.Types[call.Fun]; ok && value.IsType() {
				continue
			}
		}
		relevantCalls = append(relevantCalls, call)
	}
	if len(relevantCalls) == 0 {
		return nil
	}

	if caller == nil {
		callers := index.candidateCallers(procedure, relevantCalls, true)
		if len(callers) == 0 {
			// Synthetic graph keys do not name an SSA procedure. They may still
			// associate with exactly one typed caller, but never with the first
			// caller found at the same source position.
			callers = index.candidateCallers(procedure, relevantCalls, false)
		}
		if len(callers) != 1 {
			return index.unmappedEvents(pass, procedure, relevantCalls)
		}
		caller = callers[0]
	}

	result := make([]protocolCallEvent, 0, len(relevantCalls))
	blockIndex := -1
	for _, call := range relevantCalls {
		candidates := make([]protocolCallEvent, 0, 1)
		for _, candidate := range index.byLparen[call.Lparen] {
			if candidate.Call == call && candidate.Caller == caller {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) != 1 {
			return index.unmappedEvents(pass, procedure, relevantCalls)
		}
		event := candidates[0]
		if blockIndex < 0 {
			blockIndex = event.ID.BlockIndex
		} else if event.ID.BlockIndex != blockIndex {
			// A single AST CFG node cannot be linearized across multiple SSA
			// blocks without inventing an order. Fail closed at the mapping layer.
			return index.unmappedEvents(pass, procedure, relevantCalls)
		}
		if event.Builtin == "" {
			event.Builtin = protocolASTBuiltinName(pass, call)
		}
		result = append(result, event)
	}
	sort.SliceStable(result, func(left, right int) bool {
		return protocolCallEventLess(result[left], result[right])
	})
	return result
}

func (index protocolCallEventIndex) candidateCallers(
	procedure string,
	calls []*ast.CallExpr,
	requireProcedure bool,
) []*ssa.Function {
	callers := make(map[*ssa.Function]bool)
	for _, call := range calls {
		for _, event := range index.byLparen[call.Lparen] {
			if event.Call != call || event.Caller == nil ||
				(requireProcedure && event.ID.Procedure != procedure) {
				continue
			}
			callers[event.Caller] = true
		}
	}
	result := make([]*ssa.Function, 0, len(callers))
	for caller := range callers {
		result = append(result, caller)
	}
	sort.Slice(result, func(left, right int) bool {
		return fmt.Sprintf("%p", result[left]) < fmt.Sprintf("%p", result[right])
	})
	return result
}

func (index protocolCallEventIndex) unmappedEvents(
	pass *analysis.Pass,
	procedure string,
	calls []*ast.CallExpr,
) []protocolCallEvent {
	result := make([]protocolCallEvent, 0, len(calls))
	for _, call := range calls {
		event := protocolCallEvent{
			ID: protocolCallSiteID{
				Procedure: procedure,
				Position:  call.Lparen,
				Phase:     index.phaseByCall[call],
			},
			Call:    call,
			Phase:   index.phaseByCall[call],
			Mapped:  false,
			Builtin: protocolASTBuiltinName(pass, call),
		}
		result = append(result, event)
	}
	// Position is used only to make an already-failed mapping deterministic;
	// it never establishes semantic order for mapped execution.
	sort.SliceStable(result, func(left, right int) bool {
		return result[left].ID.Position < result[right].ID.Position
	})
	return result
}

func (index protocolCallEventIndex) eventForCall(call *ast.CallExpr) (protocolCallEvent, bool) {
	if call == nil {
		return protocolCallEvent{}, false
	}
	var result protocolCallEvent
	found := false
	for _, event := range index.byLparen[call.Lparen] {
		if event.Call != call {
			continue
		}
		if found {
			return protocolCallEvent{}, false
		}
		result = event
		found = true
	}
	return result, found
}

func protocolCallIsBuiltin(event protocolCallEvent) bool {
	if event.Builtin != "" {
		return true
	}
	if event.Instruction == nil || event.Instruction.Common() == nil {
		return false
	}
	_, ok := event.Instruction.Common().Value.(*ssa.Builtin)
	return ok
}

func protocolASTBuiltinName(pass *analysis.Pass, call *ast.CallExpr) string {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return ""
	}
	identifier, ok := stripParens(call.Fun).(*ast.Ident)
	if !ok {
		return ""
	}
	if _, ok := pass.TypesInfo.Uses[identifier].(*types.Builtin); ok {
		return identifier.Name
	}
	return ""
}
