// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestCleanTreePlanSchemaMatchesStrictModel(t *testing.T) {
	t.Parallel()

	schema := compileCleanTreePlanSchema(t)
	fixture := newVerifyFixture(t)
	planData, err := os.ReadFile(resolveFromRoot(fixture.root, fixture.options.PlanPath))
	if err != nil {
		t.Fatal(err)
	}
	var planDocument map[string]any
	if err := json.Unmarshal(planData, &planDocument); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(planDocument); err != nil {
		t.Fatalf("valid strict plan rejected by schema: %v", err)
	}
	planDocument["forged_completion_marker"] = true
	if err := schema.Validate(planDocument); err == nil {
		t.Fatal("schema accepted an unknown marker-only completion field")
	}
}

func TestReviewedCleanTreePlanLoadsAndMatchesSchema(t *testing.T) {
	t.Parallel()

	planPath := filepath.Join("..", "..", "testdata", "gates", "clean-tree-v4.json")
	if _, err := LoadPlan(planPath); err != nil {
		t.Fatalf("LoadPlan(reviewed plan) error = %v", err)
	}
	planData, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	var planDocument map[string]any
	if err := json.Unmarshal(planData, &planDocument); err != nil {
		t.Fatal(err)
	}
	schema := compileCleanTreePlanSchema(t)
	if err := schema.Validate(planDocument); err != nil {
		t.Fatalf("reviewed plan rejected by schema: %v", err)
	}
}

func compileCleanTreePlanSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaPath := filepath.Join("..", "..", "testdata", "gates", "clean-tree-v4.schema.json")
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var schemaDocument any
	if err := json.Unmarshal(schemaData, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://github.com/invowk/invowk/tools/goplint/testdata/gates/clean-tree-v4.schema.json"
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}
