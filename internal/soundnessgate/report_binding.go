// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/invowk/goplint/internal/soundnessevidence"
)

const (
	// RunReportBindingFormatVersion is the supported companion binding format.
	RunReportBindingFormatVersion = 1

	// runReportBindingSuffix names the companion binding written beside a
	// retained aggregate report.
	runReportBindingSuffix = ".binding.json"
)

// RunReportBinding binds one retained aggregate report to the immutable plan
// and Go toolchain that produced it. A RunReport is a self-attested census: it
// carries no toolchain, plan, resource, or timestamp identity, so a consumer
// that did not execute the producer cannot otherwise tell which toolchain and
// plan produced the bytes it holds. This companion record supplies exactly that
// linkage. It is not an authenticity proof: the same writer produces both
// files, so it detects replay across toolchains, manifests, registries, and
// profiles — not fabrication by whoever writes them.
type RunReportBinding struct {
	FormatVersion   int              `json:"format_version"`
	ReportSHA256    string           `json:"report_sha256"`
	PlanID          string           `json:"plan_id"`
	Profile         ProfileID        `json:"profile"`
	WorkspaceDigest string           `json:"workspace_digest"`
	ManifestDigest  string           `json:"manifest_digest"`
	RegistryDigest  string           `json:"registry_digest"`
	Toolchain       ToolchainBinding `json:"toolchain"`
	Resources       ResourceBudget   `json:"resources"`
}

// DeriveRunReportBinding derives the companion binding for one retained report
// from the immutable plan whose execution produced it. Like every other
// artifact type in this package, RunReportBinding is a serialized JSON record
// with exported members rather than an encapsulated value object, so this is a
// derivation function and not a value-object constructor.
func DeriveRunReportBinding(plan ExecutionPlan, report RunReport) (RunReportBinding, error) {
	if plan.Profile != report.Profile || plan.Workspace.Digest != report.WorkspaceDigest ||
		plan.Manifest.Digest != report.ManifestDigest {
		return RunReportBinding{}, errors.New("aggregate report identities do not match the plan that produced it")
	}
	digest, err := CanonicalRunReportDigest(report)
	if err != nil {
		return RunReportBinding{}, err
	}
	binding := RunReportBinding{
		FormatVersion:   RunReportBindingFormatVersion,
		ReportSHA256:    digest,
		PlanID:          plan.PlanID,
		Profile:         report.Profile,
		WorkspaceDigest: report.WorkspaceDigest,
		ManifestDigest:  report.ManifestDigest,
		RegistryDigest:  plan.Registry.Digest,
		Toolchain:       plan.Toolchain,
		Resources:       plan.Resources,
	}
	if err := binding.Validate(); err != nil {
		return RunReportBinding{}, err
	}
	return binding, nil
}

// Validate rejects structurally incomplete or unknown-profile bindings.
func (binding RunReportBinding) Validate() error {
	if binding.FormatVersion != RunReportBindingFormatVersion {
		return fmt.Errorf(
			"aggregate report binding format_version = %d, want %d",
			binding.FormatVersion,
			RunReportBindingFormatVersion,
		)
	}
	if !isKnownProfile(binding.Profile) {
		return fmt.Errorf("aggregate report binding profile = %q, want a reviewed profile", binding.Profile)
	}
	digests := []struct {
		name  string
		value string
	}{
		{name: "report_sha256", value: binding.ReportSHA256},
		{name: "plan_id", value: binding.PlanID},
		{name: "workspace_digest", value: binding.WorkspaceDigest},
		{name: "manifest_digest", value: binding.ManifestDigest},
		{name: "registry_digest", value: binding.RegistryDigest},
		{name: "toolchain.digest", value: binding.Toolchain.Digest},
	}
	for _, digest := range digests {
		if err := soundnessevidence.ValidateDigest("aggregate report binding "+digest.name, digest.value); err != nil {
			return fmt.Errorf("validate aggregate report binding %q: %w", digest.name, err)
		}
	}
	if binding.Toolchain.GoVersion == "" || binding.Toolchain.GOOS == "" || binding.Toolchain.GOARCH == "" {
		return errors.New("aggregate report binding toolchain is incomplete")
	}
	expectedToolchainDigest, err := toolchainDigest(binding.Toolchain)
	if err != nil {
		return err
	}
	if binding.Toolchain.Digest != expectedToolchainDigest {
		return errors.New("aggregate report binding toolchain digest does not match its own toolchain identity")
	}
	if err := binding.Resources.validate(); err != nil {
		return fmt.Errorf("validate aggregate report binding resources: %w", err)
	}
	return nil
}

// CanonicalRunReportDigest returns the canonical byte identity of a retained
// aggregate report. Producers and consumers must derive the report digest only
// through this function so the two sides cannot drift.
func CanonicalRunReportDigest(report RunReport) (string, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("encode canonical aggregate report identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

// CurrentToolchainBinding returns the Go toolchain identity of this process.
func CurrentToolchainBinding() (ToolchainBinding, error) {
	return currentToolchainBinding()
}

// ToolchainBindingDigest returns the canonical identity digest of one Go
// toolchain and target platform, so a caller can build a self-consistent
// binding without duplicating the digest payload.
func ToolchainBindingDigest(binding ToolchainBinding) (string, error) {
	return toolchainDigest(binding)
}

// RunReportBindingPath returns the companion binding path for a report path.
func RunReportBindingPath(reportPath string) string {
	return reportPath + runReportBindingSuffix
}

// PublishRunReportBinding exclusively writes the companion binding beside one
// retained aggregate report.
func PublishRunReportBinding(ctx context.Context, reportPath string, binding RunReportBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if err := writeExclusiveJSON(ctx, RunReportBindingPath(reportPath), binding); err != nil {
		return fmt.Errorf("publish aggregate report binding: %w", err)
	}
	return nil
}

// LoadRunReportBinding strictly decodes and validates one companion binding.
func LoadRunReportBinding(ctx context.Context, path string) (RunReportBinding, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return RunReportBinding{}, fmt.Errorf("load aggregate report binding %s: %w", path, err)
	}
	var binding RunReportBinding
	if err := decodeStrictJSON(data, &binding); err != nil {
		return RunReportBinding{}, fmt.Errorf("decode aggregate report binding %s: %w", path, err)
	}
	if err := binding.Validate(); err != nil {
		return RunReportBinding{}, fmt.Errorf("validate aggregate report binding %s: %w", path, err)
	}
	return binding, nil
}
