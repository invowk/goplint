// SPDX-License-Identifier: MPL-2.0

// Package profileownership conservatively routes goplint assurance profiles.
package profileownership

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/invowk/goplint/internal/soundnessgate"
)

const FormatVersion = 2

// Class is one reviewed change class assigned to a governed path family.
type Class string

const (
	// ClassDocumentation covers prose and bookkeeping that no gate executes or reads as input.
	ClassDocumentation Class = "documentation"
	// ClassConsumer covers root-module code that consumes goplint without owning analyzer behavior.
	ClassConsumer Class = "consumer"
	// ClassHarness covers gate orchestration that can lose evidence but cannot change analyzer verdicts.
	ClassHarness Class = "harness"
	// ClassAnalyzerSemantics covers everything that can influence any analyzer verdict or proof obligation.
	ClassAnalyzerSemantics Class = "analyzer-semantics"
)

type (
	// Manifest maps governed repository path families to their reviewed change class.
	Manifest struct {
		FormatVersion int    `json:"format_version"`
		Rules         []Rule `json:"rules"`
	}

	// Rule owns one exact path or recursive prefix pattern.
	Rule struct {
		Pattern string `json:"pattern"`
		Class   Class  `json:"class"`
	}

	// Context is the complete event and changed-path routing input.
	Context struct {
		Event              string
		ChangedPaths       []string
		MergeBaseAvailable bool
		ShallowRepository  bool
	}

	// Decision is a deterministic profile and visible conservative reason.
	Decision struct {
		Profile soundnessgate.ProfileID `json:"profile"`
		Class   Class                   `json:"class"`
		Reason  string                  `json:"reason"`
		Paths   []string                `json:"paths"`
	}
)

// executableInputFamilies enumerates every governed family whose files a gate
// executes or reads as input. A documentation classification is structurally
// rejected for these families: prose re-binding must never rebless drift in
// configuration, manifests, schemas, scripts, baselines, thresholds, or
// retained evidence.
var executableInputFamilies = []string{
	".github/workflows",
	"openspec/changes/archive/2026-07-19-close-goplint-soundness-review-gaps/evidence",
	"openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence",
	"scripts",
	"tools/goplint/bench",
	"tools/goplint/scripts",
	"tools/goplint/spec",
	"tools/goplint/testdata",
	"tools/mutation",
}

// executableInputExactPaths enumerates single gate-input files outside the
// families above.
var executableInputExactPaths = []string{
	".golangci.yml",
	".pre-commit-config.yaml",
	"Makefile",
	"go.mod",
	"go.sum",
}

// Load strictly decodes and validates an ownership manifest file.
func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading ownership manifest: %w", err)
	}
	return Decode(data)
}

// Decode strictly decodes and validates ownership manifest bytes. The
// json/v2 decoder rejects duplicate object members, invalid UTF-8, and
// unknown members. Field-name matching is case-sensitive; manifest tags are
// snake_case and produced by writers in this package, so casing is
// controlled.
func Decode(data []byte) (Manifest, error) {
	var manifest Manifest
	if err := jsonv2.Unmarshal(data, &manifest, jsonv2.RejectUnknownMembers(true)); err != nil {
		return Manifest{}, fmt.Errorf("decoding ownership manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate checks canonical, non-ambiguous exact or recursive-prefix rules and
// rejects any documentation classification over executable gate inputs.
func (manifest Manifest) Validate() error {
	if manifest.FormatVersion != FormatVersion || len(manifest.Rules) == 0 {
		return errors.New("goplint ownership manifest has an invalid version or empty rule set")
	}
	previous := ""
	for index, rule := range manifest.Rules {
		if rule.Pattern == "" || filepath.IsAbs(rule.Pattern) || strings.ContainsAny(rule.Pattern, "\\\x00\r\n") ||
			strings.Contains(rule.Pattern, "..") {
			return fmt.Errorf("goplint ownership rules[%d] has unsafe pattern %q", index, rule.Pattern)
		}
		if strings.Contains(rule.Pattern, "*") && !strings.HasSuffix(rule.Pattern, "/**") {
			return fmt.Errorf("goplint ownership rule %q is not an exact path or recursive prefix", rule.Pattern)
		}
		if _, known := classRank(rule.Class); !known {
			return fmt.Errorf("goplint ownership rule %q has invalid class %q", rule.Pattern, rule.Class)
		}
		if previous != "" && rule.Pattern <= previous {
			return errors.New("goplint ownership rules are duplicate or non-canonical")
		}
		previous = rule.Pattern
		if rule.Class == ClassDocumentation {
			if err := manifest.validateDocumentationRule(rule); err != nil {
				return err
			}
		}
	}
	return nil
}

// Route selects completion for exhaustive events, the least expensive tier
// that covers the highest change class present in a proven path set, and the
// semantic profile for every missing or ambiguous case.
func (manifest Manifest) Route(input Context) (Decision, error) {
	if err := manifest.Validate(); err != nil {
		return Decision{}, err
	}
	paths, err := canonicalPaths(input.ChangedPaths)
	if err != nil {
		return Decision{
			Profile: soundnessgate.ProfileSemantic,
			Class:   ClassAnalyzerSemantics,
			Reason:  "invalid changed-path context",
			Paths:   []string{},
		}, nil
	}
	switch input.Event {
	case "completion", "release", "schedule", "workflow_dispatch":
		return Decision{
			Profile: soundnessgate.ProfileComplete,
			Class:   ClassAnalyzerSemantics,
			Reason:  "event requires exhaustive completion evidence",
			Paths:   paths,
		}, nil
	case "pull_request", "push", "pre_commit":
	default:
		return Decision{
			Profile: soundnessgate.ProfileSemantic,
			Class:   ClassAnalyzerSemantics,
			Reason:  "unknown or ambiguous event context",
			Paths:   paths,
		}, nil
	}
	if input.ShallowRepository || !input.MergeBaseAvailable || len(paths) == 0 {
		return Decision{
			Profile: soundnessgate.ProfileSemantic,
			Class:   ClassAnalyzerSemantics,
			Reason:  "changed-path proof is missing or incomplete",
			Paths:   paths,
		}, nil
	}
	highest := ClassDocumentation
	highestRank, _ := classRank(highest)
	for _, path := range paths {
		class, matched := manifest.ClassForPath(path)
		if !matched {
			return Decision{
				Profile: soundnessgate.ProfileSemantic,
				Class:   ClassAnalyzerSemantics,
				Reason:  "unknown path fails closed",
				Paths:   paths,
			}, nil
		}
		if rank, _ := classRank(class); rank > highestRank {
			highest, highestRank = class, rank
		}
	}
	return Decision{
		Profile: profileForClass(highest),
		Class:   highest,
		Reason:  fmt.Sprintf("highest changed-path class is %q", highest),
		Paths:   paths,
	}, nil
}

// ClassForPath resolves the most specific governing rule: an exact pattern
// wins outright, then the longest matching recursive prefix.
func (manifest Manifest) ClassForPath(path string) (Class, bool) {
	bestLength := -1
	var best Class
	matched := false
	for _, rule := range manifest.Rules {
		if rule.Pattern == path {
			return rule.Class, true
		}
		prefix, recursive := strings.CutSuffix(rule.Pattern, "/**")
		if recursive && (path == prefix || strings.HasPrefix(path, prefix+"/")) && len(prefix) > bestLength {
			bestLength, best, matched = len(prefix), rule.Class, true
		}
	}
	return best, matched
}

func (manifest Manifest) validateDocumentationRule(rule Rule) error {
	prefix, recursive := strings.CutSuffix(rule.Pattern, "/**")
	if !recursive {
		if !strings.HasSuffix(rule.Pattern, ".md") {
			return fmt.Errorf("goplint ownership documentation rule %q is not a prose markdown path", rule.Pattern)
		}
		for _, family := range executableInputFamilies {
			if pathWithin(rule.Pattern, family) {
				return fmt.Errorf(
					"goplint ownership documentation rule %q is inside executable-input family %q", rule.Pattern, family,
				)
			}
		}
		return nil
	}
	for _, family := range executableInputFamilies {
		if pathWithin(prefix, family) {
			return fmt.Errorf(
				"goplint ownership documentation rule %q is inside executable-input family %q", rule.Pattern, family,
			)
		}
		if pathWithin(family, prefix) && !manifest.familyCoveredByNonDocumentationRule(family) {
			return fmt.Errorf(
				"goplint ownership documentation rule %q engulfs executable-input family %q without a more specific non-documentation rule",
				rule.Pattern, family,
			)
		}
	}
	for _, exact := range executableInputExactPaths {
		if pathWithin(exact, prefix) {
			if class, matched := manifest.ClassForPath(exact); !matched || class == ClassDocumentation {
				return fmt.Errorf(
					"goplint ownership documentation rule %q engulfs executable input %q", rule.Pattern, exact,
				)
			}
		}
	}
	return nil
}

// familyCoveredByNonDocumentationRule reports whether an executable-input
// family resolves to a non-documentation class for every possible member,
// which requires a covering rule at least as specific as the family itself.
func (manifest Manifest) familyCoveredByNonDocumentationRule(family string) bool {
	for _, rule := range manifest.Rules {
		if rule.Class == ClassDocumentation {
			continue
		}
		prefix, recursive := strings.CutSuffix(rule.Pattern, "/**")
		if recursive && pathWithin(family, prefix) && len(prefix) >= len(family) {
			return true
		}
	}
	return false
}

func classRank(class Class) (int, bool) {
	switch class {
	case ClassDocumentation:
		return 0, true
	case ClassConsumer:
		return 1, true
	case ClassHarness:
		return 2, true
	case ClassAnalyzerSemantics:
		return 3, true
	default:
		return 0, false
	}
}

func profileForClass(class Class) soundnessgate.ProfileID {
	switch class {
	case ClassDocumentation:
		return soundnessgate.ProfileDocumentation
	case ClassConsumer:
		return soundnessgate.ProfileConsumer
	case ClassHarness:
		return soundnessgate.ProfileHarness
	default:
		return soundnessgate.ProfileSemantic
	}
}

// pathWithin reports whether path equals ancestor or lives under it.
func pathWithin(path, ancestor string) bool {
	return path == ancestor || strings.HasPrefix(path, ancestor+"/")
}

func canonicalPaths(paths []string) ([]string, error) {
	result := slices.Clone(paths)
	for index := range result {
		result[index] = filepath.ToSlash(filepath.Clean(strings.TrimSpace(result[index])))
		if result[index] == "." || result[index] == ".." || strings.HasPrefix(result[index], "../") ||
			filepath.IsAbs(result[index]) || strings.ContainsAny(result[index], "\x00\r\n") {
			return nil, fmt.Errorf("unsafe changed path %q", result[index])
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	return result, nil
}
