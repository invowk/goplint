// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestDiagnosticCategorySpec(t *testing.T) {
	t.Parallel()

	t.Run("known category", func(t *testing.T) {
		t.Parallel()

		spec, ok := diagnosticCategorySpec(CategoryPrimitive)
		if !ok {
			t.Fatal("expected primitive category to be known")
		}
		if spec.Name != CategoryPrimitive {
			t.Fatalf("spec.Name = %q, want %q", spec.Name, CategoryPrimitive)
		}
		if spec.BaselinePolicy != BaselineSuppressible {
			t.Fatalf("spec.BaselinePolicy = %v, want %v", spec.BaselinePolicy, BaselineSuppressible)
		}
	})

	t.Run("unknown category", func(t *testing.T) {
		t.Parallel()

		spec, ok := diagnosticCategorySpec("does-not-exist")
		if ok {
			t.Fatal("expected unknown category to report ok=false")
		}
		if spec != (CategorySpec{}) {
			t.Fatalf("spec = %+v, want zero value", spec)
		}
	})
}

func TestIsKnownDiagnosticCategory(t *testing.T) {
	t.Parallel()

	if !IsKnownDiagnosticCategory(CategoryMissingValidate) {
		t.Fatal("expected missing-validate category to be known")
	}
	if IsKnownDiagnosticCategory("not-a-category") {
		t.Fatal("expected unknown category to report false")
	}
}

func TestIsBaselineSuppressibleCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		category string
		want     bool
	}{
		{name: "suppressible", category: CategoryPrimitive, want: true},
		{name: "always-visible", category: CategoryUnknownDirective, want: false},
		{name: "audit-only", category: CategoryStaleException, want: false},
		{name: "unknown", category: "nope", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsBaselineSuppressibleCategory(tt.category)
			if got != tt.want {
				t.Fatalf("IsBaselineSuppressibleCategory(%q) = %v, want %v", tt.category, got, tt.want)
			}
		})
	}
}
