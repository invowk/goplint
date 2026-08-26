# Goplint soundness-gate execution

The goplint assurance gate uses one versioned manifest and five profiles. The
profile changes how much evidence is freshly executed; it never changes the
analyzer's production semantics or turns an inconclusive result into success.

| Profile | Intended use | Assurance claim |
|---|---|---|
| `documentation` | Prose-only diffs: goplint docs, OpenSpec change artifacts, agent docs, and the three root goplint markdown files | Runs only the static docs-guard anchoring validator; no analyzer execution and no repository audit. It makes no analyzer-soundness, performance, or completion claim. |
| `consumer` | Root application changes proven not to affect goplint semantics or orchestration | One exact-tree repository audit serves baseline, full-scan, exception, and one-sample performance-smoke consumers. It makes no analyzer-soundness or completion claim. |
| `harness` | Orchestration changes: CI workflows, pre-commit config, the Makefile, the soundness-gate executors, and goplint scripts | Adds the goplint module test suite and executor parity (byte-identical normalized run reports between the plan-serial reference and parallel executors on the reviewed fixture) to the consumer surface. It never substitutes for semantic assurance. |
| `semantic` | Goplint analyzer code, tests, evidence, manifests, thresholds, or governing specifications | Re-executes every causal soundness population and five-sample runner-class performance certification. |
| `complete` | Explicit completion, release, scheduled, and exhaustive dispatch contexts | Runs the semantic profile plus retained exact-tree freshness. |

`make check-goplint-soundness` classifies staged or event-provided changes
through the versioned ownership manifest
`spec/soundness-ownership.v2.json` (the v1 manifest was deleted). Every
governed path family carries one of four classes:

- `documentation`: goplint docs (`docs/goplint/**`), OpenSpec change artifacts
  (`openspec/changes/**`), agent docs (`.agents/**` plus `AGENTS.md`), and the
  three root markdown files in ``;
- `consumer`: `cmd/**`, `internal/**`, `pkg/**`, `go.mod`, and `go.sum`;
- `harness`: `.github/workflows/**`, `.pre-commit-config.yaml`, the
  `Makefile`, `internal/soundnessgate/**`,
  `cmd/soundness-gate/**`,
  `cmd/soundness-report-compare/**`, and
  `scripts/**`;
- `analyzer-semantics`: everything else under `**`, the
  governing `openspec/specs/goplint-*/**` and
  `openspec/specs/lint-tooling-quality-gates/**` specifications, and the
  archived evidence directories under `openspec/changes/archive/*/evidence/`.

Routing picks the highest class present in the diff (documentation < consumer
< harness < analyzer-semantics) and maps it to a profile:
documentation→`documentation`, consumer→`consumer`, harness→`harness`, and
analyzer-semantics→`semantic`. Unknown paths, shallow history, missing merge
bases, empty or ambiguous diffs, and unknown events still fail closed to
`semantic`; completion, release, schedule, and dispatch events force
`complete`. A harness-class diff never substitutes for semantic assurance:
any analyzer-semantics path in the diff escalates to `semantic`. Manifest
validation structurally rejects a `documentation` class over any
executable-input family (configuration, manifests, schemas, scripts,
baselines, thresholds, retained evidence).

The explicit targets bypass classification:

```bash
make check-goplint-docs
make check-goplint-soundness-consumer
make check-goplint-soundness-harness
make check-goplint-soundness-semantic
make check-goplint-soundness-complete
```

`check-goplint-soundness-core` remains a compatibility alias for the semantic
target. New automation and documentation should use `semantic`.

## Documentation tier

`make check-goplint-docs` runs `cmd/docs-guard`: a static validator that
completes in seconds and loads no Go packages. It checks that every claim row
in `docs/goplint/evidence-index.md` names at least one existing test, gate, or
observation anchor, and that every referenced Make target, subgate ID,
command, and repository path exists in the current tree. Failures name the
exact stale reference and cannot be baselined, excepted, or inline-ignored.
Documentation-class diffs run only this tier — no analyzer execution and no
repository audit.

## Harness tier

The `harness` profile in `spec/soundness-gate.v1.json` binds the subgates
`baseline`, `exceptions`, `full-scan`, `harness-parity`, `module-tests`,
`performance-smoke`, and `repository-audit`. `module-tests` runs the goplint
module test suite through `scripts/check-module-tests.sh`. `harness-parity`
(`cmd/harness-parity`, also `make check-goplint-harness-parity`) executes the
reviewed fixture manifest
`testdata/parity/soundness-gate.fixture.json` through both the
plan-serial reference executor and the parallel executor and requires
byte-identical normalized run reports (via
`soundnessgate.CompareNormalizedRunReports`), within a one-minute fixture
budget (typically under a second warm). `make check-goplint-soundness-harness`
forces the profile. The `module-tests` and `harness-parity` subgates are also
part of the semantic and complete profiles.

## Trigger surfaces

The pre-commit hook (`goplint-behavior` →
`make check-goplint-soundness-routed` →
`scripts/check-routed-soundness.sh`) is capped below the
semantic tier: documentation diffs run docs-guard, consumer diffs run the
shared repository audit, and harness or analyzer-semantics diffs run the
consumer tier locally while printing the authoritative CI tier and the
explicit Make targets to run it by hand.
`make check-goplint-soundness-semantic` and
`make check-goplint-soundness-complete` remain available for explicit local
runs.

CI (`lint.yml`) routes pull requests from the cumulative base-to-head diff and
push-to-main from the push diff; schedule, release, and dispatch events keep
forcing `complete`. A documentation-class diff runs docs-guard in the plan job
and skips the worker and aggregation jobs entirely.

**Escape hatch:** `GOPLINT_FORCE_SEMANTIC=1` escalates any routed profile
except `complete` to `semantic`, in both the local routed script and the CI
plan job. Removal criterion: remove the escape hatch after one release ships
with zero misrouting diagnoses that needed it.

## Local resource policy

The default executor generates an immutable plan and schedules dependency-ready
work against separate CPU, memory, and worker-slot budgets. CPU defaults to the
effective process `GOMAXPROCS`; Linux memory discovery retains headroom, and
portable fallbacks are conservative. Each child receives bounded
`GOMAXPROCS`, Go build `-p`, and Go test `-parallel` values from its reservation.

Override discovery with command flags:

```bash
go run ./cmd/soundness-gate -profile semantic \
  -cpu-budget 8 -memory-budget-bytes 34359738368 -max-workers 6
```

The equivalent environment variables are
`GOPLINT_SOUNDNESS_CPU_UNITS`, `GOPLINT_SOUNDNESS_MEMORY_BYTES`, and
`GOPLINT_SOUNDNESS_MAX_WORKERS`. `GOPLINT_SOUNDNESS_EXECUTOR=plan-serial`
selects the immutable-plan serial reference for parity diagnosis with
`cmd/soundness-report-compare`.
Resource limits may delay or fail work, but cannot shrink required populations.
Dependency-ready work is admitted deterministically in descending reservation
order, with canonical work-unit identity as the final tie breaker. This keeps
small jobs from fragmenting the runner before a critical large unit can start.
The tight algorithmic certification phase reserves the full local plan CPU
budget so its runner-class thresholds are not distorted by colocated work.
The weighted analyzer race/repeat census executes as six deterministic
four-CPU work groups (`race-repeat-1` through `race-repeat-6`); on the
reviewed local 24-CPU class the groups run concurrently, while in distributed
CI each group reserves one complete four-CPU worker so the heaviest race
shard stays inside its weight-derived timeout. Group assignment partitions
every plan work unit exactly once by descending effective weight, so the
union of the groups is the exact analyzer census by construction.

## Race and repeat timing

`spec/goplint-test-timings.v1.json` contains three fresh samples for every live
top-level `Test`, `Fuzz`, and `Example`. The planner rejects unknown, duplicate,
or kind-mismatched entries, exposes conservative default weights for newly
discovered members, and uses deterministic longest-processing-time allocation.
Nested cases are included in the timing census and must satisfy the reviewed
dominance policy.

Refresh the manifest after adding, removing, renaming, or materially changing
analyzer tests:

```bash
make update-goplint-race-repeat-timings
make check-goplint-race-repeat
```

The race/repeat command has two independently planned phases. The analyzer
phase builds and digests one normal and one race analyzer test binary per
exact plan, compares the top-level census compiled into both binaries, and
fails closed if normal-only or race-only build-tagged members would escape
either mode. It then executes the weighted analyzer census; in the aggregate
manifest this phase is expressed as the six deterministic work groups
`race-repeat-1` through `race-repeat-6` (`--group index/6`), each executing
its exact disjoint share of the plan work units with two workers of two CPUs.
`race-repeat-supporting` runs the remaining module packages once under race
and exactly three times normally. The direct Make target runs both phases in
full. Every required member must execute at the declared count; missing,
duplicated, skipped, crashed, timed-out, or wrong-binary results are blocking.

## Smoke versus certification

`make check-goplint-performance-smoke` runs one full-scan measurement plus one
iteration of fast solver and recursive-tabulation workloads under broad
catastrophic-regression limits. Passing smoke is not statistically stable
performance certification and does not support an analyzer-soundness claim.

`make check-goplint-benchmarks` is the semantic/completion certification tier.
It runs the algorithmic and full-scan phases together for direct use. The
immutable plan schedules those phases independently: `benchmarks` enforces the
tight solver, allocation, generated-analyzer, and state-count thresholds, while
`benchmark-full-scan` retains five fresh wall/RSS samples. Both use the same
reviewed policy, medians, exact toolchain, and runner class. A smoke result
cannot be relabeled as certification, and a runner/toolchain mismatch is
blocking.

## Immutable plans and distributed bundles

The distributed interface is the same contract used by GitHub Actions:

```bash
out="$(mktemp -d)"
go run ./cmd/soundness-gate -action plan -profile semantic \
  -runner-class github-ubuntu-x64-4cpu -plan "$out/plan.json"
go run ./cmd/soundness-gate -action work -plan "$out/plan.json" \
  -work-unit aggregate-contract -bundle-dir "$out/bundles"
go run ./cmd/soundness-gate -action aggregate -plan "$out/plan.json" \
  -bundle-dir "$out/bundles" \
  -repository-audit "$out/bundles/repository-audit/repository-audit.json" \
  -report "$out/report.json" -telemetry "$out/telemetry.json"
```

The final aggregate requires a bundle for every planned work unit, so the last
command is expected to fail with the missing identities until all units have
run. Repository-audit consumers additionally receive the exact audit artifact
through `-repository-audit`.

The checked contracts are:

- `spec/soundness-execution-plan.v1.schema.json` for workspace, manifest,
  registry, toolchain, command/binary, resources, dependencies, census,
  shards, and expected reports;
- `spec/soundness-work-bundle.v2.schema.json` for a worker's timestamped
  terminal outcome, exact bindings, content-bound report/audit digests,
  observations, populations, metrics, and self-digest;
- `spec/soundness-run-report.v1.schema.json` for the no-gap aggregate verdict;
- `spec/soundness-run-telemetry.v1.schema.json` for queue, wall, CPU, peak RSS,
  reservations, timeout cause, populations, critical path, and maximum
  concurrent reservations.

Workers recompute the checkout, plan, manifest, registry, toolchain, command,
binary, census, embedded-report, and dependency-artifact digests. The
aggregator recomputes the downloaded shared-audit digest, rejects stale,
foreign, duplicate, missing, partial, or conflicting bundles, and retains both
the normalized report and aggregate telemetry. Worker loss, timeout,
cancellation, artifact expiry, and partial upload therefore surface as
actionable missing-work or stale-binding failures instead of partial success.

GitHub Actions reproduces this as plan/audit, bounded matrix-worker, and final
aggregate jobs. Artifacts have unique work-unit names, support safe job reruns,
and are merged only for strict aggregation. Scheduled and release events force
the completion profile. The migration-era legacy serial reference lane and its
`goplint-parity` comparison were removed after the recorded hosted runs in
[`soundness-gate-performance.md`](soundness-gate-performance.md) established
byte-identical normalized evidence and the required wall-time reduction.

## Dual-digest completion record and prose re-binding

The retained completion record is the generated `clean-tree-run.v5.json`
under `testdata/gates/`, produced from the reviewed plan
`testdata/gates/clean-tree-v5.json`, the path
selection `clean-tree-v5.paths`, and validated against
`clean-tree-v4.schema.json` (the v3 files were renamed; a retained v3 record
is rejected with an explicit migration notice directing regeneration under
the v4 names).

The record's repository identity carries two digests, both computed from the
ownership manifest read out of the synthetic tree itself:

- `semantic_tree_digest` covers every class any gate executes or reads
  (consumer, harness, and analyzer-semantics content), plus ungoverned paths
  conservatively;
- `prose_tree_digest` covers the documentation class.

A `provenance` block attributes the retained aggregate report to the exact
semantic content that produced it.

Prose-only drift no longer requires regeneration:
`make rebind-goplint-clean-tree-evidence` re-binds the retained record in
seconds. Re-binding recomputes both digests, revalidates the task ledgers and
the diff census, carries the aggregate report forward untouched, and records
the carried-forward provenance (`kind: "re-bound"`). Semantic-content drift
makes re-binding fail closed naming the drifted paths; full regeneration with
`make generate-goplint-clean-tree-evidence` is then required. The freshness
verifier accepts re-bound records and tells you to re-bind when only prose is
stale.

Expected task-ledger names, paths, order, and pending sets live only in the
reviewed plan `clean-tree-v5.json`; code and schema validate structure only.
Closing or archiving an OpenSpec change therefore edits exactly one reviewed
file.

## Completion proof

After all intended files and task bookkeeping are final, generate (or, when
only prose drifted since a valid record, re-bind) and verify the retained
exact-tree evidence, then run the completion profile:

```bash
make check-goplint-soundness-semantic
make generate-goplint-clean-tree-evidence   # or: make rebind-goplint-clean-tree-evidence
make check-goplint-clean-tree-evidence
make check-goplint-soundness-complete
```

Any subsequent semantic-content change makes the retained proof stale and
requires regeneration; prose-only drift only requires re-binding.
