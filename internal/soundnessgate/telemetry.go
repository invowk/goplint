// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// TelemetryFormatVersion is the supported aggregate telemetry format.
	TelemetryFormatVersion = 1

	// EnvTelemetryPath is the optional destination for aggregate telemetry.
	EnvTelemetryPath = "GOPLINT_SOUNDNESS_TELEMETRY_PATH"

	terminalStatusPassed = "passed"
)

type (
	// ResourceReservation records the resources reserved for one work unit.
	// A zero memory reservation denotes the legacy unbounded serial policy; the
	// execution-plan scheduler replaces it with an explicit positive budget.
	ResourceReservation struct {
		CPUUnits    int   `json:"cpu_units"`
		MemoryBytes int64 `json:"memory_bytes"`
		WorkerSlots int   `json:"worker_slots"`
	}

	// RunTelemetry records aggregate execution timing and resource evidence.
	RunTelemetry struct {
		FormatVersion           int                `json:"format_version"`
		RunID                   string             `json:"run_id"`
		Profile                 ProfileID          `json:"profile"`
		WorkspaceDigest         string             `json:"workspace_digest"`
		ManifestDigest          string             `json:"manifest_digest"`
		StartedAt               time.Time          `json:"started_at"`
		FinishedAt              time.Time          `json:"finished_at"`
		WallDurationNanoseconds int64              `json:"wall_duration_nanoseconds"`
		CriticalPathNanoseconds int64              `json:"critical_path_nanoseconds"`
		MaxReservedCPUUnits     int                `json:"max_reserved_cpu_units"`
		MaxReservedMemoryBytes  int64              `json:"max_reserved_memory_bytes"`
		Subgates                []SubgateTelemetry `json:"subgates"`
	}

	// SubgateTelemetry records one serial or scheduled work unit's measurements.
	SubgateTelemetry struct {
		ID                       string              `json:"id"`
		QueuedAt                 time.Time           `json:"queued_at"`
		StartedAt                time.Time           `json:"started_at"`
		FinishedAt               time.Time           `json:"finished_at"`
		QueueDurationNanoseconds int64               `json:"queue_duration_nanoseconds"`
		WallDurationNanoseconds  int64               `json:"wall_duration_nanoseconds"`
		CPUTimeNanoseconds       int64               `json:"cpu_time_nanoseconds"`
		PeakRSSBytes             int64               `json:"peak_rss_bytes"`
		ReservedResources        ResourceReservation `json:"reserved_resources"`
		TimeoutSeconds           int                 `json:"timeout_seconds"`
		TimedOut                 bool                `json:"timed_out"`
		TerminalStatus           string              `json:"terminal_status"`
		Populations              []Population        `json:"populations"`
	}
)

// Validate verifies telemetry identity, canonical order, measurements, and
// population evidence without imposing machine-specific performance limits.
func (telemetry RunTelemetry) Validate() error {
	if telemetry.FormatVersion != TelemetryFormatVersion {
		return fmt.Errorf("soundness telemetry format_version = %d, want %d", telemetry.FormatVersion, TelemetryFormatVersion)
	}
	if err := validateIdentifier("soundness telemetry run_id", telemetry.RunID); err != nil {
		return err
	}
	if !isKnownProfile(telemetry.Profile) {
		return fmt.Errorf("soundness telemetry profile = %q, want a reviewed profile", telemetry.Profile)
	}
	if err := soundnessevidence.ValidateDigest("soundness telemetry workspace_digest", telemetry.WorkspaceDigest); err != nil {
		return fmt.Errorf("validate soundness telemetry workspace digest: %w", err)
	}
	if err := soundnessevidence.ValidateDigest("soundness telemetry manifest_digest", telemetry.ManifestDigest); err != nil {
		return fmt.Errorf("validate soundness telemetry manifest digest: %w", err)
	}
	if telemetry.StartedAt.IsZero() || telemetry.FinishedAt.Before(telemetry.StartedAt) {
		return errors.New("soundness telemetry has invalid aggregate timestamps")
	}
	if telemetry.WallDurationNanoseconds < 0 || telemetry.CriticalPathNanoseconds < 0 {
		return errors.New("soundness telemetry has negative aggregate duration")
	}
	if telemetry.MaxReservedCPUUnits <= 0 || telemetry.MaxReservedMemoryBytes < 0 {
		return errors.New("soundness telemetry has invalid maximum reservations")
	}
	if len(telemetry.Subgates) == 0 {
		return errors.New("soundness telemetry has no subgates")
	}
	previousID := ""
	for index := range telemetry.Subgates {
		subgate := telemetry.Subgates[index]
		if err := subgate.validate(index); err != nil {
			return err
		}
		if previousID != "" && subgate.ID < previousID {
			return errors.New("soundness telemetry subgates must use canonical id order")
		}
		previousID = subgate.ID
	}
	return nil
}

// NormalizeForComparison returns canonical semantic/resource telemetry with
// volatile run, timestamp, duration, CPU-time, and peak-RSS fields removed.
func (telemetry RunTelemetry) NormalizeForComparison() RunTelemetry {
	normalized := telemetry
	normalized.RunID = "normalized-run"
	normalized.StartedAt = time.Unix(0, 0).UTC()
	normalized.FinishedAt = normalized.StartedAt
	normalized.WallDurationNanoseconds = 0
	normalized.CriticalPathNanoseconds = 0
	normalized.Subgates = slices.Clone(telemetry.Subgates)
	for index := range normalized.Subgates {
		subgate := &normalized.Subgates[index]
		subgate.QueuedAt = normalized.StartedAt
		subgate.StartedAt = normalized.StartedAt
		subgate.FinishedAt = normalized.StartedAt
		subgate.QueueDurationNanoseconds = 0
		subgate.WallDurationNanoseconds = 0
		subgate.CPUTimeNanoseconds = 0
		subgate.PeakRSSBytes = 0
		subgate.Populations = slices.Clone(subgate.Populations)
		slices.SortFunc(subgate.Populations, func(left, right Population) int {
			return strings.Compare(left.ID, right.ID)
		})
	}
	slices.SortFunc(normalized.Subgates, func(left, right SubgateTelemetry) int {
		return strings.Compare(left.ID, right.ID)
	})
	return normalized
}

// NormalizedJSON returns deterministic comparison bytes for aggregate
// telemetry after removing fields that are allowed to vary between runs.
func (telemetry RunTelemetry) NormalizedJSON() ([]byte, error) {
	normalized := telemetry.NormalizeForComparison()
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode normalized soundness telemetry: %w", err)
	}
	return data, nil
}

// LoadTelemetry strictly decodes and validates retained aggregate telemetry.
func LoadTelemetry(ctx context.Context, path string) (RunTelemetry, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return RunTelemetry{}, fmt.Errorf("load soundness telemetry %s: %w", path, err)
	}
	var telemetry RunTelemetry
	if err := decodeStrictJSON(data, &telemetry); err != nil {
		return RunTelemetry{}, fmt.Errorf("decode soundness telemetry %s: %w", path, err)
	}
	if err := telemetry.Validate(); err != nil {
		return RunTelemetry{}, fmt.Errorf("validate soundness telemetry %s: %w", path, err)
	}
	return telemetry, nil
}

func (telemetry SubgateTelemetry) validate(index int) error {
	if err := validateIdentifier(fmt.Sprintf("soundness telemetry subgates[%d].id", index), telemetry.ID); err != nil {
		return err
	}
	if telemetry.QueuedAt.IsZero() || telemetry.StartedAt.Before(telemetry.QueuedAt) || telemetry.FinishedAt.Before(telemetry.StartedAt) {
		return fmt.Errorf("soundness telemetry subgates[%d] has invalid timestamps", index)
	}
	if telemetry.QueueDurationNanoseconds < 0 || telemetry.WallDurationNanoseconds < 0 ||
		telemetry.CPUTimeNanoseconds < 0 || telemetry.PeakRSSBytes < 0 {
		return fmt.Errorf("soundness telemetry subgates[%d] has a negative measurement", index)
	}
	if telemetry.ReservedResources.CPUUnits <= 0 || telemetry.ReservedResources.MemoryBytes < 0 ||
		telemetry.ReservedResources.WorkerSlots <= 0 {
		return fmt.Errorf("soundness telemetry subgates[%d] has invalid reserved resources", index)
	}
	if telemetry.TimeoutSeconds <= 0 || telemetry.TimeoutSeconds > maximumTimeoutSeconds {
		return fmt.Errorf("soundness telemetry subgates[%d].timeout_seconds = %d, want 1..%d", index, telemetry.TimeoutSeconds, maximumTimeoutSeconds)
	}
	if telemetry.TimedOut || telemetry.TerminalStatus != terminalStatusPassed {
		return fmt.Errorf("soundness telemetry subgates[%d] is not a successful terminal result", index)
	}
	if len(telemetry.Populations) == 0 {
		return fmt.Errorf("soundness telemetry subgates[%d] has no populations", index)
	}
	seenPopulations := make(map[string]bool, len(telemetry.Populations))
	previousPopulationID := ""
	for populationIndex, population := range telemetry.Populations {
		if err := validateIdentifier(
			fmt.Sprintf("soundness telemetry subgates[%d].populations[%d].id", index, populationIndex),
			population.ID,
		); err != nil {
			return err
		}
		if population.Count <= 0 {
			return fmt.Errorf("soundness telemetry subgates[%d].populations[%d].count = %d, want positive", index, populationIndex, population.Count)
		}
		if seenPopulations[population.ID] {
			return fmt.Errorf("soundness telemetry subgate %q contains duplicate population id %q", telemetry.ID, population.ID)
		}
		seenPopulations[population.ID] = true
		if previousPopulationID != "" && population.ID < previousPopulationID {
			return fmt.Errorf("soundness telemetry subgate %q populations must use canonical id order", telemetry.ID)
		}
		previousPopulationID = population.ID
	}
	return nil
}
