// SPDX-License-Identifier: MPL-2.0

package racerepeat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	statusPassed   = "passed"
	statusFailed   = "failed"
	statusTimedOut = "timed-out"
	statusCanceled = "canceled"
)

type (
	// TestObservation records structured top-level test2json terminal counts.
	TestObservation struct {
		ID     string `json:"id"`
		Runs   int    `json:"runs"`
		Passes int    `json:"passes"`
		Fails  int    `json:"fails"`
		Skips  int    `json:"skips"`
	}

	// WorkResult binds one structured execution result to its exact plan unit.
	WorkResult struct {
		FormatVersion  int               `json:"format_version"`
		PlanID         string            `json:"plan_id"`
		WorkUnitID     string            `json:"work_unit_id"`
		BinaryDigest   string            `json:"binary_digest"`
		ExpectedIDs    []string          `json:"expected_ids"`
		Observed       []TestObservation `json:"observed"`
		TerminalStatus string            `json:"terminal_status"`
		OutputDigest   string            `json:"output_digest"`
	}

	test2JSONEvent struct {
		Action  string `json:"Action"`
		Package string `json:"Package"`
		Test    string `json:"Test"`
	}
)

// ParseWorkResult validates structured test2json output against one exact
// work-unit population.
func ParseWorkResult(plan Plan, unit WorkUnit, output []byte, terminalStatus string) (WorkResult, error) {
	result := WorkResult{
		FormatVersion: ResultFormatVersion, PlanID: plan.PlanID, WorkUnitID: unit.ID,
		BinaryDigest: unit.BinaryDigest, ExpectedIDs: slices.Clone(unit.MemberIDs),
		Observed: []TestObservation{}, TerminalStatus: terminalStatus,
		OutputDigest: soundnessevidence.DigestBytes(output),
	}
	observed := make(map[string]TestObservation, len(unit.MemberIDs))
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event test2JSONEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return result, fmt.Errorf("decode race/repeat test2json event: %w", err)
		}
		if event.Test == "" || strings.Contains(event.Test, "/") {
			continue
		}
		if !slices.Contains(unit.MemberIDs, event.Test) {
			return result, fmt.Errorf("race/repeat work unit %q observed unexpected top-level member %q", unit.ID, event.Test)
		}
		observation := observed[event.Test]
		observation.ID = event.Test
		switch event.Action {
		case "run":
			observation.Runs++
		case "pass":
			observation.Passes++
		case "fail":
			observation.Fails++
		case "skip":
			observation.Skips++
		}
		observed[event.Test] = observation
	}
	if err := scanner.Err(); err != nil {
		return result, fmt.Errorf("scan race/repeat test2json output: %w", err)
	}
	for _, memberID := range unit.MemberIDs {
		observation, exists := observed[memberID]
		if !exists {
			observation.ID = memberID
		}
		result.Observed = append(result.Observed, observation)
	}
	if err := result.Validate(plan); err != nil {
		return result, err
	}
	return result, nil
}

// Validate checks exact result identity and successful no-gap execution.
func (result WorkResult) Validate(plan Plan) error {
	if result.FormatVersion != ResultFormatVersion || result.PlanID != plan.PlanID {
		return errors.New("race/repeat result has an invalid version or plan identity")
	}
	var unit WorkUnit
	found := false
	for _, candidate := range plan.WorkUnits {
		if candidate.ID == result.WorkUnitID {
			unit = candidate
			found = true
			break
		}
	}
	if !found || result.BinaryDigest != unit.BinaryDigest || !slices.Equal(result.ExpectedIDs, unit.MemberIDs) {
		return errors.New("race/repeat result does not match its work-unit or binary binding")
	}
	if err := soundnessevidence.ValidateDigest("race/repeat result output", result.OutputDigest); err != nil {
		return fmt.Errorf("validate race/repeat result output digest: %w", err)
	}
	if result.TerminalStatus != statusPassed {
		return fmt.Errorf("race/repeat work unit %q terminal status = %q", result.WorkUnitID, result.TerminalStatus)
	}
	if len(result.Observed) != len(unit.MemberIDs) {
		return fmt.Errorf("race/repeat work unit %q observed %d of %d members", unit.ID, len(result.Observed), len(unit.MemberIDs))
	}
	for index, memberID := range unit.MemberIDs {
		observation := result.Observed[index]
		if observation.ID != memberID || observation.Runs != 1 || observation.Passes != 1 ||
			observation.Fails != 0 || observation.Skips != 0 {
			return fmt.Errorf("race/repeat member %q has non-exact terminal counts %+v", memberID, observation)
		}
	}
	return nil
}

// CanonicalWorkResultJSON returns deterministic retained work-result bytes.
func CanonicalWorkResultJSON(result WorkResult, plan Plan) ([]byte, error) {
	if err := result.Validate(plan); err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat work result: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode canonical race/repeat work result: %w", err)
	}
	return append(data, '\n'), nil
}
