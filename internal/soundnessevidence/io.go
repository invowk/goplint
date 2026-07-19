// SPDX-License-Identifier: MPL-2.0

package soundnessevidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LoadRegistry strictly decodes and validates an evidence registry.
func LoadRegistry(ctx context.Context, path string) (Registry, error) {
	var registry Registry
	if err := readStrictJSONFile(ctx, path, &registry); err != nil {
		return Registry{}, fmt.Errorf("load evidence registry %s: %w", path, err)
	}
	if err := registry.Validate(); err != nil {
		return Registry{}, fmt.Errorf("validate evidence registry %s: %w", path, err)
	}
	return registry, nil
}

// LoadObservations strictly decodes every regular JSON file below root.
// Non-JSON files and symbolic links are rejected rather than ignored.
func LoadObservations(ctx context.Context, root string) ([]SemanticObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load evidence observations: %w", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect evidence observation root %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("evidence observation root %s is not a directory", root)
	}
	paths := make([]string, 0)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk evidence observation path %s: %w", path, walkErr)
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("walk evidence observations: %w", err)
		}
		if path == root || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("evidence observation path %s is a symbolic link", path)
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect evidence observation path %s: %w", path, err)
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("evidence observation path %s is not a regular file", path)
		}
		if filepath.Ext(path) != ".json" {
			return fmt.Errorf("evidence observation path %s is not a JSON output", path)
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("enumerate evidence observations: %w", err)
	}
	slices.Sort(paths)
	observations := make([]SemanticObservation, 0, len(paths))
	for _, path := range paths {
		var observation SemanticObservation
		if err := readStrictJSONFile(ctx, path, &observation); err != nil {
			return nil, fmt.Errorf("load evidence observation %s: %w", path, err)
		}
		if err := observation.Validate(); err != nil {
			return nil, fmt.Errorf("validate evidence observation %s: %w", path, err)
		}
		observations = append(observations, observation)
	}
	return observations, nil
}

// EmitObservationFromEnvironment binds and atomically publishes one semantic
// observation. When the aggregate evidence environment is absent, it returns
// an empty path so focused local tests remain usable outside the gate.
func EmitObservationFromEnvironment(
	ctx context.Context,
	observation SemanticObservation,
) (string, error) {
	return emitObservation(ctx, observation, os.LookupEnv)
}

// BindingFromEnvironment returns the strict aggregate binding injected into an
// evidence producer.
func BindingFromEnvironment() (ObservationBinding, error) {
	return bindingFromLookup(os.LookupEnv)
}

// DigestBytes returns the canonical digest representation used by evidence
// bindings and aggregate manifests.
func DigestBytes(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func emitObservation(
	ctx context.Context,
	observation SemanticObservation,
	lookupEnv func(string) (string, bool),
) (string, error) {
	directory, enabled := lookupEnv(EnvEvidenceDir)
	if !enabled {
		return "", nil
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("emit evidence observation: %w", err)
	}
	if strings.TrimSpace(directory) == "" {
		return "", errors.New("aggregate evidence directory is empty")
	}
	if observation.Binding != (ObservationBinding{}) {
		return "", errors.New("evidence producer supplied a binding instead of using the aggregate environment")
	}
	binding, err := bindingFromLookup(lookupEnv)
	if err != nil {
		return "", err
	}
	observation.FormatVersion = ObservationFormatVersion
	observation.Binding = binding
	if observation.ProducerID == "" {
		observation.ProducerID = binding.SubgateID
	}
	if err := observation.Validate(); err != nil {
		return "", fmt.Errorf("validate emitted evidence observation: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", fmt.Errorf("inspect aggregate evidence directory %s: %w", directory, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("aggregate evidence directory %s is not a directory", directory)
	}
	path, err := writeAtomicJSON(ctx, directory, observation)
	if err != nil {
		return "", fmt.Errorf("publish evidence observation: %w", err)
	}
	return path, nil
}

func bindingFromLookup(lookupEnv func(string) (string, bool)) (ObservationBinding, error) {
	values := []struct {
		name   string
		target *string
	}{
		{name: EnvRunID},
		{name: EnvWorkspaceDigest},
		{name: EnvManifestDigest},
		{name: EnvCommandDigest},
		{name: EnvSubgateID},
	}
	binding := ObservationBinding{}
	values[0].target = &binding.RunID
	values[1].target = &binding.WorkspaceDigest
	values[2].target = &binding.ManifestDigest
	values[3].target = &binding.CommandDigest
	values[4].target = &binding.SubgateID
	for _, value := range values {
		environmentValue, exists := lookupEnv(value.name)
		if !exists {
			return ObservationBinding{}, fmt.Errorf("aggregate evidence environment %s is unset", value.name)
		}
		*value.target = environmentValue
	}
	if err := binding.Validate(); err != nil {
		return ObservationBinding{}, err
	}
	return binding, nil
}

func readStrictJSONFile(ctx context.Context, path string, target any) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("read JSON file %s before I/O: %w", path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("read JSON file %s after I/O: %w", path, err)
	}
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

func writeAtomicJSON(ctx context.Context, directory string, value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode JSON: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(directory, ".observation-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary observation: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func(cause error) error {
		closeErr := temporary.Close()
		removeErr := os.Remove(temporaryPath)
		return errors.Join(cause, closeErr, removeErr)
	}
	if err := temporary.Chmod(0o600); err != nil {
		return "", cleanup(fmt.Errorf("set temporary observation permissions: %w", err))
	}
	if _, err := temporary.Write(data); err != nil {
		return "", cleanup(fmt.Errorf("write temporary observation: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return "", cleanup(err)
	}
	if err := temporary.Sync(); err != nil {
		return "", cleanup(fmt.Errorf("sync temporary observation: %w", err))
	}
	if err := temporary.Close(); err != nil {
		removeErr := os.Remove(temporaryPath)
		return "", errors.Join(fmt.Errorf("close temporary observation: %w", err), removeErr)
	}
	finalPath := strings.TrimSuffix(temporaryPath, ".tmp") + ".json"
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		removeErr := os.Remove(temporaryPath)
		return "", errors.Join(fmt.Errorf("publish temporary observation: %w", err), removeErr)
	}
	return finalPath, nil
}
