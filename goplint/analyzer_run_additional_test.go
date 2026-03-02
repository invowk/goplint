// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"strings"
	"testing"
)

func TestLoadRunInputsExplicitMissingConfigPath(t *testing.T) {
	t.Parallel()

	state := &flagState{}
	resetFlagStateDefaults(state)

	rc := runConfig{
		configPath:         "does-not-exist.toml",
		configPathExplicit: true,
	}
	_, _, err := loadRunInputs(state, rc)
	if err == nil {
		t.Fatal("expected missing explicit config path to fail")
	}
	if !strings.Contains(err.Error(), "reading config") || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("expected config-file-not-found error, got %v", err)
	}
}

func TestLoadRunInputsExplicitMissingBaselinePath(t *testing.T) {
	t.Parallel()

	state := &flagState{}
	resetFlagStateDefaults(state)

	rc := runConfig{
		baselinePath:         "does-not-exist-baseline.toml",
		baselinePathExplicit: true,
	}
	_, _, err := loadRunInputs(state, rc)
	if err == nil {
		t.Fatal("expected missing explicit baseline path to fail")
	}
	if !strings.Contains(err.Error(), "reading baseline") || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("expected baseline-file-not-found error, got %v", err)
	}
}

func TestValidateRunConfigAllowsNoCFAWhenMissionChecksDisabled(t *testing.T) {
	t.Parallel()

	rc := runConfig{checkValidateUsage: true, noCFA: true}
	if err := validateRunConfig(rc); err != nil {
		t.Fatalf("expected no-cfa to be allowed for non-mission check, got %v", err)
	}
}
