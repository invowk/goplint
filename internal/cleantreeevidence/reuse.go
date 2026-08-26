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

// reuseAggregateReport admits a caller-produced aggregate run report in place
// of executing the planned aggregate command. Admission runs
// validateAggregateReport against the freshly materialized synthetic worktree —
// the same admission check the default path applies to a report it just
// produced — so an admitted report proves byte-equal profile, manifest,
// registry, and recomputed workspace identities and is therefore evidentially
// indistinguishable from a re-executed one. Any failure is returned as-is:
// falling back to execution would hide the caller's mistake behind the full
// aggregate cost.
func reuseAggregateReport(
	ctx context.Context,
	worktree string,
	reportPath string,
	planned AggregateReportPlan,
	command CommandPlan,
) (CommandOutcome, AggregateReportIdentity, error) {
	started := time.Now()
	report, err := soundnessgate.LoadRunReport(ctx, reportPath)
	if err != nil {
		return CommandOutcome{}, AggregateReportIdentity{}, fmt.Errorf(
			"load reused aggregate run report %q: %w",
			reportPath,
			err,
		)
	}
	identity, err := validateAggregateReport(ctx, worktree, planned, report, "")
	if err != nil {
		return CommandOutcome{}, AggregateReportIdentity{}, fmt.Errorf(
			"reused aggregate run report %q is not a proof for the recorded synthetic tree: %w",
			reportPath,
			err,
		)
	}
	// The retained log is derived only from verified identities, never from the
	// caller's local report path, so the record stays reproducible.
	log := fmt.Sprintf(
		"%s%s\nverified profile %s against synthetic workspace digest %s, manifest %s, and registry %s\n",
		reusedAggregateReportPrefix,
		identity.SHA256,
		report.Profile,
		report.WorkspaceDigest,
		identity.ManifestSHA256,
		identity.RegistrySHA256,
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
