// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"io/fs"
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
	if !strings.Contains(err.Error(), "reading config") || !errors.Is(err, fs.ErrNotExist) {
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
	if !strings.Contains(err.Error(), "reading baseline") || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("expected baseline-file-not-found error, got %v", err)
	}
}

func TestValidateRunConfigRejectsExplicitEmptyPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rc   runConfig
		want string
	}{
		{
			name: "empty explicit config path",
			rc: runConfig{
				configPathExplicit: true,
				configPath:         "",
			},
			want: "flag --config was provided with an empty path",
		},
		{
			name: "empty explicit baseline path",
			rc: runConfig{
				baselinePathExplicit: true,
				baselinePath:         "  ",
			},
			want: "flag --baseline was provided with an empty path",
		},
		{
			name: "invalid cfg max states",
			rc: runConfig{
				cfgMaxStates: -1,
			},
			want: "flag --cfg-max-states must be > 0",
		},
		{
			name: "invalid witness max steps",
			rc: runConfig{
				cfgWitnessMaxSteps: -1,
			},
			want: "flag --cfg-witness-max-steps must be > 0",
		},
		{
			name: "invalid refinement max iterations",
			rc: runConfig{
				cfgRefinementMaxIterations: -1,
			},
			want: "flag --protocol-refinement-max-iterations must be > 0",
		},
		{
			name: "invalid feasibility max queries",
			rc: runConfig{
				cfgFeasibilityMaxQueries: -1,
			},
			want: "flag --protocol-feasibility-max-queries must be > 0",
		},
		{
			name: "invalid feasibility timeout",
			rc: runConfig{
				cfgFeasibilityTimeoutMS: -1,
			},
			want: "flag --protocol-feasibility-timeout-ms must be > 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateRunConfig(tt.rc)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateRunConfig() error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestRemovedProtocolFlagsAreUnknown(t *testing.T) {
	t.Parallel()

	h := newAnalyzerHarness()
	for _, name := range []string{
		"ubv-mode",
		"cfg-backend",
		"cfg-interproc-engine",
		"cfg-inconclusive-policy",
		"cfg-feasibility-engine",
		"cfg-refinement-mode",
		"cfg-alias-mode",
	} {
		if h.Analyzer.Flags.Lookup(name) != nil {
			t.Fatalf("removed flag --%s is still registered", name)
		}
		if err := h.Analyzer.Flags.Set(name, "removed"); err == nil {
			t.Fatalf("removed flag --%s was accepted", name)
		}
	}
}

func TestParseIncludePackagesOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		rc            runConfig
		want          []string
		wantHas       bool
		wantErrSubstr string
	}{
		{
			name: "not explicit",
			rc:   runConfig{},
		},
		{
			name: "explicit empty clears override",
			rc: runConfig{
				includePackagesExplicit: true,
				includePackages:         "",
			},
			want:    []string{},
			wantHas: true,
		},
		{
			name: "trims entries",
			rc: runConfig{
				includePackagesExplicit: true,
				includePackages:         " pkg/a ,pkg/b ",
			},
			want:    []string{"pkg/a", "pkg/b"},
			wantHas: true,
		},
		{
			name: "rejects empty segment",
			rc: runConfig{
				includePackagesExplicit: true,
				includePackages:         "pkg/a,,pkg/b",
			},
			wantErrSubstr: "empty package prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, has, err := parseIncludePackagesOverride(tt.rc)
			if tt.wantErrSubstr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErrSubstr) {
					t.Fatalf("parseIncludePackagesOverride() error = %q, want substring %q", err.Error(), tt.wantErrSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseIncludePackagesOverride() unexpected error: %v", err)
			}
			if has != tt.wantHas {
				t.Fatalf("parseIncludePackagesOverride() has = %v, want %v", has, tt.wantHas)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("parseIncludePackagesOverride() got=%v want=%v", got, tt.want)
			}
		})
	}
}
