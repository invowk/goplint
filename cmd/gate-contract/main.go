// SPDX-License-Identifier: MPL-2.0

// Command gate-contract validates that the canonical goplint soundness target
// cannot silently omit a declared proof surface.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type gateContract struct {
	FormatVersion      int                  `json:"format_version"`
	AggregateTarget    string               `json:"aggregate_target"`
	Checks             []gateContractCheck  `json:"checks"`
	NonVacuityMarkers  []gateContractMarker `json:"non_vacuity_markers"`
	RequiredCITriggers []string             `json:"required_ci_triggers"`
}

type gateContractCheck struct {
	Target   string             `json:"target"`
	Evidence string             `json:"evidence"`
	Markers  []string           `json:"markers"`
	Tests    []gateContractTest `json:"tests,omitempty"`
}

type gateContractTest struct {
	Name     string `json:"name"`
	Evidence string `json:"evidence"`
}

type gateContractMarker struct {
	Evidence string `json:"evidence"`
	Marker   string `json:"marker"`
}

type fileReader func(path string) ([]byte, error)

func main() {
	contractPath := flag.String("contract", "testdata/gates/soundness-v1.json", "soundness gate contract")
	root := flag.String("root", "../..", "repository root")
	flag.Parse()
	contract, err := loadContract(*contractPath)
	if err == nil {
		err = validateContract(contract, *root, os.ReadFile)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint gate contract:", err)
		os.Exit(1)
	}
	fmt.Printf("goplint gate contract: %d blocking checks, %d non-vacuity markers, %d CI triggers\n",
		len(contract.Checks), len(contract.NonVacuityMarkers), len(contract.RequiredCITriggers))
}

func loadContract(path string) (gateContract, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return gateContract{}, fmt.Errorf("read contract: %w", err)
	}
	var contract gateContract
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contract); err != nil {
		return gateContract{}, fmt.Errorf("decode contract: %w", err)
	}
	return contract, nil
}

func validateContract(contract gateContract, root string, read fileReader) error {
	if contract.FormatVersion != 1 || contract.AggregateTarget == "" || len(contract.Checks) == 0 {
		return errors.New("empty or unsupported gate contract")
	}
	makefile, err := read(filepath.Join(root, "Makefile"))
	if err != nil {
		return fmt.Errorf("read Makefile: %w", err)
	}
	dependencies, err := makeTargetDependencies(string(makefile), contract.AggregateTarget)
	if err != nil {
		return err
	}
	runnerBacked := len(dependencies) == 0 && makeTargetRecipeContains(
		string(makefile),
		contract.AggregateTarget,
		"cmd/soundness-gate",
	)
	if len(dependencies) == 0 && !runnerBacked {
		return fmt.Errorf("aggregate target %s has neither dependencies nor the soundness-gate runner", contract.AggregateTarget)
	}
	seen := make(map[string]bool, len(contract.Checks))
	for _, check := range contract.Checks {
		if check.Target == "" || check.Evidence == "" || len(check.Markers) == 0 || seen[check.Target] {
			return fmt.Errorf("incomplete or duplicate gate check %q", check.Target)
		}
		seen[check.Target] = true
		if !runnerBacked && !slices.Contains(dependencies, check.Target) {
			return fmt.Errorf("aggregate target omits %s", check.Target)
		}
		if _, err := makeTargetDependencies(string(makefile), check.Target); err != nil {
			return err
		}
		seenMarkers := make(map[string]bool, len(check.Markers))
		for _, marker := range check.Markers {
			if marker == "" || seenMarkers[marker] {
				return fmt.Errorf("%s: empty or duplicate evidence marker %q", check.Target, marker)
			}
			seenMarkers[marker] = true
			if err := requireEvidenceMarker(root, check.Evidence, marker, read); err != nil {
				return fmt.Errorf("%s: %w", check.Target, err)
			}
		}
		seenTests := make(map[string]bool, len(check.Tests))
		for _, test := range check.Tests {
			if test.Name == "" || test.Evidence == "" || seenTests[test.Name] {
				return fmt.Errorf("%s: incomplete or duplicate required test %q", check.Target, test.Name)
			}
			seenTests[test.Name] = true
			if err := requireGoTestDefinition(root, test.Evidence, test.Name, read); err != nil {
				return fmt.Errorf("%s test %s: %w", check.Target, test.Name, err)
			}
		}
	}
	if !runnerBacked && len(dependencies) != len(contract.Checks) {
		return fmt.Errorf("aggregate dependency count = %d, contract checks = %d", len(dependencies), len(contract.Checks))
	}
	for _, marker := range contract.NonVacuityMarkers {
		if marker.Evidence == "" || marker.Marker == "" {
			return errors.New("empty non-vacuity marker")
		}
		if err := requireEvidenceMarker(root, marker.Evidence, marker.Marker, read); err != nil {
			return fmt.Errorf("non-vacuity proof: %w", err)
		}
	}
	workflow, err := read(filepath.Join(root, ".github", "workflows", "lint.yml"))
	if err != nil {
		return fmt.Errorf("read lint workflow: %w", err)
	}
	for _, trigger := range contract.RequiredCITriggers {
		if trigger == "" || !strings.Contains(string(workflow), trigger) {
			return fmt.Errorf("lint workflow omits required trigger %q", trigger)
		}
	}
	return nil
}

func makeTargetRecipeContains(makefile, target, marker string) bool {
	prefix := target + ":"
	insideTarget := false
	for line := range strings.SplitSeq(makefile, "\n") {
		if strings.HasPrefix(line, prefix) {
			insideTarget = true
			continue
		}
		if !insideTarget {
			continue
		}
		if line == "" {
			continue
		}
		if line[0] != '\t' {
			return false
		}
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func makeTargetDependencies(makefile, target string) ([]string, error) {
	prefix := target + ":"
	for line := range strings.SplitSeq(makefile, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, prefix)))
		return fields, nil
	}
	return nil, fmt.Errorf("makefile target %s is missing", target)
}

func requireEvidenceMarker(root, path, marker string, read fileReader) error {
	data, err := read(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return fmt.Errorf("read evidence %s: %w", path, err)
	}
	if !strings.Contains(string(data), marker) {
		return fmt.Errorf("evidence %s omits marker %q", path, marker)
	}
	return nil
}

func requireGoTestDefinition(root, path, testName string, read fileReader) error {
	data, err := read(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return fmt.Errorf("read test evidence %s: %w", path, err)
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, data, 0)
	if err != nil {
		return fmt.Errorf("parse test evidence %s: %w", path, err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && function.Name.Name == testName {
			return nil
		}
	}
	return fmt.Errorf("test evidence %s omits top-level definition %q", path, testName)
}
