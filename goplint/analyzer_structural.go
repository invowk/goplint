// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// funcOptionsParamThreshold is the maximum number of non-option parameters
// a constructor should have before the functional options pattern is
// suggested. Constructors with more than this many params are flagged.
const funcOptionsParamThreshold = 3

// --- Phase 1: Collectors ---

// collectOptionTypes identifies named func types that follow the functional
// options pattern: type XxxOption func(*Xxx). For each such type, it records
// the option type name → target struct name mapping.
func collectOptionTypes(pass *analysis.Pass, node *ast.GenDecl, out map[string]string) {
	if node.Tok != token.TYPE {
		return
	}

	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		// Skip type aliases — they inherit the aliased type's identity.
		if ts.Assign.IsValid() {
			continue
		}

		// Only care about func types at the AST level.
		if _, isFuncType := ts.Type.(*ast.FuncType); !isFuncType {
			continue
		}

		// Use the type checker to resolve the underlying signature and
		// check if it matches the func(*Struct) option pattern.
		obj := pass.TypesInfo.Defs[ts.Name]
		if obj == nil {
			continue
		}

		targetName, ok := isOptionFuncType(obj.Type())
		if !ok {
			continue
		}

		out[ts.Name.Name] = targetName
	}
}

// trackConstructorDetails records constructor functions (NewXxx) with their
// return type, parameter count, and whether they accept variadic options.
// This replaces the simpler trackConstructors when structural checks are active.
func trackConstructorDetails(pass *analysis.Pass, fn *ast.FuncDecl, seen map[string]*constructorFuncInfo) {
	if fn.Recv != nil {
		return // methods are not constructors
	}
	name := fn.Name.Name
	if !strings.HasPrefix(name, "New") || len(name) <= 3 {
		return
	}

	info := &constructorFuncInfo{
		pos: fn.Name.Pos(),
	}

	// Resolve return type name and detect interface returns.
	if fn.Type.Results != nil {
		info.returnTypeName = resolveReturnTypeName(pass, fn.Type.Results)
		info.returnsInterface = returnsInterface(pass, fn.Type.Results)
	}

	// Count parameters and detect variadic option pattern.
	if fn.Type.Params != nil {
		info.paramCount = countParams(fn.Type.Params)

		// Check if the last parameter is a variadic option type.
		if info.paramCount > 0 {
			lastField := fn.Type.Params.List[len(fn.Type.Params.List)-1]
			if _, isEllipsis := lastField.Type.(*ast.Ellipsis); isEllipsis {
				ellipsis := lastField.Type.(*ast.Ellipsis)
				elemType := pass.TypesInfo.TypeOf(ellipsis.Elt)
				if elemType != nil {
					if targetName, ok := isOptionFuncType(elemType); ok {
						info.hasVariadicOpt = true
						info.variadicOptionTarget = targetName
						if named, isNamed := types.Unalias(elemType).(*types.Named); isNamed {
							info.variadicOptionTypeName = named.Obj().Name()
						}
						// Subtract the variadic option param from the count.
						// countParams counts each name in a field, but variadic
						// is typically unnamed or has one name.
						names := len(lastField.Names)
						if names == 0 {
							names = 1
						}
						info.paramCount -= names
					}
				}
			}
		}
	}

	seen[name] = info
}

// countParams counts the total number of parameters in a field list,
// accounting for multi-name fields like (a, b int) counting as 2.
func countParams(fields *ast.FieldList) int {
	count := 0
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			count++ // unnamed param
		} else {
			count += len(field.Names)
		}
	}
	return count
}

// trackWithFunctions records free functions named WithXxx that return a
// known option type, mapping them to their target struct.
func trackWithFunctions(pass *analysis.Pass, fn *ast.FuncDecl, optionTypes map[string]string, out map[string][]string) {
	if fn.Recv != nil {
		return // methods are not option functions
	}

	name := fn.Name.Name
	if !strings.HasPrefix(name, "With") || len(name) <= 4 {
		return
	}

	// Must have exactly one return type.
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return
	}

	retType := pass.TypesInfo.TypeOf(fn.Type.Results.List[0].Type)
	if retType == nil {
		return
	}

	// Resolve aliases so type aliases to option types are recognized.
	retType = types.Unalias(retType)

	// Check if return type is a known option type.
	retTypeName := ""
	if named, ok := retType.(*types.Named); ok {
		retTypeName = named.Obj().Name()
	}
	if retTypeName == "" {
		return
	}

	targetStruct, isOption := optionTypes[retTypeName]
	if !isOption {
		return
	}

	out[targetStruct] = append(out[targetStruct], name)
}

// collectExportedStructsWithFields extends the basic struct collection to
// also record field metadata (name, visibility, position) for immutability
// and functional options analysis.
func collectExportedStructsWithFields(pass *analysis.Pass, node *ast.GenDecl, out *[]exportedStructInfo) {
	if node.Tok != token.TYPE {
		return
	}

	for _, spec := range node.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}

		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}

		// Only exported structs.
		if !ts.Name.IsExported() {
			continue
		}

		info := exportedStructInfo{
			name: ts.Name.Name,
			pos:  ts.Name.Pos(),
		}

		// Collect field metadata for structural checks.
		for _, field := range st.Fields.List {
			isInternal := hasInternalDirective(field.Doc, field.Comment)
			for _, fieldName := range field.Names {
				info.fields = append(info.fields, structFieldMeta{
					name:     fieldName.Name,
					exported: fieldName.IsExported(),
					internal: isInternal,
					pos:      fieldName.Pos(),
				})
			}
			// Skip embedded/anonymous fields — they don't need WithXxx
			// functions and are not individually mutable.
		}

		*out = append(*out, info)
	}
}

// --- Phase 2: Reporters ---

// reportWrongConstructorSig reports constructors whose return type does not
// match the struct they are supposed to construct. Uses exact match for
// the primary constructor (NewXxx) plus prefix match for variant constructors
// (NewXxxFromY) that DO return the right type — but flags variants whose
// return type is wrong. This avoids false positives where NewFooBar
// (intended for FooBar) is flagged against Foo.
func reportWrongConstructorSig(pass *analysis.Pass, structs []exportedStructInfo, ctors map[string]*constructorFuncInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)

	for _, s := range structs {
		qualName := fmt.Sprintf("%s.%s", pkgName, s.name)
		if cfg.isExcepted(qualName + ".constructor-sig") {
			continue
		}

		// Check all constructors matching this struct by prefix.
		prefix := "New" + s.name
		for ctorName, ctorInfo := range ctors {
			if !strings.HasPrefix(ctorName, prefix) {
				continue
			}

			// For variant constructors (name longer than "New" + structName),
			// only check if they return the target struct's type. If they
			// return a different type, they likely target a different struct
			// (e.g., NewCommandScope targets CommandScope, not Command).
			isExact := ctorName == prefix
			if !isExact && ctorInfo.returnTypeName != s.name && !ctorInfo.returnsInterface {
				continue
			}

			// Interface returns are valid factory patterns — skip the check.
			if ctorInfo.returnsInterface {
				continue
			}

			var msg string
			switch {
			case ctorInfo.returnTypeName == "":
				msg = fmt.Sprintf("constructor %s() for %s has no return type", ctorName, qualName)
			case ctorInfo.returnTypeName == s.name:
				continue // correct return type
			default:
				msg = fmt.Sprintf("constructor %s() for %s returns %s, expected %s",
					ctorName, qualName, ctorInfo.returnTypeName, s.name)
			}

			findingID := StableFindingID(CategoryWrongConstructorSig, qualName, ctorName)
			if bl.ContainsFinding(CategoryWrongConstructorSig, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, ctorInfo.pos, CategoryWrongConstructorSig, findingID, msg)
		}
	}
}

// reportMissingFuncOptions reports both:
//   - structs with constructors having too many parameters (should use options)
//   - structs with option types that have incomplete functional options wiring
func reportMissingFuncOptions(
	pass *analysis.Pass,
	structs []exportedStructInfo,
	ctors map[string]*constructorFuncInfo,
	optionTypes map[string]string,
	withFuncs map[string][]string,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	pkgName := packageName(pass.Pkg)

	// Build reverse lookup: structName → option type names.
	structOptionTypes := make(map[string][]string) // structName → []optionTypeName
	for optName, structName := range optionTypes {
		structOptionTypes[structName] = append(structOptionTypes[structName], optName)
	}

	for _, s := range structs {
		// Prefer the exact NewXxx constructor for param-count analysis.
		// Fall back to prefix match for existence checks.
		ctorName := "New" + s.name
		exactCtor := ctors[ctorName]
		anyCtor := findConstructorForStruct(s.name, ctors)

		qualName := fmt.Sprintf("%s.%s", pkgName, s.name)
		if cfg.isExcepted(qualName + ".func-options") {
			continue
		}

		optTypeNames := structOptionTypes[s.name]
		hasOptType := len(optTypeNames) > 0
		optTypeName := canonicalOptionTypeName(optTypeNames)

		// Sub-check A: Detection — too many non-option params without options.
		// Uses exact constructor match (NewXxx) for param-count analysis since
		// variant constructors may have different param counts.
		if exactCtor != nil && !hasOptType && exactCtor.paramCount > funcOptionsParamThreshold {
			msg := fmt.Sprintf("constructor %s() for %s has %d non-option parameters; consider using functional options",
				ctorName, qualName, exactCtor.paramCount)
			findingID := StableFindingID(CategoryMissingFuncOptions, qualName, ctorName, "detection")
			if !bl.ContainsFinding(CategoryMissingFuncOptions, findingID, msg) {
				reportDiagnostic(pass, exactCtor.pos, CategoryMissingFuncOptions, findingID, msg)
			}
		}

		// Sub-check B: Completeness — struct has an option type.
		if !hasOptType {
			continue
		}

		// B1: Constructor should accept variadic option param.
		// Uses exact constructor for variadic check.
		exactCtorHasMatchingVariadic := exactCtor != nil &&
			exactCtor.hasVariadicOpt &&
			exactCtor.variadicOptionTarget == s.name
		if exactCtor != nil && !exactCtorHasMatchingVariadic {
			msg := fmt.Sprintf("constructor %s() for %s does not accept variadic ...%s",
				ctorName, qualName, optTypeName)
			findingID := StableFindingID(CategoryMissingFuncOptions, qualName, ctorName, "variadic")
			if !bl.ContainsFinding(CategoryMissingFuncOptions, findingID, msg) {
				reportDiagnostic(pass, exactCtor.pos, CategoryMissingFuncOptions, findingID, msg)
			}
		}

		// If no exact constructor but a variant exists, check the variant
		// for variadic option support.
		anyCtorHasMatchingVariadic := anyCtor != nil &&
			anyCtor.hasVariadicOpt &&
			anyCtor.variadicOptionTarget == s.name
		if exactCtor == nil && anyCtor != nil && !anyCtorHasMatchingVariadic {
			msg := fmt.Sprintf("constructor %s() for %s does not accept variadic ...%s",
				ctorName, qualName, optTypeName)
			findingID := StableFindingID(CategoryMissingFuncOptions, qualName, ctorName, "variadic")
			if !bl.ContainsFinding(CategoryMissingFuncOptions, findingID, msg) {
				reportDiagnostic(pass, anyCtor.pos, CategoryMissingFuncOptions, findingID, msg)
			}
		}

		// B2: Each unexported field should have a WithFieldName() function.
		withSet := make(map[string]bool, len(withFuncs[s.name]))
		for _, w := range withFuncs[s.name] {
			withSet[w] = true
		}

		for _, f := range s.fields {
			if f.exported {
				continue // exported fields aren't set via options
			}
			if f.internal {
				continue // //plint:internal — internal state, not user-configurable
			}

			expectedWith := "With" + capitalizeFirst(f.name)
			if withSet[expectedWith] {
				continue
			}

			msg := fmt.Sprintf("struct %s has %s type but field %q has no %s() function",
				qualName, optTypeName, f.name, expectedWith)
			findingID := StableFindingID(CategoryMissingFuncOptions, qualName, f.name, "missing-with")
			if !bl.ContainsFinding(CategoryMissingFuncOptions, findingID, msg) {
				reportDiagnostic(pass, f.pos, CategoryMissingFuncOptions, findingID, msg)
			}
		}
	}
}

// reportMissingImmutability reports exported struct fields on types that have
// a constructor. If a struct uses a NewXxx constructor, its fields should be
// unexported to enforce immutability after construction. Uses prefix matching
// to find constructors (e.g., NewConfigFromFile for Config).
func reportMissingImmutability(pass *analysis.Pass, structs []exportedStructInfo, ctors map[string]*constructorFuncInfo, cfg *ExceptionConfig, bl *BaselineConfig) {
	pkgName := packageName(pass.Pkg)

	for _, s := range structs {
		if findConstructorForStruct(s.name, ctors) == nil {
			continue // no constructor — exported fields are fine
		}

		ctorName := "New" + s.name
		qualName := fmt.Sprintf("%s.%s", pkgName, s.name)
		if cfg.isExcepted(qualName + ".immutability") {
			continue
		}

		for _, f := range s.fields {
			if !f.exported {
				continue
			}

			msg := fmt.Sprintf("struct %s has %s() constructor but field %s is exported",
				qualName, ctorName, f.name)
			findingID := StableFindingID(CategoryMissingImmutability, qualName, f.name)
			if bl.ContainsFinding(CategoryMissingImmutability, findingID, msg) {
				continue
			}

			reportDiagnostic(pass, f.pos, CategoryMissingImmutability, findingID, msg)
		}
	}
}

// reportMissingStructIsValid reports exported struct types that have a
// constructor but lack an IsValid() (bool, []error) method. Struct types
// with constructors should validate their invariants via IsValid().
// Error types are excluded (same logic as --check-constructors).
func reportMissingStructIsValid(
	pass *analysis.Pass,
	structs []exportedStructInfo,
	ctors map[string]*constructorFuncInfo,
	methods map[string]*methodInfo,
	cfg *ExceptionConfig,
	bl *BaselineConfig,
) {
	pkgName := packageName(pass.Pkg)

	for _, s := range structs {
		if findConstructorForStruct(s.name, ctors) == nil {
			continue // no constructor — no obligation
		}

		// Skip error types — they typically don't need IsValid().
		if strings.HasSuffix(s.name, "Error") || methods[s.name+".Error"] != nil {
			continue
		}

		qualName := fmt.Sprintf("%s.%s", pkgName, s.name)
		if cfg.isExcepted(qualName + ".struct-isvalid") {
			continue
		}

		mi := methods[s.name+".IsValid"]
		if mi != nil {
			// Method exists — verify its signature matches the contract.
			if mi.paramCount != 0 || mi.resultTypes != expectedIsValidSig {
				msg := fmt.Sprintf("struct %s has IsValid() but wrong signature (want func() (bool, []error))", qualName)
				findingID := StableFindingID(CategoryWrongStructIsValidSig, qualName, "IsValid")
				if bl.ContainsFinding(CategoryWrongStructIsValidSig, findingID, msg) {
					continue
				}
				reportDiagnostic(pass, s.pos, CategoryWrongStructIsValidSig, findingID, msg)
			}
			continue
		}

		msg := fmt.Sprintf("struct %s has constructor but no IsValid() method", qualName)
		findingID := StableFindingID(CategoryMissingStructIsValid, qualName, "IsValid")
		if bl.ContainsFinding(CategoryMissingStructIsValid, findingID, msg) {
			continue
		}

		reportDiagnostic(pass, s.pos, CategoryMissingStructIsValid, findingID, msg)
	}
}

// canonicalOptionTypeName returns a deterministic option type name from a
// candidate list. When multiple aliases target the same struct, diagnostics
// use the lexicographically smallest name for stable output.
func canonicalOptionTypeName(optionTypeNames []string) string {
	if len(optionTypeNames) == 0 {
		return ""
	}
	names := append([]string(nil), optionTypeNames...)
	slices.Sort(names)
	return names[0]
}

// capitalizeFirst returns s with its first rune uppercased.
// Used to convert field names to expected WithXxx function names
// (e.g., "shell" → "Shell" for "WithShell").
func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
