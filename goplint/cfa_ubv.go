// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

// isVarUse reports whether the given AST node contains a "use" of varName.
// This wrapper keeps tests and call sites that only need name matching.
func isVarUse(node ast.Node, varName string) bool {
	return isVarUseTarget(nil, node, newCastTargetFromName(varName), nil, nil, nil)
}

// isVarUseTarget reports whether the given AST node contains a "use" of target
// that is not a display-only or validation call. A use means the variable's
// value is consumed by a non-trivial operation before it is validated.
//
// What counts as a use:
//   - Passing varName as a function argument: useFunc(x)
//   - Method call on varName where the method is not Validate, String,
//     Error, or GoString: x.Setup()
//   - Composite literal field value: SomeStruct{Field: x} or map[K]V{"k": x}
//   - Channel send value: ch <- x
//
// What does NOT count as a use:
//   - x.Validate() — the validation call itself
//   - x.String(), x.Error(), x.GoString() — display-only methods
//
// Closures are NOT descended into by default. When syncLits is provided,
// only those closure literals are descended into (for example, deferred
// closures and IIFEs that execute synchronously in the current path).
func isVarUseTarget(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	seen := make(map[*ast.FuncLit]bool)
	return isVarUseTargetSeen(pass, node, target, syncLits, syncCalls, methodCalls, seen)
}

func isVarUseTargetSeen(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}

		// Composite literal field value: SomeStruct{Field: x} or
		// map[K]V{x: v}. Placing an unvalidated value into a
		// struct or map field is a meaningful consumption.
		if kv, ok := n.(*ast.KeyValueExpr); ok {
			if target.matchesExpr(pass, kv.Key) {
				found = true
				return false
			}
			if target.matchesExpr(pass, kv.Value) {
				found = true
				return false
			}
			return true
		}

		// Index expression: m[x]. Using a value as an index/key is a
		// meaningful consumption before validation.
		if idx, ok := n.(*ast.IndexExpr); ok {
			if target.matchesExpr(pass, idx.Index) {
				found = true
				return false
			}
			return true
		}

		// Channel send: ch <- x. Sending an unvalidated value on
		// a channel propagates it to another goroutine without
		// validation guarantees.
		if send, ok := n.(*ast.SendStmt); ok {
			if target.matchesExpr(pass, send.Value) {
				found = true
				return false
			}
			return true
		}
		// Alias/copy assignment: y := x, var y = x, dst = f(x), etc.
		// Any RHS consumption of target before Validate() is a use.
		if assign, ok := n.(*ast.AssignStmt); ok {
			for _, rhs := range assign.Rhs {
				if target.matchesExpr(pass, rhs) || isVarUseTargetSeen(pass, rhs, target, syncLits, syncCalls, methodCalls, seen) {
					found = true
					return false
				}
			}
			return true
		}
		if valueSpec, ok := n.(*ast.ValueSpec); ok {
			for _, value := range valueSpec.Values {
				if target.matchesExpr(pass, value) || isVarUseTargetSeen(pass, value, target, syncLits, syncCalls, methodCalls, seen) {
					found = true
					return false
				}
			}
			return true
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lit := syncCalls[call]; lit != nil && lit.Body != nil && !seen[lit] {
			seen[lit] = true
			order := firstUseValidateOrderInNodeSeen(pass, lit.Body, target, syncLits, syncCalls, methodCalls, seen)
			delete(seen, lit)
			if order == ubvOrderUseBeforeValidate {
				found = true
				return false
			}
		}

		// Check for method call on varName: x.Method(...)
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if target.matchesExpr(pass, sel.X) {
				switch sel.Sel.Name {
				case "Validate", "String", "Error", "GoString":
					return true // display-only or validation — not a use
				default:
					found = true
					return false
				}
			}
		}

		// Check for varName appearing as a function argument.
		for _, arg := range call.Args {
			if target.matchesExpr(pass, arg) {
				found = true
				return false
			}
		}

		return true
	})
	return found
}

type ubvOrderResult int

const (
	ubvOrderNone ubvOrderResult = iota
	ubvOrderUseBeforeValidate
	ubvOrderValidateBeforeUse
)

func firstUseValidateOrderInNode(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) ubvOrderResult {
	seen := make(map[*ast.FuncLit]bool)
	return firstUseValidateOrderInNodeSeen(pass, node, target, syncLits, syncCalls, methodCalls, seen)
}

func firstUseValidateOrderInNodeSeen(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
) ubvOrderResult {
	if node == nil {
		return ubvOrderNone
	}

	result := ubvOrderNone
	parentMap := buildParentMap(node)
	ast.Inspect(node, func(n ast.Node) bool {
		if result != ubvOrderNone {
			return false
		}
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if lit := syncCalls[call]; lit != nil && lit.Body != nil && !seen[lit] {
				seen[lit] = true
				order := firstUseValidateOrderInNodeSeen(pass, lit.Body, target, syncLits, syncCalls, methodCalls, seen)
				delete(seen, lit)
				if order != ubvOrderNone {
					result = order
					return false
				}
			}
		}

		if isValidateCallNode(pass, n, target, parentMap, methodCalls) {
			result = ubvOrderValidateBeforeUse
			return false
		}
		if isUseNode(pass, n, target, syncLits, syncCalls, methodCalls) {
			result = ubvOrderUseBeforeValidate
			return false
		}
		return true
	})
	return result
}

func isValidateCallNode(
	pass *analysis.Pass,
	n ast.Node,
	target castTarget,
	parentMap map[ast.Node]ast.Node,
	methodCalls methodValueValidateCallSet,
) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	if receiver := methodCalls[call]; receiver != nil {
		if !target.matchesExpr(pass, receiver) {
			return false
		}
		if parent, ok := parentMap[call]; ok {
			if _, isDefer := parent.(*ast.DeferStmt); isDefer {
				return false
			}
		}
		return !isConditionallyEvaluated(call, parentMap)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Validate" {
		return false
	}
	if !target.matchesExpr(pass, sel.X) {
		return false
	}
	// UBV ordering requires a Validate call to execute before the use in the
	// same execution path. Direct defer/go wrappers do not satisfy that
	// requirement, and conditionally evaluated calls are not guaranteed.
	if parent, ok := parentMap[call]; ok {
		if _, isDefer := parent.(*ast.DeferStmt); isDefer {
			return false
		}
	}
	return !isConditionallyEvaluated(call, parentMap)
}

func isUseNode(
	pass *analysis.Pass,
	n ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	switch node := n.(type) {
	case *ast.KeyValueExpr:
		return target.matchesExpr(pass, node.Key) || target.matchesExpr(pass, node.Value)
	case *ast.IndexExpr:
		return target.matchesExpr(pass, node.Index)
	case *ast.SendStmt:
		return target.matchesExpr(pass, node.Value)
	case *ast.AssignStmt:
		for _, rhs := range node.Rhs {
			if target.matchesExpr(pass, rhs) || isVarUseTarget(pass, rhs, target, syncLits, syncCalls, methodCalls) {
				return true
			}
		}
		return false
	case *ast.ValueSpec:
		for _, value := range node.Values {
			if target.matchesExpr(pass, value) || isVarUseTarget(pass, value, target, syncLits, syncCalls, methodCalls) {
				return true
			}
		}
		return false
	case *ast.CallExpr:
		if sel, ok := node.Fun.(*ast.SelectorExpr); ok && target.matchesExpr(pass, sel.X) {
			switch sel.Sel.Name {
			case "Validate", "String", "Error", "GoString":
				return false
			default:
				return true
			}
		}
		validateSeen := false
		for _, arg := range node.Args {
			switch firstUseValidateOrderInNode(pass, arg, target, syncLits, syncCalls, methodCalls) {
			case ubvOrderUseBeforeValidate:
				if !validateSeen {
					return true
				}
				continue
			case ubvOrderValidateBeforeUse:
				validateSeen = true
				continue
			}
			if target.matchesExpr(pass, arg) || isVarUseTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
				if !validateSeen {
					return true
				}
			}
			if containsValidateCallTarget(pass, arg, target, syncLits, syncCalls, methodCalls) {
				validateSeen = true
			}
		}
		return false
	}
	return false
}

// hasUseBeforeValidateInBlock checks whether, in the nodes of a block
// starting at startIdx, a "use" of varName appears before a Validate()
// call. Returns true if the variable is used (as an argument or non-display
// method receiver) before Validate() is encountered. Closures
// in syncLits are recognized when checking for Validate() calls.
//
// Algorithm:
//  1. Scan nodes[startIdx:] in order.
//  2. If a Validate() call on varName is found first → return false (safe).
//  3. If a "use" of varName is found first → return true (UBV detected).
//  4. If neither is found → return false (no use in this block).
func hasUseBeforeValidateInBlock(
	pass *analysis.Pass,
	nodes []ast.Node,
	startIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	for i := startIdx; i < len(nodes); i++ {
		node := nodes[i]
		switch firstUseValidateOrderInNode(pass, node, target, syncLits, syncCalls, methodCalls) {
		case ubvOrderUseBeforeValidate:
			return true
		case ubvOrderValidateBeforeUse:
			return false
		}
		if isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
			return true // use before Validate() — flagged
		}
	}
	return false
}

// hasUseBeforeValidateCrossBlock performs a DFS from the defining block
// through CFG successors to detect uses of varName that occur before
// any Validate() call on that path. Unlike hasUseBeforeValidateInBlock
// which only checks within the defining block, this function checks
// across block boundaries.
//
// The function is only called when hasPathToReturnWithoutValidate returns
// false (all paths DO validate) — the question is whether any path
// uses the variable before reaching the Validate() call.
//
// Algorithm:
//  1. Start from defBlock.Succs (the cast's defining block has already
//     been checked by hasUseBeforeValidateInBlock).
//  2. For each live, unvisited successor block:
//     a. Scan nodes in order: if a use is found before Validate → flag.
//     b. If Validate is found first → prune this path (validated).
//     c. If neither is found → continue DFS to successors.
func hasUseBeforeValidateCrossBlock(
	pass *analysis.Pass,
	defBlock *gocfg.Block,
	defIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	// First check remainder of defBlock for use (same-block already
	// handled) — skip directly to successor blocks.
	// But we need to check if defBlock remainder has validate, which
	// would prune all successor paths.
	remainder := defBlock.Nodes[defIdx+1:]
	if nodeSliceContainsValidateCall(pass, remainder, target, syncLits, syncCalls, methodCalls) {
		return false // validated in same block — successors are safe
	}

	if len(defBlock.Succs) == 0 {
		return false // return block — no successors to check
	}

	visited := make(map[int32]bool)
	visited[defBlock.Index] = true

	return dfsUseBeforeValidate(pass, defBlock.Succs, target, visited, syncLits, syncCalls, methodCalls)
}

// dfsUseBeforeValidate recursively checks whether any path through
// successor blocks contains a "use" of varName before a "Validate" call.
// Blocks containing Validate() prune their path (downstream is safe).
// Blocks with no use and no Validate continue the DFS.
func dfsUseBeforeValidate(
	pass *analysis.Pass,
	succs []*gocfg.Block,
	target castTarget,
	visited map[int32]bool,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	for _, succ := range succs {
		if visited[succ.Index] {
			continue
		}
		visited[succ.Index] = true

		if !succ.Live {
			continue
		}

		// Scan this block's nodes in order: use vs validate.
		foundUse := false
		foundValidate := false
		for _, node := range succ.Nodes {
			order := firstUseValidateOrderInNode(pass, node, target, syncLits, syncCalls, methodCalls)
			if order == ubvOrderUseBeforeValidate {
				foundUse = true
				break // use found before Validate in this node
			}
			if order == ubvOrderValidateBeforeUse {
				foundValidate = true
				break // Validate found before use in this node
			}
			if isVarUseTarget(pass, node, target, syncLits, syncCalls, methodCalls) {
				foundUse = true
				break // use found before Validate in this block
			}
		}

		if foundUse {
			return true // cross-block UBV detected
		}
		if foundValidate {
			continue // this path is validated — skip successors
		}

		// Neither use nor validate in this block — continue DFS.
		if dfsUseBeforeValidate(pass, succ.Succs, target, visited, syncLits, syncCalls, methodCalls) {
			return true
		}
	}
	return false
}
