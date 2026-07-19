# SSA Constraint Refinement

Goplint's checked feasibility procedure decides a deliberately finite fragment over SSA value versions. It is not a general-purpose solver and does not claim to decide arbitrary Go predicates.

## Supported fragment

Each atomic predicate compares one SSA subject with one typed constant. Equality
and inequality support the types below; ordered integer comparisons additionally
support `<`, `<=`, `>`, and `>=`. Supported constants are:

- `nil`, when the SSA subject has a nil-able pointer, interface, slice, map,
  channel, function, or unsafe-pointer type;
- `true` or `false` for boolean subjects;
- string constants for string subjects; and
- signed or unsigned integer constants, including `uintptr`, for integer subjects.

The procedure normalizes parentheses, logical negation, short-circuit conjunction, and short-circuit disjunction into deterministic disjunctive normal form. CFG successor polarity determines whether each branch condition or its negation constrains the witness. Normalization is bounded to 256 alternatives.

Subjects are keyed by their SSA function, concrete SSA value kind, SSA name, and source position. Reassigning a source variable therefore creates a distinct constraint subject and cannot introduce a false contradiction between different value versions.

## Unsupported predicates

An unsupported atom is conservatively replaced by `true` while the formula is
marked incomplete. This produces an over-approximation: if every resulting DNF
alternative is contradictory, the original predicate is necessarily
contradictory too and checked UNSAT evidence remains sound. If any alternative
survives, the result is `unknown`, never SAT. In a disjunction, an unsupported
branch therefore contributes an unconstrained alternative and prevents an
UNSAT result.

Missing SSA, unresolved calls, subject-to-subject comparisons, non-nil
comparisons, floats, complex values, arithmetic, dynamic type assertions,
reflection, or any expression outside the grammar above introduce this
incomplete marker. A timeout returns `unknown`; exceeding the normalization
bound replaces the bounded formula with an unconstrained incomplete
alternative. Unknown results are blocking `inconclusive` outcomes and never
discharge a witness.

## Checked UNSAT evidence

The decision procedure reports UNSAT only when every normalized alternative contains either:

- equality and inequality of the same SSA subject with the same constant; or
- equalities of the same SSA subject with two different constants.

The result includes versioned evidence naming one contradictory atom pair per alternative, or an explicit constant-false certificate. A separate checker re-extracts the same over-approximated formula and verifies its completeness marker, digest, evidence version, alternative index, atom membership, and contradiction relation. Missing, malformed, timed-out, or rejected evidence becomes `unknown`; only accepted evidence may produce a trace-only `discharged-infeasible` record.

SAT preserves the witness as a `violation` or existing `inconclusive` outcome. Iterative refinement may discharge checked-UNSAT witnesses until another witness is retained, the obligation has no remaining witness, or a query/iteration limit produces blocking `inconclusive`.
