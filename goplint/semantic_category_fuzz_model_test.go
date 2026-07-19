// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const semanticCategorySeedFormatVersion = 2

type semanticCategorySeed struct {
	FormatVersion   int                     `json:"format_version"`
	Category        string                  `json:"category"`
	FeatureID       string                  `json:"feature_id"`
	Fixture         string                  `json:"fixture"`
	Symbol          string                  `json:"symbol"`
	ExpectedOutcome string                  `json:"expected_outcome"`
	Program         semanticCategoryProgram `json:"program"`
}

type semanticCategoryProgram struct {
	EntryProcedure string                       `json:"entry_procedure"`
	Identities     []string                     `json:"identities"`
	Aliases        []semanticCategoryAlias      `json:"aliases"`
	Constraints    []semanticCategoryConstraint `json:"constraints"`
	Procedures     []semanticCategoryProcedure  `json:"procedures"`
	Blocks         []semanticCategoryBlock      `json:"blocks"`
}

type semanticCategoryAlias struct {
	Alias  string `json:"alias"`
	Target string `json:"target"`
}

type semanticCategoryConstraint struct {
	ID    string `json:"id"`
	Value bool   `json:"value"`
}

type semanticCategoryProcedure struct {
	ID     string `json:"id"`
	Entry  string `json:"entry"`
	Effect string `json:"effect"`
}

type semanticCategoryBlock struct {
	ID         string                      `json:"id"`
	Operations []semanticCategoryOperation `json:"operations"`
	Successors []semanticCategoryEdge      `json:"successors"`
}

type semanticCategoryOperation struct {
	Kind      string `json:"kind"`
	Identity  string `json:"identity,omitempty"`
	Result    string `json:"result,omitempty"`
	Procedure string `json:"procedure,omitempty"`
}

type semanticCategoryEdge struct {
	To         string `json:"to"`
	Constraint string `json:"constraint,omitempty"`
	Value      bool   `json:"value,omitempty"`
}

type semanticCategoryModelOutcome string

const (
	semanticCategoryOutcomeSafe         semanticCategoryModelOutcome = "safe"
	semanticCategoryOutcomeViolation    semanticCategoryModelOutcome = "violation"
	semanticCategoryOutcomeInconclusive semanticCategoryModelOutcome = "inconclusive"
)

type semanticCategoryModelState string

const (
	semanticCategoryStateNeedsValidation semanticCategoryModelState = "needs-validation"
	semanticCategoryStateValidated       semanticCategoryModelState = "validated"
	semanticCategoryStateUnknown         semanticCategoryModelState = "unknown"
)

func observeSemanticCategorySeed(t *testing.T, data []byte) decodedFuzzSeedObservation {
	t.Helper()

	observation, err := classifySemanticCategorySeed(data)
	if err != nil {
		t.Fatalf("classify semantic category seed: %v", err)
	}
	return observation
}

func classifySemanticCategorySeed(data []byte) (decodedFuzzSeedObservation, error) {
	seed, modelOutcome, features, err := decodeSemanticCategorySeed(data)
	if err != nil {
		return decodedFuzzSeedObservation{}, err
	}
	catalog, err := loadSemanticRuleCatalog()
	if err != nil {
		return decodedFuzzSeedObservation{}, fmt.Errorf("load semantic catalog: %w", err)
	}
	oracle, ok := semanticOraclesByCategory(catalog)[seed.Category]
	if !ok || !slices.ContainsFunc(oracle.MustReport, func(entry semanticOracleEntry) bool {
		return entry.Fixture == seed.Fixture && entry.Symbol == seed.Symbol
	}) {
		return decodedFuzzSeedObservation{}, fmt.Errorf("not an exact must-report oracle tuple: %+v", seed)
	}
	featureIDs := []string{
		"category-" + seed.Category,
		"feature-" + seed.FeatureID,
		"fixture-" + seed.Fixture,
		"model-outcome-" + string(modelOutcome),
		"symbol-" + seed.Symbol,
	}
	featureIDs = append(featureIDs, features...)
	return decodedFuzzSeedObservation{
		FeatureIDs:  canonicalFuzzFeatures(featureIDs),
		Property:    "semantic-category-independent-relation",
		Observation: string(modelOutcome) + "-observed",
		Category:    seed.Category,
		Fixture:     seed.Fixture,
		Symbol:      seed.Symbol,
		Outcome:     string(modelOutcome),
	}, nil
}

func decodeSemanticCategorySeed(
	data []byte,
) (semanticCategorySeed, semanticCategoryModelOutcome, []string, error) {
	var seed semanticCategorySeed
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&seed); err != nil {
		return semanticCategorySeed{}, "", nil, fmt.Errorf("decode: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return semanticCategorySeed{}, "", nil, errors.New("decode: multiple JSON values")
	}
	if seed.FormatVersion != semanticCategorySeedFormatVersion || seed.Category == "" ||
		seed.FeatureID == "" || seed.Fixture == "" || seed.Symbol == "" || seed.ExpectedOutcome == "" {
		return semanticCategorySeed{}, "", nil, fmt.Errorf("incomplete identity: %+v", seed)
	}
	wantFeature, ok := semanticCategorySeedFeature(seed.Category)
	if !ok || wantFeature != seed.FeatureID {
		return semanticCategorySeed{}, "", nil, fmt.Errorf(
			"category %q feature = %q, want %q",
			seed.Category,
			seed.FeatureID,
			wantFeature,
		)
	}
	outcome, features, err := evaluateSemanticCategoryProgram(seed.FeatureID, seed.Program)
	if err != nil {
		return semanticCategorySeed{}, "", nil, fmt.Errorf("evaluate semantic program: %w", err)
	}
	wantOutcome := semanticCategoryOutcomeViolation
	if seed.Category == CategoryMissingConstructorValidateInc ||
		seed.Category == CategoryUseBeforeValidateInconclusive {
		wantOutcome = semanticCategoryOutcomeInconclusive
	}
	if outcome != wantOutcome || seed.ExpectedOutcome != string(outcome) {
		return semanticCategorySeed{}, "", nil, fmt.Errorf(
			"category %q semantic outcome = %q/%q, want %q",
			seed.Category,
			outcome,
			seed.ExpectedOutcome,
			wantOutcome,
		)
	}
	return seed, outcome, features, nil
}

func evaluateSemanticCategoryProgram(
	featureID string,
	program semanticCategoryProgram,
) (semanticCategoryModelOutcome, []string, error) {
	identities, aliases, constraints, procedures, blocks, err := validateSemanticCategoryProgram(program)
	if err != nil {
		return "", nil, err
	}
	entry := procedures[program.EntryProcedure].Entry
	type workItem struct {
		blockID string
		states  map[string]semanticCategoryModelState
	}
	initialStates := make(map[string]semanticCategoryModelState, len(identities))
	for identity := range identities {
		if _, isAlias := aliases[identity]; !isAlias {
			initialStates[identity] = semanticCategoryStateNeedsValidation
		}
	}
	queue := []workItem{{blockID: entry, states: initialStates}}
	seen := make(map[string]bool)
	outcome := semanticCategoryOutcomeSafe
	for len(queue) > 0 {
		if len(seen) > 128 {
			return "", nil, errors.New("semantic program exceeds 128 states")
		}
		item := queue[0]
		queue = queue[1:]
		key := semanticCategoryStateKey(item.blockID, item.states)
		if seen[key] {
			continue
		}
		seen[key] = true
		block := blocks[item.blockID]
		states := cloneSemanticCategoryStates(item.states)
		terminated := false
		for _, operation := range block.Operations {
			identity := resolveSemanticCategoryAlias(operation.Identity, aliases)
			switch operation.Kind {
			case "validate":
				switch operation.Result {
				case "nil":
					states[identity] = semanticCategoryStateValidated
				case "unknown":
					states[identity] = semanticCategoryStateUnknown
				default:
					return "", nil, fmt.Errorf("validate result %q is unsupported", operation.Result)
				}
			case "consume":
				outcome = joinSemanticCategoryOutcome(outcome, outcomeForSemanticCategoryUse(states[identity]))
			case "call":
				procedure := procedures[operation.Procedure]
				switch procedure.Effect {
				case "preserve":
				case "validate":
					states[identity] = semanticCategoryStateValidated
				case "consume":
					outcome = joinSemanticCategoryOutcome(outcome, outcomeForSemanticCategoryUse(states[identity]))
				case "unknown":
					states[identity] = semanticCategoryStateUnknown
				default:
					return "", nil, fmt.Errorf("procedure %q effect %q is unsupported", procedure.ID, procedure.Effect)
				}
			case "return":
				if featureID == semanticFeatureConstructorValidation {
					outcome = joinSemanticCategoryOutcome(outcome, outcomeForSemanticCategoryUse(states[identity]))
				} else if states[identity] == semanticCategoryStateUnknown {
					outcome = joinSemanticCategoryOutcome(outcome, semanticCategoryOutcomeInconclusive)
				}
				terminated = true
			case "unknown-effect":
				states[identity] = semanticCategoryStateUnknown
			default:
				return "", nil, fmt.Errorf("operation kind %q is unsupported", operation.Kind)
			}
		}
		if terminated {
			continue
		}
		for _, edge := range block.Successors {
			if edge.Constraint != "" && constraints[edge.Constraint] != edge.Value {
				continue
			}
			queue = append(queue, workItem{blockID: edge.To, states: cloneSemanticCategoryStates(states)})
		}
	}
	features := []string{
		"semantic-aliases-" + strconv.Itoa(len(program.Aliases)),
		"semantic-blocks-" + strconv.Itoa(len(program.Blocks)),
		"semantic-branches",
		"semantic-calls",
		"semantic-constraints-" + strconv.Itoa(len(program.Constraints)),
		"semantic-effects",
		"semantic-identities-" + strconv.Itoa(len(program.Identities)),
		"semantic-procedures-" + strconv.Itoa(len(program.Procedures)),
		"semantic-returns",
	}
	return outcome, features, nil
}

func validateSemanticCategoryProgram(
	program semanticCategoryProgram,
) (
	map[string]bool,
	map[string]string,
	map[string]bool,
	map[string]semanticCategoryProcedure,
	map[string]semanticCategoryBlock,
	error,
) {
	if program.EntryProcedure == "" || len(program.Identities) < 2 || len(program.Aliases) == 0 ||
		len(program.Constraints) == 0 || len(program.Procedures) == 0 || len(program.Blocks) < 3 {
		return nil, nil, nil, nil, nil, errors.New("semantic program omits a required variable structure")
	}
	identities := make(map[string]bool, len(program.Identities))
	for _, identity := range program.Identities {
		if identity == "" || identities[identity] {
			return nil, nil, nil, nil, nil, fmt.Errorf("empty or duplicate identity %q", identity)
		}
		identities[identity] = true
	}
	aliases := make(map[string]string, len(program.Aliases))
	for _, alias := range program.Aliases {
		if !identities[alias.Alias] || !identities[alias.Target] || alias.Alias == alias.Target || aliases[alias.Alias] != "" {
			return nil, nil, nil, nil, nil, fmt.Errorf("invalid alias %+v", alias)
		}
		aliases[alias.Alias] = alias.Target
	}
	constraints := make(map[string]bool, len(program.Constraints))
	for _, constraint := range program.Constraints {
		if constraint.ID == "" {
			return nil, nil, nil, nil, nil, errors.New("empty constraint identity")
		}
		if _, duplicate := constraints[constraint.ID]; duplicate {
			return nil, nil, nil, nil, nil, fmt.Errorf("duplicate constraint %q", constraint.ID)
		}
		constraints[constraint.ID] = constraint.Value
	}
	procedures := make(map[string]semanticCategoryProcedure, len(program.Procedures))
	for _, procedure := range program.Procedures {
		if procedure.ID == "" || procedure.Entry == "" || procedure.Effect == "" || procedures[procedure.ID].ID != "" {
			return nil, nil, nil, nil, nil, fmt.Errorf("invalid or duplicate procedure %+v", procedure)
		}
		procedures[procedure.ID] = procedure
	}
	if procedures[program.EntryProcedure].ID == "" {
		return nil, nil, nil, nil, nil, fmt.Errorf("entry procedure %q is missing", program.EntryProcedure)
	}
	blocks := make(map[string]semanticCategoryBlock, len(program.Blocks))
	hasBranch, hasCall, hasReturn, hasEffect := false, false, false, false
	for _, block := range program.Blocks {
		if block.ID == "" || blocks[block.ID].ID != "" {
			return nil, nil, nil, nil, nil, fmt.Errorf("invalid or duplicate block %q", block.ID)
		}
		blocks[block.ID] = block
		if len(block.Successors) > 1 {
			hasBranch = true
		}
		for _, operation := range block.Operations {
			if operation.Identity != "" && !identities[operation.Identity] {
				return nil, nil, nil, nil, nil, fmt.Errorf("operation references unknown identity %q", operation.Identity)
			}
			hasCall = hasCall || operation.Kind == "call"
			hasReturn = hasReturn || operation.Kind == "return"
			hasEffect = hasEffect || operation.Kind == "validate" || operation.Kind == "unknown-effect" || operation.Kind == "call"
			if operation.Kind == "call" && procedures[operation.Procedure].ID == "" {
				return nil, nil, nil, nil, nil, fmt.Errorf("call references unknown procedure %q", operation.Procedure)
			}
		}
	}
	for _, procedure := range procedures {
		if blocks[procedure.Entry].ID == "" {
			return nil, nil, nil, nil, nil, fmt.Errorf("procedure %q entry block %q is missing", procedure.ID, procedure.Entry)
		}
	}
	for _, block := range blocks {
		for _, edge := range block.Successors {
			if blocks[edge.To].ID == "" {
				return nil, nil, nil, nil, nil, fmt.Errorf("edge references missing block %q", edge.To)
			}
			if edge.Constraint != "" {
				if _, ok := constraints[edge.Constraint]; !ok {
					return nil, nil, nil, nil, nil, fmt.Errorf("edge references missing constraint %q", edge.Constraint)
				}
			}
		}
	}
	if !hasBranch || !hasCall || !hasReturn || !hasEffect {
		return nil, nil, nil, nil, nil, errors.New("semantic program does not exercise branches, calls, returns, and effects")
	}
	return identities, aliases, constraints, procedures, blocks, nil
}

func resolveSemanticCategoryAlias(identity string, aliases map[string]string) string {
	seen := make(map[string]bool)
	for aliases[identity] != "" && !seen[identity] {
		seen[identity] = true
		identity = aliases[identity]
	}
	return identity
}

func outcomeForSemanticCategoryUse(state semanticCategoryModelState) semanticCategoryModelOutcome {
	switch state {
	case semanticCategoryStateNeedsValidation:
		return semanticCategoryOutcomeViolation
	case semanticCategoryStateUnknown:
		return semanticCategoryOutcomeInconclusive
	case semanticCategoryStateValidated:
		return semanticCategoryOutcomeSafe
	default:
		return semanticCategoryOutcomeInconclusive
	}
}

func joinSemanticCategoryOutcome(
	left,
	right semanticCategoryModelOutcome,
) semanticCategoryModelOutcome {
	if left == semanticCategoryOutcomeViolation || right == semanticCategoryOutcomeViolation {
		return semanticCategoryOutcomeViolation
	}
	if left == semanticCategoryOutcomeInconclusive || right == semanticCategoryOutcomeInconclusive {
		return semanticCategoryOutcomeInconclusive
	}
	return semanticCategoryOutcomeSafe
}

func cloneSemanticCategoryStates(
	states map[string]semanticCategoryModelState,
) map[string]semanticCategoryModelState {
	clone := make(map[string]semanticCategoryModelState, len(states))
	maps.Copy(clone, states)
	return clone
}

func semanticCategoryStateKey(blockID string, states map[string]semanticCategoryModelState) string {
	identities := make([]string, 0, len(states))
	for identity := range states {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	var builder strings.Builder
	builder.WriteString(blockID)
	for _, identity := range identities {
		builder.WriteByte('|')
		builder.WriteString(identity)
		builder.WriteByte('=')
		builder.WriteString(string(states[identity]))
	}
	return builder.String()
}

func semanticCategorySeedFeature(category string) (string, bool) {
	switch category {
	case CategoryMissingConstructorValidate, CategoryMissingConstructorValidateInc:
		return semanticFeatureConstructorValidation, true
	case CategoryUnvalidatedBoundaryRequest:
		return semanticFeatureBoundaryRequest, true
	case CategoryUseBeforeValidateSameBlock, CategoryUseBeforeValidateCrossBlock,
		CategoryUseBeforeValidateInconclusive:
		return semanticFeatureUseBeforeValidation, true
	default:
		return "", false
	}
}

func TestSemanticCategorySeedRejectsUnrelatedNonemptyInput(t *testing.T) {
	t.Parallel()

	seed := semanticCategoryTestSeed(
		CategoryUnvalidatedBoundaryRequest,
		semanticFeatureBoundaryRequest,
		"unrelated-nonempty-fixture",
		"UnrelatedNonemptySymbol",
		semanticCategoryOutcomeViolation,
	)
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if _, classifyErr := classifySemanticCategorySeed(data); classifyErr == nil ||
		!strings.Contains(classifyErr.Error(), "not an exact must-report oracle tuple") {
		t.Fatalf("classifySemanticCategorySeed() error = %v, want unrelated tuple rejection", classifyErr)
	}
}

func TestSemanticCategoryProgramOrderDoesNotChangeIndependentOutcome(t *testing.T) {
	t.Parallel()

	seed := semanticCategoryTestSeed(
		CategoryUseBeforeValidateCrossBlock,
		semanticFeatureUseBeforeValidation,
		"fixture",
		"Symbol",
		semanticCategoryOutcomeViolation,
	)
	want, _, err := evaluateSemanticCategoryProgram(seed.FeatureID, seed.Program)
	if err != nil {
		t.Fatal(err)
	}
	reordered := seed.Program
	reordered.Blocks = slices.Clone(seed.Program.Blocks)
	slices.Reverse(reordered.Blocks)
	got, _, err := evaluateSemanticCategoryProgram(seed.FeatureID, reordered)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("block order changed independent outcome: got %q want %q", got, want)
	}
}

func semanticCategoryTestSeed(
	category,
	featureID,
	fixture,
	symbol string,
	outcome semanticCategoryModelOutcome,
) semanticCategorySeed {
	effect := "consume"
	operations := []semanticCategoryOperation{
		{Kind: "call", Identity: "alias", Procedure: "helper"},
		{Kind: "return", Identity: "alias"},
	}
	if outcome == semanticCategoryOutcomeInconclusive {
		effect = "unknown"
	}
	if featureID == semanticFeatureConstructorValidation && outcome == semanticCategoryOutcomeViolation {
		effect = "preserve"
	}
	return semanticCategorySeed{
		FormatVersion:   semanticCategorySeedFormatVersion,
		Category:        category,
		FeatureID:       featureID,
		Fixture:         fixture,
		Symbol:          symbol,
		ExpectedOutcome: string(outcome),
		Program: semanticCategoryProgram{
			EntryProcedure: "entry",
			Identities:     []string{"alias", "subject"},
			Aliases:        []semanticCategoryAlias{{Alias: "alias", Target: "subject"}},
			Constraints:    []semanticCategoryConstraint{{ID: "take-hazard", Value: true}},
			Procedures: []semanticCategoryProcedure{
				{ID: "entry", Entry: "entry", Effect: "preserve"},
				{ID: "helper", Entry: "helper", Effect: effect},
			},
			Blocks: []semanticCategoryBlock{
				{
					ID: "entry",
					Successors: []semanticCategoryEdge{
						{To: "hazard", Constraint: "take-hazard", Value: true},
						{To: "safe", Constraint: "take-hazard", Value: false},
					},
				},
				{ID: "hazard", Operations: operations},
				{
					ID: "safe",
					Operations: []semanticCategoryOperation{
						{Kind: "validate", Identity: "alias", Result: "nil"},
						{Kind: "return", Identity: "alias"},
					},
				},
				{ID: "helper", Operations: []semanticCategoryOperation{{Kind: "return", Identity: "alias"}}},
			},
		},
	}
}
