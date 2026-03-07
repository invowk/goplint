// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

type interprocCompatibilityViolation struct {
	Category    string
	FindingID   string
	LegacyClass interprocOutcomeClass
	IFDSClass   interprocOutcomeClass
	Description string
}

type interprocCompatTracker struct {
	enabled    bool
	violations []interprocCompatibilityViolation
}

func newInterprocCompatTracker(engine string) *interprocCompatTracker {
	return &interprocCompatTracker{enabled: engine == cfgInterprocEngineCompare}
}

func (t *interprocCompatTracker) Check(
	category string,
	findingID string,
	legacy interprocPathResult,
	ifds interprocPathResult,
	hasEquivalentUnsafe bool,
) {
	if t == nil || !t.enabled {
		return
	}
	if violation, ok := classifyInterprocDowngrade(category, findingID, legacy.Class, ifds.Class, hasEquivalentUnsafe); ok {
		t.violations = append(t.violations, violation)
	}
}

func (t *interprocCompatTracker) Err() error {
	if t == nil || len(t.violations) == 0 {
		return nil
	}
	sort.SliceStable(t.violations, func(i, j int) bool {
		if t.violations[i].Category != t.violations[j].Category {
			return t.violations[i].Category < t.violations[j].Category
		}
		return t.violations[i].FindingID < t.violations[j].FindingID
	})
	messages := make([]string, 0, len(t.violations))
	for _, violation := range t.violations {
		messages = append(messages,
			fmt.Sprintf("%s (%s): legacy=%s ifds=%s", violation.Category, violation.FindingID, violation.LegacyClass, violation.IFDSClass),
		)
	}
	return fmt.Errorf(
		"cfg-interproc-engine=compare detected forbidden no-silent-downgrade regressions: %s",
		strings.Join(messages, "; "),
	)
}

func classifyInterprocDowngrade(
	category string,
	findingID string,
	legacy interprocOutcomeClass,
	ifds interprocOutcomeClass,
	hasEquivalentUnsafe bool,
) (interprocCompatibilityViolation, bool) {
	if legacy == interprocOutcomeUnsafe && ifds == interprocOutcomeSafe {
		return interprocCompatibilityViolation{
			Category:    category,
			FindingID:   findingID,
			LegacyClass: legacy,
			IFDSClass:   ifds,
			Description: "legacy unsafe downgraded to ifds safe",
		}, true
	}
	if legacy == interprocOutcomeInconclusive && ifds == interprocOutcomeSafe && !hasEquivalentUnsafe {
		return interprocCompatibilityViolation{
			Category:    category,
			FindingID:   findingID,
			LegacyClass: legacy,
			IFDSClass:   ifds,
			Description: "legacy inconclusive downgraded to ifds safe without equivalent unsafe finding",
		}, true
	}
	return interprocCompatibilityViolation{}, false
}

func compareInterprocOutcomeSets(
	legacy map[string]interprocOutcomeClass,
	ifds map[string]interprocOutcomeClass,
	unsafeFindingIDs []string,
) []interprocCompatibilityViolation {
	sortedUnsafe := append([]string(nil), unsafeFindingIDs...)
	sort.Strings(sortedUnsafe)
	keys := make([]string, 0, len(legacy))
	for key := range legacy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	violations := make([]interprocCompatibilityViolation, 0)
	for _, key := range keys {
		legacyClass := legacy[key]
		ifdsClass, ok := ifds[key]
		if !ok {
			// Missing IFDS category findings can still correspond to inconclusive
			// outcomes emitted under split categories.
			ifdsClass = interprocOutcomeInconclusive
		}
		_, hasUnsafe := slices.BinarySearch(sortedUnsafe, key)
		if violation, bad := classifyInterprocDowngrade("compat", key, legacyClass, ifdsClass, hasUnsafe); bad {
			violations = append(violations, violation)
		}
	}
	return violations
}
