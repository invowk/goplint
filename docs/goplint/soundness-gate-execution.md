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

## Generation semantics and aggregate-report reuse

Generation materializes the reviewed selection into a detached synthetic
worktree and, by default, executes the whole reviewed command plan inside it.
It invokes the `semantic` profile rather than `complete` so the proof cannot
depend recursively on the freshness check of the record it is creating.

That default re-executes the semantic profile even when the caller just ran
`make check-goplint-soundness-semantic` over byte-identical content. The
opt-in `-reuse-aggregate-report` flag, wired as
`make generate-goplint-clean-tree-evidence-reusing`, instead admits an
aggregate report the caller already retained. Reuse is a **local iteration
aid**: the record it writes carries `provenance.kind: "reused-aggregate"`, and
the freshness verifier refuses that kind unless the caller passes
`-allow-reused-aggregate`. A completion or release claim therefore always rests
on fresh aggregate execution, because the `clean-tree-freshness` subgate in the
`complete` profile never passes that opt-in.

Admission requires all of the following, and fails closed on any one of them.
Tree-content binding, through the same admission check the default path applies
to a report it just produced:

- the file parses and validates as an aggregate run report against the manifest
  and registry read from the synthetic tree, including exact-census validation
  of every registration, producer, and population;
- its `profile` equals the plan's `aggregate_report.profile`;
- its `manifest_digest` is byte-equal to the digest of the manifest as read from
  the synthetic tree;
- its `workspace_digest` is byte-equal to the workspace digest recomputed from
  the synthetic worktree, which covers the tracked registry file along with
  every other tracked and untracked path.

Producing-run binding, through the companion `<report>.binding.json` that
`cmd/soundness-gate` writes beside any retained report:

- the binding's `report_sha256` equals the canonical digest of the reused
  report bytes, so post-production tampering with any report member — including
  a per-subgate `report_digest` — is rejected;
- its `profile`, `workspace_digest`, and `manifest_digest` equal the values
  independently recomputed above;
- its `registry_digest` is byte-equal to the digest of the registry file as read
  from the synthetic tree;
- its `toolchain` (Go version, GOOS, GOARCH, and their digest) equals the
  toolchain identity of the generating process, which rejects replaying a
  report produced under a different toolchain.

Every check that does not depend on the synthetic tree — report and binding
parsing, the report-to-binding digest linkage, the binding profile, and the
producing toolchain — runs *before* the plan starts, so a selection mistake
fails in seconds instead of after the ten earlier planned commands have burned
their runtime. The tree-bound comparisons run at the aggregate command's
position in plan order, which is where a real execution would have observed the
tree.

Every other planned command still executes inside the synthetic worktree, and
the recorded outcome for the aggregate command states reuse — its retained log
opens with `reused caller-provided aggregate report <sha256>` and names the
producing plan and toolchain — instead of claiming an execution that did not
happen. The record stays v4-shaped: `provenance.kind` is an existing member and
`"reused-aggregate"` is a new value of it, so every previously retained record
verifies exactly as before, and an older verifier reading a newer reused record
rejects it as an unknown kind.

### What reuse does not establish

A retained `RunReport` is a self-attested census. Reuse keeps its bytes and
drops its writer, so the following remain unbound and are the reason
verification refuses reused records by default:

- **Per-subgate execution status.** The report carries no status per subgate;
  `ValidateRunReport` synthesizes the passed status while cross-checking
  populations, so a census that was never executed is structurally
  indistinguishable from one that was.
- **Per-subgate `report_digest` artifacts.** They are not recomputable from any
  retained artifact. The companion binding pins the whole report's bytes, which
  catches tampering after production but not a value fabricated during it.
- **Fabrication by the producer.** The companion binding is written by the same
  process that writes the report, so it detects replay across trees, manifests,
  registries, profiles, and toolchains — not a forged pair.
- **Plan identity and resource budget.** The binding carries `plan_id` and the
  resource budget and they are recorded in the command log, but they are not
  recomputed: the plan digest covers the machine-dependent resource budget, so
  it is deliberately not reproducible across runs.
- **Wall-clock plausibility.** Nothing constrains a retained `duration_ms`, for
  reused or executed outcomes; a reviewed floor would be a policy decision
  about runner classes and is deliberately not invented here. The distinct
  provenance kind, not the duration, is what marks an outcome as not executed.
- **Timestamps.** The report has none, so "recently produced" cannot be
  asserted; only content equality with the tree can be.

### Reuse preconditions and non-retryability

Because the workspace digest is recomputed byte-for-byte, reuse only succeeds
while the caller's worktree content still matches the synthetic tree: an
uncommitted path outside the reviewed selection, a reviewed diff exclusion, or a
record rewritten since the semantic run all make the digests differ, and the
mismatch is reported instead of silently absorbed.

This makes reuse single-shot per report. Generation rewrites the on-disk record,
which is a tracked file, so the caller's workspace digest diverges from the
synthetic tree the moment generation succeeds. Re-running reuse with the same
report then fails on the workspace-digest comparison until the record is
committed. Retry with a fresh semantic run, or commit first.

The reused report must be an absolute path outside the repository, so it can
never become part of the tree it is evidence about. Reuse is also refused for
`-rebind`: re-binding carries the retained report forward under
`kind: "re-bound"`, and carrying a reused record forward would present a
never-executed aggregate as one that verification accepts by default.

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

This sequence is the only one that supports a completion claim. Reuse belongs to
the iteration loop before it, where the record is regenerated repeatedly while
the tree is still moving:

```bash
report="$(mktemp -d)/aggregate-report.json"
GOPLINT_SOUNDNESS_REPORT_PATH="$report" make check-goplint-soundness-semantic
make generate-goplint-clean-tree-evidence-reusing GOPLINT_CLEAN_TREE_REUSE_REPORT="$report"
make check-goplint-clean-tree-evidence-allowing-reuse
```

Assign `GOPLINT_SOUNDNESS_REPORT_PATH` per command as shown; never `export` it.
The report writer is exclusive, so a still-exported value makes every later
aggregate run — including `make check-goplint-soundness-complete` — fail on the
existing file *after* burning its full runtime. The reusing target refuses to
run while the variable is exported for exactly that reason.

Any subsequent semantic-content change makes the retained proof stale and
requires regeneration; prose-only drift only requires re-binding.
