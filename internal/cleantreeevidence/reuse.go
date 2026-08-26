// SPDX-License-Identifier: MPL-2.0

package cleantreeevidence

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/invowk/goplint/internal/soundnessgate"
)

// reusedAggregateReportPrefix opens the retained command log of a reused
// aggregate report. The log states reuse instead of claiming execution, so no
// reader of the record can mistake the entry for a fresh aggregate run.
const reusedAggregateReportPrefix = "reused caller-provided aggregate report "

// resolveReusedReportPath validates an opt-in reused-report selection. An empty
// selection keeps the default behavior of executing the planned aggregate
// command. A nonempty selection must name an absolute regular file outside the
// repository, so the reused report can never become part of the synthetic tree
// it is evidence about.
func resolveReusedReportPath(root, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("reused aggregate report path %q must be absolute", path)
	}
	absolute := filepath.Clean(path)
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return "", fmt.Errorf("relativize reused aggregate report path %q: %w", absolute, err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("reused aggregate report %q must be outside the repository %q", absolute, root)
	}
	if err := requireRegularFile(absolute); err != nil {
		return "", fmt.Errorf("inspect reused aggregate report: %w", err)
	}
	return absolute, nil
}

// loadReusedAggregateSelection performs every tree-independent reuse check
// before the plan starts executing. The planned aggregate command runs last, so
// deferring these checks to its position would make a caller's selection
// mistake surface only after every earlier command has burned its full runtime.
func loadReusedAggregateSelection(
	ctx context.Context,
	reportPath string,
	planned AggregateReportPlan,
) (soundnessgate.RunReport, soundnessgate.RunReportBinding, error) {
	report, err := soundnessgate.LoadRunReport(ctx, reportPath)
	if err != nil {
		return soundnessgate.RunReport{}, soundnessgate.RunReportBinding{}, fmt.Errorf(
			"load reused aggregate run report %q: %w",
			reportPath,
			err,
		)
	}
	binding, err := soundnessgate.LoadRunReportBinding(ctx, soundnessgate.RunReportBindingPath(reportPath))
	if err != nil {
		return soundnessgate.RunReport{}, soundnessgate.RunReportBinding{}, fmt.Errorf(
			"load companion binding for reused aggregate run report %q: %w; the producing run must retain one through "+
				"GOPLINT_SOUNDNESS_REPORT_PATH or -report",
			reportPath,
			err,
		)
	}
	if err := validateTreeIndependentBinding(binding, report, planned); err != nil {
		return soundnessgate.RunReport{}, soundnessgate.RunReportBinding{}, reusedBindingError(reportPath, err)
	}
	return report, binding, nil
}

// validateTreeIndependentBinding checks the report-to-binding linkage, the
// reviewed profile, and the producing toolchain. None of these depend on the
// synthetic tree, so all of them are decidable before the plan runs.
func validateTreeIndependentBinding(
	binding soundnessgate.RunReportBinding,
	report soundnessgate.RunReport,
	planned AggregateReportPlan,
) error {
	reportDigest, err := soundnessgate.CanonicalRunReportDigest(report)
	if err != nil {
		return fmt.Errorf("derive canonical digest of the reused report: %w", err)
	}
	if binding.ReportSHA256 != reportDigest {
		return fmt.Errorf("binding report digest %s, reused report %s", binding.ReportSHA256, reportDigest)
	}
	if binding.Profile != planned.Profile {
		return fmt.Errorf("binding profile %q, expected %q", binding.Profile, planned.Profile)
	}
	if binding.WorkspaceDigest != report.WorkspaceDigest {
		return fmt.Errorf(
			"binding workspace digest %s, reused report %s",
			binding.WorkspaceDigest,
			report.WorkspaceDigest,
		)
	}
	if binding.ManifestDigest != report.ManifestDigest {
		return fmt.Errorf(
			"binding manifest digest %s, reused report %s",
			binding.ManifestDigest,
			report.ManifestDigest,
		)
	}
	toolchain, err := soundnessgate.CurrentToolchainBinding()
	if err != nil {
		return fmt.Errorf("recompute current toolchain identity: %w", err)
	}
	if binding.Toolchain != toolchain {
		return fmt.Errorf(
			"producing toolchain %s (%s %s/%s), current %s (%s %s/%s)",
			binding.Toolchain.Digest,
			binding.Toolchain.GoVersion,
			binding.Toolchain.GOOS,
			binding.Toolchain.GOARCH,
			toolchain.Digest,
			toolchain.GoVersion,
			toolchain.GOOS,
			toolchain.GOARCH,
		)
	}
	return nil
}

func reusedBindingError(reportPath string, err error) error {
	return fmt.Errorf(
		"companion binding %q does not bind the reused report to this tree and toolchain: %w",
		soundnessgate.RunReportBindingPath(reportPath),
		err,
	)
}

// reuseAggregateReport admits a caller-produced aggregate run report in place
// of executing the planned aggregate command.
//
// Admission binds the report to the exact synthetic tree through
// validateAggregateReport — the same admission check the default path applies
// to a report it just produced — and additionally binds the producing run
// through the companion binding soundness-gate writes beside a retained report:
// canonical report bytes, profile, workspace, manifest, registry byte digest,
// and the producing Go toolchain, which must equal this process's toolchain.
//
// What this does NOT establish: a RunReport is a self-attested census with no
// per-subgate execution status (ValidateRunReport synthesizes the passed
// status), no verifiable per-subgate report_digest artifact, and no
// timestamps; the companion binding is written by the same producer, so it
// detects replay across trees, manifests, registries, profiles, and
// toolchains, not fabrication. That residual is why reuse records a distinct
// provenance kind that verification refuses unless the caller explicitly opts
// in.
//
// Any failure is returned as-is: falling back to execution would hide the
// caller's mistake behind the full aggregate cost.
func reuseAggregateReport(
	ctx context.Context,
	worktree string,
	reportPath string,
	report soundnessgate.RunReport,
	binding soundnessgate.RunReportBinding,
	planned AggregateReportPlan,
	command CommandPlan,
) (CommandOutcome, AggregateReportIdentity, error) {
	started := time.Now()
	identity, err := validateAggregateReport(ctx, worktree, planned, report, "")
	if err != nil {
		return CommandOutcome{}, AggregateReportIdentity{}, fmt.Errorf(
			"reused aggregate run report %q is not a proof for the recorded synthetic tree: %w",
			reportPath,
			err,
		)
	}
	if err := validateSyntheticTreeBinding(binding, identity); err != nil {
		return CommandOutcome{}, AggregateReportIdentity{}, reusedBindingError(reportPath, err)
	}
	// The retained log is derived only from verified identities, never from the
	// caller's local report path, so the record stays reproducible.
	log := fmt.Sprintf(
		"%s%s\nverified profile %s against synthetic workspace digest %s, manifest %s, and registry %s\n"+
			"bound to producing plan %s and toolchain %s (%s %s/%s)\n",
		reusedAggregateReportPrefix,
		identity.SHA256,
		report.Profile,
		report.WorkspaceDigest,
		identity.ManifestSHA256,
		identity.RegistrySHA256,
		binding.PlanID,
		binding.Toolchain.Digest,
		binding.Toolchain.GoVersion,
		binding.Toolchain.GOOS,
		binding.Toolchain.GOARCH,
	)
	return CommandOutcome{
		Name:         command.Name,
		Directory:    command.Directory,
		Args:         command.Args,
		VectorSHA256: commandVectorDigest(command.Directory, command.Args),
		DurationMS:   time.Since(started).Milliseconds(),
		Log:          log,
		LogSHA256:    digestBytes([]byte(log)),
		Passed:       true,
	}, identity, nil
}

// validateSyntheticTreeBinding requires the companion binding to agree with the
// manifest and registry byte digests recomputed from the synthetic tree, and
// with the canonical report digest that admission just derived. The plan
// identity and resource budget are carried, not recomputed: the plan digest
// covers the machine-dependent resource budget, so it is deliberately not
// reproducible across runs.
func validateSyntheticTreeBinding(
	binding soundnessgate.RunReportBinding,
	identity AggregateReportIdentity,
) error {
	if binding.ReportSHA256 != identity.SHA256 {
		return fmt.Errorf("binding report digest %s, reused report %s", binding.ReportSHA256, identity.SHA256)
	}
	if binding.ManifestDigest != identity.ManifestSHA256 {
		return fmt.Errorf(
			"binding manifest digest %s, synthetic tree %s",
			binding.ManifestDigest,
			identity.ManifestSHA256,
		)
	}
	if binding.RegistryDigest != identity.RegistrySHA256 {
		return fmt.Errorf(
			"binding registry digest %s, synthetic tree %s",
			binding.RegistryDigest,
			identity.RegistrySHA256,
		)
	}
	return nil
}
