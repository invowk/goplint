# goplint Soundness Evidence Index

This index maps each canonical guarantee to a live production boundary and to
blocking evidence that executes that boundary. Component tests remain useful,
but marker strings, lexical reachability, a nonempty corpus, or a command that
merely exits zero do not receive soundness credit.

| Guarantee | Live production boundary | Blocking proof surface |
|---|---|---|
| Exact category ownership and baseline policy | `semanticCatalog`, `semanticOwnerRegistry`, `routeProtocolCFADiagnostics` | typed category observations; semantic catalog tests; always-visible policy invariant |
| Ordered evaluation of every relevant call | `protocolCallEvent`, `buildProtocolCallEventIndex`, interprocedural graph expansion | nested/sibling call black boxes; unique SSA association tests; recursion, no-return, unresolved, determinism, race, and benchmark observations |
| Path-sensitive deferred validation | constructor SSA identity, canonical procedure summaries, IFDS return effects | unconditional/conditional, overwritten error, multiple-defer, alias, helper, panic, early-return, and unresolved-defer cases |
| Escaping closure analysis | procedure index, SSA closure bindings, ordinary call/return edges | returned/stored/passed/callback, IIFE, `go`, `defer`, recursive, nested, method-value, and capture-rebinding cases |
| Conditional validation on the exact nil-result edge | validation program edge transfer and IFDS/IDE state | production-boundary fixtures and independent generated-program comparison |
| SSA/object identity, alias kills, and result-slot correspondence | identity interner, alias analysis, constructor SSA model | historical alias, constructor identity, generic, ambiguous-capture, and rebinding fixtures |
| Unknown relevant effects remain conservative | call-site effect classification, uncertainty state, reporting route | unresolved mutation/replacement/escape/call-mapping fixtures and category-specific inconclusive observations |
| Finite summaries, recursion, and matched returns | `interprocProcedureSummary` procedure summaries, realizable call/return tabulation | recursive/mutually-recursive and summary reuse fixtures; independent integrated program model |
| Exact witnesses and checked refinement | witness edges, SSA constraint evidence, refinement validation | joined-state cases plus test-only pre-aggregation evidence corruption |
| Protocol uncertainty is unsuppressible | always-visible diagnostic sink, baseline parser/collector/updater | parser/update rejection plus baseline, full-scan, pre-commit, CI, and aggregate orchestration contracts |
| Category evidence is specific and executable | `semantic-evidence.v2.json`, runtime observation sink, bidirectional census | exact category/layer TestIDs with real emitted observations; missing/duplicate/extra/stale/zero-population adversarial tests |
| Aggregate execution is causal | `soundness-gate.v1.json`, manifest runner, bound subgate reports | exact command-vector execution; no-op, stale, empty, skipped, forged, and unrelated-failure self-tests |
| Aggregate populations are observed | subgate manifests plus the observed-member census (`PopulationsFromObservedMembers`) | exact current-run members; missing, duplicate, skipped, zero, stale, and literal-count reporter regressions |
| Independent semantics exercise integrated dimensions | `protocoloracle.Program`, reference interpreter, generated-Go source projection | exact fact/alias/constraint/procedure/call/return/terminal coverage and production differential cases |
| Historical fuzz seeds prove declared properties | decoded corpus manifest and shared production/reference decoders | exact per-seed feature/property observations (`TestCategoryFuzzSeedEvidence`); unrelated nonempty seeds are rejected |
| Scheduled oracle is a strict superset | manifest-derived enumeration and generated analyzer comparison | `make check-goplint-protocol-oracle-scheduled` and the scheduled sharded workflow |
| Mutations are causal | v2 mutant manifest (`testdata/mutation/soundness-mutants-v2.json`) with changed stages, exact source hashes, selected root tests, and assertion-level expected/actual states | clean control, compile separation, exact transformation, repeated intended mismatch, restoration, and post-control; unrelated failures are invalid |
| Finding IDs are global and layout-stable | `PackageScopedFindingID`, semantic AST keys, collision-safe collectors | same-leaf import-path isolation, layout metamorphism, repeated migration scans, duplicate/collision rejection |
| Directives are total and fail visibly | centralized directive site collection (`collectDirectiveSites`) and validation | attachment oracle plus unknown, incomplete, invalid, misplaced, duplicate, and conflict fixtures |
| Deterministic output | analyzer facts/findings, IFDS worklist, summaries, witnesses, refinements | file, package, map, worklist, equivalent-schedule, repeat, and race observations |
| Performance stays within reviewed limits | reference interpreter and full generated-analyzer pipeline | separate component/analyzer benchmarks and median-of-five reviewed thresholds |
| Completion proof matches the reviewed tree | temporary-index materializer (`cleantreeevidence.Materialize`) and retained record verifier | synthetic tree/diff/input/task/tool/manifest/observation identities plus caller index/worktree preservation |
| Documentation stays anchored to executable artifacts | `cmd/docs-guard` static validator over the governed goplint document set | `make check-goplint-docs`; stale Make targets, repository paths, subgate IDs, and dangling claim rows are exact-line failures with no baseline, exception, or inline-ignore surface |
| Harness executors stay behaviorally identical | plan-serial reference executor and parallel executor over the reviewed fixture `testdata/parity/soundness-gate.fixture.json` | `harness-parity` subgate (`make check-goplint-harness-parity`) requires byte-identical normalized run reports via `CompareNormalizedRunReports` |
| Prose drift cannot silently stale or corrupt the completion record | dual-digest repository identity (`semantic_tree_digest`, `prose_tree_digest`) computed from the in-tree ownership manifest, plus retained provenance | `Rebind` recomputes both digests, revalidates task ledgers and the diff census, and carries the aggregate report forward; `validateProvenance` and the freshness verifier fail closed on semantic drift naming the drifted paths |

## Evidence contracts

The typed registry is
[`spec/semantic-evidence.v2.json`](../../spec/semantic-evidence.v2.json).
Each entry names one exact category, layer, feature, executable TestID, and
expected observation. The census consumes observations emitted while those
tests execute and rejects credit that is absent, duplicated, extra, stale,
marker-only, or empty.

The aggregate manifest is
[`spec/soundness-gate.v1.json`](../../spec/soundness-gate.v1.json).
It binds every subgate to an exact command vector, profile, required report,
and minimum nonzero population. Producers bind their report to the active run;
a prior report or an unrelated successful command cannot satisfy the gate.

## Cross-change authority

The predecessor reconciliation is
[`cross-change-reconciliation.v2.json`](../../openspec/changes/archive/2026-07-19-close-goplint-soundness-review-gaps/evidence/cross-change-reconciliation.v2.json).
The residual review is now indexed by
[`residual-finding-inventory.v1.json`](../../openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/residual-finding-inventory.v1.json),
with dependency and archive blockers recorded in
[`change-dependencies.v1.json`](../../openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/change-dependencies.v1.json).
The accepted repeated `gpl3` to `gpl4` comparison is
[`stable-id-migration.v2.json`](../../openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/stable-id-migration.v2.json),
and its intentional population changes are exact-set reviewed by
[`stable-id-population-review.v1.json`](../../openspec/changes/archive/2026-07-19-close-residual-goplint-soundness-gaps/evidence/stable-id-population-review.v1.json).
The comparison conservatively retains deferred or stored closure captures of
validated locals as escape uncertainty; semantic local keys must not erase
those findings.
Together these records retain the v2 reconciliation as historical evidence and
map each newer finding to its production boundary, expected outcome, evidence
layers, owning requirement, and tasks.

The archived soundness-completion claim is backed by the causal mutation
profile and the reviewed combined proof bundle, now retained under the v4
artifact names. The retained bundle binds the plan-declared task ledgers and
the complete intended tracked and untracked diff. A prior clean-tree record,
current-worktree focused tests, or non-mutation subgate success cannot
substitute for either proof.

## Profiles and exact-tree lifecycle

`make check-goplint-soundness` routes the diff through the four-class
ownership manifest `spec/soundness-ownership.v2.json`: the `documentation`,
`consumer`, `harness`, and `analyzer-semantics` classes map to the
`documentation`, `consumer`, `harness`, and `semantic` profiles, the highest
class present in the diff wins, unknown context fails closed to `semantic`,
and completion, release, schedule, and dispatch events force `complete`.
Documentation-class diffs run only the static docs-guard tier
(`make check-goplint-docs`); harness-class diffs add `module-tests` and
`harness-parity` to the consumer surface
(`make check-goplint-soundness-harness`). The explicit
`make check-goplint-soundness-semantic` target runs every causal semantic
subgate except the retained exact-tree freshness verifier.
`check-goplint-soundness-core` remains only as a compatibility alias for that
semantic target.

The retained v4 authorities are the reviewed
[`command plan`](../../testdata/gates/clean-tree-v5.json),
[`path selection`](../../testdata/gates/clean-tree-v5.paths),
[`counterexample inventory`](../../testdata/gates/clean-tree-counterexamples-v3.json),
and the generated combined proof record `clean-tree-run.v5.json` (validated
against `clean-tree-v4.schema.json`; a retained v3 record is rejected with an
explicit migration notice). The record output is intentionally absent from
the selected synthetic tree and is authorized separately by the recorder to
avoid self-reference.

For a completion claim:

1. compare all tracked and non-ignored untracked changes with the explicit
   reviewed path selection, require a sorted machine-readable path and
   justification for every unrelated exclusion, reject stale or overlapping
   exclusions and every silent omission, then assemble `HEAD` plus that
   selection through a temporary Git index;
2. run the reviewed command plan in the resulting detached synthetic tree,
   including the semantic aggregate, and retain its bound report;
3. record the synthetic tree, the dual `semantic_tree_digest` and
   `prose_tree_digest` repository identity, the `provenance` attribution, the
   per-path complete-diff status/content digest and selected-or-excluded
   disposition, authorized recorder outputs, inputs, tools, task ledgers,
   counterexamples, commands, observations, mutation chain, and preservation
   digests;
4. run `make check-goplint-clean-tree-evidence` from the caller checkout; then
5. run `make check-goplint-soundness-complete`.

When only documentation-class prose drifted since a valid record,
`make rebind-goplint-clean-tree-evidence` replaces step 2 and step 3: it
recomputes both digests, revalidates the task ledgers and the diff census,
carries the aggregate report forward untouched, and records the
carried-forward provenance (`kind: "re-bound"`). Semantic-content drift makes
re-binding fail closed naming the drifted paths, and full regeneration is
required.

Record generation deliberately invokes the semantic profile. The complete profile
adds the freshness verifier, so using it during generation would make the proof
depend recursively on the record it is creating.
`make generate-goplint-clean-tree-evidence-reusing` may replace only the
aggregate execution inside step 2 with an aggregate report the caller already
retained. It is admitted solely under tree binding (profile, byte-equal
manifest digest, exact registration census, and the recomputed workspace digest
that covers the tracked registry file) plus the companion
`<report>.binding.json`, which pins the report's canonical decoded form, the
registry byte digest,
and the producing Go toolchain. The recorded outcome states reuse instead of
execution and the record carries `provenance.kind: "reused-aggregate"`, which
step 4 refuses without `-allow-reused-aggregate`; step 5 therefore still
requires fresh aggregate execution. Any mismatch fails closed rather than
falling back to re-execution. Verification replays the
retained base with the current reviewed paths and rejects any changed input or
task state while leaving the caller's real index and worktree unchanged. The
synthetic `git diff --check` excludes only the retained
`red-baseline-probes.v1.patch` artifact because checking a newly committed
nested patch misclassifies its unified-diff context prefixes as source
whitespace errors. The patch remains selected, content-digested, and exercised
by the red-baseline reconstruction contract. Expected task-ledger names,
paths, order, and pending sets are single-sourced in the reviewed v4 plan;
code and schema validate structure only (ledger paths must be
`openspec/changes/<name>/tasks.md` or
`openspec/changes/archive/<date>-<name>/tasks.md`, archived predecessors
before active changes, non-regressing archive dates, and well-formed pending
IDs). Closing or archiving a change edits exactly one reviewed file.

## Direct commands

```bash
make check-goplint-docs
make check-goplint-harness-parity
make check-goplint-module-tests
make check-semantic-spec
make check-goplint-production-integration
make check-goplint-counterexamples
make check-goplint-architecture
make check-goplint-protocol-oracle
make check-goplint-end-to-end-oracle
make check-goplint-protocol-oracle-scheduled
make check-goplint-fuzz-seeds
make check-cfg-refinement
make check-goplint-determinism
make check-goplint-targeted-mutation
make check-goplint-race-repeat
make check-goplint-full-scan
make check-goplint-benchmarks
make check-goplint-soundness-core
make check-goplint-clean-tree-evidence
make check-goplint-soundness-complete
```

`make check-baseline` and `make check-goplint-exceptions` remain separate debt
governance gates for definite policy findings. Neither surface can suppress a
protocol inconclusive outcome.
