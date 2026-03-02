// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type closureVarCallSet map[*ast.CallExpr]*ast.FuncLit
type methodValueValidateCallSet map[*ast.CallExpr]ast.Expr

func collectMethodValueValidateCallSet(calls []methodValueValidateCall) methodValueValidateCallSet {
	if len(calls) == 0 {
		return nil
	}
	out := make(methodValueValidateCallSet)
	for _, call := range calls {
		if call.call == nil || call.receiver == nil {
			continue
		}
		out[call.call] = call.receiver
	}
	return out
}

// collectDeferredClosureLits scans a function or closure body for deferred
// closures: defer func() { ... }(). Returns the set of *ast.FuncLit nodes
// that are deferred. These closures are guaranteed to execute before the
// enclosing function returns (Go spec), so Validate() calls inside them
// validate the outer function's path — unlike goroutine closures which
// execute concurrently with no ordering guarantee.
//
// The scan is shallow: it finds FuncLit nodes that are directly invoked by
// a DeferStmt. Nested closures inside deferred closures are not collected
// here — they are handled by their own collectDeferredClosureLits call
// when inspectClosureCastsCFA processes them.
func collectDeferredClosureLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	if body == nil {
		return nil
	}
	var result map[*ast.FuncLit]bool
	ast.Inspect(body, func(n ast.Node) bool {
		deferStmt, ok := n.(*ast.DeferStmt)
		if !ok {
			return true
		}
		// defer expr() — the deferred expression is always a CallExpr.
		// Parenthesized forms are equivalent: defer (func() { ... })().
		if funcLit, ok := callFuncLit(deferStmt.Call); ok {
			if result == nil {
				result = make(map[*ast.FuncLit]bool)
			}
			result[funcLit] = true
		}
		return true
	})
	return result
}

// collectImmediateClosureLits scans a function or closure body for
// immediately-invoked closures: func() { ... }(). Returns the set of
// *ast.FuncLit nodes that execute synchronously in the current path.
//
// Closures invoked by go/defer wrappers are excluded:
//   - go func() { ... }() executes concurrently (not guaranteed before return)
//   - defer func() { ... }() executes at function exit (handled separately)
func collectImmediateClosureLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	if body == nil {
		return nil
	}
	parentMap := buildParentMap(body)
	var result map[*ast.FuncLit]bool
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		lit, ok := callFuncLit(call)
		if !ok {
			return true
		}
		if parent := parentMap[call]; parent != nil {
			if _, isGo := parent.(*ast.GoStmt); isGo {
				return true
			}
			if _, isDefer := parent.(*ast.DeferStmt); isDefer {
				return true
			}
		}
		if result == nil {
			result = make(map[*ast.FuncLit]bool)
		}
		result[lit] = true
		return true
	})
	return result
}

// callFuncLit returns the function literal invoked by call, accepting both
// direct and parenthesized forms:
//   - func() { ... }()
//   - (func() { ... })()
func callFuncLit(call *ast.CallExpr) (*ast.FuncLit, bool) {
	if call == nil {
		return nil, false
	}
	lit, ok := stripParens(call.Fun).(*ast.FuncLit)
	return lit, ok
}

// collectSynchronousClosureLits returns closure literals whose Validate calls
// can satisfy outer-path validation checks in CFA:
//   - deferred closures (execute before function return)
//   - immediate IIFEs (execute synchronously at call site)
func collectSynchronousClosureLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	deferred := collectDeferredClosureLits(body)
	immediate := collectImmediateClosureLits(body)
	if len(deferred) == 0 && len(immediate) == 0 {
		return nil
	}
	result := make(map[*ast.FuncLit]bool, len(deferred)+len(immediate))
	for lit := range deferred {
		result[lit] = true
	}
	for lit := range immediate {
		result[lit] = true
	}
	return result
}

// collectSynchronousClosureVarCalls returns closure-variable call sites that
// execute synchronously for path validation (direct and defer calls).
func collectSynchronousClosureVarCalls(calls []closureVarCall) closureVarCallSet {
	if len(calls) == 0 {
		return nil
	}
	out := make(closureVarCallSet)
	for _, call := range calls {
		if call.kind == closureInvocationGo {
			continue
		}
		callExpr, ok := call.call.(*ast.CallExpr)
		if !ok || call.lit == nil {
			continue
		}
		out[callExpr] = call.lit
	}
	return out
}

// collectUBVClosureLits returns closure literals whose contents should be
// considered when checking use-before-validate ordering.
//
// Only immediate IIFEs are included. Deferred closures execute at function
// return, so a deferred Validate() must not suppress a prior use.
func collectUBVClosureLits(body *ast.BlockStmt) map[*ast.FuncLit]bool {
	return collectImmediateClosureLits(body)
}

// collectUBVClosureVarCalls returns direct closure-variable calls that should be
// considered for use-before-validate ordering.
func collectUBVClosureVarCalls(calls []closureVarCall) closureVarCallSet {
	if len(calls) == 0 {
		return nil
	}
	out := make(closureVarCallSet)
	for _, call := range calls {
		if call.kind != closureInvocationDirect {
			continue
		}
		callExpr, ok := call.call.(*ast.CallExpr)
		if !ok || call.lit == nil {
			continue
		}
		out[callExpr] = call.lit
	}
	return out
}

// buildFuncCFG constructs a control-flow graph for a function body using
// conservative mayReturn (all calls may return). Returns nil if body is nil.
//
// The conservative mayReturn stub ensures no feasible paths are pruned —
// correct for validation reachability analysis where we want to detect
// ALL paths where Validate() might be missing.
func buildFuncCFG(body *ast.BlockStmt) *gocfg.CFG {
	if body == nil {
		return nil
	}
	// Conservative: every call may return. Never prunes feasible paths.
	return gocfg.New(body, func(*ast.CallExpr) bool { return true })
}

// buildFuncCFGForPass constructs a CFG using a no-return-aware mayReturn
// predicate when type information is available.
func buildFuncCFGForPass(pass *analysis.Pass, body *ast.BlockStmt) *gocfg.CFG {
	if body == nil {
		return nil
	}
	if pass == nil || pass.TypesInfo == nil {
		return buildFuncCFG(body)
	}
	return gocfg.New(body, func(call *ast.CallExpr) bool {
		return callMayReturn(pass, call)
	})
}

func callMayReturn(pass *analysis.Pass, call *ast.CallExpr) bool {
	if pass == nil || pass.TypesInfo == nil || call == nil {
		return true
	}

	switch fun := stripParens(call.Fun).(type) {
	case *ast.Ident:
		if fun.Name == "panic" {
			return false
		}
		obj := objectForIdent(pass, fun)
		if obj == nil {
			return true
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return true
		}
		return !isKnownNoReturnFunc(fn.Pkg(), fn.Name())
	case *ast.SelectorExpr:
		obj := objectForIdent(pass, fun.Sel)
		if obj == nil {
			return true
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return true
		}
		return !isKnownNoReturnFunc(fn.Pkg(), fn.Name())
	default:
		return true
	}
}

func isKnownNoReturnFunc(pkg *types.Package, name string) bool {
	pkgPath := ""
	if pkg != nil {
		pkgPath = pkg.Path()
	}

	switch pkgPath {
	case "":
		return name == "panic"
	case "os":
		return name == "Exit"
	case "runtime":
		return name == "Goexit"
	case "log":
		switch name {
		case "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln":
			return true
		}
	case "testing":
		switch name {
		case "FailNow", "Fatal", "Fatalf":
			return true
		}
	}
	return false
}

// findDefiningBlock locates the CFG block containing the given AST node.
// Returns the block and the node's index within Block.Nodes.
// Returns (nil, -1) if the node is not found (e.g., in unreachable code
// that was eliminated from the CFG).
func findDefiningBlock(g *gocfg.CFG, target ast.Node) (*gocfg.Block, int) {
	targetPos := target.Pos()
	targetEnd := target.End()

	for _, block := range g.Blocks {
		for i, node := range block.Nodes {
			if node.Pos() == targetPos && node.End() == targetEnd {
				return block, i
			}
		}
	}
	return nil, -1
}

// validateReceiverMatcher is a predicate that checks whether a .Validate()
// call's receiver expression matches the target being tracked. This
// abstraction enables the same AST walking and IIFE/deferred-closure logic
// to serve both cast-validation (castTarget matching) and constructor-validates
// (type-identity matching).
type validateReceiverMatcher func(pass *analysis.Pass, receiverExpr ast.Expr) bool

// nodeSliceContainsValidateCall checks whether any node in the given
// slice contains a varName.Validate() selector call expression.
// Closures in syncLits are descended into (their Validate calls count as
// outer-path validation); other closures are skipped.
func nodeSliceContainsValidateCall(
	pass *analysis.Pass,
	nodes []ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	matcher := castTargetMatcher(pass, target)
	for _, node := range nodes {
		if containsValidateOnReceiver(pass, node, matcher, syncLits, syncCalls, methodCalls) {
			return true
		}
	}
	return false
}

// containsValidateCall checks whether a single AST node or any of its
// descendants contains a varName.Validate() call.
// This wrapper keeps tests and call sites that only need name matching.
func containsValidateCall(node ast.Node, varName string, syncLits map[*ast.FuncLit]bool) bool {
	target := newCastTargetFromName(varName)
	matcher := castTargetMatcher(nil, target)
	return containsValidateOnReceiver(nil, node, matcher, syncLits, nil, nil)
}

// containsValidateCallTarget checks whether a single AST node or any of its
// descendants contains a varName.Validate() call. Delegates to
// containsValidateOnReceiver with a castTarget matcher.
func containsValidateCallTarget(
	pass *analysis.Pass,
	node ast.Node,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	return containsValidateOnReceiver(pass, node, castTargetMatcher(pass, target), syncLits, syncCalls, methodCalls)
}

// castTargetMatcher returns a validateReceiverMatcher that matches
// using castTarget.matchesExpr. This bridges the castTarget API into
// the generic matcher interface.
func castTargetMatcher(pass *analysis.Pass, target castTarget) validateReceiverMatcher {
	return func(p *analysis.Pass, expr ast.Expr) bool {
		return target.matchesExpr(pass, expr)
	}
}

// typeKeyMatcher returns a validateReceiverMatcher that matches using
// type-identity key comparison. Used by constructor-validates CFA to
// check whether a .Validate() call targets the constructor's return type.
func typeKeyMatcher(returnTypeKey string) validateReceiverMatcher {
	return func(pass *analysis.Pass, expr ast.Expr) bool {
		receiverType := pass.TypesInfo.TypeOf(expr)
		if receiverType == nil {
			return false
		}
		return typeIdentityKey(receiverType) == returnTypeKey
	}
}

// containsValidateOnReceiver checks whether a single AST node or any of its
// descendants contains a .Validate() call whose receiver matches the given
// predicate. Closures (FuncLit) are NOT descended into by default — they are
// analyzed independently with their own CFGs, and a Validate() call inside a
// goroutine closure does not guarantee execution before the outer function
// returns. Immediately invoked closures (func() { ... }()) are treated as
// synchronous and are analyzed as part of the current path.
//
// Exception: closures in syncLits ARE descended into. Go guarantees that
// deferred functions execute before return, and immediate IIFEs execute
// synchronously at call site. This distinguishes:
//   - defer func() { x.Validate() }() (safe)
//   - func() { x.Validate() }() (safe)
//   - go func() { x.Validate() }() (unsafe for outer-path validation)
func containsValidateOnReceiver(
	pass *analysis.Pass,
	node ast.Node,
	matches validateReceiverMatcher,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	seen := make(map[*ast.FuncLit]bool)
	return containsValidateOnReceiverSeen(pass, node, matches, syncLits, syncCalls, methodCalls, seen)
}

func containsValidateOnReceiverSeen(
	pass *analysis.Pass,
	node ast.Node,
	matches validateReceiverMatcher,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
	seen map[*ast.FuncLit]bool,
) bool {
	found := false
	parentMap := buildParentMap(node)
	ast.Inspect(node, func(n ast.Node) bool {
		if found {
			return false
		}
		if ifStmt, ok := n.(*ast.IfStmt); ok {
			if ifStmt.Init != nil && containsValidateOnReceiverSeen(pass, ifStmt.Init, matches, syncLits, syncCalls, methodCalls, seen) {
				found = true
				return false
			}
			if ifStmt.Cond != nil && containsValidateOnReceiverSeen(pass, ifStmt.Cond, matches, syncLits, syncCalls, methodCalls, seen) {
				found = true
				return false
			}
			// A Validate call inside an if/else only guarantees validation
			// when both branches validate.
			if ifStmt.Else != nil &&
				containsValidateOnReceiverSeen(pass, ifStmt.Body, matches, syncLits, syncCalls, methodCalls, seen) &&
				containsValidateOnReceiverSeen(pass, ifStmt.Else, matches, syncLits, syncCalls, methodCalls, seen) {
				found = true
				return false
			}
			// Handled explicitly above; avoid descending into branch bodies
			// where calls are conditionally executed.
			return false
		}
		// Closures: descend only into synchronously-executed closure literals.
		// Goroutine closures are skipped: they do not guarantee execution before
		// the enclosing function returns.
		if lit, ok := n.(*ast.FuncLit); ok {
			return syncLits[lit]
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lit := syncCalls[call]; lit != nil && lit.Body != nil {
			if !seen[lit] {
				seen[lit] = true
				if containsValidateOnReceiverSeen(pass, lit.Body, matches, syncLits, syncCalls, methodCalls, seen) {
					found = true
					delete(seen, lit)
					return false
				}
				delete(seen, lit)
			}
		}
		if receiver := methodCalls[call]; receiver != nil && matches(pass, receiver) {
			if isConditionallyEvaluated(call, parentMap) {
				return true
			}
			found = true
			return false
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name != "Validate" {
			return true
		}
		if matches(pass, sel.X) {
			// Calls nested in conditionally executed contexts do not
			// guarantee validation on every path through the block.
			if isConditionallyEvaluated(call, parentMap) {
				return true
			}
			found = true
		}
		return !found
	})
	return found
}

// isConditionallyEvaluated reports whether node is nested in a context that
// might not execute on every path in the current block.
//
// Examples considered conditional:
//   - RHS of && / || (short-circuit)
//   - if/for/range/switch/select bodies
//   - loop condition/post clauses
//   - goroutine bodies/calls
//
// Contexts considered guaranteed:
//   - standalone statements
//   - if/switch init statements
//   - if/switch tag/condition expressions (except short-circuit RHS)
//   - synchronous closures (handled by caller via syncLits traversal)
func isConditionallyEvaluated(node ast.Node, parentMap map[ast.Node]ast.Node) bool {
	child := node
	for {
		parent, ok := parentMap[child]
		if !ok || parent == nil {
			return false
		}
		switch p := parent.(type) {
		case *ast.BinaryExpr:
			if (p.Op == token.LAND || p.Op == token.LOR) && p.Y == child {
				return true
			}
		case *ast.IfStmt:
			if p.Init != nil && child == p.Init {
				break
			}
			if child != p.Cond {
				return true
			}
		case *ast.ForStmt:
			if p.Init != nil && child == p.Init {
				break
			}
			return true
		case *ast.RangeStmt:
			if child != p.X {
				return true
			}
		case *ast.SwitchStmt:
			if (p.Init != nil && child == p.Init) || child == p.Tag {
				break
			}
			return true
		case *ast.TypeSwitchStmt:
			if (p.Init != nil && child == p.Init) || child == p.Assign {
				break
			}
			return true
		case *ast.SelectStmt, *ast.CaseClause, *ast.CommClause, *ast.GoStmt:
			return true
		}
		child = parent
	}
}

// blockContainsValidateCall checks all nodes in a CFG block for a
// varName.Validate() call. Closures in syncLits are
// descended into.
func blockContainsValidateCall(
	pass *analysis.Pass,
	block *gocfg.Block,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	return nodeSliceContainsValidateCall(pass, block.Nodes, target, syncLits, syncCalls, methodCalls)
}

// blockTerminatesWithoutReturn reports whether a leaf CFG block ends in a
// known no-return call (for example, panic/os.Exit/log.Fatal). Such blocks do
// not represent function return paths and must not trigger "missing Validate on
// path-to-return" diagnostics.
func blockTerminatesWithoutReturn(pass *analysis.Pass, block *gocfg.Block) bool {
	if block == nil || len(block.Succs) != 0 || len(block.Nodes) == 0 {
		return false
	}
	// Explicit return statements are true return paths.
	for _, node := range block.Nodes {
		if _, ok := node.(*ast.ReturnStmt); ok {
			return false
		}
	}
	exprStmt, ok := block.Nodes[len(block.Nodes)-1].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	return !callMayReturn(pass, call)
}

// blockValidateChecker is a predicate that reports whether a CFG block
// contains a .Validate() call matching the caller's target. This abstraction
// enables dfsUnvalidatedBlocks to serve both cast-validation (castTarget)
// and constructor-validates (type-identity) use cases.
type blockValidateChecker func(block *gocfg.Block) bool

// hasPathToReturnWithoutValidate performs a depth-first search from the
// defining block (starting after defIdx) through CFG successors. Returns
// true if any path from the cast definition to a return block never passes
// through a Validate() call on varName.
//
// Closures in syncLits are recognized as containing Validate
// calls when applicable (their execution before return is guaranteed).
//
// Algorithm:
//  1. Check remainder of defBlock.Nodes[defIdx+1:] for Validate call.
//     If found, all paths through this block are validated → return false.
//  2. If defBlock has zero successors (return block) and no Validate in
//     remainder → return true (unvalidated path to return).
//  3. DFS over successors: for each unvisited live block, if it contains
//     Validate → prune (validated). If it's a return block (zero succs) →
//     return true. Otherwise recurse into its successors.
func hasPathToReturnWithoutValidate(
	pass *analysis.Pass,
	g *gocfg.CFG,
	defBlock *gocfg.Block,
	defIdx int,
	target castTarget,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	// Check the remainder of the defining block after the cast.
	remainder := defBlock.Nodes[defIdx+1:]
	if nodeSliceContainsValidateCall(pass, remainder, target, syncLits, syncCalls, methodCalls) {
		return false // validated in same block after cast
	}

	// If no successors, this is a return block — unvalidated path exists.
	if len(defBlock.Succs) == 0 {
		return !blockTerminatesWithoutReturn(pass, defBlock)
	}

	// DFS from successors.
	visited := make(map[int32]bool)
	visited[defBlock.Index] = true

	return dfsUnvalidatedPath(pass, defBlock.Succs, target, visited, syncLits, syncCalls, methodCalls)
}

// dfsUnvalidatedPath recursively checks whether any path through the given
// successor blocks reaches a return block without encountering a Validate()
// call on varName. Closures in syncLits are descended into.
func dfsUnvalidatedPath(
	pass *analysis.Pass,
	succs []*gocfg.Block,
	target castTarget,
	visited map[int32]bool,
	syncLits map[*ast.FuncLit]bool,
	syncCalls closureVarCallSet,
	methodCalls methodValueValidateCallSet,
) bool {
	checker := func(block *gocfg.Block) bool {
		if blockTerminatesWithoutReturn(pass, block) {
			return true
		}
		return blockContainsValidateCall(pass, block, target, syncLits, syncCalls, methodCalls)
	}
	return dfsUnvalidatedBlocks(succs, visited, checker)
}

// dfsUnvalidatedBlocks performs a depth-first search through CFG blocks,
// returning true if any path from the given blocks reaches a return block
// (zero successors) without passing through a block where blockHasValidate
// returns true. This is the shared DFS engine used by both cast-validation
// (via dfsUnvalidatedPath) and constructor-validates (via
// dfsConstructorUnvalidated). The blockHasValidate predicate abstracts the
// validate-matching strategy.
func dfsUnvalidatedBlocks(blocks []*gocfg.Block, visited map[int32]bool, blockHasValidate blockValidateChecker) bool {
	for _, block := range blocks {
		if visited[block.Index] {
			continue
		}
		visited[block.Index] = true

		// Skip dead blocks — unreachable code can't constitute a
		// real execution path.
		if !block.Live {
			continue
		}

		// If this block contains Validate(), this path is safe.
		if blockHasValidate(block) {
			continue
		}

		// If this is a return block (no successors), we have an
		// unvalidated path.
		if len(block.Succs) == 0 {
			return true
		}

		// Recurse into successors.
		if dfsUnvalidatedBlocks(block.Succs, visited, blockHasValidate) {
			return true
		}
	}
	return false
}

