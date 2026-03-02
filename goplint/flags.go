// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sync"

	"golang.org/x/tools/go/analysis"
)

// flagState contains one analyzer instance's parsed flag values. Keeping this
// state instance-local avoids package-global mutable flag coupling.
type flagState struct {
	configPath                  string
	baselinePath                string
	emitFindingsPath            string
	includePackages             string // comma-separated package prefixes (CLI override)
	configPathExplicit          bool
	baselinePathExplicit        bool
	emitFindingsPathExplicit    bool
	auditExceptions             bool
	checkAll                    bool
	checkValidate               bool
	checkStringer               bool
	checkConstructors           bool
	checkConstructorSig         bool
	checkFuncOptions            bool
	checkImmutability           bool
	checkStructValidate         bool
	checkCastValidation         bool
	checkValidateUsage          bool
	checkConstructorErrUsage    bool
	checkConstructorValidates   bool
	checkValidateDelegation     bool
	checkNonZero                bool
	checkUseBeforeValidate      bool
	checkConstructorReturnError bool
	checkUseBeforeValidateCross bool
	checkRedundantConversion    bool
	checkValidateDelegationAll  bool
	noCFA                       bool
	auditReviewDates            bool
	checkEnumSync               bool
	suggestValidateAll          bool

	// runtime caches/state are intentionally analyzer-instance scoped.
	overdueReviewMu   sync.Mutex
	overdueReviewSeen map[string]bool
	configCache       sync.Map // map[configCacheKey]*configCacheEntry
	baselineCache     sync.Map // map[baselineCacheKey]*baselineCacheEntry
}

type trackedStringFlag struct {
	value    *string
	explicit *bool
}

func (f *trackedStringFlag) Set(s string) error {
	if f.value != nil {
		*f.value = s
	}
	if f.explicit != nil {
		*f.explicit = true
	}
	return nil
}

func (f *trackedStringFlag) String() string {
	if f.value == nil {
		return ""
	}
	return *f.value
}

// modeFlagSpec is the declarative table for all bool analyzer modes.
// It binds flag registration, runConfig wiring, and --check-all expansion.
type modeFlagSpec struct {
	flagName           string
	usage              string
	defaultValue       bool
	includeInCheckAll  bool
	stateBoolField     func(*flagState) *bool
	runConfigBoolField func(*runConfig) *bool
}

func (s modeFlagSpec) runConfigValue(rc *runConfig) bool {
	return *s.runConfigBoolField(rc)
}

func modeFlagSpecs() []modeFlagSpec {
	return []modeFlagSpec{
		{
			flagName:          "audit-exceptions",
			usage:             "report exception patterns that matched zero locations (stale entries)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.auditExceptions
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.auditExceptions
			},
		},
		{
			flagName:          "check-validate",
			usage:             "report named non-struct types missing Validate() error method",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkValidate
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkValidate
			},
		},
		{
			flagName:          "check-stringer",
			usage:             "report named non-struct types missing String() string method",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkStringer
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkStringer
			},
		},
		{
			flagName:          "check-constructors",
			usage:             "report exported struct types missing NewXxx() constructor function",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkConstructors
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkConstructors
			},
		},
		{
			flagName:          "check-constructor-sig",
			usage:             "report NewXxx() constructors whose return type doesn't match the struct",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkConstructorSig
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkConstructorSig
			},
		},
		{
			flagName:          "check-func-options",
			usage:             "report structs that should use or complete the functional options pattern",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkFuncOptions
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkFuncOptions
			},
		},
		{
			flagName:          "check-immutability",
			usage:             "report structs with constructors that have exported mutable fields",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkImmutability
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkImmutability
			},
		},
		{
			flagName:          "check-struct-validate",
			usage:             "report exported struct types with constructors missing Validate() error method",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkStructValidate
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkStructValidate
			},
		},
		{
			flagName:          "check-cast-validation",
			usage:             "report type conversions to DDD Value Types from non-constants without Validate() check",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkCastValidation
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkCastValidation
			},
		},
		{
			flagName:          "check-validate-usage",
			usage:             "detect unused Validate() results",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkValidateUsage
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkValidateUsage
			},
		},
		{
			flagName:          "check-constructor-error-usage",
			usage:             "detect constructor calls with error return assigned to blank identifier",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkConstructorErrUsage
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkConstructorErrUsage
			},
		},
		{
			flagName:          "check-constructor-validates",
			usage:             "report NewXxx() constructors that return types with Validate() but never call it",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkConstructorValidates
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkConstructorValidates
			},
		},
		{
			flagName:          "check-validate-delegation",
			usage:             "report structs with //goplint:validate-all whose Validate() misses field delegations",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkValidateDelegation
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkValidateDelegation
			},
		},
		{
			flagName:          "check-nonzero",
			usage:             "report struct fields using nonzero-annotated types as value (non-pointer) fields where they are semantically optional",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkNonZero
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkNonZero
			},
		},
		{
			flagName:          "audit-review-dates",
			usage:             "report exception patterns with review_after dates that have passed",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.auditReviewDates
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.auditReviewDates
			},
		},
		{
			flagName:          "check-use-before-validate",
			usage:             "report DDD Value Type variables used before Validate() in the same basic block (CFA only)",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkUseBeforeValidate
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkUseBeforeValidate
			},
		},
		{
			flagName:          "check-constructor-return-error",
			usage:             "report NewXxx() constructors for validatable types that do not return error",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkConstructorReturnError
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkConstructorReturnError
			},
		},
		{
			flagName:          "check-use-before-validate-cross",
			usage:             "report DDD Value Type variables used before Validate() across CFG blocks (CFA only, opt-in)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkUseBeforeValidateCross
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkUseBeforeValidateCross
			},
		},
		{
			flagName:          "no-cfa",
			usage:             "disable control-flow analysis and use AST heuristic for cast-validation (CFA is enabled by default)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.noCFA
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.noCFA
			},
		},
		{
			flagName:          "check-enum-sync",
			usage:             "report mismatches between Go Validate() switch cases and CUE schema disjunction members (requires //goplint:enum-cue= directive)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkEnumSync
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkEnumSync
			},
		},
		{
			flagName:          "suggest-validate-all",
			usage:             "report structs with Validate() + validatable fields but no //goplint:validate-all directive (advisory)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.suggestValidateAll
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.suggestValidateAll
			},
		},
		{
			flagName:          "check-redundant-conversion",
			usage:             "report type conversions with redundant intermediate basic-type hop (e.g., T2(string(x)) where T2(x) suffices)",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkRedundantConversion
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkRedundantConversion
			},
		},
		{
			flagName:          "check-validate-delegation-all",
			usage:             "report all structs with validatable fields: missing Validate() or incomplete delegation (no directive required)",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkValidateDelegationAll
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkValidateDelegationAll
			},
		},
		{
			flagName:          "check-all",
			usage:             "enable all DDD compliance checks (validate + stringer + constructors + structural + cast-validation + validate-usage + constructor-error-usage + constructor-validates + nonzero + redundant-conversion + validate-delegation-all + CFA)",
			defaultValue:      false,
			includeInCheckAll: false,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkAll
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkAll
			},
		},
	}
}

func bindAnalyzerFlags(analyzer *analysis.Analyzer, state *flagState) {
	analyzer.Flags.Var(&trackedStringFlag{value: &state.configPath, explicit: &state.configPathExplicit}, "config",
		"path to exceptions TOML config file")
	analyzer.Flags.Var(&trackedStringFlag{value: &state.baselinePath, explicit: &state.baselinePathExplicit}, "baseline",
		"path to baseline TOML file (suppress known findings, report only new ones)")
	// Internal: used by --update-baseline to collect exact semantic IDs.
	analyzer.Flags.Var(&trackedStringFlag{
		value:    &state.emitFindingsPath,
		explicit: &state.emitFindingsPathExplicit,
	}, "emit-findings-jsonl", "internal path to write structured findings stream")

	analyzer.Flags.StringVar(&state.includePackages, "include-packages", "",
		"comma-separated package path prefixes; only emit diagnostics for matching packages (overrides TOML include_packages)")

	for _, spec := range modeFlagSpecs() {
		analyzer.Flags.BoolVar(spec.stateBoolField(state), spec.flagName, spec.defaultValue, spec.usage)
	}
}

func resetFlagStateDefaults(state *flagState) {
	if state == nil {
		return
	}
	state.configPath = ""
	state.baselinePath = ""
	state.emitFindingsPath = ""
	state.includePackages = ""
	state.configPathExplicit = false
	state.baselinePathExplicit = false
	state.emitFindingsPathExplicit = false
	for _, spec := range modeFlagSpecs() {
		*spec.stateBoolField(state) = spec.defaultValue
	}
	state.overdueReviewMu.Lock()
	state.overdueReviewSeen = make(map[string]bool)
	state.overdueReviewMu.Unlock()
	state.configCache = sync.Map{}
	state.baselineCache = sync.Map{}
}

// runConfig holds the resolved flag values for a single run() invocation.
// Reading flag bindings into this struct at run() entry ensures run()
// never reads or mutates package-level state directly.
type runConfig struct {
	configPath                  string
	configPathExplicit          bool
	baselinePath                string
	baselinePathExplicit        bool
	emitFindingsPath            string
	emitFindingsPathExplicit    bool
	includePackages             string
	auditExceptions             bool
	checkAll                    bool
	checkValidate               bool
	checkStringer               bool
	checkConstructors           bool
	checkConstructorSig         bool
	checkFuncOptions            bool
	checkImmutability           bool
	checkStructValidate         bool
	checkCastValidation         bool
	checkValidateUsage          bool
	checkConstructorErrUsage    bool
	checkConstructorValidates   bool
	checkValidateDelegation     bool
	checkNonZero                bool
	checkUseBeforeValidate      bool
	checkConstructorReturnError bool
	checkUseBeforeValidateCross bool
	checkRedundantConversion    bool
	checkValidateDelegationAll  bool
	noCFA                       bool
	auditReviewDates            bool
	checkEnumSync               bool
	suggestValidateAll          bool
}

func newRunConfigForState(state *flagState) runConfig {
	rc := runConfig{
		configPath:               state.configPath,
		configPathExplicit:       state.configPathExplicit,
		baselinePath:             state.baselinePath,
		baselinePathExplicit:     state.baselinePathExplicit,
		emitFindingsPath:         state.emitFindingsPath,
		emitFindingsPathExplicit: state.emitFindingsPathExplicit,
		includePackages:          state.includePackages,
	}
	for _, spec := range modeFlagSpecs() {
		*spec.runConfigBoolField(&rc) = *spec.stateBoolField(state)
	}
	expandCheckAllModes(&rc)
	normalizeRunConfig(&rc)
	return rc
}

func expandCheckAllModes(rc *runConfig) {
	// Expand --check-all into individual supplementary checks.
	// Deliberately excludes --audit-exceptions (config maintenance tool
	// with per-package false positives). CFA is enabled by default
	// (opt out via --no-cfa).
	if !rc.checkAll {
		return
	}
	for _, spec := range modeFlagSpecs() {
		if !spec.includeInCheckAll {
			continue
		}
		*spec.runConfigBoolField(rc) = true
	}
}

func normalizeRunConfig(rc *runConfig) {
	if rc == nil {
		return
	}
	// UBV checks are CFA-only extensions of cast-validation. Auto-enable cast
	// validation so explicit UBV flags are never silently inert.
	if rc.checkUseBeforeValidate || rc.checkUseBeforeValidateCross {
		rc.checkCastValidation = true
	}
}
