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
| Missing `IsValid()` | `--check-isvalid` | `named type pkg.Foo has no IsValid() method` |
| Missing `String()` | `--check-stringer` | `named type pkg.Foo has no String() method` |
| Missing constructor | `--check-constructors` | `exported struct pkg.Foo has no NewFoo() constructor` |

### Structural and Signature Checks (`--check-all`)

| Check | Flag | Category |
|-------|------|----------|
| Constructor return mismatch | `--check-constructor-sig` | `wrong-constructor-sig` |
| Wrong `IsValid()` signature (named types) | `--check-isvalid` | `wrong-isvalid-sig` |
| Wrong `String()` signature (named types) | `--check-stringer` | `wrong-stringer-sig` |
| Missing/partial functional options | `--check-func-options` | `missing-func-options` |
| Constructor + exported mutable fields | `--check-immutability` | `missing-immutability` |
| Struct constructor without `IsValid()` | `--check-struct-isvalid` | `missing-struct-isvalid` |
| Wrong struct `IsValid()` signature | `--check-struct-isvalid` | `wrong-struct-isvalid-sig` |
| Unknown directive key typo | always on | `unknown-directive` |
| Stale exception pattern | `--audit-exceptions` | `stale-exception` |

### What Is Not Flagged

- Named types (`type CommandName string`) -- these _are_ the DDD Value Types
- `bool` -- exempt by design (marginal DDD value)
- `[]byte` -- I/O boundary type
- `error` -- interface, not a primitive
- Interface method signatures
- `String()`, `Error()`, `GoString()`, `MarshalText()`, `MarshalBinary()`, `MarshalJSON()` return types (interface contracts)
- Test files (`_test.go`), `init()`, `main()`, `Test*`, `Benchmark*` functions

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
| `Type.IsValid` | Suppress missing-isvalid check |
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

## Baseline Comparison

The baseline prevents DDD compliance regressions. A committed `baseline.toml` records all accepted findings; only **new** findings trigger errors.

### How It Works

1. `make update-baseline` generates `baseline.toml` with all current findings
2. `make check-baseline` compares the current state against `baseline.toml`
3. Findings **in** the baseline are silently suppressed
4. Findings **not** in the baseline are reported as errors (regressions)

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
    { id = "gpl1_...", message = "struct field pkg.Foo.Bar uses primitive type string" },
    { id = "gpl1_...", message = "parameter \"name\" of pkg.Func uses primitive type string" },
]

# Exported structs missing NewXxx() constructor
[missing-constructor]
entries = [
    { id = "gpl1_...", message = "exported struct pkg.Config has no NewConfig() constructor" },
]
```

`id` is the stable semantic identity used for suppression; `message` is for human readability.  
`messages = [...]` (v1 format) is still accepted for backward compatibility and used as fallback matching.

### CI Integration

The `goplint-baseline` job in `.github/workflows/lint.yml` runs `make check-baseline` on every PR. During rollout it is advisory (`continue-on-error: true`).

### Pre-commit Hook

An advisory pre-commit hook is configured in `.pre-commit-config.yaml`:

```bash
# Install hooks
make install-hooks

# The hook runs automatically on commit when Go files change
# It warns but does not block commits
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
        "url": "goplint://finding/gpl1_..."
      }
    ]
  }
}
```

`url` encodes the stable finding ID used by baseline v2.

Categories: `primitive`, `missing-isvalid`, `missing-stringer`, `missing-constructor`, `wrong-constructor-sig`, `wrong-isvalid-sig`, `wrong-stringer-sig`, `missing-func-options`, `missing-immutability`, `missing-struct-isvalid`, `wrong-struct-isvalid-sig`, `stale-exception`, `unknown-directive`.

## CLI Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `""` | Path to exceptions TOML config |
| `-baseline` | string | `""` | Path to baseline TOML (suppress known findings) |
| `-check-all` | bool | `false` | Enable all DDD checks (isvalid + stringer + constructors + structural checks) |
| `-check-isvalid` | bool | `false` | Report types missing `IsValid()` |
| `-check-stringer` | bool | `false` | Report types missing `String()` |
| `-check-constructors` | bool | `false` | Report structs missing `NewXxx()` |
| `-check-constructor-sig` | bool | `false` | Report constructors returning wrong type |
| `-check-func-options` | bool | `false` | Report missing/incomplete functional options pattern |
| `-check-immutability` | bool | `false` | Report constructor-backed structs with exported mutable fields |
| `-check-struct-isvalid` | bool | `false` | Report constructor-backed structs missing `IsValid()` |
| `-audit-exceptions` | bool | `false` | Report stale exception patterns |
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
    ├── analyzer.go         # Analyzer definition, run() orchestration, supplementary modes
    ├── baseline.go         # Baseline TOML loading, matching, writing
    ├── config.go           # Exception TOML loading, pattern matching
    ├── inspect.go          # Struct/func AST visitors, diagnostic emission
    ├── typecheck.go        # isPrimitive() / isPrimitiveUnderlying() type resolution
    ├── *_test.go           # Unit + integration tests
    └── testdata/src/       # analysistest fixture packages
```

The tool is a **separate Go module** to avoid adding `golang.org/x/tools` and `github.com/BurntSushi/toml` to the main project's dependency tree.

## Testing

```bash
# Run all tests
cd tools/goplint && go test ./goplint/

# Run with race detector
cd tools/goplint && go test -race ./goplint/

# Run a specific test
cd tools/goplint && go test -v -run TestBaselineSuppression ./goplint/
```

## License

[MPL-2.0](../../LICENSE)
