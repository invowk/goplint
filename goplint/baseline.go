// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// BaselineConfig holds accepted findings loaded from a baseline TOML file.
// Findings present in the baseline are suppressed during analysis, so only
// new regressions are reported. Use loadBaseline to parse from disk and
// writeBaseline to generate from collected findings.
type BaselineConfig struct {
	Primitive              BaselineCategory `toml:"primitive"`
	MissingValidate        BaselineCategory `toml:"missing-validate"`
	MissingStringer        BaselineCategory `toml:"missing-stringer"`
	MissingConstructor     BaselineCategory `toml:"missing-constructor"`
	WrongConstructorSig    BaselineCategory `toml:"wrong-constructor-sig"`
	WrongValidateSig       BaselineCategory `toml:"wrong-validate-sig"`
	WrongStringerSig       BaselineCategory `toml:"wrong-stringer-sig"`
	MissingFuncOptions     BaselineCategory `toml:"missing-func-options"`
	MissingImmutability    BaselineCategory `toml:"missing-immutability"`
	MissingStructValidate  BaselineCategory `toml:"missing-struct-validate"`
	WrongStructValidateSig BaselineCategory `toml:"wrong-struct-validate-sig"`
	UnvalidatedCast        BaselineCategory `toml:"unvalidated-cast"`
	UnusedValidateResult   BaselineCategory `toml:"unused-validate-result"`
	UnusedConstructorError      BaselineCategory `toml:"unused-constructor-error"`
	MissingConstructorValidate     BaselineCategory `toml:"missing-constructor-validate"`
	IncompleteValidateDelegation BaselineCategory `toml:"incomplete-validate-delegation"`
	NonZeroValueField            BaselineCategory `toml:"nonzero-value-field"`
	EnumCueMissingGo             BaselineCategory `toml:"enum-cue-missing-go"`
	EnumCueExtraGo               BaselineCategory `toml:"enum-cue-extra-go"`

	// lookupByID is an O(1) index keyed by category → finding ID.
	lookupByID map[string]map[string]bool
	// lookupByMessage is a legacy O(1) index keyed by category → message.
	lookupByMessage map[string]map[string]bool
}

// BaselineFinding holds a single accepted finding entry in baseline v2.
// ID is the stable semantic identity. Message is retained for readability.
type BaselineFinding struct {
	ID      string `toml:"id"`
	Message string `toml:"message"`
}

// BaselineCategory holds accepted findings for one category.
// entries is the v2 schema, messages is the v1 compatibility field.
type BaselineCategory struct {
	Entries  []BaselineFinding `toml:"entries"`
	Messages []string          `toml:"messages"`
}

// loadBaseline reads and parses a baseline TOML file, building an internal
// lookup index for fast Contains checks.
//
// Returns an empty baseline (matches nothing) if path is empty or the file
// does not exist. This graceful fallback lets old PRs without a baseline
// file pass the check.
func loadBaseline(path string) (*BaselineConfig, error) {
	if path == "" {
		return emptyBaseline(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyBaseline(), nil
		}
		return nil, fmt.Errorf("reading baseline: %w", err)
	}

	var cfg BaselineConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing baseline TOML: %w", err)
	}

	cfg.buildLookup()

	return &cfg, nil
}

// Contains reports whether a finding with the given category and message
// is present in the baseline (legacy message-based matching).
func (b *BaselineConfig) Contains(category, message string) bool {
	return b.ContainsFinding(category, "", message)
}

// ContainsFinding reports whether a finding is present in the baseline.
// Matching prefers stable finding ID and falls back to message for v1
// baseline compatibility.
func (b *BaselineConfig) ContainsFinding(category, findingID, message string) bool {
	if b == nil {
		return false
	}
	if findingID != "" && b.lookupByID != nil {
		if ids, ok := b.lookupByID[category]; ok && ids[findingID] {
			return true
		}
	}
	if message != "" && b.lookupByMessage != nil {
		if msgs, ok := b.lookupByMessage[category]; ok && msgs[message] {
			return true
		}
	}
	return false
}

// Count returns the total number of baseline entries across all categories.
func (b *BaselineConfig) Count() int {
	if b == nil {
		return 0
	}
	return countCategory(b.Primitive) +
		countCategory(b.MissingValidate) +
		countCategory(b.MissingStringer) +
		countCategory(b.MissingConstructor) +
		countCategory(b.WrongConstructorSig) +
		countCategory(b.WrongValidateSig) +
		countCategory(b.WrongStringerSig) +
		countCategory(b.MissingFuncOptions) +
		countCategory(b.MissingImmutability) +
		countCategory(b.MissingStructValidate) +
		countCategory(b.WrongStructValidateSig) +
		countCategory(b.UnvalidatedCast) +
		countCategory(b.UnusedValidateResult) +
		countCategory(b.UnusedConstructorError) +
		countCategory(b.MissingConstructorValidate) +
		countCategory(b.IncompleteValidateDelegation) +
		countCategory(b.NonZeroValueField) +
		countCategory(b.EnumCueMissingGo) +
		countCategory(b.EnumCueExtraGo)
}

// buildLookup populates the internal lookup maps from the parsed TOML data.
func (b *BaselineConfig) buildLookup() {
	b.lookupByID = make(map[string]map[string]bool, 16)
	b.lookupByMessage = make(map[string]map[string]bool, 16)

	categoryData := []struct {
		key string
		cat BaselineCategory
	}{
		{CategoryPrimitive, b.Primitive},
		{CategoryMissingValidate, b.MissingValidate},
		{CategoryMissingStringer, b.MissingStringer},
		{CategoryMissingConstructor, b.MissingConstructor},
		{CategoryWrongConstructorSig, b.WrongConstructorSig},
		{CategoryWrongValidateSig, b.WrongValidateSig},
		{CategoryWrongStringerSig, b.WrongStringerSig},
		{CategoryMissingFuncOptions, b.MissingFuncOptions},
		{CategoryMissingImmutability, b.MissingImmutability},
		{CategoryMissingStructValidate, b.MissingStructValidate},
		{CategoryWrongStructValidateSig, b.WrongStructValidateSig},
		{CategoryUnvalidatedCast, b.UnvalidatedCast},
		{CategoryUnusedValidateResult, b.UnusedValidateResult},
		{CategoryUnusedConstructorError, b.UnusedConstructorError},
		{CategoryMissingConstructorValidate, b.MissingConstructorValidate},
		{CategoryIncompleteValidateDelegation, b.IncompleteValidateDelegation},
		{CategoryNonZeroValueField, b.NonZeroValueField},
		{CategoryEnumCueMissingGo, b.EnumCueMissingGo},
		{CategoryEnumCueExtraGo, b.EnumCueExtraGo},
	}

	for _, c := range categoryData {
		ids, msgs := categorySets(c.cat)
		b.lookupByID[c.key] = ids
		b.lookupByMessage[c.key] = msgs
	}
}

// WriteBaseline writes a baseline TOML file from categorized findings.
// The findings map is keyed by category constant and stores v2 baseline
// entries (stable finding ID + human-readable message).
// Empty categories are omitted from the output.
func WriteBaseline(path string, findings map[string][]BaselineFinding) error {
	var sb strings.Builder

	sb.WriteString("# SPDX-License-Identifier: MPL-2.0\n")
	sb.WriteString("#\n")
	sb.WriteString("# goplint baseline — accepted DDD compliance findings\n")
	sb.WriteString(fmt.Sprintf("# Generated: %s\n", time.Now().UTC().Format("2006-01-02")))
	sb.WriteString("# Regenerate: make update-baseline\n")

	// Count total findings for the header comment.
	total := 0
	for category, entries := range findings {
		total += len(normalizeBaselineFindings(category, entries))
	}
	sb.WriteString(fmt.Sprintf("# Total: %d findings\n", total))

	// Write each category as a TOML section with sorted v2 entries.
	// Order matches the diagnostic category constants for consistency.
	categories := []struct {
		key   string
		label string
	}{
		{CategoryPrimitive, "Bare primitive type usage"},
		{CategoryMissingValidate, "Named types missing Validate() method"},
		{CategoryMissingStringer, "Named types missing String() method"},
		{CategoryMissingConstructor, "Exported structs missing NewXxx() constructor"},
		{CategoryWrongConstructorSig, "Constructors with wrong return type"},
		{CategoryWrongValidateSig, "Named types with wrong Validate() signature"},
		{CategoryWrongStringerSig, "Named types with wrong String() signature"},
		{CategoryMissingFuncOptions, "Structs missing functional options pattern"},
		{CategoryMissingImmutability, "Structs with constructor but exported mutable fields"},
		{CategoryMissingStructValidate, "Structs with constructor but no Validate() method"},
		{CategoryWrongStructValidateSig, "Structs with Validate() but wrong signature"},
		{CategoryUnvalidatedCast, "Type conversions to DDD types without Validate() check"},
		{CategoryUnusedValidateResult, "Validate() calls with result completely discarded"},
		{CategoryUnusedConstructorError, "Constructor calls with error return assigned to blank identifier"},
		{CategoryMissingConstructorValidate, "Constructors returning validatable types without calling Validate()"},
		{CategoryIncompleteValidateDelegation, "Structs with validate-all missing field Validate() delegation"},
		{CategoryNonZeroValueField, "Struct fields using nonzero types as value (non-pointer)"},
		{CategoryEnumCueMissingGo, "CUE disjunction members missing from Go Validate() switch"},
		{CategoryEnumCueExtraGo, "Go Validate() switch cases not present in CUE disjunction"},
	}

	for _, cat := range categories {
		entries := normalizeBaselineFindings(cat.key, findings[cat.key])
		if len(entries) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("\n# %s\n", cat.label))
		sb.WriteString(fmt.Sprintf("[%s]\n", cat.key))
		sb.WriteString("entries = [\n")
		for _, entry := range entries {
			// quote() produces TOML-compatible basic strings.
			sb.WriteString(fmt.Sprintf("    { id = %s, message = %s },\n",
				quote(entry.ID), quote(entry.Message)))
		}
		sb.WriteString("]\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// emptyBaseline returns a baseline that matches nothing.
func emptyBaseline() *BaselineConfig {
	b := &BaselineConfig{}
	b.buildLookup()
	return b
}

// countCategory counts unique entries in one baseline category across both
// v2 entries and legacy v1 messages.
func countCategory(cat BaselineCategory) int {
	seen := make(map[string]bool, len(cat.Entries)+len(cat.Messages))
	for _, entry := range cat.Entries {
		if entry.ID != "" {
			seen["id:"+entry.ID] = true
			continue
		}
		if entry.Message != "" {
			seen["msg:"+entry.Message] = true
		}
	}
	for _, msg := range cat.Messages {
		if msg == "" {
			continue
		}
		seen["msg:"+msg] = true
	}
	return len(seen)
}

// categorySets builds ID and message lookup sets from one category value.
func categorySets(cat BaselineCategory) (map[string]bool, map[string]bool) {
	ids := make(map[string]bool, len(cat.Entries))
	messages := make(map[string]bool, len(cat.Entries)+len(cat.Messages))

	for _, entry := range cat.Entries {
		if entry.ID != "" {
			ids[entry.ID] = true
		}
		if entry.Message != "" {
			messages[entry.Message] = true
		}
	}
	for _, msg := range cat.Messages {
		if msg == "" {
			continue
		}
		messages[msg] = true
	}
	return ids, messages
}

// normalizeBaselineFindings fills fallback IDs, removes invalid rows,
// deduplicates by ID, and sorts by ID/message for stable diffs.
func normalizeBaselineFindings(category string, in []BaselineFinding) []BaselineFinding {
	byID := make(map[string]BaselineFinding, len(in))

	for _, entry := range in {
		if entry.Message == "" {
			continue
		}
		if entry.ID == "" {
			entry.ID = FallbackFindingID(category, entry.Message)
		}
		byID[entry.ID] = entry
	}

	out := make([]BaselineFinding, 0, len(byID))
	for _, entry := range byID {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID == out[j].ID {
			return out[i].Message < out[j].Message
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// quote produces a TOML-compatible double-quoted string with proper escaping.
// TOML basic strings use the same escape sequences as Go string literals.
func quote(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
