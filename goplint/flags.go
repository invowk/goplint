// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"
)

const (
	defaultCFGMaxStates = 20_000

	defaultCFGWitnessMaxSteps = 12
	cfgSSAConstraintsEngine   = "ssa-constraints"

	defaultCFGRefinementMaxIterations = 3
	defaultCFGFeasibilityMaxQueries   = 16
	defaultCFGFeasibilityTimeoutMS    = 1000
)

// flagState contains one analyzer instance's parsed flag values. Keeping this
// state instance-local avoids package-global mutable flag coupling.
type flagState struct {
	configPath                    string
	baselinePath                  string
	emitFindingsPath              string
	includePackages               string // comma-separated package prefixes (CLI override)
	cfgMaxStates                  int
	cfgWitnessMaxSteps            int
	cfgRefinementMaxIterations    int
	cfgFeasibilityMaxQueries      int
	cfgFeasibilityTimeoutMS       int
	includePackagesExplicit       bool
	configPathExplicit            bool
	baselinePathExplicit          bool
	emitFindingsPathExplicit      bool
	auditExceptions               bool
	checkAll                      bool
	checkValidate                 bool
	checkStringer                 bool
	checkConstructors             bool
	checkConstructorSig           bool
	checkFuncOptions              bool
	checkImmutability             bool
	checkStructValidate           bool
	checkCastValidation           bool
	checkValidateUsage            bool
	checkConstructorErrUsage      bool
	checkConstructorValidates     bool
	checkValidateDelegation       bool
	checkNonZero                  bool
	checkUseBeforeValidate        bool
	checkConstructorReturnError   bool
	checkRedundantConversion      bool
	auditReviewDates              bool
	checkEnumSync                 bool
	suggestValidateAll            bool
	checkBoundaryRequest          bool
	checkCrossPlatformPath        bool
	checkPathmatrixDivergent      bool
	checkTestHomeEnvPlatform      bool
	checkCommandWaitDelay         bool
	checkCueFedPathNativeClean    bool
	checkPathBoundaryPrefix       bool
	checkVolumeMountHostToSlash   bool
	checkCobraCommandContext      bool
	checkPathDomainNativeFilepath bool

	// runtime caches/state are intentionally analyzer-instance scoped.
	overdueReviewMu            sync.Mutex
	overdueReviewSeen          map[string]bool
	configCache                sync.Map // map[configCacheKey]*configCacheEntry
	baselineCache              sync.Map // map[baselineCacheKey]*baselineCacheEntry
	calleeSummaryCache         sync.Map // map[string]calleeSummaryEntry keyed by objectKey(func)+slot
	ssaBuilder                 func(*analysis.Pass) *ssaResult
	protocolControlFactory     func(time.Duration) protocolAnalysisControl
	protocolFeasibilityBackend cfgFeasibilityBackend
	semanticEvidenceObserver   semanticEvidenceObserver
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
			usage:             "report structs with validatable fields that have missing Validate() or incomplete field delegation",
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
			usage:             "report DDD Value Type variables used before Validate() across canonical protocol paths",
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
			flagName:          "check-boundary-request-validation",
			usage:             "report exported Request/Options boundaries that use validatable parameters before Validate()",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkBoundaryRequest
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkBoundaryRequest
			},
		},
		{
			flagName:          "check-cross-platform-paths",
			usage:             "report filepath.IsAbs(filepath.FromSlash(x)) chains that miss Unix-style absolute paths on Windows",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkCrossPlatformPath
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkCrossPlatformPath
			},
		},
		{
			flagName:          "check-pathmatrix-divergent",
			usage:             "report pathmatrix.PassRelative on platform-divergent inputs (UNC/WindowsDriveAbs/WindowsRooted) without OnWindows override",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkPathmatrixDivergent
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkPathmatrixDivergent
			},
		},
		{
			flagName:          "check-test-home-env",
			usage:             "report tests that set HOME directly instead of internal/testutil.SetHomeDir",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkTestHomeEnvPlatform
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkTestHomeEnvPlatform
			},
		},
		{
			flagName:          "check-command-waitdelay",
			usage:             "report exec.CommandContext commands used without setting Cmd.WaitDelay before execution",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkCommandWaitDelay
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkCommandWaitDelay
			},
		},
		{
			flagName:          "check-cue-fed-path-native-clean",
			usage:             "report CUE-fed or repo-relative path validators that use filepath cleanup before slash normalization",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkCueFedPathNativeClean
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkCueFedPathNativeClean
			},
		},
		{
			flagName:          "check-path-boundary-prefix",
			usage:             "report path containment checks that use strings.HasPrefix without an exact path-segment boundary",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkPathBoundaryPrefix
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkPathBoundaryPrefix
			},
		},
		{
			flagName:          "check-volume-mount-host-toslash",
			usage:             "report container volume mount strings that concatenate host paths without filepath.ToSlash",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkVolumeMountHostToSlash
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkVolumeMountHostToSlash
			},
		},
		{
			flagName:          "check-cobra-command-context",
			usage:             "report Cobra command handlers that use context.Background instead of cmd.Context",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkCobraCommandContext
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkCobraCommandContext
			},
		},
		{
			flagName:          "check-path-domain-native-filepath",
			usage:             "report path-domain annotated values passed to host-native filepath functions",
			defaultValue:      false,
			includeInCheckAll: true,
			stateBoolField: func(fs *flagState) *bool {
				return &fs.checkPathDomainNativeFilepath
			},
			runConfigBoolField: func(rc *runConfig) *bool {
				return &rc.checkPathDomainNativeFilepath
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
			flagName:          "check-all",
			usage:             "enable all DDD compliance checks (validate + stringer + constructors + structural + cast-validation + validate-usage + constructor-error-usage + constructor-validates + nonzero + redundant-conversion + universal validate-delegation + protocol analysis)",
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

	analyzer.Flags.Var(&trackedStringFlag{value: &state.includePackages, explicit: &state.includePackagesExplicit}, "include-packages",
		"comma-separated package path prefixes; only emit diagnostics for matching packages (overrides TOML include_packages)")
	analyzer.Flags.IntVar(&state.cfgMaxStates, "cfg-max-states", defaultCFGMaxStates,
		"maximum protocol states explored before a blocking inconclusive outcome")
	analyzer.Flags.IntVar(&state.cfgWitnessMaxSteps, "cfg-witness-max-steps", defaultCFGWitnessMaxSteps,
		"maximum number of CFG witness steps encoded in inconclusive finding metadata")
	analyzer.Flags.IntVar(&state.cfgRefinementMaxIterations, "protocol-refinement-max-iterations", defaultCFGRefinementMaxIterations,
		"maximum checked protocol refinement iterations for one witness")
	analyzer.Flags.IntVar(&state.cfgFeasibilityMaxQueries, "protocol-feasibility-max-queries", defaultCFGFeasibilityMaxQueries,
		"maximum protocol feasibility queries per witness")
	analyzer.Flags.IntVar(&state.cfgFeasibilityTimeoutMS, "protocol-feasibility-timeout-ms", defaultCFGFeasibilityTimeoutMS,
		"maximum protocol feasibility query time in milliseconds")

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
	state.cfgMaxStates = defaultCFGMaxStates
	state.cfgWitnessMaxSteps = defaultCFGWitnessMaxSteps
	state.cfgRefinementMaxIterations = defaultCFGRefinementMaxIterations
	state.cfgFeasibilityMaxQueries = defaultCFGFeasibilityMaxQueries
	state.cfgFeasibilityTimeoutMS = defaultCFGFeasibilityTimeoutMS
	state.includePackagesExplicit = false
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
	state.calleeSummaryCache = sync.Map{}
	state.ssaBuilder = buildSSAForPass
	state.protocolControlFactory = nil
	state.protocolFeasibilityBackend = nil
	state.semanticEvidenceObserver = nil
}

// runConfig holds the resolved flag values for a single run() invocation.
// Reading flag bindings into this struct at run() entry ensures run()
// never reads or mutates package-level state directly.
type runConfig struct {
	configPath                    string
	configPathExplicit            bool
	baselinePath                  string
	baselinePathExplicit          bool
	emitFindingsPath              string
	emitFindingsPathExplicit      bool
	includePackages               string
	includePackagesExplicit       bool
	cfgMaxStates                  int
	cfgWitnessMaxSteps            int
	cfgRefinementMaxIterations    int
	cfgFeasibilityMaxQueries      int
	cfgFeasibilityTimeoutMS       int
	auditExceptions               bool
	checkAll                      bool
	checkValidate                 bool
	checkStringer                 bool
	checkConstructors             bool
	checkConstructorSig           bool
	checkFuncOptions              bool
	checkImmutability             bool
	checkStructValidate           bool
	checkCastValidation           bool
	checkValidateUsage            bool
	checkConstructorErrUsage      bool
	checkConstructorValidates     bool
	checkValidateDelegation       bool
	checkNonZero                  bool
	checkUseBeforeValidate        bool
	checkConstructorReturnError   bool
	checkRedundantConversion      bool
	auditReviewDates              bool
	checkEnumSync                 bool
	suggestValidateAll            bool
	checkBoundaryRequest          bool
	checkCrossPlatformPath        bool
	checkPathmatrixDivergent      bool
	checkTestHomeEnvPlatform      bool
	checkCommandWaitDelay         bool
	checkCueFedPathNativeClean    bool
	checkPathBoundaryPrefix       bool
	checkVolumeMountHostToSlash   bool
	checkCobraCommandContext      bool
	checkPathDomainNativeFilepath bool
	protocolControlFactory        func(time.Duration) protocolAnalysisControl
	protocolFeasibilityBackend    cfgFeasibilityBackend
	semanticEvidenceObserver      semanticEvidenceObserver
}

func newRunConfigForState(state *flagState) runConfig {
	rc := runConfig{
		configPath:                 state.configPath,
		configPathExplicit:         state.configPathExplicit,
		baselinePath:               state.baselinePath,
		baselinePathExplicit:       state.baselinePathExplicit,
		emitFindingsPath:           state.emitFindingsPath,
		emitFindingsPathExplicit:   state.emitFindingsPathExplicit,
		includePackages:            state.includePackages,
		includePackagesExplicit:    state.includePackagesExplicit,
		cfgMaxStates:               state.cfgMaxStates,
		cfgWitnessMaxSteps:         state.cfgWitnessMaxSteps,
		cfgRefinementMaxIterations: state.cfgRefinementMaxIterations,
		cfgFeasibilityMaxQueries:   state.cfgFeasibilityMaxQueries,
		cfgFeasibilityTimeoutMS:    state.cfgFeasibilityTimeoutMS,
		protocolControlFactory:     state.protocolControlFactory,
		protocolFeasibilityBackend: state.protocolFeasibilityBackend,
		semanticEvidenceObserver:   state.semanticEvidenceObserver,
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
	// with per-package false positives).
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
	// UBV extends cast validation with temporal use checks. Auto-enable cast
	// validation so explicit UBV flags are never silently inert.
	if rc.checkUseBeforeValidate {
		rc.checkCastValidation = true
	}
	if rc.cfgMaxStates == 0 {
		rc.cfgMaxStates = defaultCFGMaxStates
	}
	if rc.cfgWitnessMaxSteps == 0 {
		rc.cfgWitnessMaxSteps = defaultCFGWitnessMaxSteps
	}
	if rc.cfgRefinementMaxIterations == 0 {
		rc.cfgRefinementMaxIterations = defaultCFGRefinementMaxIterations
	}
	if rc.cfgFeasibilityMaxQueries == 0 {
		rc.cfgFeasibilityMaxQueries = defaultCFGFeasibilityMaxQueries
	}
	if rc.cfgFeasibilityTimeoutMS == 0 {
		rc.cfgFeasibilityTimeoutMS = defaultCFGFeasibilityTimeoutMS
	}
}
