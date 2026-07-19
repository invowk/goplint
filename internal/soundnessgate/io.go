// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
)

// LoadManifest strictly decodes an aggregate manifest and returns the digest
// of the exact reviewed bytes. Registry ownership is validated separately.
func LoadManifest(ctx context.Context, path string) (Manifest, string, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("load soundness manifest %s: %w", path, err)
	}
	var manifest Manifest
	if err := decodeStrictJSON(data, &manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("decode soundness manifest %s: %w", path, err)
	}
	return manifest, soundnessevidence.DigestBytes(data), nil
}

// LoadReport strictly decodes and validates a subgate report.
func LoadReport(ctx context.Context, path string) (Report, error) {
	report, _, err := loadReportWithDigest(ctx, path)
	return report, err
}

// LoadRunReport strictly decodes and validates a retained aggregate report.
func LoadRunReport(ctx context.Context, path string) (RunReport, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return RunReport{}, fmt.Errorf("load soundness run report %s: %w", path, err)
	}
	var report RunReport
	if err := decodeStrictJSON(data, &report); err != nil {
		return RunReport{}, fmt.Errorf("decode soundness run report %s: %w", path, err)
	}
	if err := report.Validate(); err != nil {
		return RunReport{}, fmt.Errorf("validate soundness run report %s: %w", path, err)
	}
	return report, nil
}

// EmitReportFromEnvironment binds and exclusively publishes one subgate
// report. When no aggregate report path is present, it returns false so the
// producer remains usable in focused local runs.
func EmitReportFromEnvironment(ctx context.Context, populations []Population) (bool, error) {
	return emitReport(ctx, populations, os.LookupEnv)
}

// CommandDigest returns the canonical identity of an exact subgate command,
// including its producer id and working directory.
func CommandDigest(subgate Subgate) (string, error) {
	payload := struct {
		ID               string   `json:"id"`
		WorkingDirectory string   `json:"working_directory"`
		Command          []string `json:"command"`
	}{
		ID:               subgate.ID,
		WorkingDirectory: subgate.WorkingDirectory,
		Command:          subgate.Command,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode soundness command identity: %w", err)
	}
	return soundnessevidence.DigestBytes(data), nil
}

func emitReport(
	ctx context.Context,
	populations []Population,
	lookupEnv func(string) (string, bool),
) (bool, error) {
	path, enabled := lookupEnv(EnvSubgateReportPath)
	if !enabled {
		return false, nil
	}
	if strings.TrimSpace(path) == "" {
		return false, errors.New("aggregate soundness report path is empty")
	}
	binding, err := bindingFromLookup(lookupEnv)
	if err != nil {
		return false, err
	}
	report := Report{
		FormatVersion: ReportFormatVersion,
		Binding:       binding,
		Status:        StatusPassed,
		Populations:   populations,
	}
	if err := report.Validate(); err != nil {
		return false, fmt.Errorf("validate emitted soundness report: %w", err)
	}
	if err := writeExclusiveJSON(ctx, path, report); err != nil {
		return false, fmt.Errorf("publish soundness report: %w", err)
	}
	return true, nil
}

func bindingFromLookup(lookupEnv func(string) (string, bool)) (soundnessevidence.ObservationBinding, error) {
	keys := []string{
		soundnessevidence.EnvRunID,
		soundnessevidence.EnvWorkspaceDigest,
		soundnessevidence.EnvManifestDigest,
		soundnessevidence.EnvCommandDigest,
		soundnessevidence.EnvSubgateID,
	}
	values := make([]string, len(keys))
	for index, key := range keys {
		value, exists := lookupEnv(key)
		if !exists {
			return soundnessevidence.ObservationBinding{}, fmt.Errorf("aggregate evidence environment %s is unset", key)
		}
		values[index] = value
	}
	binding := soundnessevidence.ObservationBinding{
		RunID:           values[0],
		WorkspaceDigest: values[1],
		ManifestDigest:  values[2],
		CommandDigest:   values[3],
		SubgateID:       values[4],
	}
	if err := binding.Validate(); err != nil {
		return soundnessevidence.ObservationBinding{}, fmt.Errorf("validate aggregate evidence binding: %w", err)
	}
	return binding, nil
}

func loadReportWithDigest(ctx context.Context, path string) (Report, string, error) {
	data, err := readFile(ctx, path)
	if err != nil {
		return Report{}, "", fmt.Errorf("load soundness report %s: %w", path, err)
	}
	var report Report
	if err := decodeStrictJSON(data, &report); err != nil {
		return Report{}, "", fmt.Errorf("decode soundness report %s: %w", path, err)
	}
	if err := report.Validate(); err != nil {
		return Report{}, "", fmt.Errorf("validate soundness report %s: %w", path, err)
	}
	return report, soundnessevidence.DigestBytes(data), nil
}

func readFile(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read file %s before I/O: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("read file %s after I/O: %w", path, err)
	}
	return data, nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func writeExclusiveJSON(ctx context.Context, path string, value any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write exclusive JSON %s: %w", path, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create exclusive report: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("write report: %w", err), closeErr)
	}
	if err := file.Sync(); err != nil {
		closeErr := file.Close()
		return errors.Join(fmt.Errorf("sync report: %w", err), closeErr)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	return nil
}
