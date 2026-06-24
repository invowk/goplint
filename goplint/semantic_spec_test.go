// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const semanticRulesCatalogVersion = "v1"

var (
	semanticOutcomeDomainAllowed   = []string{"safe", "unsafe", "inconclusive"}
	semanticInterprocEngineAllowed = []string{cfgInterprocEngineLegacy, cfgInterprocEngineIFDS, cfgInterprocEngineCompare}
	semanticEdgeFunctionTagAllowed = []string{
		string(ideEdgeFuncIdentity),
		string(ideEdgeFuncValidate),
		string(ideEdgeFuncEscape),
		string(ideEdgeFuncConsume),
	}
	semanticRefinementStatusAllowed = []string{
		cfgRefinementStatusUnsafe,
		cfgRefinementStatusInconclusiveRefined,
		cfgRefinementStatusInconclusiveRaw,
		cfgRefinementStatusProvenSafe,
	}
)

type semanticRuleCatalog struct {
	Version                string                         `json:"version"`
	Rules                  []semanticRuleSpec             `json:"rules"`
	OracleMatrix           []semanticOracleSpec           `json:"oracle_matrix"`
	HistoricalMissFixtures []string                       `json:"historical_miss_fixtures"`
	HistoricalMissOracles  []semanticHistoricalMissOracle `json:"historical_miss_oracles"`
}

type semanticRuleSpec struct {
	Category                   string   `json:"category"`
	Family                     string   `json:"family"`
	Entrypoints                []string `json:"entrypoints"`
	EnabledByFlags             []string `json:"enabled_by_flags"`
	RunControls                []string `json:"run_controls"`
	TraversalMode              string   `json:"traversal_mode"`
	StateDomain                []string `json:"state_domain"`
	OutcomeDomain              []string `json:"outcome_domain"`
	InterprocEngineModes       []string `json:"interproc_engine_modes,omitempty"`
	FactFamilies               []string `json:"fact_families,omitempty"`
	EdgeFunctionTags           []string `json:"edge_function_tags,omitempty"`
	InconclusiveReasons        []string `json:"inconclusive_reasons,omitempty"`
	RequiredMetaOnInconclusive []string `json:"required_meta_on_inconclusive,omitempty"`
	RefinementStatuses         []string `json:"refinement_statuses,omitempty"`
	RequiredMetaOnRefinement   []string `json:"required_meta_on_refinement,omitempty"`
	BaselinePolicy             string   `json:"baseline_policy"`
}

type semanticOracleSpec struct {
	Category      string                `json:"category"`
	MustReport    []semanticOracleEntry `json:"must_report"`
	MustNotReport []semanticOracleEntry `json:"must_not_report"`
}

type semanticOracleEntry struct {
	Fixture string `json:"fixture"`
	Symbol  string `json:"symbol"`
}

type semanticHistoricalMissOracle struct {
	Fixture       string                `json:"fixture"`
	Category      string                `json:"category"`
	MustReport    []semanticOracleEntry `json:"must_report"`
	MustNotReport []semanticOracleEntry `json:"must_not_report,omitempty"`
}

func semanticRulesCatalogPath() string {
	return filepath.Join(goplintModuleRootPath(), "spec", "semantic-rules.v1.json")
}

func semanticRulesSchemaPath() string {
	return filepath.Join(goplintModuleRootPath(), "spec", "schema", "semantic-rules.schema.json")
}

func goplintModuleRootPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Clean("..")
	}
	// semantic_spec.go lives in tools/goplint/goplint/, so one level up is module root.
	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func goplintPackageRootPath() string {
	return filepath.Join(goplintModuleRootPath(), "goplint")
}

func loadSemanticRuleCatalog() (semanticRuleCatalog, error) {
	path := semanticRulesCatalogPath()
	schemaPath := semanticRulesSchemaPath()
	if err := validateSemanticRuleCatalogAgainstSchema(path, schemaPath); err != nil {
		return semanticRuleCatalog{}, err
	}
	catalog := semanticRuleCatalog{}
	if err := decodeJSONFile(path, &catalog); err != nil {
		return semanticRuleCatalog{}, fmt.Errorf("loading semantic rules catalog %q: %w", path, err)
	}
	if err := validateSemanticRuleCatalog(catalog); err != nil {
		return semanticRuleCatalog{}, err
	}
	return catalog, nil
}

func loadSemanticRuleSchema() (map[string]any, error) {
	path := semanticRulesSchemaPath()
	payload := map[string]any{}
	if err := decodeJSONFile(path, &payload); err != nil {
		return nil, fmt.Errorf("loading semantic rules schema %q: %w", path, err)
	}
	return payload, nil
}

func decodeJSONFile(path string, out any) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer func() {
		closeErr := file.Close()
		if closeErr != nil && err == nil {
			err = fmt.Errorf("closing file: %w", closeErr)
		}
	}()

	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("decoding json: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return errors.New("json document has trailing data")
		}
		return fmt.Errorf("validating json eof: %w", err)
	}
	return nil
}

func validateSemanticRuleCatalogAgainstSchema(catalogPath, schemaPath string) error {
	compiler := jsonschema.NewCompiler()

	schemaDoc := map[string]any{}
	if err := decodeJSONFile(schemaPath, &schemaDoc); err != nil {
		return fmt.Errorf("loading semantic rules schema %q for validation: %w", schemaPath, err)
	}

	// Keep schema reference stable and independent from local filesystem paths.
	const schemaURL = "https://github.com/invowk/invowk/tools/goplint/spec/schema/semantic-rules.schema.json"
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		return fmt.Errorf("registering semantic rules schema resource: %w", err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("compiling semantic rules schema %q: %w", schemaPath, err)
	}

	catalogDoc := map[string]any{}
	if err := decodeJSONFile(catalogPath, &catalogDoc); err != nil {
		return fmt.Errorf("loading semantic rules catalog %q for schema validation: %w", catalogPath, err)
	}
	if err := schema.Validate(catalogDoc); err != nil {
		return fmt.Errorf("semantic rules catalog %q does not match schema %q: %s", catalogPath, schemaPath, formatSemanticSchemaValidationErr(err))
	}
	return nil
}

func formatSemanticSchemaValidationErr(err error) string {
	validationErr, ok := errors.AsType[*jsonschema.ValidationError](err)
	if !ok {
		return err.Error()
	}
	output := validationErr.BasicOutput()
	if output == nil {
		return validationErr.Error()
	}
	keywordLocation := strings.TrimSpace(output.KeywordLocation)
	if keywordLocation == "" {
		keywordLocation = "/"
	}
	instanceLocation := strings.TrimSpace(output.InstanceLocation)
	if instanceLocation == "" {
		instanceLocation = "/"
	}
	return fmt.Sprintf("%s (keyword=%s instance=%s)", validationErr.Error(), keywordLocation, instanceLocation)
}

func validateSemanticRuleCatalog(catalog semanticRuleCatalog) error {
	if catalog.Version != semanticRulesCatalogVersion {
		return fmt.Errorf("semantic rules version must be %q (got %q)", semanticRulesCatalogVersion, catalog.Version)
	}
	if len(catalog.Rules) == 0 {
		return errors.New("semantic rules catalog must declare at least one rule")
	}

	ruleCategories := make(map[string]struct{}, len(catalog.Rules))
	for idx, rule := range catalog.Rules {
		if err := validateSemanticRuleSpec(rule); err != nil {
			return fmt.Errorf("invalid rule[%d]: %w", idx, err)
		}
		if _, exists := ruleCategories[rule.Category]; exists {
			return fmt.Errorf("duplicate rule category %q", rule.Category)
		}
		ruleCategories[rule.Category] = struct{}{}
	}

	if len(catalog.OracleMatrix) == 0 {
		return errors.New("semantic rules catalog must include oracle_matrix entries")
	}
	oracleCategories := map[string]struct{}{}
	for idx, oracle := range catalog.OracleMatrix {
		if err := validateSemanticOracleSpec(oracle); err != nil {
			return fmt.Errorf("invalid oracle_matrix[%d]: %w", idx, err)
		}
		if _, ok := ruleCategories[oracle.Category]; !ok {
			return fmt.Errorf("oracle_matrix category %q has no matching rule", oracle.Category)
		}
		if _, exists := oracleCategories[oracle.Category]; exists {
			return fmt.Errorf("duplicate oracle_matrix category %q", oracle.Category)
		}
		oracleCategories[oracle.Category] = struct{}{}
	}

	if len(catalog.HistoricalMissFixtures) == 0 {
		return errors.New("semantic rules catalog must include historical_miss_fixtures")
	}
	seenFixtures := map[string]struct{}{}
	for idx, fixture := range catalog.HistoricalMissFixtures {
		trimmed := strings.TrimSpace(fixture)
		if trimmed == "" {
			return fmt.Errorf("historical_miss_fixtures[%d] must be non-empty", idx)
		}
		if _, exists := seenFixtures[trimmed]; exists {
			return fmt.Errorf("duplicate historical_miss_fixtures entry %q", trimmed)
		}
		seenFixtures[trimmed] = struct{}{}
	}

	if len(catalog.HistoricalMissOracles) == 0 {
		return errors.New("semantic rules catalog must include historical_miss_oracles")
	}
	seenHistoricalOracles := map[string]struct{}{}
	for idx, oracle := range catalog.HistoricalMissOracles {
		if err := validateSemanticHistoricalMissOracle(oracle); err != nil {
			return fmt.Errorf("invalid historical_miss_oracles[%d]: %w", idx, err)
		}
		fixture := strings.TrimSpace(oracle.Fixture)
		if _, ok := seenFixtures[fixture]; !ok {
			return fmt.Errorf("historical_miss_oracles fixture %q is not listed in historical_miss_fixtures", fixture)
		}
		if _, exists := seenHistoricalOracles[fixture]; exists {
			return fmt.Errorf("duplicate historical_miss_oracles fixture %q", fixture)
		}
		seenHistoricalOracles[fixture] = struct{}{}
	}
	for fixture := range seenFixtures {
		if _, ok := seenHistoricalOracles[fixture]; !ok {
			return fmt.Errorf("historical_miss_fixtures entry %q is missing historical_miss_oracles coverage", fixture)
		}
	}

	return nil
}

func validateSemanticRuleSpec(rule semanticRuleSpec) error {
	if strings.TrimSpace(rule.Category) == "" {
		return errors.New("category must be non-empty")
	}
	if strings.TrimSpace(rule.Family) == "" {
		return errors.New("family must be non-empty")
	}
	if strings.TrimSpace(rule.TraversalMode) == "" {
		return errors.New("traversal_mode must be non-empty")
	}
	if err := requireUniqueNonEmpty(rule.Entrypoints, "entrypoints"); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty(rule.EnabledByFlags, "enabled_by_flags"); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty(rule.RunControls, "run_controls"); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty(rule.StateDomain, "state_domain"); err != nil {
		return err
	}
	if err := requireUniqueNonEmpty(rule.OutcomeDomain, "outcome_domain"); err != nil {
		return err
	}
	requiresInterprocSpec := strings.HasPrefix(rule.Family, "cfa-")
	if requiresInterprocSpec {
		if err := requireUniqueNonEmpty(rule.InterprocEngineModes, "interproc_engine_modes"); err != nil {
			return err
		}
		if err := requireUniqueNonEmpty(rule.FactFamilies, "fact_families"); err != nil {
			return err
		}
		if err := requireUniqueNonEmpty(rule.EdgeFunctionTags, "edge_function_tags"); err != nil {
			return err
		}
	} else {
		if len(rule.InterprocEngineModes) > 0 {
			if err := requireUniqueNonEmpty(rule.InterprocEngineModes, "interproc_engine_modes"); err != nil {
				return err
			}
		}
		if len(rule.FactFamilies) > 0 {
			if err := requireUniqueNonEmpty(rule.FactFamilies, "fact_families"); err != nil {
				return err
			}
		}
		if len(rule.EdgeFunctionTags) > 0 {
			if err := requireUniqueNonEmpty(rule.EdgeFunctionTags, "edge_function_tags"); err != nil {
				return err
			}
		}
	}
	for _, outcome := range rule.OutcomeDomain {
		if !slices.Contains(semanticOutcomeDomainAllowed, outcome) {
			return fmt.Errorf("outcome_domain contains unsupported value %q", outcome)
		}
	}
	for _, engine := range rule.InterprocEngineModes {
		if !slices.Contains(semanticInterprocEngineAllowed, engine) {
			return fmt.Errorf("interproc_engine_modes contains unsupported value %q", engine)
		}
	}
	for _, tag := range rule.EdgeFunctionTags {
		if !slices.Contains(semanticEdgeFunctionTagAllowed, tag) {
			return fmt.Errorf("edge_function_tags contains unsupported value %q", tag)
		}
	}
	for _, status := range rule.RefinementStatuses {
		if !slices.Contains(semanticRefinementStatusAllowed, status) {
			return fmt.Errorf("refinement_statuses contains unsupported value %q", status)
		}
	}
	if err := validateSemanticBaselinePolicy(rule.BaselinePolicy); err != nil {
		return err
	}

	hasInconclusive := slices.Contains(rule.OutcomeDomain, "inconclusive")
	if hasInconclusive {
		if err := requireUniqueNonEmpty(rule.InconclusiveReasons, "inconclusive_reasons"); err != nil {
			return err
		}
		if err := requireUniqueNonEmpty(rule.RequiredMetaOnInconclusive, "required_meta_on_inconclusive"); err != nil {
			return err
		}
	} else if len(rule.InconclusiveReasons) > 0 || len(rule.RequiredMetaOnInconclusive) > 0 {
		return errors.New("inconclusive fields must be omitted when outcome_domain excludes inconclusive")
	}
	hasRefinementSpec := len(rule.RefinementStatuses) > 0 || len(rule.RequiredMetaOnRefinement) > 0
	if hasRefinementSpec {
		if err := requireUniqueNonEmpty(rule.RefinementStatuses, "refinement_statuses"); err != nil {
			return err
		}
		if err := requireUniqueNonEmpty(rule.RequiredMetaOnRefinement, "required_meta_on_refinement"); err != nil {
			return err
		}
	}

	return nil
}

func validateSemanticOracleSpec(spec semanticOracleSpec) error {
	if strings.TrimSpace(spec.Category) == "" {
		return errors.New("category must be non-empty")
	}
	if len(spec.MustReport) == 0 {
		return errors.New("must_report must contain at least one entry")
	}
	if len(spec.MustNotReport) == 0 {
		return errors.New("must_not_report must contain at least one entry")
	}
	for _, entry := range spec.MustReport {
		if err := validateSemanticOracleEntry(entry, "must_report"); err != nil {
			return err
		}
	}
	for _, entry := range spec.MustNotReport {
		if err := validateSemanticOracleEntry(entry, "must_not_report"); err != nil {
			return err
		}
	}
	return nil
}

func validateSemanticOracleEntry(entry semanticOracleEntry, fieldName string) error {
	if strings.TrimSpace(entry.Fixture) == "" {
		return fmt.Errorf("%s fixture must be non-empty", fieldName)
	}
	if strings.TrimSpace(entry.Symbol) == "" {
		return fmt.Errorf("%s symbol must be non-empty", fieldName)
	}
	return nil
}

func validateSemanticHistoricalMissOracle(oracle semanticHistoricalMissOracle) error {
	if strings.TrimSpace(oracle.Fixture) == "" {
		return errors.New("fixture must be non-empty")
	}
	if strings.TrimSpace(oracle.Category) == "" {
		return errors.New("category must be non-empty")
	}
	if !IsKnownDiagnosticCategory(oracle.Category) {
		return fmt.Errorf("category %q is not known in diagnostic registry", oracle.Category)
	}
	if len(oracle.MustReport) == 0 {
		return errors.New("must_report must contain at least one entry")
	}
	for _, entry := range oracle.MustReport {
		if err := validateSemanticOracleEntry(entry, "must_report"); err != nil {
			return err
		}
	}
	for _, entry := range oracle.MustNotReport {
		if err := validateSemanticOracleEntry(entry, "must_not_report"); err != nil {
			return err
		}
	}
	return nil
}

func requireUniqueNonEmpty(values []string, fieldName string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s must contain at least one value", fieldName)
	}
	seen := map[string]struct{}{}
	for idx, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return fmt.Errorf("%s[%d] must be non-empty", fieldName, idx)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate value %q", fieldName, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateSemanticBaselinePolicy(policy string) error {
	switch strings.TrimSpace(policy) {
	case "suppressible", "always-visible", "audit-only":
		return nil
	default:
		return fmt.Errorf("baseline_policy %q is not supported", policy)
	}
}

func semanticBaselinePolicyForCategoryPolicy(policy BaselinePolicy) (string, error) {
	switch policy {
	case BaselineSuppressible:
		return "suppressible", nil
	case BaselineAlwaysVisible:
		return "always-visible", nil
	case BaselineAuditOnly:
		return "audit-only", nil
	default:
		return "", fmt.Errorf("unknown baseline policy %d", policy)
	}
}
