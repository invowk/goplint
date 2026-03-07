// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestLoadRunInputsUsesRunConfigPaths(t *testing.T) {
	t.Parallel()

	cfgPath := writeTempFile(t, "goplint-config.toml", `
[[exceptions]]
pattern = "pkg.Type.Field"
reason = "test"
`)
	blPath := writeTempFile(t, "goplint-baseline.toml", `
[primitive]
entries = [
    { id = "id-1", message = "struct field pkg.Type.Field uses primitive type string" },
]
`)

	rc := runConfig{
		configPath:           cfgPath,
		configPathExplicit:   true,
		baselinePath:         blPath,
		baselinePathExplicit: true,
	}

	state := &flagState{}
	resetFlagStateDefaults(state)

	cfg, bl, err := loadRunInputs(state, rc)
	if err != nil {
		t.Fatalf("loadRunInputs returned error: %v", err)
	}
	if cfg == nil || bl == nil {
		t.Fatal("loadRunInputs returned nil config/baseline")
	}
	if !cfg.isExcepted("pkg.Type.Field") {
		t.Fatal("expected runConfig-provided config to be loaded")
	}
	if !bl.ContainsFinding(CategoryPrimitive, "id-1", "") {
		t.Fatal("expected runConfig-provided baseline to be loaded")
	}
}

func TestValidateRunConfigAllowsMissionChecks(t *testing.T) {
	t.Parallel()

	tests := []runConfig{
		{checkCastValidation: true},
		{checkUseBeforeValidate: true},
		{checkConstructorValidates: true},
	}

	for _, rc := range tests {
		if err := validateRunConfig(rc); err != nil {
			t.Fatalf("expected %+v to pass validation, got %v", rc, err)
		}
	}
}
