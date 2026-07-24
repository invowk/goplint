// SPDX-License-Identifier: MPL-2.0

package repositoryaudit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
)

// Load strictly decodes and validates one retained repository audit.
func Load(ctx context.Context, path string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("load repository audit context: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("read repository audit %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var result Result
	if err := decoder.Decode(&result); err != nil {
		return Result{}, fmt.Errorf("decode repository audit %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Result{}, fmt.Errorf("decode repository audit %s: trailing JSON content", path)
	}
	if err := result.Validate(); err != nil {
		return Result{}, fmt.Errorf("validate repository audit %s: %w", path, err)
	}
	return result, nil
}

// WriteExclusive publishes one immutable repository audit without overwriting
// a potentially foreign or stale artifact.
func WriteExclusive(ctx context.Context, path string, result Result) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write repository audit context: %w", err)
	}
	if err := result.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode repository audit: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create repository audit directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create repository audit %s: %w", path, err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		file.Close() //nolint:errcheck // Preserve the primary write error.
		return fmt.Errorf("write repository audit %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close repository audit %s: %w", path, err)
	}
	return nil
}

// ValidateConsumer rejects stale inputs/census and applies one distinct
// read-only verdict without running the analyzer again.
func ValidateConsumer(result Result, expected InputBinding, packageIDs []string, purpose string) error {
	if err := result.Validate(); err != nil {
		return err
	}
	expected = normalizeInputs(expected)
	if !inputsEqual(result.Inputs, expected) {
		return errors.New("repository audit inputs do not match the exact current consumer inputs")
	}
	packages := canonicalStrings(packageIDs)
	if !slices.Equal(result.Packages.PackageIDs, packages) {
		return errors.New("repository audit package census does not match the current package census")
	}
	switch purpose {
	case "full-scan":
		if len(result.Baseline.New) != 0 {
			return fmt.Errorf("repository audit full-scan verdict has %d blocking new or always-visible findings", len(result.Baseline.New))
		}
	case "baseline":
		if len(result.Baseline.New) != 0 {
			return fmt.Errorf("repository audit baseline verdict has %d new finding IDs (%d stale IDs remain visible)", len(result.Baseline.New), len(result.Baseline.Stale))
		}
	case "exceptions":
		if len(result.Exceptions.StalePatterns) != 0 {
			return fmt.Errorf("repository audit exception verdict has %d globally stale patterns", len(result.Exceptions.StalePatterns))
		}
	default:
		return fmt.Errorf("repository audit consumer purpose %q is invalid", purpose)
	}
	return nil
}

func inputsEqual(left, right InputBinding) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
