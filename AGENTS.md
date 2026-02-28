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
| **Check baseline (regression gate)** | **`make check-baseline`** |
| **Update baseline** | **`make update-baseline`** |
| Run tests | `cd tools/goplint && go test ./goplint/` |
| Run tests (race) | `cd tools/goplint && go test -race ./goplint/` |
| Audit stale exceptions | `make build-goplint && ./bin/goplint -audit-exceptions -config=tools/goplint/exceptions.toml ./...` |
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
| Check enum CUE sync | `make build-goplint && ./bin/goplint -check-enum-sync -config=tools/goplint/exceptions.toml ./...` |
| CFA cast validation (default) | `make build-goplint && ./bin/goplint -check-cast-validation -config=tools/goplint/exceptions.toml ./...` |
| AST cast validation (fallback) | `make build-goplint && ./bin/goplint -check-cast-validation -no-cfa -config=tools/goplint/exceptions.toml ./...` |
| Audit overdue reviews | `make build-goplint && ./bin/goplint -audit-review-dates -config=tools/goplint/exceptions.toml ./...` |

## Scoped Rule Exception (Testing Parallelism)

`tools/goplint` integration tests mutate the shared `Analyzer.Flags` `FlagSet`, which is process-wide state. Because of this, tests in this package intentionally run sequentially and must not call `t.Parallel()` in those suites.

This is an explicit scoped exception to the default parallelism rule in `.agents/rules/testing.md`, not a general exception for the rest of the repository.

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
| `unused-validate-result` | `--check-validate-usage` or `--check-all` | Validate() called with result completely discarded |
| `nonzero-value-field` | `--check-nonzero` or `--check-all` | Struct field uses nonzero type as value (should be pointer) |
| `unused-constructor-error` | `--check-constructor-error-usage` or `--check-all` | Constructor NewXxx() error return assigned to blank identifier |
| `missing-constructor-validate` | `--check-constructor-validates` or `--check-all` | Constructor returns validatable type but never calls Validate() |
| `incomplete-validate-delegation` | `--check-validate-delegation` or `--check-all` | Struct with validate-all directive missing field Validate() delegation |
| `wrong-func-option-type` | `--check-func-options` or `--check-all` | WithXxx() parameter type does not match the struct field type |
| `enum-cue-missing-go` | `--check-enum-sync` | CUE disjunction member not in Go Validate() switch |
| `enum-cue-extra-go` | `--check-enum-sync` | Go Validate() switch case not in CUE disjunction |
| `stale-exception` | `--audit-exceptions` | TOML exception pattern matched nothing |
| `overdue-review` | `--audit-review-dates` | Exception with `review_after` date that has passed |
| `unknown-directive` | (always active) | Unrecognized key in `//goplint:` directive (typo detection) |

The `--check-all` flag enables `--check-validate`, `--check-stringer`, `--check-constructors`, `--check-constructor-sig`, `--check-func-options`, `--check-immutability`, `--check-struct-validate`, `--check-cast-validation`, `--check-validate-usage`, `--check-constructor-error-usage`, `--check-constructor-validates`, `--check-validate-delegation`, and `--check-nonzero` in a single invocation. CFA is enabled by default (opt out via `--no-cfa`). Deliberately excludes `--audit-exceptions`, `--audit-review-dates` (config maintenance tools with per-package false positives), and `--check-enum-sync` (requires per-type opt-in directive and CUE schema files).

## Architecture

```
tools/goplint/
├── main.go                 # singlechecker entry point + --update-baseline mode
├── exceptions.toml         # ~390 intentional exception patterns (primitives, constructors, func-options, etc.)
├── baseline.toml           # accepted findings baseline (generated)
├── goplint/
│   ├── analyzer.go                 # analysis.Analyzer + run() wiring + basic supplementary modes
│   ├── analyzer_cast_validation.go # cast validation: unvalidated DDD type conversions
│   ├── analyzer_constructor_usage.go # Constructor error usage: blanked error returns on NewXxx()
│   ├── analyzer_validate_usage.go  # Validate() usage: discarded results
│   ├── analyzer_constructor_validates.go # constructor body validation: Validate() call check
│   ├── analyzer_validate_delegation.go  # validate-all delegation completeness
│   ├── analyzer_nonzero.go          # nonzero analysis: fact export + struct field checking
│   ├── analyzer_enum_sync.go       # enum sync: CUE disjunction ↔ Go Validate() switch comparison
│   ├── analyzer_structural.go      # structural analysis: constructor-sig, func-options, immutability
│   ├── baseline.go             # baseline TOML loading + matching + writing
│   ├── config.go               # exception TOML loading + pattern matching + match counting
│   ├── inspect.go              # struct/func AST visitors + helpers
│   ├── typecheck.go            # isPrimitive() / isPrimitiveUnderlying() / isOptionFuncType()
│   ├── cfa.go                      # CFA toggle, cfg.New wrapper, DFS utilities
│   ├── cfa_cast_validation.go      # inspectUnvalidatedCastsCFA (CFA replacement for cast validation)
│   ├── cfa_closure.go              # inspectClosureCastsCFA (closure analysis with independent CFGs)
│   ├── cfa_collect.go              # collectCFACasts shared cast-collection for CFA (both outer + closure)
│   ├── *_test.go               # unit + integration tests
│   └── testdata/src/               # analysistest fixture packages (35 packages)
```

**Separate Go module**: `tools/goplint/` has its own `go.mod` to avoid adding `golang.org/x/tools` and `github.com/BurntSushi/toml` to the main project's dependencies.

## What Gets Flagged

- Struct fields: `Name string`, `Items []string`, `Data map[string]string`
- Function/method parameters: `func Foo(name string)`
- Function/method return types: `func Bar() (string, error)`

## What Does NOT Get Flagged

- **Named types**: `type CommandName string` — these ARE the DDD Value Types
- **`bool`**: Exempt by design decision — marginal DDD value
- **`[]byte`**: I/O boundary type, not a domain type
- **`error`**: Interface, not a primitive
- **Interface method signatures**: `type Service interface { ... }` — AST-level exclusion
- **`String()`/`Error()`/`GoString()`/`MarshalText()`/`MarshalBinary()`/`MarshalJSON()` returns**: Interface contract obligations
- **Test files**: `_test.go` files skipped entirely
- **`init()`/`main()`/`Test*`/`Benchmark*`/`Fuzz*`/`Example*`**: Skipped functions

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

Struct types marked with `//goplint:validate-all` opt into delegation completeness checking via `--check-validate-delegation`. The check verifies that the struct's `Validate()` method calls `.Validate()` on every field whose type has a `Validate()` method.

```go
//goplint:validate-all
type Config struct {
    Name  Name   // has Validate() — must be called in Config.Validate()
    Mode  Mode   // has Validate() — must be called in Config.Validate()
    plain int    // no Validate() — not checked
}
```

This directive only affects `--check-validate-delegation`. Without it, no delegation analysis is performed (opt-in to avoid false positives on structs with intentionally partial validation).

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

## Supplementary Modes

Seventeen additional analysis modes complement the primary primitive detection:

### `--check-all`

Enables all DDD compliance checks (`--check-validate`, `--check-stringer`, `--check-constructors`, `--check-constructor-sig`, `--check-func-options`, `--check-immutability`, `--check-struct-validate`, `--check-cast-validation`, `--check-validate-usage`, `--check-constructor-error-usage`, `--check-constructor-validates`, `--check-validate-delegation`, `--check-nonzero`) in a single invocation. CFA is enabled by default (opt out with `--no-cfa`). This is the recommended flag for comprehensive DDD compliance checks. Deliberately excludes `--audit-exceptions` and `--audit-review-dates` (config maintenance tools with per-package false positives).

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
- **Casts inside closures** (`go func() { DddType(s) }()`) — in AST mode, closure bodies are skipped to avoid false positive/negative from shared variable namespaces; with `--cfa`, closures are analyzed independently

**Conservative heuristic (AST mode):** Uses variable-name matching within a single function (excluding closures). If `x.Validate()` appears anywhere in the function body, all casts assigned to `x` are considered validated. No control-flow or ordering analysis. With `--cfa`, this heuristic is replaced by CFG path-reachability analysis.

### `--check-validate-usage`

Reports misuse patterns for `Validate()` calls on DDD Value Types:

**`unused-validate-result`**: The `error` return from `Validate()` is completely discarded:
- `x.Validate()` as a bare expression statement
- `_ = x.Validate()` where the error is assigned to a blank identifier

**What does NOT get flagged:**
- Calls on types without `Validate() error` (wrong signature)
- Calls inside closures are analyzed independently with their own parent maps

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

**What gets flagged:**
- `NewServer(addr string) (*Server, error)` where `Server` has `Validate()` but the body doesn't call it

**What does NOT get flagged:**
- Constructors that call `Validate()` anywhere in their body (any `.Validate()` selector call)
- Constructors returning types without `Validate()` (not DDD types)
- Constructors returning interfaces (may delegate validation to concrete implementations)
- Functions with `//goplint:ignore` directive

### `--check-validate-delegation`

Reports structs annotated with `//goplint:validate-all` whose `Validate()` method does not delegate to all fields that have `Validate()`. This is an opt-in check — only structs with the directive are analyzed.

**What gets flagged:**
- Field `FieldName` whose type has `Validate()` but is not called as `receiver.FieldName.Validate()` in the struct's `Validate()` method

**What does NOT get flagged:**
- Structs without `//goplint:validate-all` directive (opt-in only)
- Fields whose types do not have `Validate()` (non-validatable, skipped)
- Delegation via intermediate variable: `field := c.FieldName; field.Validate()` is recognized
- Anonymous embedded fields: `Name` (embedded type) is tracked as `c.Name.Validate()`

### `--check-nonzero`

Reports struct fields using nonzero-annotated types as value (non-pointer) fields. Types annotated with `//goplint:nonzero` indicate that their zero value is invalid — struct fields of such types should use `*Type` for optional fields. The annotation is propagated across packages via `analysis.Fact`, enabling cross-package enforcement.

**What gets flagged:**
- `Name CommandName` where `CommandName` has `//goplint:nonzero` — should be `Name *CommandName`
- Embedded fields: `CommandName` (anonymous embed of nonzero type)

**What does NOT get flagged:**
- `Name *CommandName` — pointer fields are correct for optional usage
- Fields of types without `//goplint:nonzero` — zero value is valid
- Fields with `//goplint:ignore` directive

### CFA (Control-Flow Analysis) — default for `--check-cast-validation`

CFA replaces the AST name-based heuristic in `--check-cast-validation` with CFG path-reachability analysis. Each function gets a control-flow graph (via `golang.org/x/tools/go/cfg`) and the analyzer checks whether *every* path from a type conversion to a function return passes through a `varName.Validate()` call. **CFA is enabled by default.** Use `--no-cfa` to fall back to the AST heuristic.

**What CFA catches that AST misses:**
- Conditional validation: `if strict { x.Validate() }` followed by unconditional use
- Dead-branch validation path: where Validate() is only reachable via an always-true/always-false branch that the CFG structurally includes

**What CFA does NOT check:**
- Use-before-validate ordering within a single basic block — CFA checks "path-to-return-without-validate," not temporal ordering
- Constant folding: `if false { x.Validate() }` — the CFG doesn't evaluate boolean expressions, but the non-false path to return is still detected as unvalidated

**Closure analysis:** CFA analyzes closure bodies (`FuncLit`) with independent CFGs instead of being skipped entirely. Each closure gets its own validation scope. Nested closures are analyzed recursively with compound prefixes (e.g., `"0/1"` for the second closure inside the first).

**Finding ID scheme:** CFA findings include a `"cfa"` discriminator in the stable finding ID. The AST mode (`--no-cfa`) produces different finding IDs.

**Compartmentalization rule:** CFA is a fully compartmentalized enhancement layer. CFA files (`cfa*.go`), functions, and tests are strictly separated from AST files/tests. CFA files may import shared helpers from `inspect.go` and `typecheck.go` but NEVER import from `analyzer_cast_validation.go`, and vice versa. `analyzer.go` is the only file that routes between worlds. Within CFA, `cfa_collect.go` provides `collectCFACasts()` shared by both `cfa_cast_validation.go` and `cfa_closure.go` to avoid cast-collection duplication.

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
- `--check-validate-delegation`: excepted via `pkg.StructName.FieldName.validate-delegation`
- `--check-nonzero`: excepted via `pkg.StructName.FieldName.nonzero`
- `--check-enum-sync`: excepted via `pkg.TypeName.memberValue.enum-cue-missing-go` or `pkg.TypeName.memberValue.enum-cue-extra-go`

## Baseline Comparison

The baseline system prevents DDD compliance regressions. A committed `baseline.toml` records all accepted findings; only **new** findings (not in the baseline) are reported as errors.

### Usage

```bash
make check-baseline    # Compare current state against baseline (CI gate)
make update-baseline   # Regenerate baseline from current state
```

### How it works

- **`--baseline=path`**: Analyzer flag. Loaded per-package in `run()`, suppresses findings whose stable `id` matches a baseline entry (with legacy message fallback). Only new findings are reported.
- **`--update-baseline=path`**: main() flag. Runs self as subprocess with `-json`, collects all findings, writes sorted TOML. Uses subprocess because `singlechecker.Main()` calls `os.Exit()` — no post-analysis aggregation is possible within the framework.

### Baseline TOML format

```toml
[primitive]
entries = [
    { id = "gpl1_...", message = "struct field pkg.Foo.Bar uses primitive type string" },
]

[missing-constructor]
entries = [
    { id = "gpl1_...", message = "exported struct pkg.Config has no NewConfig() constructor" },
]
```

Sections: `[primitive]`, `[missing-validate]`, `[missing-stringer]`, `[missing-constructor]`, `[wrong-constructor-sig]`, `[wrong-validate-sig]`, `[wrong-stringer-sig]`, `[missing-func-options]`, `[missing-immutability]`, `[missing-struct-validate]`, `[wrong-struct-validate-sig]`, `[unvalidated-cast]`, `[unused-validate-result]`, `[unused-constructor-error]`, `[missing-constructor-validate]`, `[incomplete-validate-delegation]`, `[nonzero-value-field]`. Empty sections are omitted.

`messages = [...]` (legacy v1 format) is still parsed for backward compatibility.

### When to update

Run `make update-baseline` after:
- Converting bare primitives to DDD Value Types
- Adding new exceptions to `exceptions.toml`
- Intentionally adding code that uses primitives at boundaries

### CI integration

The `goplint-baseline` job in `lint.yml` runs `make check-baseline`. It is a required check — any new findings not in the baseline will block the PR.

### Pre-commit hook

The `goplint-baseline` local hook in `.pre-commit-config.yaml` runs `make check-baseline` advisory (always exits 0). Install with `make install-hooks`.

## Gotchas

- **Preferred directive prefix is `goplint:`**: All new directive keys and documentation should use the full `//goplint:` prefix. The short `//plint:` prefix is supported as a convenience alias. The `//nolint:goplint` form is a golangci-lint convention and remains supported as an alias for `//goplint:ignore`.
- **Combined directives**: `//plint:ignore,internal` uses comma-separated keys after a single prefix (following the golangci-lint convention). Do NOT repeat the prefix: `//plint:ignore,plint:internal` is NOT supported. Unknown keys emit `unknown-directive` warnings.
- **Directive prefix matching is start-anchored**: `goplint:` and `plint:` are matched at the start of the comment content (after `//` and optional whitespace) using `strings.HasPrefix`, not anywhere in the text. A comment like `// see plint:ignore for details` does NOT trigger the directive. Only `//plint:ignore` or `// plint:ignore` at comment-start are recognized.
- **`types.Alias` (Go 1.22+)**: Type aliases (`type X = string`) are transparent — `isPrimitive` must call `types.Unalias()` to resolve them. Without this, aliases silently pass the linter.
- **Generic pointer receivers**: `*Container[T]` is `StarExpr{X: IndexExpr{...}}` in the AST. `receiverTypeName` must recurse through `StarExpr` to find the type name inside `IndexExpr`. A naive `StarExpr → Ident` check misses this.
- **Flag binding variables**: The `-config` and supplementary mode flags are package-level variables bound via `BoolVar`/`StringVar` (required by the `go/analysis` framework). However, `run()` never reads or mutates these directly — it reads them once via `newRunConfig()` into a local `runConfig` struct, and the `--check-all` expansion happens on the local struct. Integration tests use `Analyzer.Flags.Set()` + `resetFlags()` instead of manual save/restore. Tests must NOT use `t.Parallel()` — they share the `Analyzer.Flags` FlagSet.
- **`primitiveTypeName` needs `Unalias` too**: Even after `isPrimitive` correctly detects an alias as primitive, the diagnostic message must show the resolved type (`string`), not the alias name (`MyAlias`). Call `types.Unalias()` before `types.TypeString()`.
- **Qualified name format**: The analyzer prefixes all names with the package name (`pkg.Type.Field`, `pkg.Func.param`). Exception patterns can be 2-segment (matched after stripping the package prefix) or 3-segment (exact match).
- **CI baseline is required**: The `goplint-baseline` job in `lint.yml` is a required check that blocks merges on regressions. The `goplint` (full DDD audit) job remains advisory with `continue-on-error: true`. `make check-baseline` runs `-check-all -check-enum-sync` — enum sync is included in the baseline gate even though `--check-all` alone excludes it.
- **Per-package execution**: `go/analysis` analyzers run per-package. `--audit-exceptions` reports stale exceptions per-package — an exception that matches in package A but not package B will only be reported as stale during B's analysis. For a global stale audit, run against the full module (`./...`).
- **`findConstructorForStruct` determinism**: Prefers exact match (`"New" + structName`) over prefix matches. Among prefix matches, picks lexicographically first name. Prevents non-deterministic results from Go map iteration order when multiple variant constructors exist.
- **CFA import alias**: CFA files use `gocfg "golang.org/x/tools/go/cfg"` to avoid collision with the `*ExceptionConfig` parameter commonly named `cfg` in analyzer functions.
- **CFA compartmentalization**: `cfa*.go` files may import shared helpers from `inspect.go` and `typecheck.go` but NEVER from `analyzer_cast_validation.go`. The reverse is also true. `analyzer.go` is the sole routing point. Within CFA, `cfa_collect.go` is the shared cast-collection layer.
- **CFA `containsValidateCall` skips closures**: Does NOT descend into `*ast.FuncLit` bodies. A goroutine's `Validate()` does not validate the outer function's path. Accepted trade-off: deferred-closure `Validate()` is also not detected (suppress with `//goplint:ignore`).
- **CFA `if false` handling**: `go/cfg` does NOT perform constant folding. `if false { x.Validate() }` creates a structurally live block. However, the non-false path to return IS detected as unvalidated because the IfDone block has no Validate call.
- **CFA path semantics**: CFA checks "path-to-return-without-validate," not "use-before-validate." If `x.Validate()` appears anywhere on a path from the cast to a return block, that path is considered validated regardless of whether `x` is used before the Validate call.

## Test Architecture

- **Unit tests** (`config_test.go`, `typecheck_test.go`, `inspect_test.go`): White-box (same package), test all helper functions in isolation
- **E2E analysistest** (`analyzer_test.go`): Runs analyzer against 10 fixture packages in `testdata/src/`
- **Integration tests** (`integration_test.go`): Exercises full pipeline with TOML config loaded and supplementary modes; NOT parallel due to shared `Analyzer.Flags` state. Uses `setFlag()`/`resetFlags()` helpers for declarative flag management. Covers:
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
  - `TestCheckConstructorErrorUsage` — `--check-constructor-error-usage` mode (blanked error returns on constructors)
  - `TestCheckValidateDelegation` — `--check-validate-delegation` mode (incomplete field delegation)
  - `TestCheckConstructorValidates` — `--check-constructor-validates` mode (missing Validate calls in constructors)
  - `TestCheckNonZero` — `--check-nonzero` mode (nonzero types used as value fields)
  - `TestBaselineSupplementaryCategories` — baseline suppression for supplementary modes (validate, stringer, constructors)
- **CFA tests** (`cfa_test.go`, `cfa_integration_test.go`): Unit tests for CFG utilities and integration tests for CFA cast validation and closure analysis. NOT parallel. Covers:
  - `TestBuildFuncCFG_*` — CFG construction from function bodies
  - `TestFindDefiningBlock_*` — locating AST nodes in CFG blocks
  - `TestContainsValidateCall_*` — Validate() call detection in AST nodes
  - `TestCheckCastValidationCFA` — CFA path-reachability against `cfa_castvalidation` fixture
  - `TestCheckCastValidationCFAClosure` — CFA closure analysis against `cfa_closure` fixture
  - `TestCFADoesNotAffectCheckAll` — verifies `--check-all` does not enable `--cfa`
