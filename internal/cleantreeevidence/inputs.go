// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

const (
	planInputName  = "clean-tree-plan"
	pathsInputName = "path-selection"
)

var taskLinePattern = regexp.MustCompile(`^- \[([ xX])\] ([0-9]+(?:\.[0-9]+)?)\b`)

type counterexampleInventory struct {
	FormatVersion   int                            `json:"format_version"`
	Counterexamples []counterexampleInventoryEntry `json:"counterexamples"`
}

type counterexampleInventoryEntry struct {
	ID          string `json:"id"`
	Observation string `json:"observation"`
}

func collectInputs(root, planPath, pathsPath string, plan Plan) ([]InputIdentity, error) {
	inputs := make([]InputPlan, 0, len(plan.Inputs)+2)
	inputs = append(inputs,
		InputPlan{Name: planInputName, Path: planPath},
		InputPlan{Name: pathsInputName, Path: pathsPath},
	)
	inputs = append(inputs, plan.Inputs...)
	seen := make(map[string]bool, len(inputs))
	result := make([]InputIdentity, 0, len(inputs))
	for _, input := range inputs {
		if seen[input.Name] {
			return nil, fmt.Errorf("duplicate retained input name %q", input.Name)
		}
		seen[input.Name] = true
		digest, err := digestFile(resolveFromRoot(root, input.Path))
		if err != nil {
			return nil, fmt.Errorf("digest input %q: %w", input.Name, err)
		}
		result = append(result, InputIdentity{Name: input.Name, Path: input.Path, SHA256: digest})
	}
	return result, nil
}

func collectToolchain(ctx context.Context, root string, plan Plan) ([]ToolIdentity, error) {
	tools := make([]ToolIdentity, 0, len(plan.Toolchain))
	for _, planned := range plan.Toolchain {
		output, err := runCommand(ctx, root, nil, nil, planned.Command[0], planned.Command[1:]...)
		if err != nil {
			return nil, fmt.Errorf("capture %s version: %w", planned.Name, err)
		}
		version := strings.TrimSpace(string(output))
		matched, err := regexp.MatchString(planned.RequiredVersionRE, version)
		if err != nil {
			return nil, fmt.Errorf("match %s version: %w", planned.Name, err)
		}
		if !matched {
			return nil, fmt.Errorf("%s version %q does not match %q", planned.Name, version, planned.RequiredVersionRE)
		}
		tools = append(tools, ToolIdentity{
			Name:              planned.Name,
			Command:           slices.Clone(planned.Command),
			RequiredVersionRE: planned.RequiredVersionRE,
			Version:           version,
		})
	}
	return tools, nil
}

func collectTaskLedgers(root string, plan Plan) ([]TaskLedgerIdentity, error) {
	ledgers := make([]TaskLedgerIdentity, 0, len(plan.TaskLedgers))
	for _, planned := range plan.TaskLedgers {
		path := resolveFromRoot(root, planned.Path)
		identity, err := readTaskLedger(path)
		if err != nil {
			return nil, fmt.Errorf("read task ledger %q: %w", planned.Name, err)
		}
		identity.Name = planned.Name
		identity.Path = planned.Path
		if !slices.Equal(identity.PendingIDs, planned.ExpectedPending) {
			return nil, fmt.Errorf(
				"task ledger %q pending IDs %q, expected %q",
				planned.Name,
				identity.PendingIDs,
				planned.ExpectedPending,
			)
		}
		ledgers = append(ledgers, identity)
	}
	return ledgers, nil
}

func readTaskLedger(path string) (_ TaskLedgerIdentity, resultErr error) {
	if err := requireRegularFile(path); err != nil {
		return TaskLedgerIdentity{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return TaskLedgerIdentity{}, fmt.Errorf("open task ledger %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close task ledger %q: %w", path, closeErr))
		}
	}()
	identity := TaskLedgerIdentity{}
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		match := taskLinePattern.FindStringSubmatch(line)
		if match == nil {
			if strings.HasPrefix(line, "- [") {
				return TaskLedgerIdentity{}, fmt.Errorf("malformed task checkbox %q", line)
			}
			continue
		}
		id := match[2]
		if seen[id] {
			return TaskLedgerIdentity{}, fmt.Errorf("duplicate task ID %q", id)
		}
		seen[id] = true
		identity.Total++
		if match[1] == "x" || match[1] == "X" {
			identity.Completed++
		} else {
			identity.PendingIDs = append(identity.PendingIDs, id)
		}
	}
	if err := scanner.Err(); err != nil {
		return TaskLedgerIdentity{}, fmt.Errorf("scan task ledger %q: %w", path, err)
	}
	if identity.Total == 0 {
		return TaskLedgerIdentity{}, errors.New("task ledger has no checkboxes")
	}
	slices.Sort(identity.PendingIDs)
	digest, err := digestFile(path)
	if err != nil {
		return TaskLedgerIdentity{}, err
	}
	identity.SHA256 = digest
	return identity, nil
}

func collectCounterexamples(root string, plan Plan) (CounterexampleIdentity, error) {
	path := resolveFromRoot(root, plan.Counterexamples.Path)
	var inventory counterexampleInventory
	if err := decodeStrictJSONFile(path, &inventory); err != nil {
		return CounterexampleIdentity{}, fmt.Errorf("decode counterexample inventory: %w", err)
	}
	if inventory.FormatVersion != FormatVersion {
		return CounterexampleIdentity{}, fmt.Errorf("unsupported counterexample inventory format %d", inventory.FormatVersion)
	}
	observed := make([]CounterexampleObservationPlan, 0, len(inventory.Counterexamples))
	seen := make(map[string]bool, len(inventory.Counterexamples))
	for _, counterexample := range inventory.Counterexamples {
		if counterexample.ID == "" || counterexample.Observation == "" || seen[counterexample.ID] {
			return CounterexampleIdentity{}, fmt.Errorf("incomplete or duplicate counterexample %q", counterexample.ID)
		}
		seen[counterexample.ID] = true
		observed = append(observed, CounterexampleObservationPlan(counterexample))
	}
	if !slices.IsSortedFunc(observed, func(left, right CounterexampleObservationPlan) int {
		return strings.Compare(left.ID, right.ID)
	}) {
		return CounterexampleIdentity{}, errors.New("counterexample inventory IDs must be sorted")
	}
	if !slices.Equal(observed, plan.Counterexamples.Required) {
		return CounterexampleIdentity{}, fmt.Errorf("counterexample observations %q, expected %q", observed, plan.Counterexamples.Required)
	}
	digest, err := digestFile(path)
	if err != nil {
		return CounterexampleIdentity{}, err
	}
	return CounterexampleIdentity{
		Path:         plan.Counterexamples.Path,
		SHA256:       digest,
		Observations: observed,
	}, nil
}

func commandVectorDigest(directory string, args []string) string {
	payload, err := json.Marshal(struct {
		Directory string   `json:"directory"`
		Args      []string `json:"args"`
	}{Directory: directory, Args: args})
	if err != nil {
		panic(fmt.Sprintf("marshal command vector: %v", err))
	}
	return digestBytes(payload)
}

func relativeToRoot(root, path string) (string, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", fmt.Errorf("relativize %q against repository root %q: %w", path, root, err)
	}
	relative = filepath.ToSlash(relative)
	if err := validateRepoPath(relative); err != nil {
		return "", err
	}
	return relative, nil
}
