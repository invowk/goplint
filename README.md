# goplint

A custom Go static analyzer that enforces DDD Value Type conventions. It detects bare primitive types (`string`, `int`, `float64`, `[]string`, `map[string]string`, etc.) in struct fields, function parameters, and return types where named types should be used instead.

```go
// Flagged: bare primitive
type Config struct {
    Name string  // <- goplint reports this
}

// Correct: named DDD Value Type
type CommandName string

type Config struct {
    Name CommandName  // <- not flagged
}
```

## Quick Start

```bash
# Build the tool
make build-goplint

# Run primitive detection (human-readable output)
make check-types

# Run all DDD compliance checks
make check-types-all

# Check for regressions against baseline (CI gate)
make check-baseline

# Update baseline after type improvements
make update-baseline
```

## Commands

| Command | Description |
|---------|-------------|
| `make check-types` | Detect bare primitives (human output) |
| `make check-types-json` | Same, JSON output for tooling |
| `make check-types-all` | All DDD checks: primitives + method + constructor + structural checks |
| `make check-types-all-json` | Same, JSON output |
| `make check-semantic-spec` | Semantic contract and oracle checks for CFA-backed categories |
| `make check-ifds-compat` | Legacy-vs-IFDS no-silent-downgrade compatibility gate |
| `make check-cfg-refinement` | Phase C refinement gate: soundness, provenance, determinism |
| `make check-cfg-alias` | Phase D alias gate: prove SSA alias mode improves curated fixtures while remaining opt-in |
| `make check-baseline` | Regression gate: report only **new** findings vs baseline |
| `make update-baseline` | Regenerate `baseline.toml` from current codebase state |

## What Gets Detected

### Bare Primitives (always active)

| Location | Example | Diagnostic |
|----------|---------|------------|
| Struct field | `Name string` | `struct field pkg.Type.Name uses primitive type string` |
| Function param | `func Foo(name string)` | `parameter "name" of pkg.Foo uses primitive type string` |
| Return type | `func Bar() string` | `return value of pkg.Bar uses primitive type string` |

### Missing DDD Methods (`--check-all`)

| Check | Flag | Diagnostic |
|-------|------|------------|
| Missing `Validate()` | `--check-validate` | `named type pkg.Foo has no Validate() method` |
| Missing `String()` | `--check-stringer` | `named type pkg.Foo has no String() method` |
| Missing constructor | `--check-constructors` | `exported struct pkg.Foo has no NewFoo() constructor` |

### Structural and Signature Checks (`--check-all`)

| Check | Flag | Category |
|-------|------|----------|
| Constructor return mismatch | `--check-constructor-sig` | `wrong-constructor-sig` |
| Wrong `Validate()` signature (named types) | `--check-validate` | `wrong-validate-sig` |
| Wrong `String()` signature (named types) | `--check-stringer` | `wrong-stringer-sig` |
| Missing/partial functional options | `--check-func-options` | `missing-func-options` |
| Constructor + exported mutable fields | `--check-immutability` | `missing-immutability` |
| Struct constructor without `Validate()` | `--check-struct-validate` | `missing-struct-validate` |
| Wrong struct `Validate()` signature | `--check-struct-validate` | `wrong-struct-validate-sig` |
| Unknown directive key typo | always on | `unknown-directive` |
| Stale exception pattern | `--audit-exceptions` | `stale-exception` |

### What Is Not Flagged

- Named types (`type CommandName string`) -- these _are_ the DDD Value Types
- `bool` -- exempt by design (marginal DDD value)
- `error` -- interface, not a primitive
- Interface method signatures
- `String()`, `Error()`, `GoString()`, `MarshalText()`, `MarshalBinary()`, `MarshalJSON()` return types (interface contracts)
- Test files (`_test.go`), `init()`, `main()`

## Exceptions

When a bare primitive is intentional (exec boundaries, display-only fields, import cycle prevention), suppress it with an exception.

### TOML Config (`exceptions.toml`) -- preferred

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

**Pattern syntax** (dot-separated segments, `*` = single-segment wildcard):

| Pattern | Matches |
|---------|---------|
| `Type.Field` | Any package's `Type.Field` |
| `pkg.Type.Field` | Exact match |
| `*.Field` | Any type's `Field` |
| `pkg.*.*` | All fields/params in `pkg` |
| `Func.param` | Function parameter by name |
| `Func.return.0` | Unnamed return by position (0-indexed) |
| `Type.Validate` | Suppress missing-validate check |
| `Type.String` | Suppress missing-stringer check |
| `Type.constructor` | Suppress missing-constructor check |

### Inline Directives -- fallback for one-offs

```go
type Foo struct {
    Bar string //goplint:ignore -- display-only
    Baz int    //nolint:goplint
}
```

### Auditing Stale Exceptions

After refactors remove excepted code, entries in `exceptions.toml` become stale:

```bash
make build-goplint
./bin/goplint -audit-exceptions -config=tools/goplint/exceptions.toml ./... 2>&1 | sort -u
```

> Note: `--audit-exceptions` reports per-package (a `go/analysis` limitation). Pipe through `sort -u` for deduplicated results.

For CI-friendly global stale detection:

```bash
make build-goplint
./bin/goplint -audit-exceptions -global -config=tools/goplint/exceptions.toml ./...
```

`-global` aggregates stale patterns by package coverage and exits non-zero when globally stale patterns are found.

## Baseline Comparison

The baseline prevents DDD compliance regressions. A committed `baseline.toml` records all accepted findings; only **new** findings trigger errors.

### How It Works

1. `make update-baseline` generates `baseline.toml` with all current findings
2. `make check-baseline` compares the current state against `baseline.toml`
3. Findings **in** the baseline are silently suppressed
4. Findings **not** in the baseline are reported as errors (regressions)

`make check-baseline` and `make update-baseline` pin `-cfg-interproc-engine=legacy`
to keep baseline suppression deterministic while IFDS default rollout is guarded by
`make check-ifds-compat`, Phase C precision by `make check-cfg-refinement`, and
Phase D alias precision by `make check-cfg-alias`.

### Workflow

```bash
# After converting types or adding exceptions, shrink the baseline:
make update-baseline
git add tools/goplint/baseline.toml
git commit -m "chore(tools): update goplint baseline"

# Verify no regressions:
make check-baseline
```

### Baseline Format

```toml
# Bare primitive type usage
[primitive]
entries = [
    { id = "gpl3_...", message = "struct field pkg.Foo.Bar uses primitive type string" },
    { id = "gpl3_...", message = "parameter \"name\" of pkg.Func uses primitive type string" },
]

# Exported structs missing NewXxx() constructor
[missing-constructor]
entries = [
    { id = "gpl3_...", message = "exported struct pkg.Config has no NewConfig() constructor" },
]
```

`id` is the stable semantic identity used for suppression; `message` is for human readability.  
Suppression is strict ID-only. Baselines must use `entries = [...]`; legacy `messages = [...]` sections are rejected during parsing.
Baseline generation is fail-closed for ID integrity: suppressible diagnostics must
carry a `goplint://finding/<id>` URL; missing/invalid URLs abort parsing.

### CI Integration

The `goplint-baseline` and `goplint-tests` jobs in `.github/workflows/lint.yml` are required checks. `goplint-baseline` runs `make check-baseline` on every PR as the regression gate, while `goplint-tests` runs nested-module analyzer tests plus the semantic/IFDS/Phase C/Phase D script gates, including `make check-cfg-alias`.

### Pre-commit Hooks

Blocking local pre-commit hooks are configured in `.pre-commit-config.yaml`:

```bash
# Install hooks
make install-hooks

# The hooks run automatically on commit for goplint-relevant changes
# They enforce both the legacy baseline gate and the semantic/IFDS/Phase C/Phase D behavior gates
```

## JSON Output

The `-json` flag (provided by the `go/analysis` framework) produces structured output for programmatic consumption:

```bash
make check-types-json       # primitives only
make check-types-all-json   # all DDD checks
```

Each diagnostic includes a `category` field for filtering:

```json
{
  "github.com/invowk/invowk/pkg/invowkfile": {
    "goplint": [
      {
        "posn": "pkg/invowkfile/types.go:42:5",
        "message": "struct field invowkfile.Foo.Bar uses primitive type string",
        "category": "primitive",
        "url": "goplint://finding/gpl3_..."
      }
    ]
  }
}
```

`url` encodes the stable finding ID used by baseline suppression and may include machine-readable query metadata (for example `ubv_scope=same-block|cross-block` and witness fields).

Categories: `primitive`, `missing-validate`, `missing-stringer`, `missing-constructor`, `wrong-constructor-sig`, `missing-func-options`, `missing-immutability`, `wrong-validate-sig`, `wrong-stringer-sig`, `missing-struct-validate`, `wrong-struct-validate-sig`, `unvalidated-cast`, `unvalidated-cast-inconclusive`, `unused-validate-result`, `unused-constructor-error`, `missing-constructor-validate`, `missing-constructor-validate-inconclusive`, `incomplete-validate-delegation`, `nonzero-value-field`, `wrong-func-option-type`, `enum-cue-missing-go`, `enum-cue-extra-go`, `use-before-validate-same-block`, `use-before-validate-cross-block`, `use-before-validate-inconclusive`, `suggest-validate-all`, `missing-constructor-error-return`, `redundant-conversion`, `missing-struct-validate-fields`, `unknown-directive`, `stale-exception`, `overdue-review`.

## CLI Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `""` | Path to exceptions TOML config |
| `-baseline` | string | `""` | Path to baseline TOML (suppress known findings) |
| `-check-all` | bool | `false` | Enable all DDD checks (validate + stringer + constructors + structural checks) |
| `-check-validate` | bool | `false` | Report types missing `Validate()` |
| `-check-stringer` | bool | `false` | Report types missing `String()` |
| `-check-constructors` | bool | `false` | Report structs missing `NewXxx()` |
| `-check-constructor-sig` | bool | `false` | Report constructors returning wrong type |
| `-check-func-options` | bool | `false` | Report missing/incomplete functional options pattern |
| `-check-immutability` | bool | `false` | Report constructor-backed structs with exported mutable fields |
| `-check-struct-validate` | bool | `false` | Report constructor-backed structs missing `Validate()` |
| `-ubv-mode` | string | `"escape"` | UBV semantics mode: `order` or `escape` |
| `-cfg-backend` | string | `"ssa"` | Path-analysis backend selector: `ssa` or `ast` |
| `-cfg-interproc-engine` | string | `"ifds"` | Interprocedural engine selector: `legacy`, `ifds`, or `compare` |
| `-cfg-feasibility-engine` | string | `"off"` | Phase C feasibility engine: `off` or `smt` |
| `-cfg-refinement-mode` | string | `"off"` | Phase C refinement mode: `off`, `once`, or `cegar` |
| `-cfg-refinement-max-iterations` | int | `3` | Maximum Phase C refinement iterations for one witness |
| `-cfg-feasibility-max-queries` | int | `16` | Maximum Phase C feasibility queries per witness |
| `-cfg-feasibility-timeout-ms` | int | `200` | Maximum Phase C feasibility query time in milliseconds |
| `-cfg-alias-mode` | string | `"off"` | Phase D alias tracking: `off` or `ssa` (SSA-based must-alias enrichment) |
| `-cfg-max-states` | int | `20000` | Maximum CFG states explored before conservative fallback |
| `-cfg-max-depth` | int | `512` | Maximum CFG DFS depth before conservative fallback |
| `-cfg-inconclusive-policy` | string | `"error"` | Inconclusive CFA policy: `error`, `warn`, or `off` |
| `-cfg-witness-max-steps` | int | `12` | Maximum CFG witness steps encoded in inconclusive metadata |
| `-audit-exceptions` | bool | `false` | Report stale exception patterns |
| `-global` | bool | `false` | Aggregate `-audit-exceptions` globally and fail on globally stale patterns |
| `-update-baseline` | string | `""` | Generate baseline TOML at the given path |
| `-json` | bool | `false` | JSON output (built-in from `go/analysis`) |

## Architecture

```
tools/goplint/
├── main.go                 # Entry point + --update-baseline subprocess mode
├── exceptions.toml         # Intentional primitive exceptions (~85 patterns)
├── baseline.toml           # Accepted findings baseline (generated)
├── go.mod                  # Separate Go module (avoids polluting main go.mod)
└── goplint/
    ├── analyzer.go         # Analyzer definition + shared supplementary-mode helpers
    ├── flags.go            # Declarative mode flag table + registration/newRunConfig/check-all
    ├── analyzer_run.go     # run() orchestration phases (inputs/traversal/post-checks)
    ├── analyzer_cast_validation.go # AST cast-validation heuristics + auto-skip contexts
    ├── cfa*.go             # CFA layer for path-sensitive cast and UBV checks
    ├── baseline.go         # Baseline TOML loading, matching, writing
    ├── config.go           # Exception TOML loading, pattern matching
    ├── inspect.go          # Struct/func AST visitors, diagnostic emission
    ├── typecheck.go        # isPrimitive() / isPrimitiveUnderlying() type resolution
    ├── *_test.go           # Unit + integration tests
    └── testdata/src/       # analysistest fixture packages
```

The tool is a **separate Go module** to avoid adding `golang.org/x/tools` and `github.com/BurntSushi/toml` to the main project's dependency tree.

### CFA Notes

- `--check-cast-validation`, `--check-constructor-validates`, and `--check-use-before-validate` are CFA-only checks.
- CFA is always enabled for those checks; there is no CFA opt-out flag.
- `--check-use-before-validate` emits split categories: `use-before-validate-same-block` and `use-before-validate-cross-block`.
- `--cfg-interproc-engine=legacy|ifds|compare` selects the interprocedural solver path for cast/UBV/constructor-validates checks.
- `--cfg-interproc-engine=compare` runs legacy and IFDS solvers and fails on forbidden `legacy -> safe` silent downgrades.
- Default rollout is now `ifds`; keep `compare` compatibility checks and benchmark gates active for regression monitoring.
- Phase C is opt-in and IFDS-only: `--cfg-feasibility-engine=smt` must be paired with `--cfg-refinement-mode=once|cegar`.
- Phase C uses a narrow predicate vocabulary. `sat` keeps the witness live, `unsat` can discharge the current witness, and unsupported or timed-out queries degrade to `unknown`, never `safe`.
- CFA budget truncation and recursion-summary cycles emit inconclusive categories (`unvalidated-cast-inconclusive`, `use-before-validate-inconclusive`, `missing-constructor-validate-inconclusive`) with `cfg_*` metadata.
- `--cfg-inconclusive-policy` controls inconclusive emission: `error` (default), `warn` (emits with warning metadata), `off` (suppresses inconclusive findings).
- Inconclusive metadata includes bounded witness fields (`cfg_witness_kind`, `cfg_witness_blocks`, `cfg_witness_edges`, `cfg_witness_call_chain`, plus compatibility keys `witness_cfg_path`, `witness_cfg_steps`, `witness_cfg_truncated`) capped by `--cfg-witness-max-steps`.
- Refined findings add feasibility/refinement metadata (`cfg_feasibility_engine`, `cfg_feasibility_result`, `cfg_refinement_status`, `cfg_refinement_iterations`, `cfg_refinement_trigger`, `cfg_refinement_witness_hash`).
- `-emit-findings-jsonl` now also writes `kind=refinement-trace` records for Phase C-evaluated non-safe witnesses so gates can audit refined and retained outcomes that did not become user-facing findings.
- `--cfg-alias-mode=ssa` is Phase D's opt-in SSA must-alias upgrade. It stays out of `--check-all`; `make check-cfg-alias` proves the curated alias-fixture delta by keeping alias mode `off` as the control run and requiring only the `ssa` run to discharge the copy/multi-hop alias cases.
- IFDS unresolved-call handling is target-relevant: unknown helper calls only keep a finding inconclusive when the unresolved call could operate on the tracked cast/return target, so unrelated helpers like `filepath.Join`, `len`, or `copy` do not poison otherwise validated paths.
- `--ubv-mode=order` uses strict ordering semantics; `--ubv-mode=escape` focuses on values escaping before validation.
- `--ubv-mode=escape` uses recursion-safe interprocedural first-argument summaries to treat helper calls as validation only when the callee validates before escaping that argument.
- `--cfg-backend=ssa` uses type-aware no-return pruning; `--cfg-backend=ast` is conservative and treats calls as may-return.
- Auto-skip for index expressions is map-only (map lookups), not slice/array indexing.
- UBV checks treat immediate IIFEs as synchronous ordering context; deferred `Validate()` does not suppress use-before-validate findings.

## Testing

```bash
# Run all tests
cd tools/goplint && go test ./goplint/

# Run with race detector
cd tools/goplint && go test -race ./goplint/

# Run a specific test
cd tools/goplint && go test -v -run TestBaselineSuppression ./goplint/

# Run CFG-heavy benchmarks
cd tools/goplint && go test -run '^$' -bench '^BenchmarkCFGTraversal' ./goplint

# Check benchmark thresholds
./tools/goplint/scripts/check-cfg-bench-thresholds.sh

# Check Phase A semantic contracts
./tools/goplint/scripts/check-semantic-spec.sh

# Check Phase D alias opt-in behavior
./tools/goplint/scripts/check-cfg-alias.sh
```

## Soundness Docs

- `docs/goplint/current-techniques-and-semantics.md`
- `docs/goplint/state-of-the-art-soundness-roadmap.md`
- `docs/goplint/phase-a-implementation-plan.md`
- `docs/goplint/phase-b-implementation-plan.md`
- `docs/goplint/semantic-rule-spec-phase-a.md`
- `docs/goplint/semantic-rule-spec-phase-b.md`

## License

[MPL-2.0](../../LICENSE)
