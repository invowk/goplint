// SPDX-License-Identifier: MPL-2.0

// Package mutationguard defines the structured semantic mismatch contract
// shared by targeted mutation guards and the mutation runner.
package mutationguard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var canonicalTokenPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/,=-]*$`)

const (
	// EventFormatVersion is the supported structured guard-event format.
	EventFormatVersion = 1
	// EventPrefix identifies one structured guard mismatch in go test output.
	EventPrefix = "GOPLINT_MUTATION_GUARD_MISMATCH_V1 "
)

// Observation is one canonical semantic state observed by a guard assertion.
type Observation struct {
	Subject string `json:"subject"`
	State   string `json:"state"`
}

// AssertionEvent is emitted by a guard only when its semantic assertion does
// not observe the clean expected state.
type AssertionEvent struct {
	FormatVersion int         `json:"format_version"`
	AssertionID   string      `json:"assertion_id"`
	Expected      Observation `json:"expected"`
	Actual        Observation `json:"actual"`
}

// Validate verifies that an observation is a stable, single-line semantic ID.
func (observation Observation) Validate() error {
	if err := validateToken("observation subject", observation.Subject); err != nil {
		return err
	}
	if err := validateToken("observation state", observation.State); err != nil {
		return err
	}
	return nil
}

// Validate verifies that an assertion event represents a real mismatch.
func (event AssertionEvent) Validate() error {
	if event.FormatVersion != EventFormatVersion {
		return fmt.Errorf("guard event format_version = %d, want %d", event.FormatVersion, EventFormatVersion)
	}
	if err := validateToken("guard event assertion_id", event.AssertionID); err != nil {
		return err
	}
	if err := event.Expected.Validate(); err != nil {
		return fmt.Errorf("guard event expected: %w", err)
	}
	if err := event.Actual.Validate(); err != nil {
		return fmt.Errorf("guard event actual: %w", err)
	}
	if event.Expected == event.Actual {
		return errors.New("guard event expected and actual observations are identical")
	}
	return nil
}

// EncodeEvent returns the exact single-line marker written by guard helpers.
func EncodeEvent(event AssertionEvent) (string, error) {
	if err := event.Validate(); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("encode guard event: %w", err)
	}
	return EventPrefix + string(encoded), nil
}

// DecodeOutputLine parses a structured mismatch embedded in one go test output
// line. Lines without the marker return found=false.
func DecodeOutputLine(line string) (event AssertionEvent, found bool, err error) {
	_, payload, found := strings.Cut(line, EventPrefix)
	if !found {
		return AssertionEvent{}, false, nil
	}
	payload = strings.TrimSpace(payload)
	decoder := json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decodeErr := decoder.Decode(&event); decodeErr != nil {
		return AssertionEvent{}, true, fmt.Errorf("decode guard event: %w", decodeErr)
	}
	var trailing any
	if decodeErr := decoder.Decode(&trailing); !errors.Is(decodeErr, io.EOF) {
		if decodeErr == nil {
			return AssertionEvent{}, true, errors.New("decode guard event: trailing JSON value")
		}
		return AssertionEvent{}, true, fmt.Errorf("decode guard event trailing data: %w", decodeErr)
	}
	if validateErr := event.Validate(); validateErr != nil {
		return AssertionEvent{}, true, validateErr
	}
	return event, true, nil
}

func validateToken(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s %q has surrounding whitespace", name, value)
	}
	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("%s %q is not a single-line token", name, value)
	}
	if !canonicalTokenPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not a canonical token", name, value)
	}
	return nil
}
