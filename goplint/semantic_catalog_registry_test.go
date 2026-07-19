// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"strings"
	"testing"
)

func TestValidateSemanticRegistries(t *testing.T) {
	t.Parallel()

	if err := validateSemanticRegistries(); err != nil {
		t.Fatalf("validateSemanticRegistries() error: %v", err)
	}
}

func TestValidateSemanticRegistryDataRejectsInvalidOwnership(t *testing.T) {
	t.Parallel()

	baseDiagnostic := []CategorySpec{newStructuralCategory(CategoryPrimitive, BaselineSuppressible, "primitive", ownerPrimitive)}
	baseSemantic := []semanticCategorySpec{{
		Category:       CategoryPrimitive,
		Kind:           semanticKindStructural,
		Owner:          ownerPrimitive,
		OracleStrategy: oracleStrategyBoundaryFixture,
	}}
	baseOwners := []semanticOwnerRegistration{{Key: ownerPrimitive, Route: semanticRoutePrimitive, Traversal: semanticTestTraversalHandler}}
	tests := []struct {
		name        string
		diagnostics []CategorySpec
		semantics   []semanticCategorySpec
		owners      []semanticOwnerRegistration
		wantErr     string
	}{
		{
			name:        "duplicate owner key",
			diagnostics: baseDiagnostic,
			semantics:   baseSemantic,
			owners:      append(baseOwners, baseOwners[0]),
			wantErr:     "duplicate semantic owner key",
		},
		{
			name:        "owner without route",
			diagnostics: baseDiagnostic,
			semantics:   baseSemantic,
			owners:      []semanticOwnerRegistration{{Key: ownerPrimitive, Traversal: semanticTestTraversalHandler}},
			wantErr:     "has no production route",
		},
		{
			name:        "owner without executable handler",
			diagnostics: baseDiagnostic,
			semantics:   baseSemantic,
			owners:      []semanticOwnerRegistration{{Key: ownerPrimitive, Route: semanticRoutePrimitive}},
			wantErr:     "has no executable production handler",
		},
		{
			name:        "duplicate production route",
			diagnostics: baseDiagnostic,
			semantics:   baseSemantic,
			owners: append(baseOwners, semanticOwnerRegistration{
				Key:       ownerDirective,
				Route:     semanticRoutePrimitive,
				Traversal: semanticTestTraversalHandler,
			}),
			wantErr: "is registered by both",
		},
		{
			name:        "duplicate category",
			diagnostics: baseDiagnostic,
			semantics:   append(baseSemantic, baseSemantic[0]),
			owners:      baseOwners,
			wantErr:     "duplicate semantic category",
		},
		{
			name:        "stale category",
			diagnostics: baseDiagnostic,
			semantics: []semanticCategorySpec{{
				Category:       "removed-category",
				Kind:           semanticKindStructural,
				Owner:          ownerPrimitive,
				OracleStrategy: oracleStrategyBoundaryFixture,
			}},
			owners:  baseOwners,
			wantErr: "is not registered",
		},
		{
			name:        "unresolvable owner",
			diagnostics: baseDiagnostic,
			semantics: []semanticCategorySpec{{
				Category:       CategoryPrimitive,
				Kind:           semanticKindStructural,
				Owner:          "removed-owner",
				OracleStrategy: oracleStrategyBoundaryFixture,
			}},
			owners:  baseOwners,
			wantErr: "has unresolvable owner",
		},
		{
			name:        "missing category ownership",
			diagnostics: append(baseDiagnostic, newStructuralCategory(CategoryMissingValidate, BaselineSuppressible, "missing validate", ownerTypeMethodContract)),
			semantics:   baseSemantic,
			owners:      baseOwners,
			wantErr:     "has no semantic ownership",
		},
		{
			name:        "stale owner",
			diagnostics: baseDiagnostic,
			semantics:   baseSemantic,
			owners: append(baseOwners, semanticOwnerRegistration{
				Key:       ownerDirective,
				Route:     semanticRouteDirective,
				Traversal: semanticTestTraversalHandler,
			}),
			wantErr: "has no categories",
		},
		{
			name:        "incompatible oracle",
			diagnostics: baseDiagnostic,
			semantics: []semanticCategorySpec{{
				Category:       CategoryPrimitive,
				Kind:           semanticKindStructural,
				Owner:          ownerPrimitive,
				OracleStrategy: oracleStrategyLayeredProtocol,
			}},
			owners:  baseOwners,
			wantErr: "incompatible with kind",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateSemanticRegistryData(tt.diagnostics, tt.semantics, tt.owners)
			if err == nil {
				t.Fatalf("validateSemanticRegistryData() error = nil, want containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateSemanticRegistryData() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func semanticTestTraversalHandler(*semanticTraversalContext) error {
	return nil
}

func TestValidateSemanticCategoryCatalogRejectsDrift(t *testing.T) {
	t.Parallel()

	valid := semanticCatalogEntriesFromLiveRegistry()
	tests := []struct {
		name    string
		mutate  func([]semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry
		wantErr string
	}{
		{
			name: "missing category",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				return entries[:len(entries)-1]
			},
			wantErr: "is missing from category_catalog",
		},
		{
			name: "duplicate category",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				return append(entries, entries[0])
			},
			wantErr: "duplicate category_catalog category",
		},
		{
			name: "stale category",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				entries[0].Category = "removed-category"
				return entries
			},
			wantErr: "stale category_catalog entry",
		},
		{
			name: "stale owner key",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				entries[0].OwnerKey = "removed-owner"
				return entries
			},
			wantErr: "owner_key",
		},
		{
			name: "wrong kind",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				entries[0].Kind = string(semanticKindProtocol)
				return entries
			},
			wantErr: "kind",
		},
		{
			name: "missing required layer",
			mutate: func(entries []semanticCategoryCatalogEntry) []semanticCategoryCatalogEntry {
				entries[0].RequiredLayers = entries[0].RequiredLayers[:len(entries[0].RequiredLayers)-1]
				return entries
			},
			wantErr: "required_layers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entries := append([]semanticCategoryCatalogEntry(nil), valid...)
			err := validateSemanticCategoryCatalog(tt.mutate(entries))
			if err == nil {
				t.Fatalf("validateSemanticCategoryCatalog() error = nil, want containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateSemanticCategoryCatalog() error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func semanticCatalogEntriesFromLiveRegistry() []semanticCategoryCatalogEntry {
	live := semanticCategoryRegistry()
	entries := make([]semanticCategoryCatalogEntry, 0, len(live))
	for _, spec := range live {
		entries = append(entries, semanticCategoryCatalogEntry{
			Category:       spec.Category,
			Kind:           string(spec.Kind),
			OwnerKey:       string(spec.Owner),
			OracleStrategy: string(spec.OracleStrategy),
			RequiredLayers: evidenceLayerStrings(requiredOracleLayersForKind(spec.Kind)),
		})
	}
	return entries
}
