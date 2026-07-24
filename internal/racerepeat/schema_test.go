// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func TestRaceRepeatSchemasAcceptCanonicalArtifacts(t *testing.T) {
	t.Parallel()

	plan := testRaceRepeatPlan(t)
	unit := plan.WorkUnits[0]
	result, err := ParseWorkResult(plan, unit, test2JSONOutput(unit.MemberIDs, "pass"), statusPassed)
	if err != nil {
		t.Fatal(err)
	}
	timingData, err := CanonicalTimingJSON(testTimingManifest())
	if err != nil {
		t.Fatal(err)
	}
	planData, err := CanonicalPlanJSON(plan)
	if err != nil {
		t.Fatal(err)
	}
	resultData, err := CanonicalWorkResultJSON(result, plan)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name       string
		schemaFile string
		data       []byte
	}{
		{name: "timing", schemaFile: "goplint-test-timings.v1.schema.json", data: timingData},
		{name: "plan", schemaFile: "race-repeat-plan.v1.schema.json", data: planData},
		{name: "result", schemaFile: "race-repeat-result.v1.schema.json", data: resultData},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			validateSchemaFixture(t, testCase.schemaFile, testCase.data)
		})
	}
}

func validateSchemaFixture(t *testing.T, schemaFile string, data []byte) {
	t.Helper()
	schemaData, err := os.ReadFile(filepath.Join("..", "..", "spec", schemaFile))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	const schemaURL = "https://invowk.dev/schema.json"
	var schemaDocument any
	if err := json.Unmarshal(schemaData, &schemaDocument); err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource(schemaURL, schemaDocument); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(value); err != nil {
		t.Fatalf("schema validation error = %v", err)
	}
}
