// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

var causalMutationProperties = []string{
	"clean-control-passed",
	"declared-guard-selected",
	"exact-anchor-selected",
	"exact-transformation-applied",
	"intended-mismatch-observed",
	"mismatch-repeatable",
	"mutant-compiled",
	"post-control-passed",
	"source-restored",
}

func collectAggregateReport(
	ctx context.Context,
	root string,
	reportPath string,
	planned AggregateReportPlan,
) (AggregateReportIdentity, error) {
	report, err := soundnessgate.LoadRunReport(ctx, reportPath)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("load aggregate run report: %w", err)
	}
	identity, err := validateAggregateReport(ctx, root, planned, report)
	if err != nil {
		return AggregateReportIdentity{}, err
	}
	return identity, nil
}

func validateAggregateReport(
	ctx context.Context,
	root string,
	planned AggregateReportPlan,
	report soundnessgate.RunReport,
) (AggregateReportIdentity, error) {
	manifestPath := resolveFromRoot(root, planned.ManifestPath)
	manifestByteDigest, err := digestFile(manifestPath)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("digest aggregate manifest: %w", err)
	}
	manifest, manifestDigest, err := soundnessgate.LoadManifest(ctx, manifestPath)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("load aggregate manifest: %w", err)
	}
	if manifest.RegistryPath != planned.RegistryPath {
		return AggregateReportIdentity{}, fmt.Errorf(
			"aggregate manifest registry_path %q, expected %q",
			manifest.RegistryPath,
			planned.RegistryPath,
		)
	}
	if manifestDigest != manifestByteDigest {
		return AggregateReportIdentity{}, errors.New("aggregate manifest digest algorithm disagrees with clean-tree identity")
	}
	registryPath := resolveFromRoot(root, planned.RegistryPath)
	registry, err := soundnessevidence.LoadRegistry(ctx, registryPath)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("load aggregate evidence registry: %w", err)
	}
	if err := soundnessgate.ValidateRunReport(report, manifest, registry); err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("validate retained aggregate report: %w", err)
	}
	if report.Profile != planned.Profile {
		return AggregateReportIdentity{}, fmt.Errorf("aggregate report profile %q, expected %q", report.Profile, planned.Profile)
	}
	if report.ManifestDigest != manifestDigest {
		return AggregateReportIdentity{}, fmt.Errorf(
			"aggregate report manifest digest %s, current %s",
			report.ManifestDigest,
			manifestDigest,
		)
	}
	workspaceDigest, err := soundnessgate.WorkspaceDigest(ctx, root)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("compute aggregate workspace digest: %w", err)
	}
	if report.WorkspaceDigest != workspaceDigest {
		return AggregateReportIdentity{}, fmt.Errorf(
			"aggregate report workspace digest %s, current %s",
			report.WorkspaceDigest,
			workspaceDigest,
		)
	}
	registryDigest, err := digestFile(registryPath)
	if err != nil {
		return AggregateReportIdentity{}, fmt.Errorf("digest aggregate registry: %w", err)
	}
	reportDigest, err := digestJSON(report)
	if err != nil {
		return AggregateReportIdentity{}, err
	}
	return AggregateReportIdentity{
		OutputFile:     planned.OutputFile,
		SHA256:         reportDigest,
		ManifestPath:   planned.ManifestPath,
		ManifestSHA256: manifestDigest,
		RegistryPath:   planned.RegistryPath,
		RegistrySHA256: registryDigest,
		Report:         report,
	}, nil
}

func collectMutationProofs(plan Plan, report soundnessgate.RunReport) ([]MutationProof, error) {
	if len(plan.MutationProofs) == 0 {
		return []MutationProof{}, nil
	}
	observations := make(map[string]soundnessevidence.SemanticObservation, len(report.Observations))
	for _, observation := range report.Observations {
		observations[observation.RegistrationID] = observation
	}
	subgates := make(map[string]soundnessgate.SubgateResult, len(report.Subgates))
	for _, subgate := range report.Subgates {
		subgates[subgate.ID] = subgate
	}
	proofs := make([]MutationProof, 0, len(plan.MutationProofs))
	for _, planned := range plan.MutationProofs {
		observation, exists := observations[planned.Observation]
		if !exists {
			return nil, fmt.Errorf("mutation proof %q observation %q is missing", planned.Name, planned.Observation)
		}
		if observation.Layer != soundnessevidence.LayerMutation ||
			observation.Result.Outcome != soundnessevidence.OutcomeMutantKilled ||
			!slices.Equal(observation.Properties, causalMutationProperties) {
			return nil, fmt.Errorf("mutation proof %q lacks the exact causal observation contract", planned.Name)
		}
		subgate, exists := subgates[observation.ProducerID]
		if !exists {
			return nil, fmt.Errorf("mutation proof %q producer %q is missing", planned.Name, observation.ProducerID)
		}
		populations := make(map[string]int, len(subgate.Populations))
		for _, population := range subgate.Populations {
			populations[population.ID] = population.Count
		}
		mutants := populations["causal-mutants"]
		proof := MutationProof{
			Name:                 planned.Name,
			Observation:          planned.Observation,
			CleanControlPassed:   mutants > 0 && populations["clean-controls"] >= 2*mutants,
			MutantSelected:       mutants > 0 && populations["selected-guards"] >= mutants,
			IntendedMismatchSeen: mutants > 0 && populations["intended-mismatches"] >= mutants,
			Restored:             mutants > 0 && populations["restorations"] >= mutants,
			PostControlPassed:    mutants > 0 && populations["clean-controls"] >= 2*mutants,
		}
		if !proof.CleanControlPassed || !proof.MutantSelected || !proof.IntendedMismatchSeen || !proof.Restored || !proof.PostControlPassed {
			return nil, fmt.Errorf("mutation proof %q has incomplete causal populations", planned.Name)
		}
		proofs = append(proofs, proof)
	}
	return proofs, nil
}

func digestJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode canonical JSON identity: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("canonical JSON identity is empty")
	}
	return digestBytes(data), nil
}
