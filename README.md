# goplint

`goplint` is Invowk's Go static analyzer for DDD value-type structure and
value-protocol correctness. It is a standalone Go module.

The analyzer combines three families of checks:

- structural checks over Go syntax and types, such as primitive fields,
  constructor signatures, immutability, and `Validate`/`String` contracts;
- protocol checks proving that values are validated, validation errors control
  continuation, constructors validate the object they return, and values do
  not escape before validation;
- cross-artifact checks, including Go/CUE enum synchronization, exception
  governance, and versioned cross-package protocol facts.

## Quick start

```bash
make build-goplint
make check-types
make check-types-all
make check-goplint-soundness
make check-baseline
```

The main commands are:

| Command | Purpose |
|---|---|
| `make check-types` | Report bare primitive usage |
| `make check-types-all` | Run all DDD checks |
| `make check-goplint-soundness` | Route staged changes through the four-class ownership taxonomy and run the selected resource-aware profile |
| `make check-goplint-docs` | Run the static docs-guard documentation tier (no analyzer execution or repository audit) |
| `make check-goplint-soundness-consumer` | Force the one-audit consumer profile; this makes no analyzer-soundness claim |
| `make check-goplint-soundness-harness` | Force the harness profile (consumer surface plus module tests and executor parity) |
| `make check-goplint-harness-parity` | Require byte-identical normalized run reports from the plan-serial and parallel executors on the reviewed fixture |
| `make check-goplint-module-tests` | Run the goplint module test suite |
| `make check-goplint-soundness-semantic` | Force the full semantic soundness profile |
| `make check-goplint-soundness-complete` | Run the completion profile, including retained exact-tree freshness |
| `make generate-goplint-clean-tree-evidence` | Generate the retained v4 dual-digest run record from the reviewed paths and plan |
| `make generate-goplint-clean-tree-evidence-reusing` | Generate the retained record from an aggregate report already retained for this exact tree instead of re-executing it (local iteration only) |
| `make rebind-goplint-clean-tree-evidence` | Re-bind the retained record after prose-only drift, carrying the aggregate report forward |
| `make check-goplint-clean-tree-evidence` | Verify the retained exact-tree proof without changing the caller's index or worktree |
| `make check-goplint-clean-tree-evidence-allowing-reuse` | Verify while accepting a reused-aggregate record; local iteration only, never a completion claim |
| `make check-goplint-mutation-kernel-coverage` | Verify causal mutant coverage for every mutation-required semantic category |
| `make check-goplint-production-integration` | Exercise the canonical domain and uncertainty reasons through real analyzer paths |
| `make check-goplint-counterexamples` | Run historical soundness counterexamples against the real analyzer |
| `make check-goplint-architecture` | Reject alternate, fallback, compatibility, or legacy production semantics |
| `make check-semantic-spec` | Validate the semantic catalog and behavioral oracles |
| `make check-goplint-protocol-oracle` | Compare the solver-core component with the independent bounded reference model |
| `make check-goplint-protocol-oracle-scheduled` | Run the manifest-derived strict-superset oracle profile used by scheduled CI |
| `make check-goplint-end-to-end-oracle` | Compare generated Go through extraction, SSA, propagation, aggregation, and diagnostics with the independent reference |
| `make check-goplint-fuzz-seeds` | Replay committed fuzz corpora deterministically |
| `make check-cfg-refinement` | Validate SSA constraint extraction, evidence checking, and refinement |
| `make check-goplint-determinism` | Compare normalized outputs across repeated and reordered runs |
| `make check-goplint-targeted-mutation` | Require every versioned soundness mutant to be killed |
| `make check-goplint-race-repeat` | Run the reviewed race and repeat-count evidence |
| `make update-goplint-race-repeat-timings` | Refresh the reviewed three-sample weighted test timing manifest |
| `make check-goplint-full-scan` | Run the blocking canonical production scan |
| `make check-goplint-performance-smoke` | Run one-sample catastrophic-regression smoke; this is not certification |
| `make check-goplint-benchmarks` | Enforce five-sample runner-class performance certification |
| `make check-baseline` | Reject findings absent from the accepted baseline |
| `make update-baseline` | Regenerate the baseline using canonical semantics |

For a completion claim, use the exact proof sequence:

```bash
make check-goplint-soundness-semantic
make generate-goplint-clean-tree-evidence   # or: make rebind-goplint-clean-tree-evidence
make check-goplint-clean-tree-evidence
make check-goplint-soundness-complete
```

Generation consumes the reviewed v4 path selection and command plan, invokes
the `semantic` profile rather than `complete` to avoid recursive freshness
verification, and writes only the retained `clean-tree-run.v5.json`
dual-digest record. During the iteration loop only,
`make generate-goplint-clean-tree-evidence-reusing` admits an aggregate report
the caller already retained: it must bind the synthetic tree (profile,
byte-equal manifest digest, exact registration census, and the recomputed
workspace digest covering the tracked registry file) and must come with the
companion `<report>.binding.json` that pins the report's canonical decoded
form, the registry byte
digest, and the producing Go toolchain. Any mismatch fails closed instead of
re-executing. The resulting record carries
`provenance.kind: "reused-aggregate"`, which the freshness verifier refuses
without `-allow-reused-aggregate`, so a completion claim still needs fresh
aggregate execution. When only documentation-class prose
drifted since a valid record, `make rebind-goplint-clean-tree-evidence`
re-binds it in seconds; semantic-content drift makes re-binding fail closed
naming the drifted paths. Missing or stale retained evidence cannot be
baselined, excepted, or inline-ignored.

Profile routing, resource discovery and overrides, timing refresh, smoke versus
certification, immutable plan/bundle schemas, CI reproduction, telemetry, and
distributed failure diagnostics are documented in
[`../../docs/goplint/soundness-gate-execution.md`](docs/goplint/soundness-gate-execution.md).

The mutation-kernel coverage subgate binds the semantic-rules catalog, the
blocking v2 profile, and its mutant catalog. Each category whose semantic rule
requires the `mutation` layer must be covered by a selected causal mutant with
stage and structured assertion-mismatch metadata. Uncovered required
categories cannot be exempted, baselined, excepted, or inline-ignored.

## Canonical protocol pipeline

Protocol checks have one production pipeline and no runtime engine selector:

1. Go syntax and `go/types` identify raw sources, validatable values,
   constructors, methods, and result slots.
2. Go SSA assigns package-qualified value and abstract-object identities.
   Flow-sensitive must-alias facts are killed by rebinding, stores, new
   allocations, and ambiguous joins.
3. Each function and function literal is registered as an analyzable
   procedure. Source calls are associated with unique SSA call instructions
   and expanded into call/matching-return micro-nodes in SSA evaluation order;
   ambiguous relevant mappings are blocking inconclusive outcomes.
4. A deterministic interprocedural supergraph models matched returns,
   call-to-return effects, summaries, recursion, known non-returning calls,
   escaping closures, and deferred calls applied in LIFO order at realizable
   returns.
5. IFDS typestate and escape facts are combined with IDE-style conditional
   validation/error relations. A `Validate()` call changes state only on an
   edge proving its associated error is nil.
6. Candidate witnesses are replayed against the supported SSA constraint
   fragment. Only independently checked UNSAT evidence discharges a witness;
   SAT retains it and unsupported or exhausted reasoning is inconclusive.

The supported constraint fragment is documented in
[`spec/ssa-constraints.md`](spec/ssa-constraints.md). The protocol lattice,
identity boundary, joins, and result vocabulary are documented in
[`spec/protocol-domain.md`](spec/protocol-domain.md).

### Result vocabulary

- `violation`: a feasible unsafe path or definite unmet obligation;
- `inconclusive`: a relevant obligation could not be proved because identity,
  call effects, SSA, facts, evidence, or a resource budget were insufficient;
- `discharged-infeasible`: checked UNSAT evidence proved that one candidate
  witness cannot execute. This is evidence, not a suppressible finding.

Uncertainty is always blocking for protocol checks. There is no warning,
exception, inline-ignore, or baseline policy that turns an inconclusive
protocol outcome into success. Ordinary exception and baseline mechanisms are
available only for definite policy findings, never for proof uncertainty.

### Property boundary

The solver is deliberately finite. It supports SSA-versioned nil, boolean,
string, and integer equality/inequality atoms with normalized negation,
conjunction, and disjunction. Pointer/interface predicates, unsupported
operations, unresolved relevant calls, ambiguous may-alias identities,
incompatible facts, missing SSA, rejected evidence, and exhausted budgets are
reported as inconclusive rather than assumed safe.

The virtual runtime security model is unrelated to this analysis: goplint
proves only the documented source-level properties and is not a runtime
sandbox or general theorem prover.

## Resource controls

Resource flags bound work but cannot disable semantic layers:

| Flag | Default | Meaning on exhaustion |
|---|---:|---|
| `-cfg-max-states` | `20000` | blocking inconclusive |
| `-cfg-witness-max-steps` | `12` | truncates explanation metadata only |
| `-protocol-refinement-max-iterations` | `3` | blocking inconclusive |
| `-protocol-feasibility-max-queries` | `16` | blocking inconclusive |
| `-protocol-feasibility-timeout-ms` | `5000` | blocking inconclusive |

The analyzer rejects removed compatibility flags as unknown. In particular,
there is no selectable protocol backend, interprocedural engine, alias mode,
feasibility engine, refinement mode, UBV semantic mode, or inconclusive policy.

## Structural checks

Examples of findings include:

| Check | Example category |
|---|---|
| Bare primitive field, parameter, or result | `primitive` |
| Missing or malformed value-type methods | `missing-validate`, `wrong-validate-sig`, `missing-stringer` |
| Missing or malformed constructors | `missing-constructor`, `wrong-constructor-sig` |
| Mutable constructor-backed structs | `missing-immutability` |
| Unchecked raw-to-value conversion | `unvalidated-cast` |
| Value escape before validation | `use-before-validate-same-block`, `use-before-validate-cross-block` |
| Constructor returns an unvalidated object | `missing-constructor-validate` |
| Protocol proof cannot complete | corresponding `*-inconclusive` category |
| Discarded validation or constructor error | `unused-validate-result`, `unused-constructor-error` |
| Go/CUE enum drift | `enum-cue-missing-go`, `enum-cue-extra-go` |

Named value types, interface contract methods, test functions, `init`, and
`main` are exempt where documented by their rule. Use `-help` on the built
analyzer for the authoritative list of structural check flags.

## Exceptions

Intentional boundaries belong in `exceptions.toml`:

```toml
[[exceptions]]
pattern = "ExecuteRequest.Name"
reason = "Cobra adapter boundary"
```

Inline exceptions are reserved for narrow local cases:

```go
type Request struct {
    DisplayLabel string //goplint:ignore -- presentation-only boundary
}
```

Run `make check-goplint-exceptions` to reject stale patterns and overdue review
dates. Prefer precise patterns and actionable reasons.

## Baseline semantics

`baseline.toml` records accepted findings by stable semantic ID:

```toml
[primitive]
entries = [
  { id = "gpl4_...", message = "struct field pkg.Type.Field uses primitive type string" },
]
```

Suppression is ID-only; the message is review context. The obsolete
`messages = [...]` format is rejected. Baseline generation fails closed if a
suppressible diagnostic lacks a valid `goplint://finding/<id>` URL. Protocol
inconclusive categories and any finding carrying an inconclusive outcome are
always visible: baseline parsing and update reject them even when their stable
IDs and messages are otherwise valid.

The current `gpl4` identity includes the full import path and a source-local
semantic key. Package leaf names, raw token positions, file-set order, and
diagnostic prose are not identity inputs. Duplicate emission or a collided ID
fails collection and baseline writing instead of silently replacing a record.

Both `make check-baseline` and `make update-baseline` use the same flagless
canonical protocol semantics. Update the baseline only after the semantic
soundness profile passes, and review stable-ID changes as semantic migrations
rather than accepting unexplained churn.

## Structured evidence

JSON diagnostics retain the category and stable finding URL. Internal JSONL
records also carry normalized protocol reasons, witnesses, fact versions, SSA
subjects, refinement iterations, and evidence digests. Protocol metadata uses
the live vocabulary `violation`, `inconclusive`, and
`discharged-infeasible`.

The semantic catalog at [`spec/semantic-rules.v1.json`](spec/semantic-rules.v1.json)
maps every category to a registered implementation owner and an independent
oracle. The typed evidence registry at
[`spec/semantic-evidence.v2.json`](spec/semantic-evidence.v2.json) binds every
category and evidence layer to an executable test and exact expected
observation. The census consumes observations emitted by those executions and
rejects missing, duplicate, extra, stale, marker-only, or zero-population
credit.

Mutation-layer stage credit is generated from
[`testdata/mutation/soundness-mutants-v2.json`](testdata/mutation/soundness-mutants-v2.json),
not from a fixed stage list. Each mutant declares its changed stages, exact
source anchor and hashes, selected root tests, and expected assertion-level
semantic mismatches. A failed test name alone is not a kill: the runner accepts
only the declared structured mismatch after clean controls and compilation,
then repeats the observation, restores the source, and passes the post-control.

The reviewed aggregate manifest at
[`spec/soundness-gate.v1.json`](spec/soundness-gate.v1.json) declares the exact
subgate commands, profiles, evidence outputs, and nonzero populations. Its
runner rejects a successful no-op just as it rejects a failed command: every
subgate must produce current, bound observations. Runtime reporters derive
population counts from unique observed member identities; literal numeric
population flags are rejected. Pre-commit and CI classify changes through the
versioned ownership manifest, then execute the resource-aware `consumer`,
`semantic`, or `complete` profile. The `complete` profile additionally verifies
the retained synthetic-tree record, so record generation runs the semantic
profile and cannot recurse into its own freshness check.

## Testing

```bash
go test -count=1 ./...
go test -race -count=1 ./...

cd ../..
make check-goplint-soundness
make check-baseline
make check-goplint-exceptions
```

Pre-commit and CI route changes through the four-class ownership taxonomy to
the documentation, consumer, harness, semantic, or completion profile; the
local pre-commit surface is capped below the semantic tier and prints the
authoritative CI tier for heavier diffs. The lint workflow runs the shared
audit plus immutable matrix/aggregate topology, and a documentation-class
diff runs only docs-guard in the plan job; recorded hosted parity and
wall-time acceptance for the migration are retained in
`docs/goplint/soundness-gate-performance.md`.
Before claiming a soundness change complete,
record the reviewed
v4 synthetic tree and complete tracked/non-ignored-untracked diff census (or
re-bind the retained record when only prose drifted), run
`make check-goplint-clean-tree-evidence`, then run
`make check-goplint-soundness-complete`. Every omitted changed path needs a
sorted machine-readable reviewed exclusion; stale, unjustified, or overlapping
exclusions fail verification.

## Architecture and evidence

Important implementation surfaces:

- `goplint/protocol_domain.go`: protocol states, joins, and outcomes;
- `goplint/protocol_identity.go` and `protocol_alias_analysis.go`: SSA/object
  identity and alias tracking;
- `goplint/protocol_validation_effects.go`: conditional validation effects;
- `goplint/protocol_procedure_index.go` and `cfa_call_events.go`: function and
  closure procedures plus unique ordered SSA call events;
- `goplint/cfa_interproc_graph.go`, `cfa_ifds_solver.go`, and
  `constructor_deferred.go`: canonical supergraph, tabulation, and LIFO
  deferred effects;
- `goplint/cfa_ssa_constraints.go` and `cfa_refinement.go`: checked witness
  feasibility and refinement;
- `goplint/protocol_summary_fact.go`: v5 cross-package summaries bound to the
  exact package, function/receiver identity, signature slots, ordered effects,
  and conditional result relation;
- `goplint/semantic_catalog_registry.go`: owner registry and catalog checks;
- `goplint/semantic_spec_oracle_test.go`: behavioral historical oracles;
- `goplint/protocol_oracle_generated_test.go`: supporting solver-core bounded model;
- `goplint/protocol_oracle_e2e_test.go`: required generated-Go end-to-end comparison;
- `internal/soundnessevidence/` and `internal/soundnessgate/`: typed executed
  observations and causal manifest runner;
- `internal/cleantreeevidence/`: temporary-index exact-tree capture and replay;
- `testdata/mutation/`: targeted soundness mutation manifest;
- `bench/thresholds.toml`: reviewed Fedora workstation benchmark policy.
- `bench/thresholds.github-ubuntu-x64-4cpu.toml`: reviewed GitHub-hosted CI
  benchmark policy selected by the lint workflow. Keep runner-class timing
  limits separate; allocation, memory, state-count, and population checks stay
  blocking in both policies.

See [the current semantic reference](docs/goplint/current-techniques-and-semantics.md)
and [the evidence index](docs/goplint/evidence-index.md) for the maintained
design boundary and verification map.

## License

[MPL-2.0](../../LICENSE)
