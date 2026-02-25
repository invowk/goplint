// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ExceptionConfig holds the exception rules loaded from TOML.
type ExceptionConfig struct {
	Settings   Settings    `toml:"settings"`
	Exceptions []Exception `toml:"exceptions"`

	// matchCounts tracks how many times each exception pattern was matched.
	// Populated during isExcepted calls; used by --audit-exceptions to
	// detect stale entries that matched zero locations.
	matchCounts map[int]int
}

// Settings configures global analyzer behavior.
type Settings struct {
	// SkipTypes lists type names that should never be flagged
	// (e.g., "bool", "error", "context.Context", "any").
	SkipTypes []string `toml:"skip_types"`
	// ExcludePaths lists path substrings that cause files to be skipped.
	// If any substring matches the file path, the file is excluded.
	ExcludePaths []string `toml:"exclude_paths"`
}

// Exception represents a single exception rule for an intentional
// primitive type usage.
type Exception struct {
	// Pattern is a dot-separated path that identifies the location.
	// Supports glob: * matches any single segment.
	// Examples: "ExecuteRequest.Name", "uroot.*.name", "tuiserver.WriteRequest.*"
	Pattern string `toml:"pattern"`
	// Reason documents why this primitive usage is intentional.
	Reason string `toml:"reason"`
}

// loadConfig reads and parses the exceptions TOML file.
// Returns an empty config (no exceptions) if path is empty or the file
// doesn't exist.
func loadConfig(path string) (*ExceptionConfig, error) {
	if path == "" {
		return &ExceptionConfig{matchCounts: make(map[int]int)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ExceptionConfig{matchCounts: make(map[int]int)}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg ExceptionConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config TOML: %w", err)
	}

	cfg.matchCounts = make(map[int]int, len(cfg.Exceptions))

	return &cfg, nil
}

// isExcepted checks whether a qualified name (e.g., "pkg.Type.Field")
// matches any exception pattern in the config.
//
// Patterns can be 2-segment (Type.Field) or 3-segment (pkg.Type.Field).
// A 2-segment pattern matches against the name with the package prefix
// stripped, enabling backward-compatible exception definitions.
func (c *ExceptionConfig) isExcepted(qualifiedName string) bool {
	// Also try matching without the package prefix for 2-segment patterns.
	stripped := qualifiedName
	if i := strings.Index(qualifiedName, "."); i >= 0 {
		stripped = qualifiedName[i+1:]
	}

	for i, exc := range c.Exceptions {
		if matchPattern(exc.Pattern, qualifiedName) || matchPattern(exc.Pattern, stripped) {
			if c.matchCounts != nil {
				c.matchCounts[i]++
			}
			return true
		}
	}
	return false
}

// isSkippedType checks whether a type name is in the skip_types list.
func (c *ExceptionConfig) isSkippedType(typeName string) bool {
	for _, st := range c.Settings.SkipTypes {
		if st == typeName {
			return true
		}
	}
	return false
}

// isExcludedPath checks whether a file path contains any of the
// exclude_paths substrings.
func (c *ExceptionConfig) isExcludedPath(filePath string) bool {
	for _, ep := range c.Settings.ExcludePaths {
		if strings.Contains(filePath, ep) {
			return true
		}
	}
	return false
}

// staleExceptions returns the indices of exception patterns that matched
// zero locations during the analysis run. Used by --audit-exceptions to
// detect exception config rot.
func (c *ExceptionConfig) staleExceptions() []int {
	var stale []int
	for i := range c.Exceptions {
		if c.matchCounts[i] == 0 {
			stale = append(stale, i)
		}
	}
	return stale
}

// matchPattern matches a glob-style pattern against a qualified name.
// Patterns use dot-separated segments where * matches any single segment.
//
// Examples:
//   - "Foo.Bar" matches "Foo.Bar"
//   - "*.Bar" matches "Foo.Bar", "Baz.Bar"
//   - "Foo.*" matches "Foo.Bar", "Foo.Baz"
//   - "*.*.name" matches "pkg.Type.name"
func matchPattern(pattern, name string) bool {
	patParts := strings.Split(pattern, ".")
	nameParts := strings.Split(name, ".")

	if len(patParts) != len(nameParts) {
		return false
	}

	for i, pp := range patParts {
		if pp == "*" {
			continue
		}
		matched, err := filepath.Match(pp, nameParts[i])
		if err != nil || !matched {
			return false
		}
	}
	return true
}
