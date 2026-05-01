// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	// Safe for unsynchronized access: go/analysis runs per-package, and each
	// run() creates a fresh ExceptionConfig via loadConfig().
	matchCounts map[int]int
}

type configCacheKey struct {
	path          string
	strictMissing bool
}

type configCacheEntry struct {
	template *ExceptionConfig
	err      error
}

// Settings configures global analyzer behavior.
type Settings struct {
	// SkipTypes lists type names that should never be flagged
	// (e.g., "bool", "error", "context.Context", "any").
	SkipTypes []string `toml:"skip_types"`
	// ExcludePaths lists path substrings that cause files to be skipped.
	// If any substring matches the file path, the file is excluded.
	ExcludePaths []string `toml:"exclude_paths"`
	// IncludePackages restricts diagnostic emission to packages whose
	// import path starts with one of these prefixes. Empty means no filter
	// (all packages emit diagnostics). Third-party packages are still
	// analyzed for fact export but their findings are suppressed.
	IncludePackages []string `toml:"include_packages"`
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
	// ReviewAfter is an optional ISO date (e.g., "2025-12-01") indicating
	// when this exception should be re-evaluated. Used by --audit-review-dates
	// to flag overdue exceptions.
	ReviewAfter string `toml:"review_after,omitempty"`
	// BlockedBy documents what work item or condition must be resolved
	// before this exception can be removed (e.g., "Tier 3.7 tui baseline").
	BlockedBy string `toml:"blocked_by,omitempty"`
}

// loadConfig reads and parses the exceptions TOML file.
// Returns an empty config (no exceptions) if path is empty.
// If strictMissing is false, a missing file also yields an empty config.
// If strictMissing is true, a missing file returns an error.
func loadConfig(path string, strictMissing bool) (*ExceptionConfig, error) {
	if path == "" {
		return &ExceptionConfig{matchCounts: make(map[int]int)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if strictMissing {
				return nil, fmt.Errorf("reading config: %w", err)
			}
			return &ExceptionConfig{matchCounts: make(map[int]int)}, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg ExceptionConfig
	meta, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing config TOML: %w", err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("parsing config TOML: unknown keys: %s", joinTOMLKeys(undecoded))
	}
	if err := validateExceptionPatterns(cfg.Exceptions); err != nil {
		return nil, err
	}
	normalizedPrefixes, err := normalizeIncludePackagePrefixes(cfg.Settings.IncludePackages)
	if err != nil {
		return nil, err
	}
	cfg.Settings.IncludePackages = normalizedPrefixes

	cfg.matchCounts = make(map[int]int, len(cfg.Exceptions))

	return &cfg, nil
}

// loadConfigCached reads exceptions config through a process-local cache.
// The returned config is always a per-run clone with fresh match counters.
func loadConfigCached(state *flagState, path string, strictMissing bool) (*ExceptionConfig, error) {
	if state == nil {
		return loadConfig(path, strictMissing)
	}
	key := configCacheKey{path: path, strictMissing: strictMissing}
	entry := loadCacheEntry(&state.configCache, key, func() *configCacheEntry {
		cfg, err := loadConfig(path, strictMissing)
		cacheEntry := &configCacheEntry{err: err}
		if err == nil {
			cacheEntry.template = configTemplate(cfg)
		}
		return cacheEntry
	})
	if entry.err != nil {
		return nil, entry.err
	}
	return cloneExceptionConfig(entry.template), nil
}

func configTemplate(cfg *ExceptionConfig) *ExceptionConfig {
	if cfg == nil {
		return &ExceptionConfig{}
	}
	return &ExceptionConfig{
		Settings: Settings{
			SkipTypes:       slices.Clone(cfg.Settings.SkipTypes),
			ExcludePaths:    slices.Clone(cfg.Settings.ExcludePaths),
			IncludePackages: slices.Clone(cfg.Settings.IncludePackages),
		},
		Exceptions: slices.Clone(cfg.Exceptions),
	}
}

func cloneExceptionConfig(template *ExceptionConfig) *ExceptionConfig {
	if template == nil {
		return &ExceptionConfig{matchCounts: make(map[int]int)}
	}
	clone := &ExceptionConfig{
		Settings: Settings{
			SkipTypes:       slices.Clone(template.Settings.SkipTypes),
			ExcludePaths:    slices.Clone(template.Settings.ExcludePaths),
			IncludePackages: slices.Clone(template.Settings.IncludePackages),
		},
		Exceptions:  slices.Clone(template.Exceptions),
		matchCounts: make(map[int]int, len(template.Exceptions)),
	}
	return clone
}

func joinTOMLKeys(keys []toml.Key) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key.String())
	}
	return strings.Join(parts, ", ")
}

// ShouldAnalyzePackage reports whether diagnostics should be emitted for the
// given package path. Returns true when include_packages is empty (no filter)
// or when the path matches any import-path segment in include_packages. Non-matching
// packages are still analyzed for fact export but their findings are suppressed.
func (c *ExceptionConfig) ShouldAnalyzePackage(pkgPath string) bool {
	if len(c.Settings.IncludePackages) == 0 {
		return true
	}
	for _, prefix := range c.Settings.IncludePackages {
		if prefix == "" {
			continue
		}
		if pkgPath == prefix || strings.HasPrefix(pkgPath, prefix+"/") {
			return true
		}
	}
	return false
}

func normalizeIncludePackagePrefixes(prefixes []string) ([]string, error) {
	if len(prefixes) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(prefixes))
	for i, prefix := range prefixes {
		trimmed := strings.TrimSpace(prefix)
		if trimmed == "" {
			return nil, fmt.Errorf("parsing config TOML: settings.include_packages[%d]: empty package prefix", i)
		}
		normalized = append(normalized, trimmed)
	}
	return normalized, nil
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
	if _, after, ok := strings.Cut(qualifiedName, "."); ok {
		stripped = after
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
	return slices.Contains(c.Settings.SkipTypes, typeName)
}

// isExcludedPath checks whether a file path contains any of the
// exclude_paths substrings.
func (c *ExceptionConfig) isExcludedPath(filePath string) bool {
	normalizedPath := strings.ReplaceAll(filepath.ToSlash(filePath), "\\", "/")
	for _, ep := range c.Settings.ExcludePaths {
		normalizedExclude := strings.ReplaceAll(filepath.ToSlash(ep), "\\", "/")
		if strings.Contains(normalizedPath, normalizedExclude) {
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

func validateExceptionPatterns(exceptions []Exception) error {
	for i, exc := range exceptions {
		if err := validateExceptionPattern(exc.Pattern); err != nil {
			return fmt.Errorf("parsing config TOML: exceptions[%d].pattern: %w", i, err)
		}
	}
	return nil
}

func validateExceptionPattern(pattern string) error {
	parts := strings.Split(pattern, ".")
	if len(parts) == 0 {
		return errors.New("must contain at least one segment")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("empty segment in pattern %q", pattern)
		}
		if part == "*" {
			continue
		}
		if _, err := filepath.Match(part, "probe"); err != nil {
			return fmt.Errorf("invalid glob %q: %w", part, err)
		}
	}
	return nil
}
