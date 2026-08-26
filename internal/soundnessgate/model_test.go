// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"strings"
	"testing"

	"github.com/invowk/goplint/internal/soundnessevidence"
)

func TestManifestValidateRejectsBidirectionalDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*Manifest, *soundnessevidence.Registry)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*Manifest, *soundnessevidence.Registry) {},
			wantError: "",
		},
		{
			name: "missing registration dependency",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].RequiredRegistrationIDs = nil
			},
			wantError: "is missing from its producer subgate",
		},
		{
			name: "extra registration dependency",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].RequiredRegistrationIDs = []string{"cast-validation.production", "forged.registration"}
			},
			wantError: "extra registration",
		},
		{
			name: "wrong producer owner",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].RequiredRegistrationIDs = []string{"use-before-validation.production"}
			},
			wantError: "owned by producer",
		},
		{
			name: "successful zero minimum",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].RequiredPopulations[0].Minimum = 0
			},
			wantError: "want positive",
		},
		{
			name: "empty category registry",
			mutate: func(_ *Manifest, registry *soundnessevidence.Registry) {
				registry.Registrations = nil
			},
			wantError: "no registrations",
		},
		{
			name: "duplicate subgate",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[2].ID = manifest.Subgates[1].ID
			},
			wantError: "duplicate subgate",
		},
		{
			name: "unknown dependency",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].Dependencies = []string{"unknown"}
			},
			wantError: "depends on unknown subgate",
		},
		{
			name: "dependency cycle",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].Dependencies = []string{"semantic-production-b"}
				manifest.Subgates[2].Dependencies = []string{"semantic-production-a"}
			},
			wantError: "dependency cycle",
		},
		{
			name: "profile membership drift",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Subgates[1].ProfileIDs = []ProfileID{ProfileComplete}
			},
			wantError: "profile_ids",
		},
		{
			name: "unscheduled semantic subgate",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Profiles[3].SubgateIDs = manifest.Profiles[3].SubgateIDs[:1]
			},
			wantError: "complete profile must equal semantic",
		},
		{
			name: "unscheduled complete subgate",
			mutate: func(manifest *Manifest, _ *soundnessevidence.Registry) {
				manifest.Profiles[0].SubgateIDs = manifest.Profiles[0].SubgateIDs[:2]
			},
			wantError: "complete profile must equal semantic",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manifest := validGateManifest()
			registry := validGateRegistry()
			test.mutate(&manifest, &registry)
			assertGateErrorContains(t, manifest.Validate(registry), test.wantError)
		})
	}
}

func TestSubgateValidateReportRejectsVacuousAndStaleResults(t *testing.T) {
	t.Parallel()

	subgate := validGateManifest().Subgates[1]
	binding := validGateBinding(subgate)
	tests := []struct {
		name      string
		mutate    func(*Report)
		wantError string
	}{
		{
			name:      "valid",
			mutate:    func(*Report) {},
			wantError: "",
		},
		{
			name: "zero population",
			mutate: func(report *Report) {
				report.Populations[0].Count = 0
			},
			wantError: "want at least",
		},
		{
			name: "extra population",
			mutate: func(report *Report) {
				report.Populations = append(report.Populations, Population{ID: "extra", Count: 1})
			},
			wantError: "population count",
		},
		{
			name: "stale command binding",
			mutate: func(report *Report) {
				report.Binding.CommandDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			},
			wantError: "stale or mismatched",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			report := Report{
				FormatVersion: ReportFormatVersion,
				Binding:       binding,
				Status:        StatusPassed,
				Populations:   []Population{{ID: "cases", Count: 1}},
			}
			test.mutate(&report)
			assertGateErrorContains(t, subgate.ValidateReport(report, binding), test.wantError)
		})
	}
}

func assertGateErrorContains(t *testing.T, err error, substring string) {
	t.Helper()
	if substring == "" {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error containing %q", substring)
	}
	if !strings.Contains(err.Error(), substring) {
		t.Fatalf("error = %q, want substring %q", err, substring)
	}
}
