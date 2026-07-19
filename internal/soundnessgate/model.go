// SPDX-License-Identifier: MPL-2.0

// Package soundnessgate executes and validates the canonical goplint
// soundness-subgate manifest.
package soundnessgate

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	maximumTimeoutSeconds = 86_400
	cleanTreeFreshnessID  = "clean-tree-freshness"

	// ManifestFormatVersion is the supported aggregate manifest format.
	ManifestFormatVersion = 1
	// ReportFormatVersion is the supported subgate report format.
	ReportFormatVersion = 1
	// RunReportFormatVersion is the supported retained aggregate report format.
	RunReportFormatVersion = 1

	// EnvReportPath is the optional destination for the retained aggregate report.
	EnvReportPath = "GOPLINT_SOUNDNESS_REPORT_PATH"
	// EnvSubgateReportPath is the aggregate-selected path for a producer report.
	EnvSubgateReportPath = "GOPLINT_SOUNDNESS_SUBGATE_REPORT_PATH"

	// ProfileCore executes every causal subgate except recursive freshness.
	ProfileCore ProfileID = "core"
	// ProfileComplete executes the core profile plus clean-tree freshness.
	ProfileComplete ProfileID = "complete"

	// StatusPassed is the only accepted successful producer report status.
	StatusPassed ReportStatus = "passed"
)

type (
	// Manifest is the reviewed aggregate contract for every soundness subgate.
	Manifest struct {
		FormatVersion int       `json:"format_version"`
		RegistryPath  string    `json:"registry_path"`
		Profiles      []Profile `json:"profiles"`
		Subgates      []Subgate `json:"subgates"`
	}

	// Profile is one reviewed selection of canonical manifest subgates.
	Profile struct {
		ID         ProfileID `json:"id"`
		SubgateIDs []string  `json:"subgate_ids"`
	}

	// ProfileID identifies a reviewed aggregate execution profile.
	ProfileID string

	// Subgate defines one exact command vector and its required fresh outputs.
	Subgate struct {
		ID                      string                  `json:"id"`
		WorkingDirectory        string                  `json:"working_directory"`
		Command                 []string                `json:"command"`
		TimeoutSeconds          int                     `json:"timeout_seconds"`
		ReportFile              string                  `json:"report_file"`
		RequiredRegistrationIDs []string                `json:"required_registration_ids"`
		RequiredPopulations     []PopulationRequirement `json:"required_populations"`
	}

	// PopulationRequirement defines one named, minimum nonzero admitted
	// population that a subgate must actually report.
	PopulationRequirement struct {
		ID      string `json:"id"`
		Minimum int    `json:"minimum"`
	}

	// ReportStatus is the machine result of a subgate command.
	ReportStatus string

	// Report is the fresh command-bound output required from every subgate.
	Report struct {
		FormatVersion int                                  `json:"format_version"`
		Binding       soundnessevidence.ObservationBinding `json:"binding"`
		Status        ReportStatus                         `json:"status"`
		Populations   []Population                         `json:"populations"`
	}

	// Population records an actually admitted corpus, category, seed, mutant,
	// deterministic reordering, benchmark, or counterexample population.
	Population struct {
		ID    string `json:"id"`
		Count int    `json:"count"`
	}

	// RunReport is the retained, fully validated result of one aggregate run.
	RunReport struct {
		FormatVersion   int                                     `json:"format_version"`
		Profile         ProfileID                               `json:"profile"`
		RunID           string                                  `json:"run_id"`
		WorkspaceDigest string                                  `json:"workspace_digest"`
		ManifestDigest  string                                  `json:"manifest_digest"`
		Subgates        []SubgateResult                         `json:"subgates"`
		Observations    []soundnessevidence.SemanticObservation `json:"observations"`
	}

	// SubgateResult binds the accepted report and populations to one exact
	// manifest command.
	SubgateResult struct {
		ID            string       `json:"id"`
		CommandDigest string       `json:"command_digest"`
		ReportDigest  string       `json:"report_digest"`
		Populations   []Population `json:"populations"`
	}
)

// Validate verifies the manifest and its bidirectional registry ownership.
func (manifest Manifest) Validate(registry soundnessevidence.Registry) error {
	if manifest.FormatVersion != ManifestFormatVersion {
		return fmt.Errorf("soundness manifest format_version = %d, want %d", manifest.FormatVersion, ManifestFormatVersion)
	}
	if err := validateRepositoryPath("soundness manifest registry_path", manifest.RegistryPath, false); err != nil {
		return err
	}
	if len(manifest.Subgates) == 0 {
		return errors.New("soundness manifest has no subgates")
	}
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("soundness manifest registry: %w", err)
	}
	registryByID := make(map[string]soundnessevidence.Registration, len(registry.Registrations))
	for _, registration := range registry.Registrations {
		registryByID[registration.ID] = registration
	}
	seenSubgates := make(map[string]bool, len(manifest.Subgates))
	seenRegistrations := make(map[string]bool, len(registry.Registrations))
	previousSubgateID := ""
	for index := range manifest.Subgates {
		subgate := &manifest.Subgates[index]
		if err := subgate.validate(index); err != nil {
			return err
		}
		if seenSubgates[subgate.ID] {
			return fmt.Errorf("soundness manifest contains duplicate subgate id %q", subgate.ID)
		}
		seenSubgates[subgate.ID] = true
		if previousSubgateID != "" && subgate.ID < previousSubgateID {
			return fmt.Errorf("soundness manifest subgates must use canonical id order: %q precedes %q", subgate.ID, previousSubgateID)
		}
		previousSubgateID = subgate.ID
		for _, registrationID := range subgate.RequiredRegistrationIDs {
			registration, exists := registryByID[registrationID]
			if !exists {
				return fmt.Errorf("soundness subgate %q requires extra registration %q", subgate.ID, registrationID)
			}
			if registration.ProducerID != subgate.ID {
				return fmt.Errorf(
					"soundness subgate %q requires registration %q owned by producer %q",
					subgate.ID,
					registrationID,
					registration.ProducerID,
				)
			}
			if seenRegistrations[registrationID] {
				return fmt.Errorf("soundness manifest requires registration %q more than once", registrationID)
			}
			seenRegistrations[registrationID] = true
		}
	}
	for _, registration := range registry.Registrations {
		if !seenSubgates[registration.ProducerID] {
			return fmt.Errorf("evidence registration %q has unknown producer subgate %q", registration.ID, registration.ProducerID)
		}
		if !seenRegistrations[registration.ID] {
			return fmt.Errorf("evidence registration %q is missing from its producer subgate", registration.ID)
		}
	}
	if err := manifest.validateProfiles(); err != nil {
		return err
	}
	coreSubgates, err := manifest.SubgatesForProfile(ProfileCore)
	if err != nil {
		return err
	}
	coreIDs := make(map[string]bool, len(coreSubgates))
	for _, subgate := range coreSubgates {
		coreIDs[subgate.ID] = true
	}
	for _, registration := range registry.Registrations {
		if !coreIDs[registration.ProducerID] {
			return fmt.Errorf("evidence registration %q producer %q is absent from the core profile", registration.ID, registration.ProducerID)
		}
	}
	return nil
}

// SubgatesForProfile returns the canonical manifest subgates selected by one
// reviewed profile.
func (manifest Manifest) SubgatesForProfile(profileID ProfileID) ([]Subgate, error) {
	var selectedProfile *Profile
	for index := range manifest.Profiles {
		if manifest.Profiles[index].ID == profileID {
			selectedProfile = &manifest.Profiles[index]
			break
		}
	}
	if selectedProfile == nil {
		return nil, fmt.Errorf("soundness manifest has no profile %q", profileID)
	}
	subgatesByID := make(map[string]Subgate, len(manifest.Subgates))
	for _, subgate := range manifest.Subgates {
		subgatesByID[subgate.ID] = subgate
	}
	result := make([]Subgate, 0, len(selectedProfile.SubgateIDs))
	for _, subgateID := range selectedProfile.SubgateIDs {
		subgate, exists := subgatesByID[subgateID]
		if !exists {
			return nil, fmt.Errorf("soundness profile %q references unknown subgate %q", profileID, subgateID)
		}
		result = append(result, subgate)
	}
	return result, nil
}

// Validate verifies that a report is structurally strict and nonnegative.
// The owning subgate subsequently enforces exact populations and nonzero minima.
func (report Report) Validate() error {
	if report.FormatVersion != ReportFormatVersion {
		return fmt.Errorf("soundness report format_version = %d, want %d", report.FormatVersion, ReportFormatVersion)
	}
	if err := report.Binding.Validate(); err != nil {
		return fmt.Errorf("validate soundness report binding: %w", err)
	}
	if report.Status != StatusPassed {
		return fmt.Errorf("soundness report status = %q, want %q", report.Status, StatusPassed)
	}
	if len(report.Populations) == 0 {
		return errors.New("soundness report has no populations")
	}
	seen := make(map[string]bool, len(report.Populations))
	previousID := ""
	for index, population := range report.Populations {
		if err := validateIdentifier(fmt.Sprintf("soundness report populations[%d].id", index), population.ID); err != nil {
			return err
		}
		if population.Count < 0 {
			return fmt.Errorf("soundness report populations[%d].count = %d, want non-negative", index, population.Count)
		}
		if seen[population.ID] {
			return fmt.Errorf("soundness report contains duplicate population id %q", population.ID)
		}
		seen[population.ID] = true
		if previousID != "" && population.ID < previousID {
			return fmt.Errorf("soundness report populations must use canonical id order: %q precedes %q", population.ID, previousID)
		}
		previousID = population.ID
	}
	return nil
}

// Validate verifies the retained report's identities, canonical ordering, and
// observation-to-command bindings.
func (report RunReport) Validate() error {
	if report.FormatVersion != RunReportFormatVersion {
		return fmt.Errorf("soundness run report format_version = %d, want %d", report.FormatVersion, RunReportFormatVersion)
	}
	if report.Profile != ProfileCore && report.Profile != ProfileComplete {
		return fmt.Errorf(
			"soundness run report profile = %q, want %q or %q",
			report.Profile,
			ProfileCore,
			ProfileComplete,
		)
	}
	if err := validateIdentifier("soundness run report run_id", report.RunID); err != nil {
		return err
	}
	if err := soundnessevidence.ValidateDigest("soundness run report workspace_digest", report.WorkspaceDigest); err != nil {
		return fmt.Errorf("validate soundness run report workspace digest: %w", err)
	}
	if err := soundnessevidence.ValidateDigest("soundness run report manifest_digest", report.ManifestDigest); err != nil {
		return fmt.Errorf("validate soundness run report manifest digest: %w", err)
	}
	if len(report.Subgates) == 0 {
		return errors.New("soundness run report has no subgates")
	}
	commandDigests := make(map[string]string, len(report.Subgates))
	previousSubgateID := ""
	for index, subgate := range report.Subgates {
		if err := subgate.validate(index); err != nil {
			return err
		}
		if _, exists := commandDigests[subgate.ID]; exists {
			return fmt.Errorf("soundness run report contains duplicate subgate id %q", subgate.ID)
		}
		commandDigests[subgate.ID] = subgate.CommandDigest
		if previousSubgateID != "" && subgate.ID < previousSubgateID {
			return errors.New("soundness run report subgates must use canonical id order")
		}
		previousSubgateID = subgate.ID
	}
	seenRegistrations := make(map[string]bool, len(report.Observations))
	previousRegistrationID := ""
	for index, observation := range report.Observations {
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("soundness run report observations[%d]: %w", index, err)
		}
		if seenRegistrations[observation.RegistrationID] {
			return fmt.Errorf("soundness run report contains duplicate registration id %q", observation.RegistrationID)
		}
		seenRegistrations[observation.RegistrationID] = true
		if previousRegistrationID != "" && observation.RegistrationID < previousRegistrationID {
			return errors.New("soundness run report observations must use canonical registration order")
		}
		previousRegistrationID = observation.RegistrationID
		if observation.Binding.RunID != report.RunID ||
			observation.Binding.WorkspaceDigest != report.WorkspaceDigest ||
			observation.Binding.ManifestDigest != report.ManifestDigest {
			return fmt.Errorf("soundness run report observation %q has stale aggregate identity", observation.RegistrationID)
		}
		commandDigest, exists := commandDigests[observation.ProducerID]
		if !exists {
			return fmt.Errorf("soundness run report observation %q has unknown producer %q", observation.RegistrationID, observation.ProducerID)
		}
		if observation.Binding.CommandDigest != commandDigest {
			return fmt.Errorf("soundness run report observation %q has stale command identity", observation.RegistrationID)
		}
	}
	return nil
}

// ValidateReport verifies the exact fresh report and population contract for a
// completed subgate command.
func (subgate Subgate) ValidateReport(
	report Report,
	expectedBinding soundnessevidence.ObservationBinding,
) error {
	if err := report.Validate(); err != nil {
		return fmt.Errorf("soundness subgate %q report: %w", subgate.ID, err)
	}
	if report.Binding != expectedBinding {
		return fmt.Errorf("soundness subgate %q report has a stale or mismatched binding", subgate.ID)
	}
	if len(report.Populations) != len(subgate.RequiredPopulations) {
		return fmt.Errorf(
			"soundness subgate %q report population count = %d, want exactly %d",
			subgate.ID,
			len(report.Populations),
			len(subgate.RequiredPopulations),
		)
	}
	for index, requirement := range subgate.RequiredPopulations {
		population := report.Populations[index]
		if population.ID != requirement.ID {
			return fmt.Errorf(
				"soundness subgate %q report population[%d].id = %q, want %q",
				subgate.ID,
				index,
				population.ID,
				requirement.ID,
			)
		}
		if population.Count < requirement.Minimum {
			return fmt.Errorf(
				"soundness subgate %q population %q count = %d, want at least %d",
				subgate.ID,
				population.ID,
				population.Count,
				requirement.Minimum,
			)
		}
	}
	return nil
}

// ValidateRunReport cross-validates a retained aggregate report against the
// canonical manifest and semantic registry.
func ValidateRunReport(
	report RunReport,
	manifest Manifest,
	registry soundnessevidence.Registry,
) error {
	if err := manifest.Validate(registry); err != nil {
		return err
	}
	if err := report.Validate(); err != nil {
		return err
	}
	profileSubgates, err := manifest.SubgatesForProfile(report.Profile)
	if err != nil {
		return err
	}
	if len(report.Subgates) != len(profileSubgates) {
		return fmt.Errorf(
			"soundness run report subgate count = %d, want exactly %d",
			len(report.Subgates),
			len(profileSubgates),
		)
	}
	expectedBindings := make(map[string]soundnessevidence.ObservationBinding, len(profileSubgates))
	for index, subgate := range profileSubgates {
		result := report.Subgates[index]
		if result.ID != subgate.ID {
			return fmt.Errorf("soundness run report subgates[%d].id = %q, want %q", index, result.ID, subgate.ID)
		}
		expectedBinding, err := createSubgateBinding(
			report.RunID,
			report.WorkspaceDigest,
			report.ManifestDigest,
			subgate,
		)
		if err != nil {
			return err
		}
		if result.CommandDigest != expectedBinding.CommandDigest {
			return fmt.Errorf("soundness run report subgate %q has stale command digest", subgate.ID)
		}
		expectedBindings[subgate.ID] = expectedBinding
		syntheticReport := Report{
			FormatVersion: ReportFormatVersion,
			Binding:       expectedBinding,
			Status:        StatusPassed,
			Populations:   result.Populations,
		}
		if err := subgate.ValidateReport(syntheticReport, expectedBinding); err != nil {
			return err
		}
	}
	if err := soundnessevidence.ValidateObservations(registry, report.Observations, expectedBindings); err != nil {
		return fmt.Errorf("validate soundness run report semantic census: %w", err)
	}
	return nil
}

func (subgate Subgate) validate(index int) error {
	if err := validateIdentifier(fmt.Sprintf("soundness manifest subgates[%d].id", index), subgate.ID); err != nil {
		return err
	}
	if err := validateRepositoryPath(
		fmt.Sprintf("soundness manifest subgates[%d].working_directory", index),
		subgate.WorkingDirectory,
		true,
	); err != nil {
		return err
	}
	if len(subgate.Command) == 0 {
		return fmt.Errorf("soundness manifest subgates[%d].command is empty", index)
	}
	for argumentIndex, argument := range subgate.Command {
		if strings.TrimSpace(argument) == "" {
			return fmt.Errorf("soundness manifest subgates[%d].command[%d] is empty", index, argumentIndex)
		}
	}
	if subgate.TimeoutSeconds <= 0 || subgate.TimeoutSeconds > maximumTimeoutSeconds {
		return fmt.Errorf(
			"soundness manifest subgates[%d].timeout_seconds = %d, want 1..%d",
			index,
			subgate.TimeoutSeconds,
			maximumTimeoutSeconds,
		)
	}
	if err := validateReportFile(index, subgate.ReportFile); err != nil {
		return err
	}
	if err := validateCanonicalIdentifiers(
		fmt.Sprintf("soundness manifest subgates[%d].required_registration_ids", index),
		subgate.RequiredRegistrationIDs,
	); err != nil {
		return err
	}
	if len(subgate.RequiredPopulations) == 0 {
		return fmt.Errorf("soundness manifest subgates[%d].required_populations is empty", index)
	}
	seenPopulations := make(map[string]bool, len(subgate.RequiredPopulations))
	previousPopulationID := ""
	for populationIndex, population := range subgate.RequiredPopulations {
		if err := validateIdentifier(
			fmt.Sprintf("soundness manifest subgates[%d].required_populations[%d].id", index, populationIndex),
			population.ID,
		); err != nil {
			return err
		}
		if population.Minimum <= 0 {
			return fmt.Errorf(
				"soundness manifest subgates[%d].required_populations[%d].minimum = %d, want positive",
				index,
				populationIndex,
				population.Minimum,
			)
		}
		if seenPopulations[population.ID] {
			return fmt.Errorf("soundness manifest subgate %q contains duplicate population id %q", subgate.ID, population.ID)
		}
		seenPopulations[population.ID] = true
		if previousPopulationID != "" && population.ID < previousPopulationID {
			return fmt.Errorf("soundness manifest subgate %q populations must use canonical id order", subgate.ID)
		}
		previousPopulationID = population.ID
	}
	return nil
}

func (manifest Manifest) validateProfiles() error {
	if len(manifest.Profiles) != 2 ||
		manifest.Profiles[0].ID != ProfileCore ||
		manifest.Profiles[1].ID != ProfileComplete {
		return errors.New("soundness manifest profiles must be exactly core then complete")
	}
	allSubgateIDs := make([]string, 0, len(manifest.Subgates))
	for _, subgate := range manifest.Subgates {
		allSubgateIDs = append(allSubgateIDs, subgate.ID)
	}
	for index, profile := range manifest.Profiles {
		if len(profile.SubgateIDs) == 0 {
			return fmt.Errorf("soundness manifest profiles[%d].subgate_ids is empty", index)
		}
		if err := validateCanonicalIdentifiers(
			fmt.Sprintf("soundness manifest profiles[%d].subgate_ids", index),
			profile.SubgateIDs,
		); err != nil {
			return err
		}
		for _, subgateID := range profile.SubgateIDs {
			if !slices.Contains(allSubgateIDs, subgateID) {
				return fmt.Errorf("soundness profile %q references unknown subgate %q", profile.ID, subgateID)
			}
		}
	}
	completeIDs := manifest.Profiles[1].SubgateIDs
	if !slices.Equal(completeIDs, allSubgateIDs) {
		return errors.New("soundness complete profile must contain every subgate")
	}
	if !slices.Contains(completeIDs, cleanTreeFreshnessID) {
		return fmt.Errorf("soundness complete profile omits %q", cleanTreeFreshnessID)
	}
	wantCoreIDs := slices.DeleteFunc(slices.Clone(completeIDs), func(subgateID string) bool {
		return subgateID == cleanTreeFreshnessID
	})
	if !slices.Equal(manifest.Profiles[0].SubgateIDs, wantCoreIDs) {
		return fmt.Errorf("soundness core profile must contain every subgate except %q", cleanTreeFreshnessID)
	}
	return nil
}

func (result SubgateResult) validate(index int) error {
	if err := validateIdentifier(fmt.Sprintf("soundness run report subgates[%d].id", index), result.ID); err != nil {
		return err
	}
	if err := soundnessevidence.ValidateDigest(
		fmt.Sprintf("soundness run report subgates[%d].command_digest", index),
		result.CommandDigest,
	); err != nil {
		return fmt.Errorf("validate soundness run report subgate command digest: %w", err)
	}
	if err := soundnessevidence.ValidateDigest(
		fmt.Sprintf("soundness run report subgates[%d].report_digest", index),
		result.ReportDigest,
	); err != nil {
		return fmt.Errorf("validate soundness run report subgate report digest: %w", err)
	}
	if len(result.Populations) == 0 {
		return fmt.Errorf("soundness run report subgates[%d].populations is empty", index)
	}
	seen := make(map[string]bool, len(result.Populations))
	previousPopulationID := ""
	for populationIndex, population := range result.Populations {
		if err := validateIdentifier(
			fmt.Sprintf("soundness run report subgates[%d].populations[%d].id", index, populationIndex),
			population.ID,
		); err != nil {
			return err
		}
		if population.Count <= 0 {
			return fmt.Errorf(
				"soundness run report subgates[%d].populations[%d].count = %d, want positive",
				index,
				populationIndex,
				population.Count,
			)
		}
		if seen[population.ID] {
			return fmt.Errorf("soundness run report subgate %q has duplicate population %q", result.ID, population.ID)
		}
		seen[population.ID] = true
		if previousPopulationID != "" && population.ID < previousPopulationID {
			return fmt.Errorf("soundness run report subgate %q populations must use canonical id order", result.ID)
		}
		previousPopulationID = population.ID
	}
	return nil
}

func validateReportFile(index int, path string) error {
	if err := validateRepositoryPath(fmt.Sprintf("soundness manifest subgates[%d].report_file", index), path, false); err != nil {
		return err
	}
	if filepath.Base(filepath.FromSlash(path)) != filepath.FromSlash(path) {
		return fmt.Errorf("soundness manifest subgates[%d].report_file %q must be a file name", index, path)
	}
	if filepath.Ext(path) != ".json" {
		return fmt.Errorf("soundness manifest subgates[%d].report_file %q must use .json", index, path)
	}
	return nil
}

func validateRepositoryPath(name, path string, allowDot bool) error {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") {
		return fmt.Errorf("%s %q must be a repository-relative slash path", name, path)
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean != path || (!allowDot && path == ".") || path == ".." || strings.HasPrefix(path, "../") {
		return fmt.Errorf("%s %q is not a clean repository-relative path", name, path)
	}
	if path == ".git" || strings.HasPrefix(path, ".git/") {
		return fmt.Errorf("%s %q may not address git metadata", name, path)
	}
	return nil
}

func validateCanonicalIdentifiers(name string, values []string) error {
	seen := make(map[string]bool, len(values))
	for index, value := range values {
		if err := validateIdentifier(fmt.Sprintf("%s[%d]", name, index), value); err != nil {
			return err
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate %q", name, value)
		}
		seen[value] = true
	}
	if !slices.IsSorted(values) {
		return fmt.Errorf("%s must use canonical lexical order", name)
	}
	return nil
}

func validateIdentifier(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("%s %q has surrounding whitespace", name, value)
	}
	return nil
}
