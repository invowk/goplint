// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"testing"
	"time"
)

func TestRunTelemetryNormalizedJSONIsDeterministic(t *testing.T) {
	t.Parallel()

	left := validRunTelemetry()
	right := validRunTelemetry()
	right.RunID = "run-other"
	right.StartedAt = right.StartedAt.Add(time.Hour)
	right.FinishedAt = right.FinishedAt.Add(2 * time.Hour)
	right.WallDurationNanoseconds = 99
	right.CriticalPathNanoseconds = 98
	right.Subgates[0], right.Subgates[1] = right.Subgates[1], right.Subgates[0]
	for index := range right.Subgates {
		right.Subgates[index].QueuedAt = right.Subgates[index].QueuedAt.Add(time.Hour)
		right.Subgates[index].StartedAt = right.Subgates[index].StartedAt.Add(time.Hour)
		right.Subgates[index].FinishedAt = right.Subgates[index].FinishedAt.Add(time.Hour)
		right.Subgates[index].QueueDurationNanoseconds++
		right.Subgates[index].WallDurationNanoseconds++
		right.Subgates[index].CPUTimeNanoseconds++
		right.Subgates[index].PeakRSSBytes++
	}

	leftJSON, err := left.NormalizedJSON()
	if err != nil {
		t.Fatalf("left.NormalizedJSON() error = %v", err)
	}
	rightJSON, err := right.NormalizedJSON()
	if err != nil {
		t.Fatalf("right.NormalizedJSON() error = %v", err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatalf("normalized telemetry differs:\nleft:  %s\nright: %s", leftJSON, rightJSON)
	}
}

func TestRunTelemetryValidateRejectsInvalidMeasurements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*RunTelemetry)
	}{
		{
			name: "negative CPU time",
			mutate: func(telemetry *RunTelemetry) {
				telemetry.Subgates[0].CPUTimeNanoseconds = -1
			},
		},
		{
			name: "invalid reservation",
			mutate: func(telemetry *RunTelemetry) {
				telemetry.Subgates[0].ReservedResources.CPUUnits = 0
			},
		},
		{
			name: "noncanonical subgates",
			mutate: func(telemetry *RunTelemetry) {
				telemetry.Subgates[0], telemetry.Subgates[1] = telemetry.Subgates[1], telemetry.Subgates[0]
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			telemetry := validRunTelemetry()
			test.mutate(&telemetry)
			if err := telemetry.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want non-nil")
			}
		})
	}
}

func validRunTelemetry() RunTelemetry {
	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	subgates := []SubgateTelemetry{
		validSubgateTelemetry("a-subgate", startedAt),
		validSubgateTelemetry("b-subgate", startedAt.Add(time.Second)),
	}
	return RunTelemetry{
		FormatVersion:           TelemetryFormatVersion,
		RunID:                   "run-test",
		Profile:                 ProfileCore,
		WorkspaceDigest:         runnerTestWorkspaceDigest,
		ManifestDigest:          "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		StartedAt:               startedAt,
		FinishedAt:              startedAt.Add(3 * time.Second),
		WallDurationNanoseconds: int64(3 * time.Second),
		CriticalPathNanoseconds: int64(3 * time.Second),
		MaxReservedCPUUnits:     2,
		MaxReservedMemoryBytes:  2048,
		Subgates:                subgates,
	}
}

func validSubgateTelemetry(id string, startedAt time.Time) SubgateTelemetry {
	return SubgateTelemetry{
		ID:                       id,
		QueuedAt:                 startedAt.Add(-time.Second),
		StartedAt:                startedAt,
		FinishedAt:               startedAt.Add(time.Second),
		QueueDurationNanoseconds: int64(time.Second),
		WallDurationNanoseconds:  int64(time.Second),
		CPUTimeNanoseconds:       int64(500 * time.Millisecond),
		PeakRSSBytes:             1024,
		ReservedResources: ResourceReservation{
			CPUUnits:    2,
			MemoryBytes: 2048,
			WorkerSlots: 1,
		},
		TimeoutSeconds: 30,
		TimedOut:       false,
		TerminalStatus: terminalStatusPassed,
		Populations:    []Population{{ID: "cases", Count: 1}},
	}
}
