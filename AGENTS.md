# goplint

A standalone `go/analysis` analyzer that detects bare primitive types (`string`, `int`, `float64`, `[]string`, `map[string]string`, etc.) in struct fields, function parameters, and return types. Enforces the project's DDD Value Type convention where named types (e.g., `type CommandName string`) should be used instead of raw primitives.

## Purpose

Replaces the manual full-codebase scan that agents performed via `/improve-type-system`. Instead of reading every Go file, agents can run `make check-types-all-json` to get a structured JSON report of all DDD compliance gaps.

## Quick Reference

| Task | Command |
|------|---------|
| Build | `make build-goplint` |
| Run (human output) | `make check-types` |
| Run (JSON for agents) | `make check-types-json` |
| **Run all DDD checks** | **`make check-types-all`** |
| **Run all DDD checks (JSON)** | **`make check-types-all-json`** |
| **Check canonical soundness core** | **`make check-goplint-soundness-core`** |
| **Check completion proof** | **`make check-goplint-soundness-complete`** |
| **Generate retained exact-tree proof** | **`make generate-goplint-clean-tree-evidence`** |
| **Verify retained exact-tree proof** | **`make check-goplint-clean-tree-evidence`** |
| **Check mutation-kernel coverage** | **`make check-goplint-mutation-kernel-coverage`** |
| **Check production integration** | **`make check-goplint-production-integration`** |
| **Check historical counterexamples** | **`make check-goplint-counterexamples`** |
| **Check production architecture** | **`make check-goplint-architecture`** |
| **Check semantic catalog/oracles** | **`make check-semantic-spec`** |
| **Check solver-core reference model** | **`make check-goplint-protocol-oracle`** |
| **Check scheduled strict-superset oracle** | **`make check-goplint-protocol-oracle-scheduled`** |
| **Check generated-Go E2E oracle** | **`make check-goplint-end-to-end-oracle`** |
| **Check SSA refinement** | **`make check-cfg-refinement`** |
| **Check determinism** | **`make check-goplint-determinism`** |
| **Check targeted mutation** | **`make check-goplint-targeted-mutation`** |
| **Check race/repeat evidence** | **`make check-goplint-race-repeat`** |
| **Check canonical full scan** | **`make check-goplint-full-scan`** |
| **Check baseline (regression gate)** | **`make check-baseline`** |
| **Check exception governance** | **`make check-goplint-exceptions`** |
| **Update baseline** | **`make update-baseline`** |
| Run tests | `cd tools/goplint && go test ./goplint/` |
| Run tests (race) | `cd tools/goplint && go test -race ./goplint/` |
| Check missing Validate | `make build-goplint && ./bin/goplint -check-validate -config=tools/goplint/exceptions.toml ./...` |
| Check missing String | `make build-goplint && ./bin/goplint -check-stringer -config=tools/goplint/exceptions.toml ./...` |
| Check missing constructors | `make build-goplint && ./bin/goplint -check-constructors -config=tools/goplint/exceptions.toml ./...` |
| Check constructor signatures | `make build-goplint && ./bin/goplint -check-constructor-sig -config=tools/goplint/exceptions.toml ./...` |
| Check functional options | `make build-goplint && ./bin/goplint -check-func-options -config=tools/goplint/exceptions.toml ./...` |
| Check immutability | `make build-goplint && ./bin/goplint -check-immutability -config=tools/goplint/exceptions.toml ./...` |
| Check struct Validate | `make build-goplint && ./bin/goplint -check-struct-validate -config=tools/goplint/exceptions.toml ./...` |
| Check cast validation | `make build-goplint && ./bin/goplint -check-cast-validation -config=tools/goplint/exceptions.toml ./...` |
| Check Validate usage | `make build-goplint && ./bin/goplint -check-validate-usage -config=tools/goplint/exceptions.toml ./...` |
| Check constructor error usage | `make build-goplint && ./bin/goplint -check-constructor-error-usage -config=tools/goplint/exceptions.toml ./...` |
| Check constructor validates | `make build-goplint && ./bin/goplint -check-constructor-validates -config=tools/goplint/exceptions.toml ./...` |
| Check validate delegation | `make build-goplint && ./bin/goplint -check-validate-delegation -config=tools/goplint/exceptions.toml ./...` |
| Check nonzero fields | `make build-goplint && ./bin/goplint -check-nonzero -config=tools/goplint/exceptions.toml ./...` |
| Check redundant conversions | `make build-goplint && ./bin/goplint -check-redundant-conversion -config=tools/goplint/exceptions.toml ./...` |
| Check cross-platform paths | `make build-goplint && ./bin/goplint -check-cross-platform-paths -config=tools/goplint/exceptions.toml ./...` |
| Check boundary request validation | `make build-goplint && ./bin/goplint -check-boundary-request-validation -config=tools/goplint/exceptions.toml ./...` |
| Check enum CUE sync | `make build-goplint && ./bin/goplint -check-enum-sync -config=tools/goplint/exceptions.toml ./...` |
| Canonical cast validation | `make build-goplint && ./bin/goplint -check-cast-validation -config=tools/goplint/exceptions.toml ./...` |

The targeted mutation command consumes the v2 manifest and profiles under
`testdata/mutation/`. A causal kill requires the declared structured
assertion-level mismatch; compilation failure, timeout, panic, a generic test
failure, or the right test failing at the wrong assertion is invalid. Evidence
stages are derived from each mutant's `changed_stages` field.

The mutation-kernel coverage subgate binds `spec/semantic-rules.v1.json`, the
blocking v2 profile, and its mutant catalog. Every semantic category whose
`required_layers` includes `mutation` must have at least one selected causal
mutant with non-empty `changed_stages` and structured `expected_mismatches`.
The subgate emits a deterministic category-to-mutant census; uncovered
required categories cannot be exempted, baselined, excepted, or inline-ignored.

For a completion claim, run the core gate, generate the retained record, verify
it, and then run the complete gate:

```bash
make check-goplint-soundness-core
make generate-goplint-clean-tree-evidence
make check-goplint-clean-tree-evidence
make check-goplint-soundness-complete
```

Generation consumes the reviewed `clean-tree-v3.paths` and
`clean-tree-v3.json` inputs, invokes the `core` profile to avoid recursive
freshness verification, and writes only `clean-tree-run.v3.json`. Missing or
stale retained evidence is blocking and cannot be baselined, excepted, or
inline-ignored.

## Testing Parallelism

`tools/goplint` tests now use per-test analyzer instances (`NewAnalyzer()`/`newAnalyzerWithState`) instead of shared process-wide analyzer flag state. Integration suites are parallelized and guarded by a bounded `analysistest` semaphore to avoid process exhaustion on constrained runners.

## Diagnostic Categories

Each diagnostic emitted by the analyzer carries a `category` field (visible in `-json` output). Agents should use this for programmatic filtering:

| Category | Flag | Description |
|----------|------|-------------|
| `primitive` | (always active) | Bare primitive in struct field / function param / return type |
| `missing-validate` | `--check-validate` or `--check-all` | Named type missing `Validate()` method |
| `missing-stringer` | `--check-stringer` or `--check-all` | Named type missing `String()` method |
| `missing-constructor` | `--check-constructors` or `--check-all` | Exported struct missing `NewXxx()` constructor |
| `wrong-constructor-sig` | `--check-constructor-sig` or `--check-all` | Constructor `NewXxx()` returns wrong type |
| `wrong-validate-sig` | `--check-validate` or `--check-all` | Named type has `Validate()` but wrong signature |
| `wrong-stringer-sig` | `--check-stringer` or `--check-all` | Named type has `String()` but wrong signature |
| `missing-func-options` | `--check-func-options` or `--check-all` | Struct should use or complete functional options |
| `missing-immutability` | `--check-immutability` or `--check-all` | Struct with constructor has exported mutable fields |
| `missing-struct-validate` | `--check-struct-validate` or `--check-all` | Struct with constructor missing `Validate()` method |
| `wrong-struct-validate-sig` | `--check-struct-validate` or `--check-all` | Struct has `Validate()` but wrong signature |
| `unvalidated-cast` | `--check-cast-validation` or `--check-all` | Type conversion to DDD type from non-constant without `Validate()` check |
| `unvalidated-cast-inconclusive` | `--check-cast-validation` or `--check-all` | Protocol analysis could not prove cast validation safety |
| `use-before-validate-same-block` | `--check-use-before-validate` or `--check-all` | DDD Value Type variable used before Validate() in the defining CFG block |
| `use-before-validate-cross-block` | `--check-use-before-validate` or `--check-all` | DDD Value Type variable used before Validate() across successor CFG blocks |
| `use-before-validate-inconclusive` | `--check-use-before-validate` or `--check-all` | Protocol analysis could not prove UBV safety |
| `missing-constructor-error-return` | `--check-constructor-return-error` or `--check-all` | Constructor for validatable type does not return error |
| `unused-validate-result` | `--check-validate-usage` or `--check-all` | Validate() called with result completely discarded |
| `nonzero-value-field` | `--check-nonzero` or `--check-all` | Struct field uses nonzero type as value (should be pointer) |
| `unused-constructor-error` | `--check-constructor-error-usage` or `--check-all` | Constructor NewXxx() error return assigned to blank identifier |
| `missing-constructor-validate` | `--check-constructor-validates` or `--check-all` | Constructor returns validatable type but never calls Validate() |
| `missing-constructor-validate-inconclusive` | `--check-constructor-validates` or `--check-all` | Protocol analysis could not prove constructor Validate coverage |
| `incomplete-validate-delegation` | `--check-validate-delegation` or `--check-all` | Struct missing field Validate() delegation |
| `missing-struct-validate-fields` | `--check-validate-delegation` or `--check-all` | Struct with validatable fields but no Validate() method |
| `wrong-func-option-type` | `--check-func-options` or `--check-all` | WithXxx() parameter type does not match the struct field type |
| `redundant-conversion` | `--check-redundant-conversion` or `--check-all` | Type conversion with redundant intermediate basic-type hop |
| `cross-platform-path` | `--check-cross-platform-paths` or `--check-all` | V1: filepath.IsAbs(filepath.FromSlash(x)) chain misses Unix-style absolute paths on Windows. V2: filepath.IsAbs called on a `//goplint:cue-fed-path` typed value without a preceding `strings.HasPrefix(x, "/")` guard. |
| `pathmatrix-divergent-pass-relative` | `--check-pathmatrix-divergent` or `--check-all` | `pathmatrix.PassRelative` used on a platform-divergent input constant (`InputUNC`, `InputWindowsDriveAbs`, `InputWindowsRooted`) without an `OnWindows` override. Recommends `pathmatrix.PassHostNativeAbs(input)` instead. Test-side counterpart of the cross-platform-path bug class. |
| `missing-command-waitdelay` | `--check-command-waitdelay` or `--check-all` | `exec.CommandContext` command is executed or returned without setting `Cmd.WaitDelay` first. |
| `cue-fed-path-native-clean` | `--check-cue-fed-path-native-clean` or `--check-all` | Exported path validator sends CUE-fed/repo-relative input through native `filepath` cleanup before slash-normalized validation. |
| `path-boundary-prefix` | `--check-path-boundary-prefix` or `--check-all` | Path containment check uses broad `strings.HasPrefix` without an exact `..` or separator-boundary check. |
| `volume-mount-host-toslash` | `--check-volume-mount-host-toslash` or `--check-all` | Container volume mount formats a host path before `filepath.ToSlash`, producing invalid Windows mount specs. |
| `cobra-command-context` | `--check-cobra-command-context` or `--check-all` | Cobra command handler uses `context.Background()` instead of `cmd.Context()`, dropping signal cancellation and caller deadlines. |
| `unvalidated-boundary-request` | `--check-boundary-request-validation` or `--check-all` | Exported Request/Options boundary uses a validatable parameter before checked `Validate()` |
| `enum-cue-missing-go` | `--check-enum-sync` | CUE disjunction member not in Go Validate() switch |
| `enum-cue-extra-go` | `--check-enum-sync` | Go Validate() switch case not in CUE disjunction |
| `suggest-validate-all` | `--suggest-validate-all` | Advisory: struct may benefit from `//goplint:validate-all` |
| `stale-exception` | `--audit-exceptions` | TOML exception pattern matched nothing |
| `overdue-review` | `--audit-review-dates` | Exception with `review_after` date that has passed |
| `unknown-directive` | (always active) | Unrecognized key in `//goplint:` directive (typo detection) |

The `--check-all` flag enables `--check-validate`, `--check-stringer`, `--check-constructors`, `--check-constructor-sig`, `--check-func-options`, `--check-immutability`, `--check-struct-validate`, `--check-cast-validation`, `--check-validate-usage`, `--check-constructor-error-usage`, `--check-constructor-validates`, `--check-validate-delegation`, `--check-nonzero`, `--check-use-before-validate`, `--check-constructor-return-error`, `--check-redundant-conversion`, `--check-boundary-request-validation`, `--check-cross-platform-paths`, `--check-pathmatrix-divergent`, `--check-command-waitdelay`, `--check-cue-fed-path-native-clean`, `--check-path-boundary-prefix`, `--check-volume-mount-host-toslash`, and `--check-cobra-command-context` in a single invocation. `--check-all` includes the canonical protocol checks. Deliberately excludes `--audit-exceptions` and `--audit-review-dates` (config maintenance tools enforced by `make check-goplint-exceptions`), `--check-enum-sync` (requires per-type opt-in directive and CUE schema files), and `--suggest-validate-all` (advisory mode).

## Architecture

```
tools/goplint/
├── main.go                 # singlechecker entry point + --update-baseline mode
├── exceptions.toml         # governed intentional exception patterns (primitives, constructors, func-options, etc.)
├── baseline.toml           # accepted findings baseline (generated)
├── goplint/
│   ├── analyzer.go                 # default Analyzer + NewAnalyzer factory
│   ├── flags.go                    # declarative mode flag table + flag binding/newRunConfig/check-all expansion
│   ├── analyzer_run.go             # runWithState() orchestration + run input loading
│   ├── analyzer_cast_validation.go # cast validation: unvalidated DDD type conversions
│   ├── analyzer_constructor_usage.go # Constructor error usage: blanked error returns on NewXxx()
│   ├── analyzer_validate_usage.go  # Validate() usage: discarded results
│   ├── analyzer_constructor_validates.go # constructor body validation: Validate() call check
│   ├── analyzer_validate_delegation.go  # validate-all delegation completeness + universal delegation check
│   ├── analyzer_nonzero.go          # nonzero analysis: fact export + struct field checking
│   ├── analyzer_redundant_conversion.go # redundant conversion: NamedType(basic(namedExpr)) detection
│   ├── analyzer_boundary_request_validation.go # exported Request/Options boundary validation ordering
│   ├── analyzer_enum_sync.go       # enum sync: CUE disjunction ↔ Go Validate() switch comparison
│   ├── analyzer_structural.go      # structural analysis: constructor-sig, func-options, immutability
│   ├── baseline.go             # baseline TOML loading + matching + writing
│   ├── config.go               # exception TOML loading + pattern matching + match counting
│   ├── inspect.go              # struct/func AST visitors + helpers
│   ├── typecheck.go            # isPrimitive() / isPrimitiveUnderlying() / isOptionFuncType()
│   ├── cfa.go                      # shared CFG and AST path-analysis helpers
│   ├── cfa_cast_validation.go      # canonical path-sensitive cast validation
│   ├── cfa_closure.go              # inspectClosureCastsCFA (closure analysis with independent CFGs)
│   ├── cfa_collect.go              # shared cast and method-value collection
│   ├── finding_sink.go             # JSONL machine findings stream used by baseline generation
│   ├── *_test.go               # unit + integration tests
│   └── testdata/src/               # analysistest fixture packages
```

**Separate Go module**: `tools/goplint/` has its own `go.mod` to avoid adding `golang.org/x/tools` and `github.com/BurntSushi/toml` to the main project's dependencies.

## What Gets Flagged

- Struct fields: `Name string`, `Items []string`, `Data map[string]string`
- Function/method parameters: `func Foo(name string)`
- Function/method return types: `func Bar() (string, error)`

## What Does NOT Get Flagged

- **Named types**: `type CommandName string` — these ARE the DDD Value Types
- **`bool`**: Exempt by design decision — marginal DDD value
- **`error`**: Interface, not a primitive
- **Interface method signatures**: `type Service interface { ... }` — AST-level exclusion
- **`String()`/`Error()`/`GoString()`/`MarshalText()`/`MarshalBinary()`/`MarshalJSON()` returns**: Interface contract obligations
- **Test files**: `_test.go` files skipped entirely
- **`init()`/`main()`**: Skipped functions

## Exception Mechanism

Two layers, used together:

### 1. TOML Config (`exceptions.toml`) — primary

```toml
[settings]
skip_types = ["bool", "error", "context.Context", "any"]
exclude_paths = ["specs/", "internal/testutil/"]

[[exceptions]]
pattern = "ExecuteRequest.Name"
reason = "Cobra + interface boundary"

[[exceptions]]
pattern = "uroot.*.name"
reason = "display-only labels in 12+ unexported structs"
```

**Pattern syntax**: Dot-separated segments, `*` matches any single segment.
- `Type.Field` — 2-segment, matches after stripping package prefix
- `pkg.Type.Field` — 3-segment, exact match
- `*.Field` — wildcard, any type with that field name
- `pkg.*.*` — broad wildcard, all fields/params in a package

### 2. Inline Directives — fallback for one-offs

Directives use the `//goplint:` prefix (preferred) or short `//plint:` alias. Multiple directive keys can be combined with commas, following the golangci-lint convention:

```go
type Foo struct {
    Bar string //plint:ignore -- display-only (short form, preferred)
    Baz int    //nolint:goplint
    Qux string //goplint:ignore -- full prefix form
}
```

**Accepted directive forms**: `//goplint:ignore`, `//plint:ignore`, `//nolint:goplint`.

**Combined directives**: Multiple keys separated by commas (single prefix, no prefix repetition):

```go
type Server struct {
    //plint:ignore,internal -- suppress primitive finding AND exclude from func-options
    cache string
}
```

**Unknown directive keys** (typos, future keys in an old binary) emit an `unknown-directive` warning diagnostic. For example, `//goplint:ignorr` would warn about the unrecognized key `"ignorr"`.

### 3. Internal-State Directive — functional options exclusion

Fields marked with `//plint:internal` are excluded from the `--check-func-options` completeness check. Use this for fields that represent internal state (caches, mutexes, computed values) that should not be initialized via functional options.

```go
type Server struct {
    addr  string
    //plint:internal -- managed by background goroutine
    cache string
}
```

This directive only affects `--check-func-options`. Other checks (primitive detection, immutability) still apply.

When a field needs both primitive suppression and func-options exclusion, use the combined form: `//plint:ignore,internal`.

### 4. Render Directive — display text suppression

Functions marked with `//plint:render` have their return type findings suppressed. Use this for functions that intentionally return bare `string` as rendered display text. Parameters are still checked (they should be typed domain values).

```go
//plint:render
func RenderHostNotSupportedError(host HostName) string {
    return fmt.Sprintf("host %s is not supported", host)
}
```

On struct fields, `//plint:render` behaves like `//plint:ignore` (suppresses the finding entirely).

Can be combined with other directives: `//plint:render,internal`.

### 5. Validate-All Directive — delegation completeness

`--check-validate-delegation` always checks delegation completeness for structs with validatable fields. Structs marked with `//goplint:validate-all` receive the same delegation check and are also used to communicate intent.

```go
//goplint:validate-all
type Config struct {
    Name  Name   // has Validate() — must be called in Config.Validate()
    Mode  Mode   // has Validate() — must be called in Config.Validate()
    plain int    // no Validate() — not checked
}
```

This directive strengthens intent and review clarity, but delegation analysis is no longer opt-in.

### 6. Constant-Only Directive — constructor-validates exemption

Types marked with `//goplint:constant-only` are exempt from `--check-constructor-validates`. Use this for types whose `Validate()` method exists for completeness but is never called in production because all values come from compile-time constants.

```go
//goplint:constant-only
type Severity string

func (s Severity) Validate() error { ... }

// NewSeverity is NOT flagged by --check-constructor-validates
// because Severity is constant-only.
func NewSeverity(s string) (*Severity, error) { ... }
```

This directive only affects `--check-constructor-validates`. Other checks (primitive detection, validate, stringer) still apply.

### 7. Mutable Directive — immutability exemption

Struct types marked with `//goplint:mutable` are exempt from `--check-immutability`. Use this for structs that intentionally have exported mutable fields despite using a constructor.

```go
//goplint:mutable
type Builder struct {
    Output string  // exported, but no immutability diagnostic
}

func NewBuilder() *Builder { return &Builder{} }
```

This directive is struct-level — it suppresses all immutability findings for the struct's exported fields. It coexists with TOML `pkg.Struct.immutability` exceptions.

### 8. No-Delegate Directive — field-level delegation exemption

Fields marked with `//goplint:no-delegate` are excluded from `--check-validate-delegation` even though their type has a `Validate()` method. Use this for fields that are intentionally validated by external callers rather than in the struct's own `Validate()`.

```go
//goplint:validate-all
type Config struct {
    Name Name
    //goplint:no-delegate -- validated by the caller
    Mode Mode
}
```

### 9. Trusted-Boundary Directive — boundary request validation exemption

Functions marked with `//goplint:trusted-boundary` are exempt from `--check-boundary-request-validation`. Use this only when the function is a wrapper that delegates to a boundary that validates the same request/options value, or when it validates a documented partial option shape that cannot use the full `Validate()` method.

```go
//goplint:trusted-boundary -- Exec validates only fields used by exec; RunOptions.Validate requires Image for run paths.
func (e *Engine) Exec(ctx context.Context, opts RunOptions) error {
    if err := validateExecInputs(opts); err != nil {
        return err
    }
    // ...
}
```

### 10. Cue-Fed-Path Directive — cross-platform path detection (V2)

Types marked with `//goplint:cue-fed-path` carry a `CueFedPathFact` exported via `analysis.Fact` propagation. The `--check-cross-platform-paths` analyzer treats values of these types as forward-slash CUE-fed paths and flags any `filepath.IsAbs` call on such a value that lacks a preceding `strings.HasPrefix(x, "/")` guard. The V1 rule (`filepath.FromSlash → filepath.IsAbs` chain) catches the most common bug shape; V2 closes the gap when callers skip `filepath.FromSlash` and call `IsAbs` directly on a CUE-fed-typed value — semantically identical bug, different syntactic shape.

```go
// FilesystemPath holds CUE-fed forward-slash paths.
//
//goplint:cue-fed-path
type FilesystemPath string

// FLAGGED — CueFedPath value passed to filepath.IsAbs without slash guard.
func bad(p FilesystemPath) bool {
    return filepath.IsAbs(string(p))
}

// NOT FLAGGED — strings.HasPrefix("/") guard precedes filepath.IsAbs.
func good(p FilesystemPath) bool {
    s := string(p)
    if strings.HasPrefix(s, "/") {
        return true
    }
    return filepath.IsAbs(s)
}
```

Provenance is preserved through these single-arg, string-returning transformations: `string(...)`, `strings.TrimSpace`/`Trim`/`TrimLeft`/`TrimRight`/`TrimPrefix`/`TrimSuffix`, `filepath.FromSlash`/`ToSlash`/`Clean`, and `path.Clean`. Other transformations break provenance (documented false-negative).

Currently annotated:
- `pkg/types.FilesystemPath`
- `pkg/invowkfile.WorkDir`
- `pkg/invowkfile.ContainerfilePath`
- `pkg/invowkfile.ScriptContent`
- `pkg/invowkmod.SubdirectoryPath`

When adding a new type that holds a CUE-fed forward-slash path, add the directive so V2 can enforce cross-platform absoluteness invariants.

## Supplementary Modes

Nineteen additional analysis modes complement the primary primitive detection:

### `--check-all`

Enables all DDD compliance checks (`--check-validate`, `--check-stringer`, `--check-constructors`, `--check-constructor-sig`, `--check-func-options`, `--check-immutability`, `--check-struct-validate`, `--check-cast-validation`, `--check-validate-usage`, `--check-constructor-error-usage`, `--check-constructor-validates`, `--check-validate-delegation`, `--check-nonzero`, `--check-use-before-validate`, `--check-constructor-return-error`, `--check-redundant-conversion`, `--check-boundary-request-validation`, `--check-cross-platform-paths`, `--check-pathmatrix-divergent`, `--check-command-waitdelay`, `--check-cue-fed-path-native-clean`, `--check-path-boundary-prefix`, `--check-volume-mount-host-toslash`, `--check-cobra-command-context`) in a single invocation. This is the recommended flag for comprehensive DDD compliance checks. Deliberately excludes `--audit-exceptions` and `--audit-review-dates` (config maintenance tools enforced by `make check-goplint-exceptions`), `--check-enum-sync` (requires per-type opt-in directive and CUE schema files), and `--suggest-validate-all` (advisory mode).

### `--audit-exceptions`

Reports exception patterns that matched **zero locations** in the current package's analysis pass. Detects stale entries that accumulate in `exceptions.toml` after refactors remove the code they excepted.

**Limitation**: Since `go/analysis` runs per-package, `--audit-exceptions` reports stale exceptions per-package. An exception matching in package A is reported as "stale" in package B. For a true global audit, pipe output through `sort -u` or look at the package where the exception is expected to match.

### `--check-validate`

Reports named non-struct types (`type Foo string`, `type Bar int`) that lack a `Validate() error` method, or that have a `Validate()` method with the wrong signature. Only checks types backed by primitives (string, int, bool, float). Skips struct types (which use composite `Validate()` delegation), interface types, and type aliases (`type X = Y`, which inherit methods from the aliased type). For unexported types, also checks for `validate()` (lowercase), matching the project convention.

When `Validate()` exists but has a non-compliant signature (e.g., `Validate() (bool, []error)` instead of `Validate() error`), a `wrong-validate-sig` diagnostic is emitted instead of `missing-validate`.

### `--check-stringer`

Reports named non-struct types lacking a `String() string` method, or that have a `String()` method with the wrong signature. Same scope as `--check-validate`. Recognizes both value and pointer receivers.

When `String()` exists but has a non-compliant signature (e.g., `String() int` or `String(x int) string`), a `wrong-stringer-sig` diagnostic is emitted instead of `missing-stringer`.

### `--check-constructors`

Reports **exported** struct types that have no `NewXxx()` constructor function in the same package. Uses **prefix matching** — any function starting with `"New" + structName` whose return type resolves to the struct satisfies the check (e.g., `NewMetadataFromSource` satisfies `Metadata`). This eliminates false positives for variant constructors. Unexported structs and non-struct types are skipped. **Error types are automatically excluded**: structs whose name ends with `Error` or that implement the `error` interface (have an `Error() string` method) are skipped, since error types are typically constructed via struct literals.

### `--check-constructor-sig`

Reports `NewXxx()` constructor functions whose return type does not match the struct they construct. For example, `NewConfig()` must return `*Config` or `Config` — returning `*Server` is flagged. Handles multi-return patterns like `(*Config, error)` by checking the first non-error return type. **Skips interface return types** — factory functions returning interfaces (e.g., `NewEngine() Engine` where `Engine` is an interface) are a valid Go pattern and are not flagged. Only checks constructors that exist — missing constructors are `--check-constructors`' concern.

### `--check-func-options`

Two sub-checks for the [functional options pattern](https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis):

**Detection**: Flags exported structs whose `NewXxx()` constructor has more than 3 non-option parameters. Suggests converting to functional options. Skips structs that already have an option type.

**Completeness**: For structs that already have a functional options type (`type XxxOption func(*Xxx)`), verifies:
- The constructor `NewXxx()` accepts `...XxxOption` as a variadic parameter
- Each unexported field has a corresponding `WithFieldName()` function
- Fields marked with `//plint:internal` are excluded from the completeness check

Option types are detected by function signature (`func(*TargetStruct)`), not naming convention, so both `type Option func(*Server)` and `type ServerOption func(*Server)` are recognized.

### `--check-immutability`

Reports exported struct fields on types that have a `NewXxx()` constructor. If a struct uses a constructor pattern, its fields should be unexported (accessed via getter methods). Each exported field is flagged individually. Structs without constructors are not checked (they may be DTOs/config types where exported fields are intentional).

### `--check-struct-validate`

Reports **exported** struct types that have a `NewXxx()` constructor but lack a `Validate() error` method. While `--check-validate` covers non-struct named types (which define their own primitive validation), struct types need their own check because nothing enforces that constructor-backed structs validate their invariants. Error types are excluded (same logic as `--check-constructors`). When `Validate()` exists but has a non-compliant signature, a `wrong-struct-validate-sig` diagnostic is emitted instead.

### `--check-cast-validation`

Reports type conversions from raw primitives (string, int, etc.) to DDD Value Types where `Validate()` is never called on the result variable within the same function. Detects patterns like `CommandName(userInput)` where the cast produces a potentially invalid value that enters the system unchecked.

**What gets flagged:**
- `x := DddType(runtimeString)` where `x.Validate()` is never called in the function
- `return DddType(runtimeString)` — unassigned cast in a return statement
- `useFunc(DddType(runtimeString))` — unassigned cast as a function argument

**What does NOT get flagged (auto-skip contexts):**
- Casts from **constants** (`DddType("literal")`, `DddType(namedConst)`) — developer can see the value
- Casts between **named types** (`DddType(otherNamedType)`) — not a raw primitive
- Casts to types **without `Validate()`** — not DDD types
- **Map index** lookups (`m[DddType(s)]`) — invalid key returns zero/false
- **Comparison** operands (`DddType(s) == expected`) — string equality works regardless
- **Switch tag** expression (`switch DddType(s) { case ...: }`) — semantically a comparison
- **`fmt.*` function** arguments (`fmt.Sprintf("...", DddType(s))`) — display-only
- **Chained `.Validate()`** (`DddType(s).Validate()`) — validated directly on cast result
- **Error-message sources** (`DddType(err.Error())`, `DddType(fmt.Sprintf(...))`) — display text, not raw input
- **`strings.*` comparison** arguments (`strings.Contains(string(DddType(s)), "prefix")`) — comparison predicates (Contains, HasPrefix, HasSuffix, EqualFold) are semantically comparison operations
- **`bytes.*` comparison** arguments (`bytes.Contains([]byte(string(DddType(s))), ...)`) — byte-slice comparison predicates (Contains, HasPrefix, HasSuffix, EqualFold) mirror the `strings.*` exemptions
- **`slices.*` comparison** arguments (`slices.Contains(items, DddType(s))`) — membership/lookup predicates (Contains, ContainsFunc, Index, IndexFunc) are semantically comparison operations
- **`errors.*` comparison** arguments (`errors.Is(err, DddType(s))`) — error identity/type matching (Is, As) are semantically comparison operations
- **Casts that are validated on all paths before return** — closure bodies are analyzed independently.

**Protocol path analysis:** Uses SSA identity and conditional CFG-edge effects from each cast site to function returns. A cast is valid only when every successful return path carries a matching checked `Validate()` effect.

### `--check-validate-usage`

Reports misuse patterns for `Validate()` calls on DDD Value Types:

**`unused-validate-result`**: The `error` return from `Validate()` is completely discarded:
- `x.Validate()` as a bare expression statement
- `_ = x.Validate()` where the error is assigned to a blank identifier

**What does NOT get flagged:**
- Calls on types without `Validate() error` (wrong signature)
- Calls inside closures are analyzed independently with their own parent maps

### `--check-boundary-request-validation`

Reports exported error-returning functions and methods that accept validatable `Request` or `Options` structs and read those parameters before a checked `Validate()` guard. The rule catches boundary methods where a complete request invariant exists but the method consumes fields before enforcing it.

**What gets flagged:**
- `func (s *Service) Resolve(req Request) error { return use(req.Name) }`
- `func Create(opts CreateOptions) (string, error) { if opts.Name == "" { ... }; ... }`
- Passing the request/options value into an unexported helper before validating it

**What does NOT get flagged:**
- Defaulting a field before validation, for example `if req.BaseImage == "" { req.BaseImage = defaultImage }`
- Nil pointer guards that terminate before validation
- Checked `if err := req.Validate(); err != nil { return ... }` prologues
- Trusted wrappers marked with `//goplint:trusted-boundary`

**Exception keys:** `pkg.FuncName.param.boundary-request-validation` or `pkg.FuncName.boundary-request-validation`.

### `--check-constructor-error-usage`

Reports `NewXxx()` constructor calls where the error return is assigned to a blank identifier (`_`), silently discarding construction failures.

**`unused-constructor-error`**: The error return from a constructor is explicitly blanked:
- `result, _ := NewFoo(input)` — error discarded in short variable declaration
- `result, _ = NewFoo(input)` — error discarded in regular assignment

**What does NOT get flagged:**
- Functions that don't start with "New" (e.g., `ParseFoo()`)
- Functions that don't return `error` as the last return type (e.g., `NewBaz() (*Baz, int)`)
- `_, err := NewFoo()` where the value is blanked but error is captured
- Single-return constructors (e.g., `NewBar() *Bar`)
- Calls inside closures are analyzed independently

### `--check-constructor-validates`

Reports `NewXxx()` constructor functions that return types with a `Validate()` method but never call `Validate()` in their body. This enforces the `Validate() Wiring Rule` from `go-patterns.md` — constructors SHOULD call `Validate()` to enforce invariants at construction time.

This check uses the canonical path-sensitive protocol analysis. It verifies that every successful return path conditionally validates the exact returned object. A constructor that validates on only one branch (e.g., `if fast { return f, nil }` while the else branch calls `f.Validate()`) is flagged because the "fast" path skips validation.

**What gets flagged:**
- `NewServer(addr string) (*Server, error)` where `Server` has `Validate()` but the body doesn't call it
- `NewFoo(name string, fast bool) (*Foo, error)` where `Validate()` is only called on one return path
- `NewBox[T any](...) (Box[T], error)` that validates only a different instantiation (for example `Box[string]` instead of `Box[int]`)

**What does NOT get flagged:**
- Constructors that conditionally validate the returned object on all successful return paths
- Constructors returning types without `Validate()` (not DDD types)
- Constructors returning interfaces (may delegate validation to concrete implementations)
- Functions with `//goplint:ignore` directive

### `--check-validate-delegation`

Checks delegation completeness for structs with validatable fields. The mode combines directive-aware and universal semantics:

1. **Missing Validate()**: Struct has validatable fields but no `Validate()` method at all.
2. **Incomplete delegation**: Struct has `Validate()` and validatable fields but does not delegate to all of them.

Validatable fields include direct types with `Validate()`, embedded fields, and collection fields (slice/map) whose element types are validatable.

**What does NOT get flagged:**
- Structs without any validatable fields
- Error-type structs
- Fields with `//goplint:no-delegate` directive
- Complete delegation (all validatable fields delegated)
- Delegation via intermediate variable: `field := c.FieldName; field.Validate()`
- Anonymous embedded fields delegated as `c.Name.Validate()`
- Delegation via receiver helper methods, including range-loop delegation inside the helper body
- Delegation via same-package helper functions such as `appendOptionalValidation(...)` / `appendEachValidation(...)` when the helper body actually calls `Validate()` on the forwarded field or collection elements
- Direct or helper-method delegation guarded by `field != nil` or zero-value checks such as `field != ""`, `field != 0`, or `field != false` when those guards make the `Validate()` call unconditional for the non-zero case

**Exception keys:**
- Missing Validate(): `pkg.StructName.struct-validate-fields`
- Incomplete delegation: `pkg.StructName.FieldName.validate-delegation`

### `--check-nonzero`

Reports struct fields using nonzero-annotated types as value (non-pointer) fields. Types annotated with `//goplint:nonzero` indicate that their zero value is invalid — struct fields of such types should use `*Type` for optional fields. The annotation is propagated across packages via `analysis.Fact`, enabling cross-package enforcement.

**What gets flagged:**
- `Name CommandName` where `CommandName` has `//goplint:nonzero` — should be `Name *CommandName`
- Embedded fields: `CommandName` (anonymous embed of nonzero type)

**What does NOT get flagged:**
- `Name *CommandName` — pointer fields are correct for optional usage
- Fields of types without `//goplint:nonzero` — zero value is valid
- Fields with `//goplint:ignore` directive

### `--check-redundant-conversion`

Reports type conversions with a redundant intermediate basic-type hop. Detects `NamedType(basic(namedExpr))` where both the outer target and inner argument are named types sharing the same underlying type, making the intermediate conversion to the basic type (e.g., `string`, `int`) unnecessary.

**What gets flagged:**
- `TokenB(string(tokenA))` — both `TokenA` and `TokenB` have underlying type `string`
- `CountB(int(countA))` — both `CountA` and `CountB` have underlying type `int`
- `SameType(string(sameTypeVar))` — same type converting through its underlying type

**What does NOT get flagged:**
- `TokenB(tokenA)` — direct conversion, no intermediate hop
- `TokenB(string("literal"))` — inner arg is an untyped constant, not a named type
- `TokenB(string(bareString))` — inner arg is raw `string`, not a named type
- `string(tokenA)` — outer target is a basic type, not named
- `TokenB(OtherNamed(tokenA))` — intermediate is a named type (not basic), out of scope
- Conversions where outer and inner arg have different underlying types
- Functions with `//goplint:ignore` directive

**Exception key:** `pkg.FuncName.redundant-conversion`

**Bonus:** Fixing redundant intermediate conversions also eliminates false positives in `--check-cast-validation`, which sees the intermediate basic type as a "raw primitive" source.

### Canonical protocol analysis

`--check-cast-validation` has one production pipeline and no backend selector. It combines `go/cfg`, Go SSA identity, conditional validation effects, and finite IFDS/IDE-style tabulation. A `Validate()` invocation changes state only on the successor where its exact error result is proven nil.

**What path-sensitive analysis catches:**
- Conditional validation: `if strict { x.Validate() }` followed by unconditional use
- Dead-branch validation path: where Validate() is only reachable via an always-true/always-false branch that the CFG structurally includes

**What cast validation does NOT check without `--check-use-before-validate`:**
- Use-before-validate ordering within a single basic block. Enable `--check-use-before-validate` (included in `--check-all`) to detect this.
- Constant folding: `if false { x.Validate() }` — the CFG doesn't evaluate boolean expressions, but the non-false path to return is still detected as unvalidated

**Closure analysis:** Closure bodies (`FuncLit`) receive independent CFGs and validation scopes. Nested closures are analyzed recursively with compound prefixes (e.g., `"0/1"` for the second closure inside the first). Outer-path validation descends into deferred closures and immediate IIFEs, but not goroutine closures.

**Routing rule:** `analyzer_run.go` invokes the canonical protocol checks directly. `cfa_collect.go` provides shared cast and method-value discovery for declared functions and closures.

### `--check-use-before-validate`

Reports DDD Value Type variables that are used before `Validate()` is called along executable CFG paths. Enabling this check implicitly enables `--check-cast-validation`. Findings are split by category into same-block and cross-block variants.

**What counts as a "use":**
- Passing the variable as a function argument: `useFunc(x)`
- Method call on the variable where the method is not `Validate`, `String`, `Error`, or `GoString`: `x.Setup()`
- Composite literal field value: `SomeStruct{Field: x}` or `map[K]V{"k": x}`
- Channel send value: `ch <- x`

**What does NOT count as a "use":**
- `x.Validate()` — the validation call itself
- `x.String()`, `x.Error()`, `x.GoString()` — display-only methods

**Scope:** Full-path canonical escape semantics. SSA/object identities and
conditional validation effects prove ordering across blocks and calls. Relevant
unresolved effects, ambiguous aliases, or unavailable SSA are blocking
inconclusive outcomes; there is no alternate UBV or CFG backend.

The check reports both same-block and cross-block findings when a value is used before a reachable `Validate()` call on the same execution path.

**What gets flagged:**
- `x := DddType(raw); useFunc(x); x.Validate()` — use precedes validate in same block
- `x := DddType(raw); if cond { useFunc(x) }; x.Validate()` — use in a successor block precedes validate

**What does NOT get flagged:**
- `x := DddType(raw); x.Validate(); useFunc(x)` — validate precedes use
- `x := DddType(raw); fmt.Println(x.String()); x.Validate()` — String() is display-only
- Casts that already fail the `--check-cast-validation` path-to-return check — UBV is only checked when all paths DO have validate

### `--check-enum-sync`

Compares Go `Validate()` switch case literals against CUE schema disjunction members for types annotated with `//goplint:enum-cue=<CUEPath>`. The CUE schema is loaded from `*_schema.cue` files in the same package directory.

**What gets flagged:**
- `enum-cue-missing-go`: A CUE disjunction member is not present in the Go `Validate()` switch
- `enum-cue-extra-go`: A Go switch case is not present in the CUE disjunction

**What does NOT get flagged:**
- Types without the `//goplint:enum-cue=` directive (opt-in only)
- Types in packages without `*_schema.cue` files (a missing-schema diagnostic is emitted instead)

**Directive format:** `//goplint:enum-cue=#RuntimeType` where the value after `=` is a CUE path expression (e.g., `#RuntimeType`, `#FlagType`). Placed on the type declaration.

**Not included in `--check-all`** — requires per-type opt-in and only works in packages with CUE schemas.

### `--audit-review-dates`

Reports exceptions with `review_after` dates (ISO 8601 format, e.g., `"2025-12-01"`) that have passed. Use this to identify overdue exceptions that need re-evaluation. Exceptions can also have a `blocked_by` field documenting what must be resolved before the exception can be removed.

### Exception integration

All supplementary modes respect the TOML exception config:
- `--check-validate`: excepted via `pkg.TypeName.Validate`
- `--check-stringer`: excepted via `pkg.TypeName.String`
- `--check-constructors`: excepted via `pkg.StructName.constructor`
- `--check-constructor-sig`: excepted via `pkg.StructName.constructor-sig`
- `--check-func-options`: excepted via `pkg.StructName.func-options`
- `--check-immutability`: excepted via `pkg.StructName.immutability`
- `--check-struct-validate`: excepted via `pkg.StructName.struct-validate`
- `--check-cast-validation`: excepted via `pkg.FuncName.cast-validation`
- `--check-validate-usage`: excepted via `pkg.FuncName.validate-usage`
- `--check-constructor-error-usage`: excepted via `pkg.FuncName.constructor-error-usage`
- `--check-constructor-validates`: excepted via `pkg.ConstructorName.constructor-validate`
- `--check-validate-delegation`: missing Validate() excepted via `pkg.StructName.struct-validate-fields`; incomplete delegation via `pkg.StructName.FieldName.validate-delegation`
- `--check-nonzero`: excepted via `pkg.StructName.FieldName.nonzero`
- `--check-redundant-conversion`: excepted via `pkg.FuncName.redundant-conversion`
- `--check-cross-platform-paths`: excepted via `pkg.FuncName.cross-platform-path`
- `--check-pathmatrix-divergent`: excepted via `pkg.FuncName.pathmatrix-divergent`
- `--check-command-waitdelay`: excepted via `pkg.FuncName.command-waitdelay`
- `--check-cue-fed-path-native-clean`: excepted via `pkg.FuncName.cue-fed-path-native-clean`
- `--check-path-boundary-prefix`: excepted via `pkg.FuncName.path-boundary-prefix`
- `--check-volume-mount-host-toslash`: excepted via `pkg.FuncName.volume-mount-host-toslash`
- `--check-cobra-command-context`: excepted via `pkg.FuncName.cobra-command-context`
- `--check-boundary-request-validation`: excepted via `pkg.FuncName.param.boundary-request-validation` or `pkg.FuncName.boundary-request-validation`
- `--check-enum-sync`: excepted via `pkg.TypeName.memberValue.enum-cue-missing-go` or `pkg.TypeName.memberValue.enum-cue-extra-go`

## Baseline Comparison

The baseline system prevents DDD compliance regressions. A committed
`baseline.toml` records accepted definite policy findings; only **new** policy
findings (not in the baseline) are reported as errors. Protocol proof
uncertainty is never accepted debt: inconclusive categories and outcomes stay
visible and blocking regardless of baseline, configured exceptions, or inline
ignore directives.

### Usage

```bash
make check-baseline    # Compare current state against baseline (CI gate)
make update-baseline   # Regenerate baseline from current state
```

`make check-baseline` and `make update-baseline` use the same flagless canonical
protocol semantics as CI. Run `make check-goplint-soundness` before regenerating
the baseline and review stable-ID changes rather than accepting migration noise.

### How it works

- **`--baseline=path`**: Analyzer flag. Loaded per-package in `run()`, suppresses findings whose stable `id` matches a baseline entry. Only new findings are reported.
- **`--update-baseline=path`**: main() flag. Runs self as subprocess and injects `-emit-findings-jsonl=<tmp>` so baseline generation consumes machine-stable finding IDs from a JSONL stream. Uses subprocess because `singlechecker.Main()` calls `os.Exit()` — no post-analysis aggregation is possible within the framework.

### Baseline TOML format

```toml
[primitive]
entries = [
    { id = "gpl4_...", message = "struct field pkg.Foo.Bar uses primitive type string" },
]

[missing-constructor]
entries = [
    { id = "gpl4_...", message = "exported struct pkg.Config has no NewConfig() constructor" },
]
```

Sections are generated only for categories whose registry policy is
`BaselineSuppressible`; empty sections are omitted. In particular,
`[unvalidated-cast-inconclusive]`,
`[missing-constructor-validate-inconclusive]`, and
`[use-before-validate-inconclusive]` are invalid. Entries for a mixed category
such as `unvalidated-boundary-request` are also rejected when their structured
outcome is inconclusive.

`messages = [...]` (legacy v1 format) is no longer accepted; baseline files must use `entries = [{id, message}]`.
Baseline generation is fail-closed for ID integrity: suppressible diagnostics must
carry a `goplint://finding/<id>` URL; missing/invalid URLs abort parsing. Baseline
collection and update also abort if any protocol inconclusive category or
inconclusive outcome is present; resolve the analyzer precision gap or the
production code instead of moving it to another suppression surface.

### When to update

Run `make update-baseline` after:
- Converting bare primitives to DDD Value Types
- Adding new exceptions to `exceptions.toml`
- Intentionally adding code that uses primitives at boundaries

### CI integration

The `goplint`, `goplint-baseline`, `goplint-exceptions`, and `goplint-tests` jobs
in `lint.yml` are required checks. The full scan is blocking, baseline and
exception jobs govern accepted definite debt, and `goplint-tests` runs the
causal core profile with `make check-goplint-soundness-core`. The scheduled
oracle workflow runs its manifest-derived strict superset separately.

### Pre-commit hook

The local hooks in `.pre-commit-config.yaml` block on `make check-baseline`,
`make check-goplint-exceptions`, and `make check-goplint-soundness-core` for
goplint-relevant changes. Install with `make install-hooks`.

The retained exact-tree record is a completion artifact, not an ordinary CI
input. Generate it from the reviewed temporary-index synthetic tree using the
core profile, verify it with `make check-goplint-clean-tree-evidence`, then run
`make check-goplint-soundness-complete`. Never make record generation invoke
the complete profile: that would recurse into the record's own freshness
check.

## Gotchas

- **Preferred directive prefix is `goplint:`**: All new directive keys and documentation should use the full `//goplint:` prefix. The short `//plint:` prefix is supported as a convenience alias. The `//nolint:goplint` form is a golangci-lint convention and remains supported as an alias for `//goplint:ignore`.
- **Directive validation is centralized**: file, declaration, type, field,
  function, method, parameter, and statement attachments are parsed before any
  consumer runs. Unknown, incomplete, invalid, misplaced, duplicate, or
  conflicting directives emit deterministic actionable `unknown-directive`
  diagnostics. `//plint:ignore,internal` uses comma-separated keys after one
  prefix; do not repeat the prefix as `//plint:ignore,plint:internal`.
- **Directive prefix matching is start-anchored**: `goplint:` and `plint:` are matched at the start of the comment content (after `//` and optional whitespace) using `strings.HasPrefix`, not anywhere in the text. A comment like `// see plint:ignore for details` does NOT trigger the directive. Only `//plint:ignore` or `// plint:ignore` at comment-start are recognized.
- **`types.Alias` (Go 1.22+)**: Type aliases (`type X = string`) are transparent — `isPrimitive` must call `types.Unalias()` to resolve them. Without this, aliases silently pass the linter.
- **Generic pointer receivers**: `*Container[T]` is `StarExpr{X: IndexExpr{...}}` in the AST. `receiverTypeName` must recurse through `StarExpr` to find the type name inside `IndexExpr`. A naive `StarExpr → Ident` check misses this.
- **Flag state model**: `NewAnalyzer()` constructs analyzers with isolated `flagState`; there is no package-level shared analyzer instance. Bool modes are declared in `modeFlagSpecs` (`flags.go`) and used for registration, `newRunConfigForState()` snapshotting, and `--check-all` expansion to reduce wiring drift.
- **Tracked string flag semantics**: `--config`, `--baseline`, and `--include-packages` are bound with explicitness tracking. Explicit empty values for `--config`/`--baseline` are rejected during run-config validation; explicit empty `--include-packages` intentionally clears package-prefix filtering. Comma lists with empty segments (for example `a,,b`) are invalid.
- **`primitiveTypeName` needs `Unalias` too**: Even after `isPrimitive` correctly detects an alias as primitive, the diagnostic message must show the resolved type (`string`), not the alias name (`MyAlias`). Call `types.Unalias()` before `types.TypeString()`.
- **Qualified name format**: The analyzer prefixes all names with the package name (`pkg.Type.Field`, `pkg.Func.param`). Exception patterns can be 2-segment (matched after stripping the package prefix) or 3-segment (exact match).
- **CI full scan, baseline, and analyzer tests are required**: the canonical
  full DDD scan, baseline regression gate, exception governance, race tests,
  and soundness gate all block merges.
- **Per-package execution**: `go/analysis` analyzers run per-package. `--audit-exceptions` reports stale exceptions per-package — an exception that matches in package A but not package B will only be reported as stale during B's analysis. Use `make check-goplint-exceptions` for the global gate.
- **`findConstructorForStruct` determinism**: Prefers exact match (`"New" + structName`) over prefix matches. Among prefix matches, picks lexicographically first name. Prevents non-deterministic results from Go map iteration order when multiple variant constructors exist.
- **CFG import alias**: Protocol-analysis files use `gocfg "golang.org/x/tools/go/cfg"` to avoid collision with the `*ExceptionConfig` parameter commonly named `cfg` in analyzer functions.
- **SSA alias tracking is mandatory**: Canonical must-alias enrichment lets
  `y := x; y.Validate()` discharge `x` only while identity remains proven.
  Rebinding, stores, allocations, and ambiguous joins kill the fact. SSA is
  built on demand rather than declared as an analyzer prerequisite.
- **Protocol proofs stay flow-sensitive**: A `Validate()` or helper-summary
  effect applies only on the continuation where its exact error result is
  proven nil. Discarded helper results do not validate, and a nil-success edge
  must never validate the non-nil continuation. Resolve aliases at each source
  occurrence: rebinding after validation preserves the earlier proof, while
  rebinding before validation prevents it.
- **`go/cfg` builder callback**: `gocfg.New(body, mayReturn)` requires a non-nil `mayReturn` callback if the body can contain bare call-expression statements (`consume(x)`). Passing `nil` can panic during CFG construction when the builder evaluates call return behavior.
- **Canonical protocol routing**: `analyzer_run.go` invokes path-sensitive checks directly; there is no alternate backend switch. `cfa_collect.go` is the shared cast-collection layer.
- **Ordered call events**: Source syntax may associate a call with SSA, but
  semantic ordering comes only from a unique SSA caller/block/instruction
  identity. Nested and sibling calls expand into distinct call/return
  micro-nodes. Missing, duplicate, cross-block, or generic-instantiation
  ambiguity is blocking `call-mapping` uncertainty, never guessed AST order.
- **Closure procedures**: Every function literal is registered independently
  of its visible invocation shape. IIFEs, returned/stored/passed callbacks,
  method values, `go`, and `defer` closures use ordinary procedure summaries;
  unresolved capture identity is blocking uncertainty.
- **Deferred validation**: Deferred calls are effects over the canonical
  supergraph/IFDS state, applied in LIFO order at each realizable return.
  Conditional skips, error overwrites, early exits, panics, ambiguous captures,
  and unresolved calls must not discharge a constructor obligation. Do not add
  an AST assignment scanner or unconditional defer-node validation shortcut.
- **UBV closure ordering semantics**: Deferred `Validate()` runs at function
  return and therefore does not retroactively protect an earlier use.
- **Evidence credit is executable**: `spec/semantic-evidence.v2.json` names
  exact category/layer tests and expected observations; the census accepts only
  observations emitted by those executions. `spec/soundness-gate.v1.json`
  binds exact command vectors and populations. Marker strings, lexical symbol
  reachability, nonempty corpora, and successful no-op recipes are not proof.
- **No-return terminal paths**: Leaf CFG blocks ending in no-return calls (`panic`, `os.Exit`, `runtime.Goexit`, `log.Fatal*`, `testing.FailNow/Fatal*`) are treated as terminating paths, not implicit return paths. They must not trigger unvalidated-cast or constructor-validates path-to-return findings.
- **Method-value Validate tracking**: Protocol analysis and constructor-validates recognize `Validate` method values (`vf := x.Validate; vf()`) including simple alias chains (`alias := vf; alias()`). Storing a method value without calling it does not count as validation.
- **Method-expression Validate tracking**: Protocol analysis also recognizes method expressions (`vf := Type.Validate; vf(x)`) by mapping the first call argument as the receiver for Validate matching.
- **Parallel assignment aliasing**: Alias collectors that build closure/method/delegation bindings must resolve all RHS expressions against the pre-assignment state and append events after resolution. Updating alias maps incrementally during `a, b = b, a`-style assignments yields incorrect bindings.
- **Rebinding invalidation (closure vars + method values)**: Any reassignment to a tracked closure variable or method-value variable records a tombstone when the new RHS cannot be proven to preserve validation semantics. This intentionally prefers false positives over false negatives for rebinding-heavy code.
- **No-return alias tracking**: Terminal-path checks treat aliases to no-return functions (`exit := os.Exit; exit(1)`) as terminating only when the alias binding active at call-site still points to a known no-return target. Later rebinding cancels terminal inference.
- **Canonical protocol contract**: `--check-cast-validation`, `--check-constructor-validates`, and `--check-use-before-validate` always run through the single SSA/IFDS pipeline.
- **`if false` handling**: `go/cfg` does NOT perform constant folding. `if false { x.Validate() }` creates a structurally live block. The non-false path remains unvalidated and is checked during witness refinement.
- **Conditional path semantics**: A validation transfer is attached only to the continuation where the exact validation error is proven nil. `--check-use-before-validate` additionally checks temporal use ordering.
- **Trusted-boundary is narrow**: `//goplint:trusted-boundary` only suppresses `--check-boundary-request-validation`. Prefer adding a real checked `Validate()` prologue. Use the directive only for wrappers that delegate to another validating boundary, or for partial option shapes where the full `Validate()` contract is intentionally not applicable.
- **Constructor-validates protocol analysis**: `--check-constructor-validates`
  verifies every realizable successful return from its exact SSA/CFG edge,
  returned object, and returned error identity. Cross-package helpers use
  `ProtocolSummaryFact` v5, bound to the exact package/function/receiver and
  compatible signature slots with ordered conditional effects; no annotation,
  legacy fact, or unconditional compatibility fact is accepted.
- **Stable finding identity is global**: `gpl4` IDs include the full import path
  plus a source-local semantic key. Package leaf names, raw `token.Pos`,
  file-set order, and diagnostic messages are forbidden identity inputs;
  duplicate or collided IDs fail collection and baseline writes.
- **Protocol inconclusive outcomes are blocking**: Missing SSA, unresolved
  relevant calls, ambiguous identities, incompatible facts, rejected evidence,
  and exhausted state/refinement/query/time budgets report dedicated
  `*-inconclusive` categories. Witness size is bounded by
  `--cfg-witness-max-steps`; no flag suppresses uncertainty.
- **The semantic oracle contract is behavioral**: Keep
  `tools/goplint/spec/semantic-rules.v1.json` synchronized with registered owner
  keys and `semantic_spec_oracle_test.go`. Historical `must_report`,
  `must_not_report`, and `must_be_inconclusive` cases must be proven by real
  analyzer output at symbol level.
- **Analysistest limiter token lifetime**: For helpers that acquire `analysistestParallelLimiter`, release the token inside the same helper invocation (for example, `defer func() { <-analysistestParallelLimiter }()`). Using `t.Cleanup` to release helper-acquired tokens can hold slots until test completion and cause avoidable stalls when one test performs multiple analysis runs.

## Test Architecture

- **Unit tests** (`config_test.go`, `typecheck_test.go`, `inspect_test.go`): White-box (same package), test all helper functions in isolation
- **E2E analysistest** (`analyzer_test.go`): Runs analyzer against 10 fixture packages in `testdata/src/`
- **Integration tests** (`integration_test.go`): Exercises full pipeline with TOML config loaded and supplementary modes. Tests run in parallel using per-test analyzer harnesses plus a bounded `analysistest` limiter. Covers:
  - `TestAnalyzerWithConfig` — TOML exception patterns
  - `TestAnalyzerWithRealExceptionsToml` — real `exceptions.toml` parse validation
  - `TestCheckValidate` — `--check-validate` mode
  - `TestCheckStringer` — `--check-stringer` mode
  - `TestCheckConstructors` — `--check-constructors` mode
  - `TestCheckConstructorSig` — `--check-constructor-sig` mode (wrong return types)
  - `TestCheckFuncOptions` — `--check-func-options` mode (detection + completeness)
  - `TestCheckImmutability` — `--check-immutability` mode (exported fields with constructor)
  - `TestCheckStructValidate` — `--check-struct-validate` mode (missing Validate on constructor-backed structs)
  - `TestAuditExceptions` — `--audit-exceptions` stale entry detection
  - `TestCheckAll` — `--check-all` combined mode (all categories in one fixture)
  - `TestBaselineSuppression` — `--baseline` mode (known findings suppressed, new ones reported)
  - `TestCheckCastValidation` — `--check-cast-validation` mode (unvalidated DDD type conversions)
  - `TestCheckValidateUsage` — `--check-validate-usage` mode (discarded Validate results)
  - `TestCheckBoundaryRequestValidation` — `--check-boundary-request-validation` mode (request/options boundary prologues)
  - `TestCheckConstructorErrorUsage` — `--check-constructor-error-usage` mode (blanked error returns on constructors)
  - `TestCheckValidateDelegation` — `--check-validate-delegation` mode (incomplete field delegation)
  - `TestCheckConstructorValidates` — `--check-constructor-validates` mode (missing Validate calls in constructors)
  - `TestConstructorValidatesCrossPackage` — cross-package conditional protocol-summary propagation
  - `TestCheckNonZero` — `--check-nonzero` mode (nonzero types used as value fields)
  - `TestCheckRedundantConversion` — `--check-redundant-conversion` mode (redundant intermediate basic-type hops)
  - `TestCheckValidateDelegationUniversal` — `--check-validate-delegation` universal delegation coverage
  - `TestBaselineSupplementaryCategories` — baseline suppression for supplementary modes (validate, stringer, constructors)
- **Protocol path tests** (`cfa_test.go`, `cfa_integration_test.go`): Unit tests for CFG utilities and integration tests for path-sensitive cast validation and closure analysis. Suites are parallelized and use per-test analyzer instances for isolation. Covers:
  - `TestBuildFuncCFG_*` — CFG construction from function bodies
  - `TestFindDefiningBlock_*` — locating AST nodes in CFG blocks
  - `TestContainsValidateCall_*` — Validate() call detection in AST nodes
  - `TestCheckCastValidationCFA` — canonical path analysis against `cfa_castvalidation` fixture
  - `TestCheckCastValidationCFAClosure` — closure analysis against `cfa_closure` fixture
  - `TestCheckCastValidationCFAMethodValue` — method-value Validate tracking for cast validation
  - `TestCheckCastValidationCFAClosureVarAlias` — closure-variable alias execution tracking
  - `TestCheckCastValidationCFANoReturnTerminator` — no-return sink handling in CFG leaves
  - `TestCheckUseBeforeValidateCFA` — use-before-validate detection against `use_before_validate` fixture
  - `TestCheckUseBeforeValidateMethodValue` — UBV ordering with method-value Validate calls
  - `TestCheckAllEnablesCFABackedCastValidation` — verifies `--check-all` enables canonical cast validation
- **Semantic contract tests** (`semantic_spec_*.go`, `semantic_catalog_registry_test.go`): Validate catalog/schema consistency, registered owners, blocking inconclusive metadata, oracle behavior, and historical miss replay.
- **CFG benchmarks** (`cfa_bench_test.go`): Traversal benchmarks for CFG-heavy cast/UBV paths. Guardrail command: `./tools/goplint/scripts/check-cfg-bench-thresholds.sh` with thresholds in `tools/goplint/bench/thresholds.toml`.
