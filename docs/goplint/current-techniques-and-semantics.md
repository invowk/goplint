# goplint Current Semantics

This document is the implementation-current semantic reference for Invowk's
`tools/goplint` analyzer. The executable catalog and tests remain authoritative
when prose and code disagree.

## Analysis families

goplint registers every diagnostic category as one of three families:

1. Structural checks inspect Go syntax and type information. They cover bare
   primitives, value-type methods, constructors, functional options,
   immutability, path boundaries, and related repository conventions.
2. Protocol checks track validation obligations across control flow and calls.
   They cover raw-to-value casts, use/escape before validation, constructor
   validation, closures, helpers, methods, and conditional validation errors.
3. Cross-artifact checks compare facts outside one syntax tree, including
   cross-package protocol summaries, Go/CUE enum domains, exception policy,
   and accepted stable finding IDs.

[`spec/semantic-rules.v1.json`](../../spec/semantic-rules.v1.json)
mirrors the canonical Go category registry with every category, family, and
oracle strategy. Each owner is a typed traversal or post-traversal callback
used by the real analyzer. Catalog validation and the machine-readable census
reject administrative-only owners, duplicate or stale categories, incompatible
or missing oracles, and missing required evidence layers. The separate typed
evidence registry binds every exact category/layer pair to an executable TestID
and expected observation; credit comes from emitted execution observations,
not symbol names or artifact markers.

## Canonical protocol domain

Protocol analysis uses interned identities for SSA values and abstract objects.
Facts distinguish allocations, parameters, receivers, result slots, copies,
and phi nodes. There is no type-only identity fallback.

Must-alias information is flow-sensitive. Rebinding, stores, new allocations,
and path joins kill facts that are no longer certain. A relevant may-alias
ambiguity cannot discharge an obligation; it produces a blocking inconclusive
outcome.

The typestate domain and joins are defined in
[`protocol-domain.md`](../../spec/protocol-domain.md). Its public
result vocabulary is:

- `violation`: an executable unsafe witness or definite unmet obligation;
- `inconclusive`: proof is blocked by unsupported or exhausted reasoning;
- `discharged-infeasible`: checked evidence proves one candidate witness is
  infeasible.

The last value is retained evidence, not a finding or a general proof of
safety. Violations outrank inconclusive paths during fail-closed aggregation;
otherwise any relevant uncertainty is blocking.

## Validation effects

Every relevant `Validate()` call is represented as a relation between its
receiver identity and error-result identity. The receiver becomes validated
only on a continuation edge that proves the associated error is nil.

The same conditional-effect API handles direct selectors, method expressions,
captured method values, interface-resolved calls, explicit error variables,
inverted checks, and dominating terminating error branches. Merely assigning,
logging, blanking, returning without correspondence, or otherwise discarding a
validation error does not validate continued execution.

Cross-package summaries use `ProtocolSummaryFact` v5. Each fact is bound to the
attached package, exact function or receiver identity, signature arity and slot
roles, compatible slot types, ordered effects, and the exact condition-result
slot. Missing, legacy, malformed, or incompatible relevant facts are
inconclusive as a whole; a compatible subset cannot be reinterpreted as a
complete no-effect summary. Filtered packages still export facts needed by
analyzed consumers.

Constructor checks additionally prove object correspondence: the validated
identity must reach the constructor's matching result slot. Each return is
classified from its exact SSA/CFG edge and returned error identity; condition
text or branch ancestry cannot classify both sides of `err != nil` as failure.
A validation of a different same-typed value does not satisfy the obligation.

Deferred constructor calls are modeled as procedure effects over the canonical
graph and applied in LIFO order at each realizable return. Successful discharge
requires the matching validation and its own nil error relation on every
successful return path. A conditional defer, overwritten result, early exit,
panic, ambiguous capture, or unresolved effect remains unvalidated or
inconclusive as appropriate.

## Interprocedural analysis

The canonical solver builds one deterministic package inventory containing
every function declaration and function literal, including literals stored in
package initializer expressions as well as returned, stored, passed,
callback-style, IIFE, `go`, and `defer` closures. Stable procedure identity
deduplicates package and enclosing-function discovery. Captured identities come
from SSA closure bindings; unresolved relevant procedures or ambiguous captures
are blocking inconclusive outcomes.

It constructs a deterministic exploded supergraph with procedure, CFG, call,
return, and call-to-return nodes. Every relevant source call must associate
with one exact SSA caller, block, and instruction. Nested and sibling calls are
expanded into separate call/matching-return micro-nodes in SSA order. Missing,
duplicate, cross-block, or generic-instantiation ambiguity is reported as
`call-mapping` uncertainty rather than guessed from AST position. Calls and
returns carry call-site identities, so a callee return reaches only its matching
caller continuation. Recursion converges through finite facts and summary reuse.

IFDS-style propagation carries object typestate and escape facts. IDE-style
edge functions carry conditional validation/error relations. A resolved call
has no unconditional bypass: its call-to-return edge must model every relevant
effect. An unresolved call becomes inconclusive only when its effect can change
a tracked obligation.

The centralized `mayReturn` contract recognizes compiler/CFG behavior and
modeled terminal calls, including `panic`, `os.Exit`, `runtime.Goexit`,
`log.Fatal`, `log.Fatalf`, and `log.Fatalln`, plus soundly resolved aliases.
Unknown calls remain conservatively returning.

## Checked feasibility and refinement

Candidate witnesses are checked using the finite fragment in
[`ssa-constraints.md`](../../spec/ssa-constraints.md):
SSA-versioned nil, boolean, string, and integer equality or inequality atoms,
with normalized negation, conjunction, and disjunction.

The decision procedure emits normalized contradiction evidence for UNSAT.
A separate checker must accept that evidence before a witness is discharged.
SAT retains the witness. Unsupported predicates, missing SSA, timeouts,
query/iteration limits, malformed evidence, or checker rejection produce
inconclusive outcomes.

Refinement replays a symbolic witness and iteratively adds supported predicates
until it retains a feasible violation, checks an infeasibility proof, or reaches
a resource limit. No configuration can disable this semantic layer or convert
unknown reasoning into success.

## Determinism and governance

Graph edges, worklists, findings, witnesses, summaries, and refinement evidence
are normalized before comparison or serialization. Repeated-run and reordered
worklist/package tests require byte-identical output.

Stable `gpl4` finding IDs encode the full import path plus source-local semantic
identity rather than package leaf names, raw token positions, file-set order,
or diagnostic prose. Baselines suppress only exact IDs, use the v2
`entries = [{id, message}]` format, and are generated using the same canonical
analyzer configuration used by CI. Duplicate emission or an ID collision fails
both findings-stream and analysis-JSON collection before a baseline can be
written.

Directive discovery is likewise centralized across file, declaration, type,
field, function, method, parameter, and supported statement attachments.
Unknown, incomplete, invalid, misplaced, duplicate, or conflicting directives
fail visibly before a consumer runs. Protocol inconclusive categories and
mixed-category inconclusive outcomes are always visible: baseline parsing,
collection, and update reject them, and exception or inline-ignore policy is
not consulted. Exception patterns and review dates are independently audited
for definite policy findings.

## Verification contract

`make check-goplint-soundness` conservatively routes changes to the `consumer`,
`semantic`, or `complete` aggregate profile. The explicit semantic target is
the causal analyzer-soundness gate. A reviewed machine-readable manifest
declares exact command vectors, required bound reports, and minimum nonzero
populations. The runner rejects missing, skipped, stale, duplicate, empty,
forged, or successful no-op subgates. The semantic profile runs:

- real-analyzer production integration and historical counterexamples;
- alternate-production-authority absence checks;
- the total semantic catalog and behavioral boundary oracles;
- the bounded independent protocol interpreter as supporting solver-core evidence;
- the required generated-Go end-to-end comparison through extraction,
  propagation, aggregation, and diagnostics;
- committed fuzz-seed replay;
- constraint/evidence/refinement checks;
- real package/file/map/worklist/schedule deterministic output checks;
- the causal zero-survivor targeted mutation profile, whose v2 manifest binds
  each mutant to exact changed stages, source anchors and hashes, selected root
  tests, and expected assertion-level semantic mismatches;
- race/repeat and blocking full-repository scans;
- separate reference-component and full generated-analyzer performance
  thresholds plus aggregate-gate self-validation.

The scheduled oracle enumerates a manifest-derived strict superset of the semantic
cases and compares every generated program with the production analyzer in a
blocking sharded workflow.

Mutation execution is causal rather than test-name based. The runner requires
clean pre-controls, an exact source transformation, successful compilation,
the declared structured mismatch with its full subtest and assertion ID, a
repeatable identical mismatch, exact restoration, and clean post-controls.
Setup failures, unrelated assertions, panic, timeout, compilation failure, and
environment failures are invalid outcomes. Mutation-layer evidence receives
only the canonical union of `changed_stages` declared by mutants mapped to that
category.

A soundness completion claim additionally requires a retained exact-tree proof.
The recorder uses a temporary Git index to assemble the reviewed path selection
on its base commit, first checks that every tracked or non-ignored untracked
change is selected or has a sorted, justified reviewed exclusion, executes the
semantic profile in that synthetic tree, and binds the tree, per-path complete-diff
census, tools, inputs, all three dependency-ordered task ledgers, commands,
observations, populations, and causal mutation sequence. Stale exclusions,
silent omissions, partial predecessor state, or reordered change ledgers are
blocking. `make check-goplint-clean-tree-evidence` replays and verifies that
record without modifying the caller's real index or worktree.
`make check-goplint-soundness-complete` adds this freshness verifier. Record
generation uses semantic rather than complete to avoid a recursive dependency.

CI additionally runs nested-module tests with the race detector, baseline and
exception governance, and a blocking canonical full-repository scan. See the
[evidence index](./evidence-index.md) for exact files and commands.

## Resource boundary

State, feasibility-query, refinement-iteration, and feasibility-time
budgets bound computation. Exhaustion is inconclusive. Witness-length limits
truncate explanation metadata only. The analyzer provides no production
selector for alternate protocol semantics, backends, engines, alias policies,
or uncertainty policies.
