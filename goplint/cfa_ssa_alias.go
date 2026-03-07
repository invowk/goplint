// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ssa"
)

// maxAliasSetSize caps the alias set to avoid unbounded growth in functions
// with many copy assignments. Beyond this limit the set is conservatively
// cleared (no aliases recognized).
const maxAliasSetSize = 32

// enrichTargetWithSSAAlias returns a copy of target with an SSA-derived
// alias set attached. If SSA information is unavailable or the cast
// expression cannot be resolved, the original target is returned unchanged.
func enrichTargetWithSSAAlias(
	ssaFn *ssa.Function,
	ac cfaAssignedCast,
) castTarget {
	if ssaFn == nil {
		return ac.target
	}
	aliases := computeMustAliasKeys(ssaFn, ac.pos)
	if len(aliases) == 0 {
		return ac.target
	}
	// Remove the primary target key from aliases — it is already matched
	// by the targetKey comparison in matchesExpr.
	delete(aliases, ac.target.targetKey)
	if len(aliases) == 0 {
		return ac.target
	}
	enriched := ac.target
	enriched.aliasKeys = aliases
	return enriched
}

// computeMustAliasKeys finds all source-level variables that provably hold
// the same SSA value as the cast expression result — and are never
// reassigned to a different value in the function.
//
// The algorithm uses DebugRef instructions emitted in GlobalDebug mode:
//
//  1. Find the SSA Convert/ChangeType instruction matching the cast position.
//  2. Collect all DebugRef instructions where X == castValue.
//  3. Exclude any object that has another DebugRef with a different X
//     (indicating reassignment).
func computeMustAliasKeys(
	ssaFn *ssa.Function,
	castNode ast.Node,
) ssaAliasSet {
	castValue := findSSACastValue(ssaFn, castNode)
	if castValue == nil {
		return nil
	}

	// Phase 1: collect all DebugRef instructions in the function.
	// Build two maps:
	//   objToValues: object → set of distinct SSA values assigned via DebugRef
	//   castAliasObjs: objects whose DebugRef X matches the cast value
	type objInfo struct {
		key         string
		valueCount  int
		aliasesCast bool
	}
	objMap := make(map[types.Object]*objInfo)

	for _, block := range ssaFn.Blocks {
		for _, instr := range block.Instrs {
			dbg, ok := instr.(*ssa.DebugRef)
			if !ok || dbg.IsAddr {
				continue
			}
			obj := dbg.Object()
			if obj == nil {
				continue
			}
			info, exists := objMap[obj]
			if !exists {
				info = &objInfo{key: objectKey(obj)}
				objMap[obj] = info
			}
			if dbg.X == castValue {
				info.aliasesCast = true
			} else {
				info.valueCount++
			}
		}
	}

	// Phase 2: build the alias set from objects that alias the cast value
	// AND were never assigned a different value.
	result := make(ssaAliasSet)
	for _, info := range objMap {
		if !info.aliasesCast {
			continue
		}
		// Exclude objects with any DebugRef pointing to a different value.
		// This conservatively handles reassignment: y := x; y = other
		// produces two DebugRefs for y (one with castValue, one without),
		// so y is excluded from the alias set.
		if info.valueCount > 0 {
			continue
		}
		if info.key == "" {
			continue
		}
		result[info.key] = true
		if len(result) > maxAliasSetSize {
			return nil
		}
	}

	return result
}

// findSSACastValue locates the SSA value produced by a type conversion
// at the given AST node position. SSA and AST use different position
// anchors for type conversions: AST CallExpr.Pos() is the type name
// start, while SSA ChangeType/Convert.Pos() usually points at the Lparen.
//
// We deliberately prefer the earliest conversion instruction in the node span.
// Nested helper calls inside the cast argument (for example,
// T(strings.TrimSpace(raw))) also produce in-range SSA values, but they are not
// the cast result and must not drive alias inference.
func findSSACastValue(ssaFn *ssa.Function, castNode ast.Node) ssa.Value {
	if ssaFn == nil || castNode == nil {
		return nil
	}
	nodeStart := castNode.Pos()
	nodeEnd := castNode.End()
	if !nodeStart.IsValid() {
		return nil
	}

	var best ssa.Value
	var bestPos token.Pos
	for _, block := range ssaFn.Blocks {
		for _, instr := range block.Instrs {
			val, ok := instr.(ssa.Value)
			if !ok {
				continue
			}
			valPos := val.Pos()
			if !valPos.IsValid() || valPos < nodeStart || valPos >= nodeEnd {
				continue
			}
			switch val.(type) {
			case *ssa.ChangeType, *ssa.Convert:
				if best == nil || valPos < bestPos {
					best = val
					bestPos = valPos
				}
			}
		}
	}
	return best
}

// enrichAssignedCastsWithSSA attaches SSA-derived alias sets to all assigned
// casts when alias mode is active.
func enrichAssignedCastsWithSSA(
	pass *analysis.Pass,
	ssaRes *ssaResult,
	fn *ast.FuncDecl,
	assignedCasts []cfaAssignedCast,
) {
	if ssaRes == nil || fn == nil || fn.Name == nil {
		return
	}
	obj, ok := pass.TypesInfo.Defs[fn.Name]
	if !ok || obj == nil {
		return
	}
	typesFunc, ok := obj.(*types.Func)
	if !ok {
		return
	}
	ssaFn := ssaFuncForTypesFunc(ssaRes, typesFunc)
	if ssaFn == nil {
		return
	}
	for i := range assignedCasts {
		assignedCasts[i].target = enrichTargetWithSSAAlias(
			ssaFn, assignedCasts[i],
		)
	}
}

// enrichAssignedCastsWithSSAClosure attaches SSA-derived alias sets for
// casts within a closure body. Closures are separate *ssa.Function objects
// in SSA; this helper locates the correct one by matching positions.
func enrichAssignedCastsWithSSAClosure(
	ssaRes *ssaResult,
	lit *ast.FuncLit,
	assignedCasts []cfaAssignedCast,
) {
	if ssaRes == nil || ssaRes.Pkg == nil || lit == nil {
		return
	}
	ssaFn := findSSAClosureFunc(ssaRes.Pkg, lit.Pos())
	if ssaFn == nil {
		return
	}
	for i := range assignedCasts {
		assignedCasts[i].target = enrichTargetWithSSAAlias(
			ssaFn, assignedCasts[i],
		)
	}
}

// findSSAClosureFunc locates an anonymous *ssa.Function by matching
// its position against the FuncLit position. Anonymous functions in SSA
// are nested inside their parent functions.
func findSSAClosureFunc(pkg *ssa.Package, pos token.Pos) *ssa.Function {
	if pkg == nil || !pos.IsValid() {
		return nil
	}
	// Walk all functions in the package (including anonymous ones)
	// looking for one whose position matches the FuncLit.
	for _, mem := range pkg.Members {
		fn, ok := mem.(*ssa.Function)
		if !ok {
			continue
		}
		if found := searchAnonymousFuncs(fn, pos); found != nil {
			return found
		}
	}
	return nil
}

// searchAnonymousFuncs recursively searches a function and its anonymous
// children for one matching the given position.
func searchAnonymousFuncs(fn *ssa.Function, pos token.Pos) *ssa.Function {
	for _, anon := range fn.AnonFuncs {
		if anon.Pos() == pos {
			return anon
		}
		if found := searchAnonymousFuncs(anon, pos); found != nil {
			return found
		}
	}
	return nil
}
