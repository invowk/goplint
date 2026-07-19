# Canonical goplint protocol domain

This document is the semantic reference for goplint's protocol checks. The scope is property-relative: it describes validation, constructor, and use-before-validation obligations inside the supported SSA model. It is not a claim about arbitrary Go execution.

## State and ordering

Each tracked SSA/object identity has a must-validation component and hazard/uncertainty sets. `validated` is below `validation-required` in the information order. The control-flow join therefore keeps `validated` only when every incoming executable path proves it. Escape and consumption hazards, and uncertainty reasons, join by set union.

The initial uncertainty vocabulary is: `unsupported-predicate`, `unresolved-call`, `ambiguous-identity`, `incompatible-fact`, `missing-fact`, `missing-ssa`, `state-budget`, `query-budget`, `iteration-budget`, `timeout`, `evidence-rejected`, `unsupported-instruction`, `reflection`, `unsafe`, `concurrent-mutation`, and `escaped-heap-mutation`.

## Conditional validation

A `Validate() error` invocation creates a relation between one receiver identity and one error-result identity. The receiver moves from `validation-required` to `validated` only on an edge proving that associated error identity nil. A non-nil edge preserves the obligation. An unsupported or unresolved condition preserves the obligation and adds a stable uncertainty reason.

Escape or protected consumption before successful validation records a hazard. A copy transfers a must-alias relation. Rebinding, a new allocation, or a store kills the previous relation. A path join preserves an alias only when every predecessor has the same relation.

## Outcomes

For one obligation, any supported feasible hazardous path yields `violation`, even if another path is unresolved. If there is no feasible violation but any relevant path is unresolved, the result is blocking `inconclusive`. Otherwise no diagnostic is emitted. `discharged-infeasible` is structured trace evidence for one independently checked UNSAT witness; it is never a top-level safe result and cannot suppress uncertainty on another path.

An effect is relevant only when it is forward-reachable from the obligation origin, can reach the protected sink or constructor return, and may validate, mutate, alias, escape, consume, terminate, or constrain feasibility for the tracked identity. Unknown behavior outside that slice does not affect the obligation.
