// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLiveGateContract(t *testing.T) {
	t.Parallel()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error: %v", err)
	}
	moduleRoot := filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
	repositoryRoot := filepath.Clean(filepath.Join(moduleRoot, "..", ".."))
	contract, err := loadContract(filepath.Join(moduleRoot, "testdata", "gates", "soundness-v1.json"))
	if err != nil {
		t.Fatalf("loadContract() error: %v", err)
	}
	if err := validateContract(contract, repositoryRoot, os.ReadFile); err != nil {
		t.Fatalf("validateContract() error: %v", err)
	}
}

func TestValidateContractRejectsVacuousGateShapes(t *testing.T) {
	t.Parallel()

	base := gateContract{
		FormatVersion:   1,
		AggregateTarget: "soundness",
		Checks: []gateContractCheck{
			{
				Target:   "integration",
				Evidence: "integration.sh",
				Markers:  []string{"real-analyzer", "TestIntegrationA", "TestIntegrationB"},
				Tests: []gateContractTest{
					{Name: "TestIntegrationA", Evidence: "integration_test.go"},
					{Name: "TestIntegrationB", Evidence: "integration_test.go"},
				},
			},
			{Target: "oracle", Evidence: "oracle.sh", Markers: []string{"generated programs"}},
		},
		NonVacuityMarkers:  []gateContractMarker{{Evidence: "oracle_test.go", Marker: "zero generated programs"}},
		RequiredCITriggers: []string{"docs/goplint/**"},
	}
	files := map[string]string{
		"Makefile":                   "soundness: integration oracle\nintegration:\n\t./integration.sh\noracle:\n\t./oracle.sh\n",
		"integration.sh":             "real-analyzer TestIntegrationA TestIntegrationB",
		"integration_test.go":        "package probe\n\nfunc TestIntegrationA(t *testing.T) {}\nfunc TestIntegrationB(t *testing.T) {}",
		"oracle.sh":                  "generated programs",
		"oracle_test.go":             "zero generated programs",
		".github/workflows/lint.yml": "docs/goplint/**",
	}
	reader := mapReader(files)
	if err := validateContract(base, ".", reader); err != nil {
		t.Fatalf("valid contract rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(gateContract, map[string]string) (gateContract, map[string]string)
		want   string
	}{
		{
			name: "omitted aggregate dependency",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["Makefile"] = strings.ReplaceAll(files["Makefile"], "soundness: integration oracle", "soundness: integration")
				return contract, files
			},
			want: "omits oracle",
		},
		{
			name: "empty selection marker",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["oracle.sh"] = "go test -run '^$'"
				return contract, files
			},
			want: "omits marker",
		},
		{
			name: "skipped analyzer integration",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["integration.sh"] = "unit-only"
				return contract, files
			},
			want: "real-analyzer",
		},
		{
			name: "missing selected required test",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["integration.sh"] = strings.ReplaceAll(files["integration.sh"], "TestIntegrationB", "")
				return contract, files
			},
			want: "TestIntegrationB",
		},
		{
			name: "missing required test definition",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["integration_test.go"] = "package probe\n\nfunc TestIntegrationA(t *testing.T) {}"
				return contract, files
			},
			want: "TestIntegrationB",
		},
		{
			name: "commented required test definition",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files["integration_test.go"] = "package probe\n\nfunc TestIntegrationA(t *testing.T) {}\n// func TestIntegrationB(t *testing.T) {}"
				return contract, files
			},
			want: "omits top-level definition",
		},
		{
			name: "duplicate check marker",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				contract.Checks = slices.Clone(contract.Checks)
				contract.Checks[0].Markers = []string{"real-analyzer", "real-analyzer"}
				return contract, files
			},
			want: "duplicate evidence marker",
		},
		{
			name: "duplicate required test",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				contract.Checks = slices.Clone(contract.Checks)
				contract.Checks[0].Tests = []gateContractTest{
					{Name: "TestIntegrationA", Evidence: "integration_test.go"},
					{Name: "TestIntegrationA", Evidence: "integration_test.go"},
				}
				return contract, files
			},
			want: "duplicate required test",
		},
		{
			name: "missing non-vacuity evidence",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				delete(files, "oracle_test.go")
				return contract, files
			},
			want: "non-vacuity proof",
		},
		{
			name: "missing trigger",
			mutate: func(contract gateContract, files map[string]string) (gateContract, map[string]string) {
				files[".github/workflows/lint.yml"] = "on: pull_request"
				return contract, files
			},
			want: "required trigger",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cloned := make(map[string]string, len(files))
			maps.Copy(cloned, files)
			contract, changed := tt.mutate(base, cloned)
			err := validateContract(contract, ".", mapReader(changed))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateContract() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func mapReader(files map[string]string) fileReader {
	return func(path string) ([]byte, error) {
		content, ok := files[filepath.ToSlash(filepath.Clean(path))]
		if !ok {
			return nil, errors.New("missing fixture")
		}
		return []byte(content), nil
	}
}
