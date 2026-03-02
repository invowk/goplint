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
	UseBeforeValidate             BaselineCategory `toml:"use-before-validate"`
	SuggestValidateAll            BaselineCategory `toml:"suggest-validate-all"`
	MissingConstructorErrorReturn BaselineCategory `toml:"missing-constructor-error-return"`
	RedundantConversion           BaselineCategory `toml:"redundant-conversion"`
	MissingStructValidateFields   BaselineCategory `toml:"missing-struct-validate-fields"`

	// lookupByID is an O(1) index keyed by category → finding ID.
	lookupByID map[string]map[string]bool
	// lookupByMessage is a legacy O(1) index keyed by category → message.
	lookupByMessage map[string]map[string]bool
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
// entries is the v2 schema, messages is the v1 compatibility field.
type BaselineCategory struct {
	Entries  []BaselineFinding `toml:"entries"`
	Messages []string          `toml:"messages"`
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
	if cached, ok := state.baselineCache.Load(key); ok {
		entry := cached.(*baselineCacheEntry)
		return entry.config, entry.err
	}

	cfg, err := loadBaseline(path, strictMissing)
	entry := &baselineCacheEntry{config: cfg, err: err}
	state.baselineCache.Store(key, entry)
	return cfg, err
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
		// Strict ID matching: when caller provides a finding ID, do not
		// fall back to message matching, which can over-suppress distinct
		// findings that share the same text.
		return false
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
	total := 0
	for _, cat := range BaselinedCategoryNames() {
		total += countCategory(b.categoryForName(cat))
	}
	return total
}

// buildLookup populates the internal lookup maps from the parsed TOML data.
func (b *BaselineConfig) buildLookup() {
	baselinedCats := BaselinedCategoryNames()
	b.lookupByID = make(map[string]map[string]bool, len(baselinedCats))
	b.lookupByMessage = make(map[string]map[string]bool, len(baselinedCats))

	for _, cat := range baselinedCats {
		ids, msgs := categorySets(b.categoryForName(cat))
		b.lookupByID[cat] = ids
		b.lookupByMessage[cat] = msgs
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

	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

func (b *BaselineConfig) categoryForName(name string) BaselineCategory {
	switch name {
	case CategoryPrimitive:
		return b.Primitive
	case CategoryMissingValidate:
		return b.MissingValidate
	case CategoryMissingStringer:
		return b.MissingStringer
	case CategoryMissingConstructor:
		return b.MissingConstructor
	case CategoryWrongConstructorSig:
		return b.WrongConstructorSig
	case CategoryWrongValidateSig:
		return b.WrongValidateSig
	case CategoryWrongStringerSig:
		return b.WrongStringerSig
	case CategoryMissingFuncOptions:
		return b.MissingFuncOptions
	case CategoryMissingImmutability:
		return b.MissingImmutability
	case CategoryMissingStructValidate:
		return b.MissingStructValidate
	case CategoryWrongStructValidateSig:
		return b.WrongStructValidateSig
	case CategoryUnvalidatedCast:
		return b.UnvalidatedCast
	case CategoryUnusedValidateResult:
		return b.UnusedValidateResult
	case CategoryUnusedConstructorError:
		return b.UnusedConstructorError
	case CategoryMissingConstructorValidate:
		return b.MissingConstructorValidate
	case CategoryIncompleteValidateDelegation:
		return b.IncompleteValidateDelegation
	case CategoryNonZeroValueField:
		return b.NonZeroValueField
	case CategoryWrongFuncOptionType:
		return b.WrongFuncOptionType
	case CategoryEnumCueMissingGo:
		return b.EnumCueMissingGo
	case CategoryEnumCueExtraGo:
		return b.EnumCueExtraGo
	case CategoryUseBeforeValidate:
		return b.UseBeforeValidate
	case CategorySuggestValidateAll:
		return b.SuggestValidateAll
	case CategoryMissingConstructorErrorReturn:
		return b.MissingConstructorErrorReturn
	case CategoryRedundantConversion:
		return b.RedundantConversion
	case CategoryMissingStructValidateFields:
		return b.MissingStructValidateFields
	default:
		return BaselineCategory{}
	}
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
