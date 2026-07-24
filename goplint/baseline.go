// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

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
	UnusedValidateResult          BaselineCategory `toml:"unused-validate-result"`
	UnusedConstructorError        BaselineCategory `toml:"unused-constructor-error"`
	MissingConstructorValidate    BaselineCategory `toml:"missing-constructor-validate"`
	IncompleteValidateDelegation  BaselineCategory `toml:"incomplete-validate-delegation"`
	NonZeroValueField             BaselineCategory `toml:"nonzero-value-field"`
	WrongFuncOptionType           BaselineCategory `toml:"wrong-func-option-type"`
	EnumCueMissingGo              BaselineCategory `toml:"enum-cue-missing-go"`
	EnumCueExtraGo                BaselineCategory `toml:"enum-cue-extra-go"`
	UseBeforeValidateSameBlock    BaselineCategory `toml:"use-before-validate-same-block"`
	UseBeforeValidateCrossBlock   BaselineCategory `toml:"use-before-validate-cross-block"`
	SuggestValidateAll            BaselineCategory `toml:"suggest-validate-all"`
	MissingConstructorErrorReturn BaselineCategory `toml:"missing-constructor-error-return"`
	RedundantConversion           BaselineCategory `toml:"redundant-conversion"`
	MissingStructValidateFields   BaselineCategory `toml:"missing-struct-validate-fields"`
	UnvalidatedBoundaryRequest    BaselineCategory `toml:"unvalidated-boundary-request"`
	CrossPlatformPath             BaselineCategory `toml:"cross-platform-path"`
	PathmatrixDivergent           BaselineCategory `toml:"pathmatrix-divergent-pass-relative"`
	MissingCommandWaitDelay       BaselineCategory `toml:"missing-command-waitdelay"`
	CueFedPathNativeClean         BaselineCategory `toml:"cue-fed-path-native-clean"`
	PathBoundaryPrefix            BaselineCategory `toml:"path-boundary-prefix"`
	VolumeMountHostToSlash        BaselineCategory `toml:"volume-mount-host-toslash"`
	CobraCommandContext           BaselineCategory `toml:"cobra-command-context"`
	PathDomainNativeFilepath      BaselineCategory `toml:"path-domain-native-filepath"`

	// lookupByID is an O(1) index keyed by category → finding ID.
	lookupByID map[string]map[string]bool
}

// BaselineEntry identifies one accepted category/id/message tuple.
type BaselineEntry struct {
	Category string `json:"category"`
	ID       string `json:"id"`
	Message  string `json:"message"`
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

// LoadBaseline strictly loads a baseline for repository-audit consumers.
func LoadBaseline(path string) (*BaselineConfig, error) {
	return loadBaseline(path, true)
}

// Entries returns a canonical copy of every accepted baseline entry.
func (b *BaselineConfig) Entries() []BaselineEntry {
	if b == nil {
		return nil
	}
	entries := make([]BaselineEntry, 0, b.Count())
	for _, category := range BaselinedCategoryNames() {
		for _, entry := range b.categoryForName(category).Entries {
			entries = append(entries, BaselineEntry{Category: category, ID: entry.ID, Message: entry.Message})
		}
	}
	slices.SortFunc(entries, func(left, right BaselineEntry) int {
		if compared := strings.Compare(left.Category, right.Category); compared != 0 {
			return compared
		}
		if compared := strings.Compare(left.ID, right.ID); compared != 0 {
			return compared
		}
		return strings.Compare(left.Message, right.Message)
	})
	return entries
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
	if findingID == "" || IsProtocolInconclusiveCategory(category) {
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
	if err := validateBaselineFindingMap(findings); err != nil {
		return fmt.Errorf("writing baseline file: %w", err)
	}
	for category, entries := range findings {
		if IsProtocolInconclusiveCategory(category) && len(entries) > 0 {
			return fmt.Errorf("writing baseline file: protocol inconclusive category %q is always visible", category)
		}
	}

	var sb strings.Builder

	sb.WriteString("# SPDX-License-Identifier: MPL-2.0\n")
	sb.WriteString("#\n")
	sb.WriteString("# goplint baseline — accepted DDD compliance findings\n")
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
		tag, _, _ := strings.Cut(field.Tag.Get("toml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		category, ok := value.Field(i).Interface().(BaselineCategory)
		if !ok {
			continue
		}
		categories[tag] = category
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

// normalizeBaselineFindings removes invalid rows and sorts by ID/message for
// stable diffs. Duplicate and collided IDs are rejected before normalization.
func normalizeBaselineFindings(_ string, in []BaselineFinding) []BaselineFinding {
	out := make([]BaselineFinding, 0, len(in))
	for _, entry := range in {
		if entry.Message == "" || entry.ID == "" {
			continue
		}
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
	seen := make(map[string]baselineIDOwner)
	for _, catName := range BaselinedCategoryNames() {
		cat := cfg.categoryForName(catName)
		for i, entry := range cat.Entries {
			if strings.TrimSpace(entry.ID) == "" {
				return fmt.Errorf("parsing baseline TOML: [%s].entries[%d].id must be non-empty", catName, i)
			}
			if strings.TrimSpace(entry.Message) == "" {
				return fmt.Errorf("parsing baseline TOML: [%s].entries[%d].message must be non-empty", catName, i)
			}
			if err := recordBaselineIDOwner(seen, catName, i, entry); err != nil {
				return fmt.Errorf("parsing baseline TOML: %w", err)
			}
		}
	}
	return nil
}

type baselineIDOwner struct {
	category string
	index    int
	message  string
}

func validateBaselineFindingMap(findings map[string][]BaselineFinding) error {
	seen := make(map[string]baselineIDOwner)
	for _, category := range BaselinedCategoryNames() {
		for index, entry := range findings[category] {
			if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Message) == "" {
				continue
			}
			if err := recordBaselineIDOwner(seen, category, index, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func recordBaselineIDOwner(
	seen map[string]baselineIDOwner,
	category string,
	index int,
	entry BaselineFinding,
) error {
	owner, ok := seen[entry.ID]
	if !ok {
		seen[entry.ID] = baselineIDOwner{category: category, index: index, message: entry.Message}
		return nil
	}
	if owner.category == category && owner.message == entry.Message {
		return fmt.Errorf(
			"duplicate finding ID %q at [%s].entries[%d] and [%s].entries[%d]",
			entry.ID,
			owner.category,
			owner.index,
			category,
			index,
		)
	}
	return fmt.Errorf(
		"collided finding ID %q at [%s].entries[%d] and [%s].entries[%d]",
		entry.ID,
		owner.category,
		owner.index,
		category,
		index,
	)
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
