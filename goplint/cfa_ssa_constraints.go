// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
	gocfg "golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/ssa"
)

const cfgSSAConstraintAlternativeLimit = 256

const cfgSSAPredicateFormulaPrefix = "ssa-formula-v1:"

type cfgSSAConstraintFormula struct {
	alternatives [][]cfgPredicateConstraint
	// unsupported marks alternatives as a conservative over-approximation of
	// the original predicate. An unsupported atom contributes true, so a
	// contradiction in every retained alternative is still a sound UNSAT proof;
	// any satisfiable retained alternative must remain UNKNOWN.
	unsupported bool
}

func cfgSSAUnsupportedFormula() cfgSSAConstraintFormula {
	return cfgSSAConstraintFormula{
		alternatives: [][]cfgPredicateConstraint{{}},
		unsupported:  true,
	}
}

type cfgSSAValueIndex struct {
	expressions map[ast.Expr]ssa.Value
	objects     map[types.Object]ssa.Value
}

func newCFGSSAValueIndex() cfgSSAValueIndex {
	return cfgSSAValueIndex{
		expressions: make(map[ast.Expr]ssa.Value),
		objects:     make(map[types.Object]ssa.Value),
	}
}

func (index cfgSSAValueIndex) empty() bool {
	return len(index.expressions) == 0 && len(index.objects) == 0
}

func extractSSAWitnessConstraintsWithControl(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	path []int32,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if pass == nil {
		return cfgSSAUnsupportedFormula(), false
	}
	if cfg == nil || len(path) < 2 {
		return cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}, false
	}
	valueIndex, expired := buildCFGSSAValueIndexWithControl(pass, control)
	if expired {
		return cfgSSAConstraintFormula{}, true
	}
	if valueIndex.empty() {
		return cfgSSAUnsupportedFormula(), false
	}
	blocksByIndex := make(map[int32]*gocfg.Block, len(cfg.Blocks))
	for _, block := range cfg.Blocks {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		if block != nil {
			blocksByIndex[block.Index] = block
		}
	}
	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}
	for index := 0; index+1 < len(path); index++ {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		from := blocksByIndex[path[index]]
		to := blocksByIndex[path[index+1]]
		if from == nil || to == nil || len(from.Succs) != 2 || len(from.Nodes) == 0 {
			continue
		}
		condition, ok := from.Nodes[len(from.Nodes)-1].(ast.Expr)
		if !ok || condition == nil {
			continue
		}
		truthy, ok := cfgPathSuccessorPolarity(from, to)
		if !ok {
			continue
		}
		predicate, expired := extractSSAPredicateFormulaWithControl(pass, valueIndex, condition, truthy, control)
		if expired {
			return cfgSSAConstraintFormula{}, true
		}
		if predicate.unsupported {
			switchPredicate := extractSSASwitchCasePredicate(pass, valueIndex, from, truthy)
			if !switchPredicate.unsupported {
				predicate = switchPredicate
			}
		}
		formula, expired = cfgSSAFormulaAndWithControl(formula, predicate, control)
		if expired {
			return cfgSSAConstraintFormula{}, true
		}
	}
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if formula.normalizeWithControl(control) {
		return cfgSSAConstraintFormula{}, true
	}
	return formula, false
}

func extractSSAConstraintsForWitnessRecordWithControl(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	witness cfgWitnessRecord,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	originPath := witness.FactOriginPath
	if len(originPath) == 0 {
		originPath = witness.CFGPath
	}
	prefix, expired := extractSSADominatingConstraintsForWitnessStartWithControl(
		pass,
		cfg,
		originPath,
		control,
	)
	if expired {
		return prefix, expired
	}
	if len(witness.WitnessEdges) == 0 {
		suffix, suffixExpired := extractSSAWitnessConstraintsWithControl(pass, cfg, witness.CFGPath, control)
		if suffixExpired {
			return cfgSSAConstraintFormula{}, true
		}
		return cfgSSAFormulaAndWithControl(prefix, suffix, control)
	}
	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}
	for _, edge := range witness.WitnessEdges {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		for _, provenance := range edge.PredicateProvenance {
			if feasibilityDeadlineReached(control) {
				return cfgSSAConstraintFormula{}, true
			}
			predicate, expired := cfgSSAFormulaFromPredicateProvenanceWithControl(provenance, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			formula, expired = cfgSSAFormulaAndWithControl(formula, predicate, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
		}
	}
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if formula.normalizeWithControl(control) {
		return cfgSSAConstraintFormula{}, true
	}
	return cfgSSAFormulaAndWithControl(prefix, formula, control)
}

// extractSSADominatingConstraintsForWitnessStartWithControl recovers the
// mandatory control prefix that precedes a fact created inside a branch. IFDS
// starts a cast obligation at its definition block, so those entry-to-origin
// predicates are not part of the forward witness edges. A branch polarity is
// admitted only when its successor dominates the origin block; this makes the
// predicate necessary on every path that can create the tracked fact.
func extractSSADominatingConstraintsForWitnessStartWithControl(
	pass *analysis.Pass,
	cfg *gocfg.CFG,
	path []int32,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	formula := cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if pass == nil || cfg == nil || len(cfg.Blocks) == 0 || len(path) == 0 {
		return formula, false
	}
	targetIndex := path[0]
	dominators := protocolCFGBlockDominators(cfg)
	targetDominators, ok := dominators[targetIndex]
	if !ok {
		return cfgSSAUnsupportedFormula(), false
	}
	valueIndex, expired := buildCFGSSAValueIndexWithControl(pass, control)
	if expired {
		return cfgSSAConstraintFormula{}, true
	}
	if valueIndex.empty() {
		return cfgSSAUnsupportedFormula(), false
	}
	for _, block := range cfg.Blocks {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		if block == nil || block.Index == targetIndex || !targetDominators[block.Index] ||
			len(block.Succs) != 2 || len(block.Nodes) == 0 {
			continue
		}
		trueDominates := block.Succs[0] != nil && targetDominators[block.Succs[0].Index]
		falseDominates := block.Succs[1] != nil && targetDominators[block.Succs[1].Index]
		if trueDominates == falseDominates {
			continue
		}
		condition, conditionOK := block.Nodes[len(block.Nodes)-1].(ast.Expr)
		if !conditionOK || condition == nil {
			predicate := cfgSSAUnsupportedFormula()
			formula, expired = cfgSSAFormulaAndWithControl(formula, predicate, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			continue
		}
		predicate, predicateExpired := extractSSAPredicateFormulaWithControl(
			pass,
			valueIndex,
			condition,
			trueDominates,
			control,
		)
		if predicateExpired {
			return cfgSSAConstraintFormula{}, true
		}
		if predicate.unsupported {
			switchPredicate := extractSSASwitchCasePredicate(pass, valueIndex, block, trueDominates)
			if !switchPredicate.unsupported {
				predicate = switchPredicate
			}
		}
		formula, expired = cfgSSAFormulaAndWithControl(formula, predicate, control)
		if expired {
			return formula, expired
		}
	}
	if formula.normalizeWithControl(control) {
		return cfgSSAConstraintFormula{}, true
	}
	return formula, false
}

func cfgSSAFormulaFromPredicateProvenance(provenance string) cfgSSAConstraintFormula {
	formula, _ := cfgSSAFormulaFromPredicateProvenanceWithControl(provenance, nil)
	return formula
}

func cfgSSAFormulaFromPredicateProvenanceWithControl(
	provenance string,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if !strings.HasPrefix(provenance, cfgSSAPredicateFormulaPrefix) {
		return cfgSSAUnsupportedFormula(), false
	}
	encoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(provenance, cfgSSAPredicateFormulaPrefix))
	if err != nil {
		return cfgSSAUnsupportedFormula(), false
	}
	type encodedConstraint struct {
		Subject string `json:"subject"`
		Op      string `json:"op"`
		Value   string `json:"value"`
	}
	var decoded [][]encodedConstraint
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return cfgSSAUnsupportedFormula(), false
	}
	formula := cfgSSAConstraintFormula{alternatives: make([][]cfgPredicateConstraint, 0, len(decoded))}
	for _, encodedAlternative := range decoded {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		alternative := make([]cfgPredicateConstraint, 0, len(encodedAlternative))
		for _, encodedAtom := range encodedAlternative {
			if feasibilityDeadlineReached(control) {
				return cfgSSAConstraintFormula{}, true
			}
			if encodedAtom.Subject == "" || !isCFGComparisonOp(encodedAtom.Op) || encodedAtom.Value == "" {
				return cfgSSAUnsupportedFormula(), false
			}
			alternative = append(alternative, cfgPredicateConstraint{
				subject: encodedAtom.Subject,
				op:      encodedAtom.Op,
				value:   encodedAtom.Value,
			})
		}
		formula.alternatives = append(formula.alternatives, alternative)
	}
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if formula.normalizeWithControl(control) {
		return cfgSSAConstraintFormula{}, true
	}
	return formula, false
}

func encodeCFGSSAPredicateFormula(formula cfgSSAConstraintFormula) string {
	if formula.unsupported {
		return ""
	}
	formula.normalize()
	type encodedConstraint struct {
		Subject string `json:"subject"`
		Op      string `json:"op"`
		Value   string `json:"value"`
	}
	encodedFormula := make([][]encodedConstraint, 0, len(formula.alternatives))
	for _, alternative := range formula.alternatives {
		encodedAlternative := make([]encodedConstraint, 0, len(alternative))
		for _, constraint := range alternative {
			encodedAlternative = append(encodedAlternative, encodedConstraint{
				Subject: constraint.subject,
				Op:      constraint.op,
				Value:   constraint.value,
			})
		}
		encodedFormula = append(encodedFormula, encodedAlternative)
	}
	encoded, err := json.Marshal(encodedFormula)
	if err != nil {
		return ""
	}
	return cfgSSAPredicateFormulaPrefix + base64.RawURLEncoding.EncodeToString(encoded)
}

func extractSSASwitchCasePredicate(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	block *gocfg.Block,
	truthy bool,
) cfgSSAConstraintFormula {
	if block == nil || len(block.Nodes) < 2 {
		return cfgSSAUnsupportedFormula()
	}
	subject, subjectOK := block.Nodes[len(block.Nodes)-2].(ast.Expr)
	constant, constantOK := block.Nodes[len(block.Nodes)-1].(ast.Expr)
	if !subjectOK || !constantOK {
		return cfgSSAUnsupportedFormula()
	}
	atom, ok := extractSSAComparisonAtom(pass, values, subject, constant, token.EQL)
	if !ok {
		return cfgSSAUnsupportedFormula()
	}
	atom.op = "neq"
	if truthy {
		atom.op = "eq"
	}
	return cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{atom}}}
}

func cfgPathSuccessorPolarity(from, to *gocfg.Block) (bool, bool) {
	if from.Succs[0] != nil && from.Succs[0].Index == to.Index {
		return true, true
	}
	if from.Succs[1] != nil && from.Succs[1].Index == to.Index {
		return false, true
	}
	return false, false
}

func extractSSAPredicateFormula(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	expression ast.Expr,
	truthy bool,
) cfgSSAConstraintFormula {
	formula, _ := extractSSAPredicateFormulaWithControl(pass, values, expression, truthy, nil)
	return formula
}

func extractSSAPredicateFormulaWithControl(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	expression ast.Expr,
	truthy bool,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	switch node := stripParens(expression).(type) {
	case *ast.Ident:
		if node.Name == "true" {
			return cfgSSAConstantFormula(truthy), false
		}
		if node.Name == "false" {
			return cfgSSAConstantFormula(!truthy), false
		}
		value, ok := cfgSSAValueForExpr(pass, values, node)
		if !ok || value == nil || !cfgSSAComparisonSupported(value.Type(), "true") {
			return cfgSSAUnsupportedFormula(), false
		}
		constant := "false"
		if truthy {
			constant = "true"
		}
		return cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{{
			subject: cfgSSAValueKey(value),
			op:      "eq",
			value:   constant,
		}}}}, false
	case *ast.UnaryExpr:
		if node.Op != token.NOT {
			return cfgSSAUnsupportedFormula(), false
		}
		return extractSSAPredicateFormulaWithControl(pass, values, node.X, !truthy, control)
	case *ast.BinaryExpr:
		switch node.Op {
		case token.LAND:
			left, expired := extractSSAPredicateFormulaWithControl(pass, values, node.X, truthy, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			right, expired := extractSSAPredicateFormulaWithControl(pass, values, node.Y, truthy, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			if truthy {
				return cfgSSAFormulaAndWithControl(left, right, control)
			}
			return cfgSSAFormulaOrWithControl(left, right, control)
		case token.LOR:
			left, expired := extractSSAPredicateFormulaWithControl(pass, values, node.X, truthy, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			right, expired := extractSSAPredicateFormulaWithControl(pass, values, node.Y, truthy, control)
			if expired {
				return cfgSSAConstraintFormula{}, true
			}
			if truthy {
				return cfgSSAFormulaOrWithControl(left, right, control)
			}
			return cfgSSAFormulaAndWithControl(left, right, control)
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			atom, ok := extractSSAComparisonAtom(pass, values, node.X, node.Y, node.Op)
			if !ok {
				atom, ok = extractSSAComparisonAtom(pass, values, node.Y, node.X, reverseComparisonToken(node.Op))
			}
			if !ok {
				return cfgSSAUnsupportedFormula(), false
			}
			if !truthy {
				atom.op = negateCFGComparisonOp(atom.op)
			}
			return cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{atom}}}, false
		default:
			return cfgSSAUnsupportedFormula(), false
		}
	default:
		return cfgSSAUnsupportedFormula(), false
	}
}

func cfgSSAConstantFormula(value bool) cfgSSAConstraintFormula {
	if value {
		return cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}
	}
	return cfgSSAConstraintFormula{}
}

func cfgSSAFormulaAndWithControl(
	left, right cfgSSAConstraintFormula,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if len(left.alternatives) == 0 || len(right.alternatives) == 0 {
		return cfgSSAConstraintFormula{unsupported: left.unsupported || right.unsupported}, false
	}
	if len(left.alternatives)*len(right.alternatives) > cfgSSAConstraintAlternativeLimit {
		return cfgSSAUnsupportedFormula(), false
	}
	result := cfgSSAConstraintFormula{
		alternatives: make([][]cfgPredicateConstraint, 0, len(left.alternatives)*len(right.alternatives)),
		unsupported:  left.unsupported || right.unsupported,
	}
	for _, leftAlternative := range left.alternatives {
		if feasibilityDeadlineReached(control) {
			return cfgSSAConstraintFormula{}, true
		}
		for _, rightAlternative := range right.alternatives {
			if feasibilityDeadlineReached(control) {
				return cfgSSAConstraintFormula{}, true
			}
			combined := append([]cfgPredicateConstraint(nil), leftAlternative...)
			combined = append(combined, rightAlternative...)
			result.alternatives = append(result.alternatives, combined)
		}
	}
	return result, feasibilityDeadlineReached(control)
}

func cfgSSAFormulaOrWithControl(
	left, right cfgSSAConstraintFormula,
	control protocolAnalysisControl,
) (cfgSSAConstraintFormula, bool) {
	if feasibilityDeadlineReached(control) {
		return cfgSSAConstraintFormula{}, true
	}
	if len(left.alternatives)+len(right.alternatives) > cfgSSAConstraintAlternativeLimit {
		return cfgSSAUnsupportedFormula(), false
	}
	result := cfgSSAConstraintFormula{
		alternatives: make([][]cfgPredicateConstraint, 0, len(left.alternatives)+len(right.alternatives)),
		unsupported:  left.unsupported || right.unsupported,
	}
	result.alternatives = append(result.alternatives, left.alternatives...)
	result.alternatives = append(result.alternatives, right.alternatives...)
	return result, feasibilityDeadlineReached(control)
}

func extractSSAComparisonAtom(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	subjectExpression ast.Expr,
	valueExpression ast.Expr,
	operator token.Token,
) (cfgPredicateConstraint, bool) {
	value, ok := cfgSSAValueForExpr(pass, values, subjectExpression)
	if !ok || value == nil {
		return cfgPredicateConstraint{}, false
	}
	if operator != token.EQL && operator != token.NEQ {
		if _, loopPhi := value.(*ssa.Phi); loopPhi {
			// Ordered Phi reasoning needs induction/initial-value constraints.
			// Treating the current bound alone as SAT would make an infeasible
			// zero-iteration loop path look realizable.
			return cfgPredicateConstraint{}, false
		}
	}
	constant, ok := exprConstraintValue(pass, valueExpression)
	if !ok || !cfgSSAComparisonSupported(value.Type(), constant) {
		return cfgPredicateConstraint{}, false
	}
	comparisonOp := cfgComparisonOp(operator)
	return cfgPredicateConstraint{
		subject: cfgSSAValueKey(value),
		op:      comparisonOp,
		value:   constant,
	}, comparisonOp != ""
}

func cfgComparisonOp(operator token.Token) string {
	switch operator {
	case token.EQL:
		return "eq"
	case token.NEQ:
		return "neq"
	case token.LSS:
		return "lt"
	case token.LEQ:
		return "le"
	case token.GTR:
		return "gt"
	case token.GEQ:
		return "ge"
	default:
		return ""
	}
}

func reverseComparisonToken(operator token.Token) token.Token {
	switch operator {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	default:
		return operator
	}
}

func negateCFGComparisonOp(operator string) string {
	switch operator {
	case "eq":
		return "neq"
	case "neq":
		return "eq"
	case "lt":
		return "ge"
	case "le":
		return "gt"
	case "gt":
		return "le"
	case "ge":
		return "lt"
	default:
		return ""
	}
}

func isCFGComparisonOp(operator string) bool {
	switch operator {
	case "eq", "neq", "lt", "le", "gt", "ge":
		return true
	default:
		return false
	}
}

func cfgSSAComparisonSupported(subjectType types.Type, constant string) bool {
	if subjectType == nil {
		return false
	}
	underlying := types.Unalias(subjectType).Underlying()
	if constant == "<nil>" {
		switch underlying.(type) {
		case *types.Chan, *types.Interface, *types.Map, *types.Pointer, *types.Signature, *types.Slice:
			return true
		default:
			basic, ok := underlying.(*types.Basic)
			return ok && basic.Kind() == types.UnsafePointer
		}
	}
	basic, ok := underlying.(*types.Basic)
	if !ok {
		return false
	}
	switch basic.Kind() {
	case types.Bool, types.String,
		types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
		types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
		return true
	default:
		return false
	}
}

func cfgSSAValueKey(value ssa.Value) string {
	parent := "package"
	if value.Parent() != nil {
		parent = value.Parent().String()
	}
	// SSA value names are unique within their parent function. Raw token.Pos
	// values depend on package-loading/file-set order and made otherwise
	// identical refinement evidence vary between analyzer runs.
	return fmt.Sprintf("%s|%T|%s", parent, value, value.Name())
}

func buildCFGSSAValueIndexWithControl(
	pass *analysis.Pass,
	control protocolAnalysisControl,
) (cfgSSAValueIndex, bool) {
	if feasibilityDeadlineReached(control) {
		return newCFGSSAValueIndex(), true
	}
	ssaResult := buildSSAForPass(pass)
	if feasibilityDeadlineReached(control) {
		return newCFGSSAValueIndex(), true
	}
	return buildCFGSSAValueIndexFromResultWithControl(ssaResult, control)
}

func buildCFGSSAValueIndexFromResult(ssaResult *ssaResult) cfgSSAValueIndex {
	if result, ok := cachedCFGSSAValueIndex(ssaResult); ok {
		return result
	}
	result, expired := buildCFGSSAValueIndexFromResultWithControl(ssaResult, nil)
	if !expired {
		storeCFGSSAValueIndex(ssaResult, result)
	}
	return result
}

func buildCFGSSAValueIndexFromResultWithControl(
	ssaResult *ssaResult,
	control protocolAnalysisControl,
) (cfgSSAValueIndex, bool) {
	if cached, ok := cachedCFGSSAValueIndex(ssaResult); ok {
		return cached, false
	}
	result := newCFGSSAValueIndex()
	if feasibilityDeadlineReached(control) {
		return result, true
	}
	if !ssaResult.availability().ready() {
		return result, false
	}
	for _, function := range protocolPackageFunctions(ssaResult) {
		if feasibilityDeadlineReached(control) {
			return newCFGSSAValueIndex(), true
		}
		if function.Synthetic == "" {
			if indexCFGSSAFunctionValuesWithControl(function, result, control) {
				return newCFGSSAValueIndex(), true
			}
		}
	}
	expired := feasibilityDeadlineReached(control)
	if !expired {
		storeCFGSSAValueIndex(ssaResult, result)
	}
	return result, expired
}

func cachedCFGSSAValueIndex(ssaResult *ssaResult) (cfgSSAValueIndex, bool) {
	if ssaResult == nil {
		return cfgSSAValueIndex{}, false
	}
	ssaResult.valueIndexMu.RLock()
	defer ssaResult.valueIndexMu.RUnlock()
	return ssaResult.valueIndex, ssaResult.valueIndexReady
}

func storeCFGSSAValueIndex(ssaResult *ssaResult, index cfgSSAValueIndex) {
	if ssaResult == nil {
		return
	}
	ssaResult.valueIndexMu.Lock()
	defer ssaResult.valueIndexMu.Unlock()
	if ssaResult.valueIndexReady {
		return
	}
	ssaResult.valueIndex = index
	ssaResult.valueIndexReady = true
}

func cfgSSAPredicateProvenance(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	from *gocfg.Block,
	to *gocfg.Block,
) []string {
	if from == nil || to == nil || len(from.Succs) != 2 || len(from.Nodes) == 0 {
		return nil
	}
	truthy, ok := cfgPathSuccessorPolarity(from, to)
	if !ok {
		return []string{fmt.Sprintf("ssa-invalid-successor|from=%d|to=%d", from.Index, to.Index)}
	}
	condition, ok := from.Nodes[len(from.Nodes)-1].(ast.Expr)
	if !ok || condition == nil {
		encodedFormula := encodeCFGSSAPredicateFormula(
			cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}},
		)
		return []string{encodedFormula}
	}
	if pass == nil || values.empty() {
		return []string{fmt.Sprintf(
			"ssa-unavailable|from=%d|to=%d|truthy=%t",
			from.Index,
			to.Index,
			truthy,
		)}
	}
	formula := extractSSAPredicateFormula(pass, values, condition, truthy)
	if formula.unsupported {
		formula = extractSSASwitchCasePredicate(pass, values, from, truthy)
	}
	if formula.unsupported && cfgSSAPredicateIsNondeterministicBranch(condition) {
		formula = cfgSSAConstraintFormula{alternatives: [][]cfgPredicateConstraint{{}}}
	}
	if formula.unsupported {
		return []string{fmt.Sprintf(
			"ssa-unsupported|from=%d|to=%d|condition=%s|truthy=%t",
			from.Index,
			to.Index,
			semanticNodeKey(pass, condition.Pos()),
			truthy,
		)}
	}
	formula.normalize()
	encodedFormula := encodeCFGSSAPredicateFormula(formula)
	if encodedFormula == "" {
		return []string{fmt.Sprintf(
			"ssa-unsupported|from=%d|to=%d|condition=%s|truthy=%t",
			from.Index,
			to.Index,
			semanticNodeKey(pass, condition.Pos()),
			truthy,
		)}
	}
	return []string{encodedFormula}
}

func cfgSSAPredicateIsNondeterministicBranch(expression ast.Expr) bool {
	switch node := stripParens(expression).(type) {
	case *ast.TypeAssertExpr:
		return true
	case *ast.UnaryExpr:
		return node.Op == token.ARROW
	default:
		return false
	}
}

func indexCFGSSAFunctionValuesWithControl(
	function *ssa.Function,
	result cfgSSAValueIndex,
	control protocolAnalysisControl,
) bool {
	if feasibilityDeadlineReached(control) {
		return true
	}
	if function == nil {
		return false
	}
	for _, parameter := range function.Params {
		if feasibilityDeadlineReached(control) {
			return true
		}
		if parameter != nil {
			result.objects[parameter.Object()] = parameter
		}
	}
	if function.Signature != nil && function.Signature.Recv() != nil && len(function.Params) > 0 {
		result.objects[function.Signature.Recv()] = function.Params[0]
	}
	for _, block := range function.Blocks {
		if feasibilityDeadlineReached(control) {
			return true
		}
		for _, instruction := range block.Instrs {
			if feasibilityDeadlineReached(control) {
				return true
			}
			debugRef, ok := instruction.(*ssa.DebugRef)
			if ok && debugRef.Expr != nil && debugRef.X != nil {
				result.expressions[stripParens(debugRef.Expr)] = debugRef.X
			}
		}
	}
	for _, anonymous := range function.AnonFuncs {
		if indexCFGSSAFunctionValuesWithControl(anonymous, result, control) {
			return true
		}
	}
	return feasibilityDeadlineReached(control)
}

func cfgSSAValueForExpr(
	pass *analysis.Pass,
	values cfgSSAValueIndex,
	expression ast.Expr,
) (ssa.Value, bool) {
	expression = stripParens(expression)
	if value := values.expressions[expression]; value != nil {
		return value, true
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok || pass == nil || pass.TypesInfo == nil {
		return nil, false
	}
	object := pass.TypesInfo.ObjectOf(identifier)
	value := values.objects[object]
	return value, value != nil
}

func (formula *cfgSSAConstraintFormula) normalize() {
	_ = formula.normalizeWithControl(nil)
}

func (formula *cfgSSAConstraintFormula) normalizeWithControl(control protocolAnalysisControl) bool {
	if feasibilityDeadlineReached(control) {
		return true
	}
	if formula == nil {
		return false
	}
	for _, alternative := range formula.alternatives {
		if feasibilityDeadlineReached(control) {
			return true
		}
		sort.Slice(alternative, func(left, right int) bool {
			return cfgConstraintKey(alternative[left]) < cfgConstraintKey(alternative[right])
		})
		if feasibilityDeadlineReached(control) {
			return true
		}
	}
	slices.SortFunc(formula.alternatives, func(left, right []cfgPredicateConstraint) int {
		return slices.CompareFunc(left, right, func(a, b cfgPredicateConstraint) int {
			return stringsCompare(cfgConstraintKey(a), cfgConstraintKey(b))
		})
	})
	return feasibilityDeadlineReached(control)
}

func cfgConstraintKey(constraint cfgPredicateConstraint) string {
	return constraint.subject + "|" + constraint.op + "|" + constraint.value
}

func stringsCompare(left, right string) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}
