// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
)

type (
	closureVarCallSet         map[*ast.CallExpr]*ast.FuncLit
	methodValueValidationCall struct {
		receiver           ast.Expr
		onSuccessfulReturn bool
	}
	methodValueValidateCallSet map[*ast.CallExpr]methodValueValidationCall
)

type noReturnCallResolver struct {
	ssa *ssaResult
}

func collectMethodValueValidateCallSet(calls []methodValueValidateCall) methodValueValidateCallSet {
	if len(calls) == 0 {
		return nil
	}
	out := make(methodValueValidateCallSet)
	for _, call := range calls {
		if call.call == nil || call.receiver == nil {
			continue
		}
		out[call.call] = methodValueValidationCall{receiver: call.receiver}
	}
	return out
}

func mergeMethodValueValidateCallSets(sets ...methodValueValidateCallSet) methodValueValidateCallSet {
	total := 0
	for _, set := range sets {
		total += len(set)
	}
	if total == 0 {
		return nil
	}
	out := make(methodValueValidateCallSet, total)
	for _, set := range sets {
		for call, validation := range set {
			if call == nil || validation.receiver == nil {
				continue
			}
			out[call] = validation
		}
	}
	if len(out) == 0 {
		return nil
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
// can satisfy outer-path validation checks:
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

// buildFuncCFGForPass constructs a CFG using a no-return-aware mayReturn
// predicate. Protocol analysis has no untyped CFG fallback.
func buildFuncCFGForPass(pass *analysis.Pass, body *ast.BlockStmt, ssaResult *ssaResult) *gocfg.CFG {
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return nil
	}
	noReturnCalls := newNoReturnCallResolver(pass, body, ssaResult)
	return gocfg.New(body, func(call *ast.CallExpr) bool {
		return callMayReturn(pass, call, noReturnCalls)
	})
}

func callMayReturn(pass *analysis.Pass, call *ast.CallExpr, noReturnCalls noReturnCallResolver) bool {
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
		if fn, ok := obj.(*types.Func); ok {
			return !isKnownNoReturnFunc(fn.Pkg(), fn.Name()) && !importedFunctionIsTerminal(pass, fn)
		}
		if variable, ok := obj.(*types.Var); ok {
			_ = variable
			return !ssaCallIsDefinitelyNoReturn(pass, call, noReturnCalls.ssa)
		}
		return true
	case *ast.SelectorExpr:
		obj := objectForIdent(pass, fun.Sel)
		if obj == nil {
			return true
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return true
		}
		return !isKnownNoReturnFunc(fn.Pkg(), fn.Name()) && !importedFunctionIsTerminal(pass, fn)
	default:
		return true
	}
}

func importedFunctionIsTerminal(pass *analysis.Pass, function *types.Func) bool {
	if pass == nil || pass.ImportObjectFact == nil || function == nil || function.Pkg() == nil ||
		pass.Pkg == nil || function.Pkg() == pass.Pkg {
		return false
	}
	fact := &ProtocolSummaryFact{}
	if !pass.ImportObjectFact(function, fact) || validateProtocolSummaryFact(fact, function) != 0 {
		return false
	}
	for _, effect := range fact.Effects {
		if effect.Kind == protocolSummaryEffectTerminal {
			return true
		}
	}
	return false
}

func newNoReturnCallResolver(
	pass *analysis.Pass,
	body *ast.BlockStmt,
	ssaResult *ssaResult,
) noReturnCallResolver {
	if pass == nil || pass.TypesInfo == nil || body == nil {
		return noReturnCallResolver{}
	}
	return noReturnCallResolver{ssa: ssaResult}
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

// castTargetMatcher returns a validateReceiverMatcher that matches
// using castTarget.matchesExpr. This bridges the castTarget API into
// the generic matcher interface.
func castTargetMatcher(pass *analysis.Pass, target castTarget) validateReceiverMatcher {
	return func(_ *analysis.Pass, expr ast.Expr) bool {
		return target.matchesExpr(pass, expr)
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
		if receiver := methodCalls[call].receiver; receiver != nil && matches(pass, receiver) {
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
		if sel.Sel.Name != validateMethodName {
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

// blockVisitBudget controls the finite-state exploration limit.
type blockVisitBudget struct {
	maxStates int
}
