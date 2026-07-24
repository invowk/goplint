// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

const (
	// ExecutionPlanFormatVersion is the supported immutable plan format.
	ExecutionPlanFormatVersion = 1

	maximumPlanWorkers = 1024
)

type (
	// ExecutionPlan binds every selected work unit and expected report to one
	// exact workspace, manifest, registry, toolchain, and resource policy.
	ExecutionPlan struct {
		FormatVersion   int                         `json:"format_version"`
		PlanID          string                      `json:"plan_id"`
		Profile         ProfileID                   `json:"profile"`
		Workspace       WorkspaceBinding            `json:"workspace"`
		Manifest        ArtifactBinding             `json:"manifest"`
		Registry        ArtifactBinding             `json:"registry"`
		Toolchain       ToolchainBinding            `json:"toolchain"`
		Resources       ResourceBudget              `json:"resources"`
		Commands        []PlanCommandBinding        `json:"commands"`
		Dependencies    []PlanDependencyBinding     `json:"dependencies"`
		Censuses        []PlanCensusBinding         `json:"censuses"`
		Shards          []PlanShardBinding          `json:"shards"`
		ExpectedReports []PlanExpectedReportBinding `json:"expected_reports"`
	}

	// WorkspaceBinding identifies the exact repository tree used by a plan.
	WorkspaceBinding struct {
		Root   string `json:"root"`
		Digest string `json:"digest"`
	}

	// ArtifactBinding identifies one reviewed repository-relative input.
	ArtifactBinding struct {
		Path   string `json:"path"`
		Digest string `json:"digest"`
	}

	// ToolchainBinding identifies the Go toolchain and target platform.
	ToolchainBinding struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
		Digest    string `json:"digest"`
	}

	// ResourceBudget records the total deterministic scheduler budget.
	ResourceBudget struct {
		CPUUnits        int    `json:"cpu_units"`
		MemoryBytes     int64  `json:"memory_bytes"`
		MaximumWorkers  int    `json:"maximum_workers"`
		RunnerClass     string `json:"runner_class"`
		SerialReference bool   `json:"serial_reference"`
	}

	// PlanCommandBinding binds one work unit to an exact command and executable.
	PlanCommandBinding struct {
		WorkUnitID        string              `json:"work_unit_id"`
		SubgateID         string              `json:"subgate_id"`
		WorkingDirectory  string              `json:"working_directory"`
		Command           []string            `json:"command"`
		CommandDigest     string              `json:"command_digest"`
		BinaryDigest      string              `json:"binary_digest"`
		ReservedResources ResourceReservation `json:"reserved_resources"`
		ExclusivityGroups []string            `json:"exclusivity_groups"`
		Distributable     bool                `json:"distributable"`
		TimeoutSeconds    int                 `json:"timeout_seconds"`
	}

	// PlanDependencyBinding records dependency-ready scheduling edges.
	PlanDependencyBinding struct {
		WorkUnitID string   `json:"work_unit_id"`
		Requires   []string `json:"requires"`
	}

	// PlanCensusBinding identifies one immutable complete member population.
	PlanCensusBinding struct {
		ID        string   `json:"id"`
		Kind      string   `json:"kind"`
		MemberIDs []string `json:"member_ids"`
		Digest    string   `json:"digest"`
	}

	// PlanShardBinding assigns one non-overlapping census subset to a work unit.
	PlanShardBinding struct {
		ID             string   `json:"id"`
		WorkUnitID     string   `json:"work_unit_id"`
		CensusID       string   `json:"census_id"`
		Mode           string   `json:"mode"`
		Iteration      int      `json:"iteration"`
		MemberIDs      []string `json:"member_ids"`
		TotalWeight    int64    `json:"total_weight"`
		TimeoutSeconds int      `json:"timeout_seconds"`
	}

	// PlanExpectedReportBinding defines one exact required terminal result.
	PlanExpectedReportBinding struct {
		WorkUnitID              string                  `json:"work_unit_id"`
		SubgateID               string                  `json:"subgate_id"`
		ReportFile              string                  `json:"report_file"`
		CommandDigest           string                  `json:"command_digest"`
		RequiredRegistrationIDs []string                `json:"required_registration_ids"`
		RequiredPopulations     []PopulationRequirement `json:"required_populations"`
	}
)

// Validate verifies the immutable plan's identity, canonical structure,
// resource bounds, graph, census, shard, and expected-report bindings.
func (plan ExecutionPlan) Validate() error {
	if plan.FormatVersion != ExecutionPlanFormatVersion {
		return fmt.Errorf("soundness execution plan format_version = %d, want %d", plan.FormatVersion, ExecutionPlanFormatVersion)
	}
	if err := soundnessevidence.ValidateDigest("soundness execution plan plan_id", plan.PlanID); err != nil {
		return fmt.Errorf("validate soundness execution plan id: %w", err)
	}
	computedID, err := plan.CalculateID()
	if err != nil {
		return err
	}
	if plan.PlanID != computedID {
		return fmt.Errorf("soundness execution plan id = %q, want %q from canonical content", plan.PlanID, computedID)
	}
	if !isKnownProfile(plan.Profile) {
		return fmt.Errorf("soundness execution plan profile = %q, want a reviewed profile", plan.Profile)
	}
	if err := plan.Workspace.validate(); err != nil {
		return err
	}
	if err := plan.Manifest.validate("manifest"); err != nil {
		return err
	}
	if err := plan.Registry.validate("registry"); err != nil {
		return err
	}
	if err := plan.Toolchain.validate(); err != nil {
		return err
	}
	if err := plan.Resources.validate(); err != nil {
		return err
	}
	commands, err := validatePlanCommands(plan.Commands, plan.Resources)
	if err != nil {
		return err
	}
	if err := validatePlanDependencies(plan.Dependencies, commands); err != nil {
		return err
	}
	censuses, err := validatePlanCensuses(plan.Censuses)
	if err != nil {
		return err
	}
	if err := validatePlanShards(plan.Shards, commands, censuses); err != nil {
		return err
	}
	if err := validatePlanExpectedReports(plan.ExpectedReports, commands); err != nil {
		return err
	}
	return nil
}

// CalculateID returns the digest of the complete plan with its self-identity
// field cleared. Callers assign this value before publishing the plan.
func (plan ExecutionPlan) CalculateID() (string, error) {
	plan.PlanID = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode soundness execution plan identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func (binding WorkspaceBinding) validate() error {
	if binding.Root != "." {
		return fmt.Errorf("soundness execution plan workspace root = %q, want repository root %q", binding.Root, ".")
	}
	if err := soundnessevidence.ValidateDigest("soundness execution plan workspace digest", binding.Digest); err != nil {
		return fmt.Errorf("validate soundness execution plan workspace digest: %w", err)
	}
	return nil
}

func (binding ArtifactBinding) validate(name string) error {
	if err := validateRepositoryPath("soundness execution plan "+name+" path", binding.Path, false); err != nil {
		return err
	}
	if err := soundnessevidence.ValidateDigest("soundness execution plan "+name+" digest", binding.Digest); err != nil {
		return fmt.Errorf("validate soundness execution plan %s digest: %w", name, err)
	}
	return nil
}

func (binding ToolchainBinding) validate() error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "go_version", value: binding.GoVersion},
		{name: "goos", value: binding.GOOS},
		{name: "goarch", value: binding.GOARCH},
	}
	for _, field := range fields {
		if err := validateIdentifier("soundness execution plan toolchain "+field.name, field.value); err != nil {
			return err
		}
	}
	if err := soundnessevidence.ValidateDigest("soundness execution plan toolchain digest", binding.Digest); err != nil {
		return fmt.Errorf("validate soundness execution plan toolchain digest: %w", err)
	}
	expectedDigest, err := calculateBindingDigest(struct {
		GoVersion string `json:"go_version"`
		GOOS      string `json:"goos"`
		GOARCH    string `json:"goarch"`
	}{GoVersion: binding.GoVersion, GOOS: binding.GOOS, GOARCH: binding.GOARCH})
	if err != nil {
		return fmt.Errorf("calculate soundness execution plan toolchain digest: %w", err)
	}
	if binding.Digest != expectedDigest {
		return errors.New("soundness execution plan toolchain digest does not match its identity fields")
	}
	return nil
}

func (budget ResourceBudget) validate() error {
	if budget.CPUUnits <= 0 || budget.MemoryBytes <= 0 || budget.MaximumWorkers <= 0 || budget.MaximumWorkers > maximumPlanWorkers {
		return errors.New("soundness execution plan has invalid resource budget")
	}
	return validateIdentifier("soundness execution plan runner_class", budget.RunnerClass)
}

func validatePlanCommands(
	commands []PlanCommandBinding,
	resources ResourceBudget,
) (map[string]PlanCommandBinding, error) {
	if len(commands) == 0 {
		return nil, errors.New("soundness execution plan has no commands")
	}
	byID := make(map[string]PlanCommandBinding, len(commands))
	previousID := ""
	for index, command := range commands {
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan commands[%d].work_unit_id", index), command.WorkUnitID); err != nil {
			return nil, err
		}
		if _, exists := byID[command.WorkUnitID]; exists {
			return nil, fmt.Errorf("soundness execution plan contains duplicate work unit %q", command.WorkUnitID)
		}
		if previousID != "" && command.WorkUnitID < previousID {
			return nil, errors.New("soundness execution plan commands must use canonical work-unit order")
		}
		previousID = command.WorkUnitID
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan commands[%d].subgate_id", index), command.SubgateID); err != nil {
			return nil, err
		}
		if err := validateRepositoryPath(fmt.Sprintf("soundness execution plan commands[%d].working_directory", index), command.WorkingDirectory, true); err != nil {
			return nil, err
		}
		if err := validateSafeCommandVector(index, command.Command); err != nil {
			return nil, err
		}
		if err := soundnessevidence.ValidateDigest(fmt.Sprintf("soundness execution plan commands[%d].command_digest", index), command.CommandDigest); err != nil {
			return nil, fmt.Errorf("validate soundness execution plan command digest: %w", err)
		}
		expectedCommandDigest, err := planCommandDigest(command)
		if err != nil {
			return nil, fmt.Errorf("calculate soundness execution plan command digest: %w", err)
		}
		if command.CommandDigest != expectedCommandDigest {
			return nil, fmt.Errorf("soundness execution plan command %q digest does not match its identity fields", command.WorkUnitID)
		}
		if err := soundnessevidence.ValidateDigest(fmt.Sprintf("soundness execution plan commands[%d].binary_digest", index), command.BinaryDigest); err != nil {
			return nil, fmt.Errorf("validate soundness execution plan binary digest: %w", err)
		}
		if command.ReservedResources.CPUUnits <= 0 || command.ReservedResources.MemoryBytes <= 0 ||
			command.ReservedResources.WorkerSlots <= 0 {
			return nil, fmt.Errorf("soundness execution plan command %q has invalid reserved resources", command.WorkUnitID)
		}
		if command.ReservedResources.CPUUnits > resources.CPUUnits ||
			command.ReservedResources.MemoryBytes > resources.MemoryBytes ||
			command.ReservedResources.WorkerSlots > resources.MaximumWorkers {
			return nil, fmt.Errorf("soundness execution plan command %q exceeds the plan resource budget", command.WorkUnitID)
		}
		if err := validateCanonicalIdentifiers(
			fmt.Sprintf("soundness execution plan commands[%d].exclusivity_groups", index),
			command.ExclusivityGroups,
		); err != nil {
			return nil, err
		}
		if command.TimeoutSeconds <= 0 || command.TimeoutSeconds > maximumTimeoutSeconds {
			return nil, fmt.Errorf("soundness execution plan command %q has invalid timeout", command.WorkUnitID)
		}
		byID[command.WorkUnitID] = command
	}
	return byID, nil
}

func validateSafeCommandVector(index int, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("soundness execution plan commands[%d].command is empty", index)
	}
	for argumentIndex, argument := range command {
		if strings.TrimSpace(argument) == "" || strings.ContainsAny(argument, "\x00\r\n") {
			return fmt.Errorf("soundness execution plan commands[%d].command[%d] is unsafe", index, argumentIndex)
		}
	}
	executable := command[0]
	if strings.HasPrefix(executable, "/") || strings.Contains(executable, "\\") ||
		executable == ".." || strings.HasPrefix(executable, "../") || strings.Contains(executable, "/../") {
		return fmt.Errorf("soundness execution plan commands[%d] executable %q is unsafe", index, executable)
	}
	if len(command) > 1 && isShellEvaluator(executable, command[1]) {
		return fmt.Errorf("soundness execution plan commands[%d] uses an unsafe shell evaluation vector", index)
	}
	return nil
}

func isShellEvaluator(executable, firstArgument string) bool {
	executable = strings.ToLower(executable)
	firstArgument = strings.ToLower(firstArgument)
	return (executable == "sh" || executable == "bash" || executable == "zsh") && firstArgument == "-c" ||
		(executable == "pwsh" || executable == "powershell") && (firstArgument == "-command" || firstArgument == "-c")
}

func validatePlanDependencies(dependencies []PlanDependencyBinding, commands map[string]PlanCommandBinding) error {
	if len(dependencies) != len(commands) {
		return fmt.Errorf("soundness execution plan dependency count = %d, want exactly %d", len(dependencies), len(commands))
	}
	byID := make(map[string][]string, len(dependencies))
	previousID := ""
	for index, dependency := range dependencies {
		if _, exists := commands[dependency.WorkUnitID]; !exists {
			return fmt.Errorf("soundness execution plan dependency references unknown work unit %q", dependency.WorkUnitID)
		}
		if _, exists := byID[dependency.WorkUnitID]; exists {
			return fmt.Errorf("soundness execution plan contains duplicate dependency binding %q", dependency.WorkUnitID)
		}
		if previousID != "" && dependency.WorkUnitID < previousID {
			return errors.New("soundness execution plan dependencies must use canonical work-unit order")
		}
		previousID = dependency.WorkUnitID
		if err := validateCanonicalIdentifiers(fmt.Sprintf("soundness execution plan dependencies[%d].requires", index), dependency.Requires); err != nil {
			return err
		}
		for _, requiredID := range dependency.Requires {
			if requiredID == dependency.WorkUnitID {
				return fmt.Errorf("soundness execution plan work unit %q depends on itself", dependency.WorkUnitID)
			}
			if _, exists := commands[requiredID]; !exists {
				return fmt.Errorf("soundness execution plan work unit %q depends on unknown work unit %q", dependency.WorkUnitID, requiredID)
			}
		}
		byID[dependency.WorkUnitID] = dependency.Requires
	}
	return validateDependencyCycles(byID)
}

func validateDependencyCycles(dependencies map[string][]string) error {
	visiting := make(map[string]bool, len(dependencies))
	visited := make(map[string]bool, len(dependencies))
	var visit func(string) error
	visit = func(workUnitID string) error {
		if visiting[workUnitID] {
			return fmt.Errorf("soundness execution plan dependency cycle includes %q", workUnitID)
		}
		if visited[workUnitID] {
			return nil
		}
		visiting[workUnitID] = true
		for _, dependencyID := range dependencies[workUnitID] {
			if err := visit(dependencyID); err != nil {
				return err
			}
		}
		delete(visiting, workUnitID)
		visited[workUnitID] = true
		return nil
	}
	for workUnitID := range dependencies {
		if err := visit(workUnitID); err != nil {
			return err
		}
	}
	return nil
}

func validatePlanCensuses(censuses []PlanCensusBinding) (map[string]PlanCensusBinding, error) {
	if len(censuses) == 0 {
		return nil, errors.New("soundness execution plan has no censuses")
	}
	byID := make(map[string]PlanCensusBinding, len(censuses))
	previousID := ""
	for index, census := range censuses {
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan censuses[%d].id", index), census.ID); err != nil {
			return nil, err
		}
		if _, exists := byID[census.ID]; exists {
			return nil, fmt.Errorf("soundness execution plan contains duplicate census %q", census.ID)
		}
		if previousID != "" && census.ID < previousID {
			return nil, errors.New("soundness execution plan censuses must use canonical id order")
		}
		previousID = census.ID
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan censuses[%d].kind", index), census.Kind); err != nil {
			return nil, err
		}
		if len(census.MemberIDs) == 0 {
			return nil, fmt.Errorf("soundness execution plan census %q has no members", census.ID)
		}
		if err := validateCanonicalIdentifiers(fmt.Sprintf("soundness execution plan censuses[%d].member_ids", index), census.MemberIDs); err != nil {
			return nil, err
		}
		if err := soundnessevidence.ValidateDigest(fmt.Sprintf("soundness execution plan censuses[%d].digest", index), census.Digest); err != nil {
			return nil, fmt.Errorf("validate soundness execution plan census digest: %w", err)
		}
		expectedDigest, err := calculateBindingDigest(struct {
			ID        string   `json:"id"`
			Kind      string   `json:"kind"`
			MemberIDs []string `json:"member_ids"`
		}{ID: census.ID, Kind: census.Kind, MemberIDs: census.MemberIDs})
		if err != nil {
			return nil, fmt.Errorf("calculate soundness execution plan census digest: %w", err)
		}
		if census.Digest != expectedDigest {
			return nil, fmt.Errorf("soundness execution plan census %q digest does not match its members", census.ID)
		}
		byID[census.ID] = census
	}
	return byID, nil
}

func validatePlanShards(shards []PlanShardBinding, commands map[string]PlanCommandBinding, censuses map[string]PlanCensusBinding) error {
	type populationKey struct {
		censusID  string
		mode      string
		iteration int
	}
	previousID := ""
	seenIDs := make(map[string]bool, len(shards))
	seenMembers := make(map[populationKey]map[string]bool, len(censuses))
	seenCensuses := make(map[string]bool, len(censuses))
	for index, shard := range shards {
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan shards[%d].id", index), shard.ID); err != nil {
			return err
		}
		if seenIDs[shard.ID] {
			return fmt.Errorf("soundness execution plan contains duplicate shard %q", shard.ID)
		}
		seenIDs[shard.ID] = true
		if previousID != "" && shard.ID < previousID {
			return errors.New("soundness execution plan shards must use canonical id order")
		}
		previousID = shard.ID
		if _, exists := commands[shard.WorkUnitID]; !exists {
			return fmt.Errorf("soundness execution plan shard %q references unknown work unit %q", shard.ID, shard.WorkUnitID)
		}
		census, exists := censuses[shard.CensusID]
		if !exists {
			return fmt.Errorf("soundness execution plan shard %q references unknown census %q", shard.ID, shard.CensusID)
		}
		if err := validateIdentifier(fmt.Sprintf("soundness execution plan shards[%d].mode", index), shard.Mode); err != nil {
			return err
		}
		if shard.Iteration <= 0 || shard.TotalWeight <= 0 || shard.TimeoutSeconds <= 0 || shard.TimeoutSeconds > maximumTimeoutSeconds {
			return fmt.Errorf("soundness execution plan shard %q has invalid execution bounds", shard.ID)
		}
		if len(shard.MemberIDs) == 0 {
			return fmt.Errorf("soundness execution plan shard %q has no members", shard.ID)
		}
		if err := validateCanonicalIdentifiers(fmt.Sprintf("soundness execution plan shards[%d].member_ids", index), shard.MemberIDs); err != nil {
			return err
		}
		key := populationKey{censusID: shard.CensusID, mode: shard.Mode, iteration: shard.Iteration}
		members := seenMembers[key]
		if members == nil {
			members = make(map[string]bool, len(census.MemberIDs))
			seenMembers[key] = members
		}
		seenCensuses[shard.CensusID] = true
		for _, memberID := range shard.MemberIDs {
			if !slices.Contains(census.MemberIDs, memberID) {
				return fmt.Errorf("soundness execution plan shard %q contains unknown census member %q", shard.ID, memberID)
			}
			if members[memberID] {
				return fmt.Errorf("soundness execution plan census %q mode %q iteration %d member %q overlaps shards", shard.CensusID, shard.Mode, shard.Iteration, memberID)
			}
			members[memberID] = true
		}
	}
	for key, members := range seenMembers {
		if len(members) != len(censuses[key.censusID].MemberIDs) {
			return fmt.Errorf("soundness execution plan shards incompletely cover census %q mode %q iteration %d", key.censusID, key.mode, key.iteration)
		}
	}
	for censusID := range censuses {
		if !seenCensuses[censusID] {
			return fmt.Errorf("soundness execution plan shards incompletely cover census %q", censusID)
		}
	}
	return nil
}

func validatePlanExpectedReports(reports []PlanExpectedReportBinding, commands map[string]PlanCommandBinding) error {
	if len(reports) != len(commands) {
		return fmt.Errorf("soundness execution plan expected report count = %d, want exactly %d", len(reports), len(commands))
	}
	seen := make(map[string]bool, len(reports))
	previousID := ""
	for index, report := range reports {
		command, exists := commands[report.WorkUnitID]
		if !exists {
			return fmt.Errorf("soundness execution plan expected report references unknown work unit %q", report.WorkUnitID)
		}
		if seen[report.WorkUnitID] {
			return fmt.Errorf("soundness execution plan contains duplicate expected report %q", report.WorkUnitID)
		}
		seen[report.WorkUnitID] = true
		if previousID != "" && report.WorkUnitID < previousID {
			return errors.New("soundness execution plan expected reports must use canonical work-unit order")
		}
		previousID = report.WorkUnitID
		if report.SubgateID != command.SubgateID || report.CommandDigest != command.CommandDigest {
			return fmt.Errorf("soundness execution plan expected report %q has mismatched command binding", report.WorkUnitID)
		}
		if err := validateReportFile(index, report.ReportFile); err != nil {
			return err
		}
		if err := validateCanonicalIdentifiers(fmt.Sprintf("soundness execution plan expected_reports[%d].required_registration_ids", index), report.RequiredRegistrationIDs); err != nil {
			return err
		}
		if len(report.RequiredPopulations) == 0 {
			return fmt.Errorf("soundness execution plan expected report %q has no required populations", report.WorkUnitID)
		}
		seenPopulations := make(map[string]bool, len(report.RequiredPopulations))
		previousPopulationID := ""
		for populationIndex, population := range report.RequiredPopulations {
			if err := validateIdentifier(
				fmt.Sprintf("soundness execution plan expected_reports[%d].required_populations[%d].id", index, populationIndex),
				population.ID,
			); err != nil {
				return err
			}
			if population.Minimum <= 0 {
				return fmt.Errorf("soundness execution plan expected report %q population %q has non-positive minimum", report.WorkUnitID, population.ID)
			}
			if seenPopulations[population.ID] {
				return fmt.Errorf("soundness execution plan expected report %q contains duplicate population %q", report.WorkUnitID, population.ID)
			}
			seenPopulations[population.ID] = true
			if previousPopulationID != "" && population.ID < previousPopulationID {
				return fmt.Errorf("soundness execution plan expected report %q populations must use canonical id order", report.WorkUnitID)
			}
			previousPopulationID = population.ID
		}
	}
	return nil
}

func calculateBindingDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode binding identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}
