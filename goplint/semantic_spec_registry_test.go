// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

func TestSemanticSpecSchemaAndCatalogParse(t *testing.T) {
	t.Parallel()

	schema, err := loadSemanticRuleSchema()
	if err != nil {
		t.Fatalf("loadSemanticRuleSchema() error: %v", err)
	}
	if got := schema["$schema"]; got == nil {
		t.Fatal("semantic schema is missing $schema")
	}
	if got := schema["properties"]; got == nil {
		t.Fatal("semantic schema is missing properties")
	}
	boundaryCategory, found := diagnosticCategorySpec(CategoryUnvalidatedBoundaryRequest)
	ownerState := "missing-category"
	if found {
		ownerState = string(boundaryCategory.Owner)
	}
	requireMutationGuardObservation(
		t,
		"catalog-completeness/boundary-owner",
		mutationGuardState("unvalidated-boundary-request-owner", string(ownerBoundaryRequest)),
		mutationGuardState("unvalidated-boundary-request-owner", ownerState),
	)

	if err := validateSemanticRuleCatalogAgainstSchema(semanticRulesCatalogPath(), semanticRulesSchemaPath()); err != nil {
		t.Fatalf("validateSemanticRuleCatalogAgainstSchema() error: %v", err)
	}

	_ = mustLoadSemanticRuleCatalog(t)
}

func TestSemanticSpecSchemaValidationRejectsInvalidCatalog(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	catalogPath := filepath.Join(tempDir, "invalid-semantic-rules.v1.json")
	contents, err := os.ReadFile(semanticRulesCatalogPath())
	if err != nil {
		t.Fatalf("ReadFile(semanticRulesCatalogPath()) error: %v", err)
	}
	invalid := strings.Replace(string(contents), "\"version\": \"v1\"", "\"version\": 1", 1)
	if err := os.WriteFile(catalogPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error: %v", catalogPath, err)
	}

	err = validateSemanticRuleCatalogAgainstSchema(catalogPath, semanticRulesSchemaPath())
	if err == nil {
		t.Fatal("expected schema validation error for invalid catalog fixture")
	}
	if !strings.Contains(err.Error(), "at '/version'") {
		t.Fatalf("schema validation error %q does not include /version location", err)
	}
}

func TestSemanticSpecRegistrySync(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	rulesByCategory := make(map[string]semanticRuleSpec, len(catalog.Rules))
	for _, rule := range catalog.Rules {
		rulesByCategory[rule.Category] = rule
	}

	expectedCFACategories := ProtocolSemanticCategoryNames()
	for _, category := range expectedCFACategories {
		rule, ok := rulesByCategory[category]
		if !ok {
			t.Fatalf("semantic catalog is missing required protocol category %q", category)
		}
		if !IsKnownDiagnosticCategory(category) {
			t.Fatalf("semantic catalog category %q is not known in diagnostic registry", category)
		}
		spec, ok := diagnosticCategorySpec(category)
		if !ok {
			t.Fatalf("diagnosticCategorySpec(%q) not found", category)
		}
		wantPolicy, err := semanticBaselinePolicyForCategoryPolicy(spec.BaselinePolicy)
		if err != nil {
			t.Fatalf("semanticBaselinePolicyForCategoryPolicy(%q) error: %v", category, err)
		}
		if rule.BaselinePolicy != wantPolicy {
			t.Fatalf("rule %q baseline policy = %q, want %q", category, rule.BaselinePolicy, wantPolicy)
		}
	}
}

func TestSemanticSpecOracleCoverageSync(t *testing.T) {
	t.Parallel()

	catalog := mustLoadSemanticRuleCatalog(t)
	oracleByCategory := make(map[string]struct{}, len(catalog.OracleMatrix))
	for _, oracle := range catalog.OracleMatrix {
		oracleByCategory[oracle.Category] = struct{}{}
	}

	for _, category := range ProtocolSemanticCategoryNames() {
		if _, ok := oracleByCategory[category]; !ok {
			t.Fatalf("oracle_matrix is missing required protocol category %q", category)
		}
	}
}

func TestSemanticSpecRejectsIncompleteTotalCoverage(t *testing.T) {
	t.Parallel()

	loaded := mustLoadSemanticRuleCatalog(t)
	tests := []struct {
		name    string
		mutate  func(semanticRuleCatalog) semanticRuleCatalog
		wantErr string
	}{
		{
			name: "missing rule contract",
			mutate: func(catalog semanticRuleCatalog) semanticRuleCatalog {
				catalog.Rules = append([]semanticRuleSpec(nil), catalog.Rules[1:]...)
				return catalog
			},
			wantErr: "has no rule contract",
		},
		{
			name: "extra rule contract",
			mutate: func(catalog semanticRuleCatalog) semanticRuleCatalog {
				extra := catalog.Rules[0]
				extra.Category = "removed-category"
				catalog.Rules = append(append([]semanticRuleSpec(nil), catalog.Rules...), extra)
				return catalog
			},
			wantErr: "rule category \"removed-category\" is not registered",
		},
		{
			name: "missing oracle strategy",
			mutate: func(catalog semanticRuleCatalog) semanticRuleCatalog {
				catalog.OracleMatrix = append([]semanticOracleSpec(nil), catalog.OracleMatrix[1:]...)
				return catalog
			},
			wantErr: "has no oracle_matrix entry",
		},
		{
			name: "extra oracle strategy",
			mutate: func(catalog semanticRuleCatalog) semanticRuleCatalog {
				extra := catalog.OracleMatrix[0]
				extra.Category = "removed-category"
				catalog.OracleMatrix = append(append([]semanticOracleSpec(nil), catalog.OracleMatrix...), extra)
				return catalog
			},
			wantErr: "category \"removed-category\" is not registered",
		},
		{
			name: "protocol oracle without inconclusive case",
			mutate: func(catalog semanticRuleCatalog) semanticRuleCatalog {
				catalog.OracleMatrix = append([]semanticOracleSpec(nil), catalog.OracleMatrix...)
				for index := range catalog.OracleMatrix {
					if catalog.OracleMatrix[index].Category == CategoryUnvalidatedBoundaryRequest {
						catalog.OracleMatrix[index].MustBeInconclusive = nil
					}
				}
				return catalog
			},
			wantErr: "must_be_inconclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSemanticRuleCatalog(tt.mutate(loaded))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateSemanticRuleCatalog() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSemanticCoverageCensusIsTotalAndDeterministic(t *testing.T) {
	t.Parallel()

	registry := mustLoadExecutableEvidenceRegistry(t)
	observations := executableEvidenceObservations(t, registry)
	var first bytes.Buffer
	if err := WriteSemanticCoverageCensus(&first, registry, observations); err != nil {
		t.Fatalf("WriteSemanticCoverageCensus(first) error: %v", err)
	}
	var second bytes.Buffer
	if err := WriteSemanticCoverageCensus(&second, registry, observations); err != nil {
		t.Fatalf("WriteSemanticCoverageCensus(second) error: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatalf("semantic coverage census is nondeterministic:\nfirst: %s\nsecond: %s", first.Bytes(), second.Bytes())
	}
	var census semanticCoverageCensus
	if err := json.Unmarshal(first.Bytes(), &census); err != nil {
		t.Fatalf("json.Unmarshal(census) error: %v", err)
	}
	if len(census.Categories) != len(ProtocolSemanticCategoryNames()) {
		t.Fatalf("semantic coverage census has %d rows, want %d", len(census.Categories), len(ProtocolSemanticCategoryNames()))
	}
}

func TestSemanticCoverageCensusRejectsBidirectionalEvidenceDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*soundnessevidence.Registry, *[]soundnessevidence.SemanticObservation)
		wantError string
	}{
		{
			name: "missing registration",
			mutate: func(registry *soundnessevidence.Registry, _ *[]soundnessevidence.SemanticObservation) {
				registry.Registrations = append([]soundnessevidence.Registration(nil), registry.Registrations[1:]...)
			},
			wantError: "has no executable observation registration",
		},
		{
			name: "missing observation",
			mutate: func(_ *soundnessevidence.Registry, observations *[]soundnessevidence.SemanticObservation) {
				*observations = append([]soundnessevidence.SemanticObservation(nil), (*observations)[1:]...)
			},
			wantError: "has no observation",
		},
		{
			name: "duplicate observation",
			mutate: func(_ *soundnessevidence.Registry, observations *[]soundnessevidence.SemanticObservation) {
				*observations = append(*observations, (*observations)[0])
			},
			wantError: "duplicate registration id",
		},
		{
			name: "extra observation",
			mutate: func(_ *soundnessevidence.Registry, observations *[]soundnessevidence.SemanticObservation) {
				(*observations)[0].RegistrationID = "forged.marker-only"
			},
			wantError: "extra registration id",
		},
		{
			name: "zero population",
			mutate: func(_ *soundnessevidence.Registry, observations *[]soundnessevidence.SemanticObservation) {
				(*observations)[0].Result.CaseCount = 0
			},
			wantError: "positive population",
		},
		{
			name: "stale producer binding",
			mutate: func(_ *soundnessevidence.Registry, observations *[]soundnessevidence.SemanticObservation) {
				(*observations)[0].Binding.WorkspaceDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
			},
			wantError: "inconsistent bindings",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			registry := mustLoadExecutableEvidenceRegistry(t)
			observations := executableEvidenceObservations(t, registry)
			test.mutate(&registry, &observations)
			_, err := buildSemanticCoverageCensus(registry, observations)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("buildSemanticCoverageCensus() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func mustLoadExecutableEvidenceRegistry(t *testing.T) soundnessevidence.Registry {
	t.Helper()
	path := filepath.Join(goplintModuleRootPath(), "spec", "semantic-evidence.v2.json")
	registry, err := soundnessevidence.LoadRegistry(t.Context(), path)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	return registry
}

func executableEvidenceObservations(
	t testing.TB,
	registry soundnessevidence.Registry,
) []soundnessevidence.SemanticObservation {
	t.Helper()

	bindings := make(map[string]soundnessevidence.ObservationBinding)
	observations := make([]soundnessevidence.SemanticObservation, 0, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		binding, exists := bindings[registration.ProducerID]
		if !exists {
			binding = soundnessevidence.ObservationBinding{
				RunID:           "run-semantic-census",
				WorkspaceDigest: soundnessevidence.DigestBytes([]byte("workspace")),
				ManifestDigest:  soundnessevidence.DigestBytes([]byte("manifest")),
				CommandDigest:   soundnessevidence.DigestBytes([]byte(registration.ProducerID)),
				SubgateID:       registration.ProducerID,
			}
			bindings[registration.ProducerID] = binding
		}
		result := soundnessevidence.ObservationResult{
			Outcome:         registration.Expected.Outcome,
			CaseCount:       registration.Expected.MinimumCases,
			DiagnosticCount: registration.Expected.Diagnostics.Minimum,
		}
		cases := syntheticSemanticCasesFromResult(
			t,
			registration.ID,
			registration.Category,
			registration.Layer,
			registration.FeatureID,
			registration.Boundary,
			result,
			registration.Expected.RequiredStages,
			registration.Expected.RequiredProperties,
			registration.Expected.RequiredDimensions,
		)
		observation := semanticObservationFromCases(
			t,
			registration.ID,
			registration.ProducerID,
			registration.TestID,
			cases,
		)
		observation.Binding = binding
		observations = append(observations, observation)
	}
	return observations
}
