// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"os"
	"reflect"
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
	Primitive                     BaselineCategory `toml:"primitive"`
	MissingValidate               BaselineCategory `toml:"missing-validate"`
	MissingStringer               BaselineCategory `toml:"missing-stringer"`
	MissingConstructor            BaselineCategory `toml:"missing-constructor"`
	WrongConstructorSig           BaselineCategory `toml:"wrong-constructor-sig"`
	WrongValidateSig              BaselineCategory `toml:"wrong-validate-sig"`
	WrongStringerSig              BaselineCategory `toml:"wrong-stringer-sig"`
	MissingFuncOptions            BaselineCategory `toml:"missing-func-options"`
	MissingImmutability           BaselineCategory `toml:"missing-immutability"`
	MissingStructValidate         BaselineCategory `toml:"missing-struct-validate"`
	WrongStructValidateSig        BaselineCategory `toml:"wrong-struct-validate-sig"`
	UnvalidatedCast               BaselineCategory `toml:"unvalidated-cast"`
	UnvalidatedCastInconclusive   BaselineCategory `toml:"unvalidated-cast-inconclusive"`
	UnusedValidateResult          BaselineCategory `toml:"unused-validate-result"`
	UnusedConstructorError        BaselineCategory `toml:"unused-constructor-error"`
	MissingConstructorValidate    BaselineCategory `toml:"missing-constructor-validate"`
	MissingConstructorValidateInc BaselineCategory `toml:"missing-constructor-validate-inconclusive"`
	IncompleteValidateDelegation  BaselineCategory `toml:"incomplete-validate-delegation"`
	NonZeroValueField             BaselineCategory `toml:"nonzero-value-field"`
	WrongFuncOptionType           BaselineCategory `toml:"wrong-func-option-type"`
	EnumCueMissingGo              BaselineCategory `toml:"enum-cue-missing-go"`
	EnumCueExtraGo                BaselineCategory `toml:"enum-cue-extra-go"`
	UseBeforeValidateSameBlock    BaselineCategory `toml:"use-before-validate-same-block"`
	UseBeforeValidateCrossBlock   BaselineCategory `toml:"use-before-validate-cross-block"`
	UseBeforeValidateInconclusive BaselineCategory `toml:"use-before-validate-inconclusive"`
	SuggestValidateAll            BaselineCategory `toml:"suggest-validate-all"`
	MissingConstructorErrorReturn BaselineCategory `toml:"missing-constructor-error-return"`
	RedundantConversion           BaselineCategory `toml:"redundant-conversion"`
	MissingStructValidateFields   BaselineCategory `toml:"missing-struct-validate-fields"`
	UnvalidatedBoundaryRequest    BaselineCategory `toml:"unvalidated-boundary-request"`
	CrossPlatformPath             BaselineCategory `toml:"cross-platform-path"`

	// lookupByID is an O(1) index keyed by category → finding ID.
	lookupByID map[string]map[string]bool
}

type baselineCacheKey struct {
	path          string
	strictMissing bool
}

type baselineCacheEntry struct {
	config *BaselineConfig
	err    error
}

// BaselineFinding holds a single accepted finding entry in baseline v2.
// ID is the stable semantic identity. Message is retained for readability.
type BaselineFinding struct {
	ID      string `toml:"id"`
	Message string `toml:"message"`
}

// BaselineCategory holds accepted findings for one category.
// entries is the canonical baseline schema.
type BaselineCategory struct {
	Entries []BaselineFinding `toml:"entries"`
}

// loadBaseline reads and parses a baseline TOML file, building an internal
// lookup index for fast Contains checks.
//
// Returns an empty baseline (matches nothing) if path is empty.
// If strictMissing is false, a missing file also yields an empty baseline.
// If strictMissing is true, a missing file returns an error.
func loadBaseline(path string, strictMissing bool) (*BaselineConfig, error) {
	if path == "" {
		return emptyBaseline(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if strictMissing {
				return nil, fmt.Errorf("reading baseline: %w", err)
			}
			return emptyBaseline(), nil
		}
		return nil, fmt.Errorf("reading baseline: %w", err)
	}

	var cfg BaselineConfig
	meta, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, fmt.Errorf("parsing baseline TOML: %w", err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("parsing baseline TOML: unknown keys: %s", joinTOMLKeys(undecoded))
	}
	if err := validateBaselineEntries(&cfg); err != nil {
		return nil, err
	}

	cfg.buildLookup()

	return &cfg, nil
}

// loadBaselineCached reads baseline data through a process-local cache.
// BaselineConfig is immutable after load/buildLookup and can be safely reused.
func loadBaselineCached(state *flagState, path string, strictMissing bool) (*BaselineConfig, error) {
	if state == nil {
		return loadBaseline(path, strictMissing)
	}
	key := baselineCacheKey{path: path, strictMissing: strictMissing}
	entry := loadCacheEntry(&state.baselineCache, key, func() *baselineCacheEntry {
		cfg, err := loadBaseline(path, strictMissing)
		return &baselineCacheEntry{config: cfg, err: err}
	})
	return entry.config, entry.err
}

// ContainsFinding reports whether a finding is present in the baseline.
// Matching is strict ID-only: message text is informational and never used
// for suppression.
func (b *BaselineConfig) ContainsFinding(category, findingID, _ string) bool {
	if b == nil {
		return false
	}
	if findingID == "" {
		return false
	}
	if b.lookupByID == nil {
		return false
	}
	ids, ok := b.lookupByID[category]
	return ok && ids[findingID]
}

// Count returns the total number of baseline entries across all categories.
func (b *BaselineConfig) Count() int {
	if b == nil {
		return 0
	}
	total := 0
	for _, cat := range BaselinedCategoryNames() {
		total += countCategory(cat, b.categoryForName(cat))
	}
	return total
}

// buildLookup populates the internal lookup maps from the parsed TOML data.
func (b *BaselineConfig) buildLookup() {
	baselinedCats := BaselinedCategoryNames()
	b.lookupByID = make(map[string]map[string]bool, len(baselinedCats))

	for _, cat := range baselinedCats {
		ids := categoryIDSet(cat, b.categoryForName(cat))
		b.lookupByID[cat] = ids
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

	// Write each suppressible category as a TOML section with sorted v2 entries.
	// Order follows the canonical diagnostic category registry.
	for _, cat := range suppressibleCategorySpecs() {
		entries := normalizeBaselineFindings(cat.Name, findings[cat.Name])
		if len(entries) == 0 {
			continue
		}

		sb.WriteString(fmt.Sprintf("\n# %s\n", cat.BaselineLabel))
		sb.WriteString(fmt.Sprintf("[%s]\n", cat.Name))
		sb.WriteString("entries = [\n")
		for _, entry := range entries {
			// quote() produces TOML-compatible basic strings.
			sb.WriteString(fmt.Sprintf("    { id = %s, message = %s },\n",
				quote(entry.ID), quote(entry.Message)))
		}
		sb.WriteString("]\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("writing baseline file: %w", err)
	}
	return nil
}

func (b *BaselineConfig) categoryForName(name string) BaselineCategory {
	cat, ok := b.categoriesByName()[name]
	if !ok {
		return BaselineCategory{}
	}
	return cat
}

func (b *BaselineConfig) categoriesByName() map[string]BaselineCategory {
	if b == nil {
		return nil
	}
	value := reflect.ValueOf(*b)
	typ := value.Type()
	categoryType := reflect.TypeFor[BaselineCategory]()
	categories := make(map[string]BaselineCategory, len(BaselinedCategoryNames()))
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Type != categoryType {
			continue
		}
		tag := strings.Split(field.Tag.Get("toml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		categories[tag] = value.Field(i).Interface().(BaselineCategory)
	}
	return categories
}

// emptyBaseline returns a baseline that matches nothing.
func emptyBaseline() *BaselineConfig {
	b := &BaselineConfig{}
	b.buildLookup()
	return b
}

// countCategory counts unique IDs in one baseline category.
func countCategory(_ string, cat BaselineCategory) int {
	seen := make(map[string]bool, len(cat.Entries))
	for _, entry := range cat.Entries {
		if entry.ID != "" {
			seen[entry.ID] = true
		}
	}
	return len(seen)
}

// categoryIDSet builds an ID lookup set from one category value.
func categoryIDSet(_ string, cat BaselineCategory) map[string]bool {
	ids := make(map[string]bool, len(cat.Entries))

	for _, entry := range cat.Entries {
		if entry.ID != "" {
			ids[entry.ID] = true
		}
	}
	return ids
}

// normalizeBaselineFindings removes invalid rows, deduplicates by ID, and
// sorts by ID/message for stable diffs.
func normalizeBaselineFindings(_ string, in []BaselineFinding) []BaselineFinding {
	byID := make(map[string]BaselineFinding, len(in))

	for _, entry := range in {
		if entry.Message == "" || entry.ID == "" {
			continue
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

func validateBaselineEntries(cfg *BaselineConfig) error {
	if cfg == nil {
		return nil
	}
	for _, catName := range BaselinedCategoryNames() {
		cat := cfg.categoryForName(catName)
		for i, entry := range cat.Entries {
			if strings.TrimSpace(entry.ID) == "" {
				return fmt.Errorf("parsing baseline TOML: [%s].entries[%d].id must be non-empty", catName, i)
			}
			if strings.TrimSpace(entry.Message) == "" {
				return fmt.Errorf("parsing baseline TOML: [%s].entries[%d].message must be non-empty", catName, i)
			}
		}
	}
	return nil
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
