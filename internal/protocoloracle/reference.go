// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"fmt"
	"sort"
	"strings"
)

const maxReferenceIdentities = 8

type referenceState struct {
	validated   [maxReferenceIdentities]bool
	violated    bool
	uncertainty uint8
	aliases     [maxReferenceIdentities]Identity
}

func (state referenceState) key() string {
	return fmt.Sprintf(
		"validated=%v|violated=%t|uncertainty=%d|aliases=%v",
		state.validated,
		state.violated,
		state.uncertainty,
		state.aliases,
	)
}

type referenceEntryFact struct {
	node  NodeRef
	state referenceState
}

func (fact referenceEntryFact) key() string {
	return fact.node.Key() + "|" + fact.state.key()
}

type referencePathEdge struct {
	entry   referenceEntryFact
	node    NodeRef
	state   referenceState
	witness []string
}

func (edge referencePathEdge) key() string {
	return edge.entry.key() + "|node=" + edge.node.Key() + "|" + edge.state.key()
}

type referenceDependency struct {
	calleeEntry   referenceEntryFact
	calleeExit    NodeRef
	callerEntry   referenceEntryFact
	callSite      uint8
	returnNode    NodeRef
	callerState   referenceState
	callerWitness []string
}

func (dependency referenceDependency) key() string {
	return fmt.Sprintf("%s|exit=%s|caller=%s|site=%d|return=%s|state=%s",
		dependency.calleeEntry.key(), dependency.calleeExit.Key(), dependency.callerEntry.key(),
		dependency.callSite, dependency.returnNode.Key(), dependency.callerState.key())
}

type referenceSummary struct {
	entry   referenceEntryFact
	exit    NodeRef
	state   referenceState
	witness []string
}

func (summary referenceSummary) key() string {
	return summary.entry.key() + "|exit=" + summary.exit.Key() + "|" + summary.state.key()
}

// Interpret evaluates a program without importing or invoking production helpers.
func Interpret(program Program, maxStates int) Result {
	result := Result{
		ByIdentity: make(map[Identity]Outcome, len(program.Identities)),
		Evidence:   make(map[Identity]IdentityResult, len(program.Identities)),
	}
	for _, identity := range program.Identities {
		identityResult := interpretIdentity(program, identity, maxStates)
		result.ByIdentity[identity] = identityResult.Outcome
		result.Evidence[identity] = identityResult
	}
	return result
}

func interpretIdentity(program Program, identity Identity, maxStates int) IdentityResult {
	if err := program.Validate(); err != nil || maxStates <= 0 || len(program.Identities) > maxReferenceIdentities {
		return IdentityResult{Outcome: OutcomeInconclusive, Uncertainty: []string{"invalid-program"}}
	}
	nodes := make(map[string]Node, len(program.Nodes))
	for _, node := range program.Nodes {
		nodes[node.Ref.Key()] = node
	}
	edges := append([]Edge(nil), program.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		return referenceEdgeKey(edges[i]) < referenceEdgeKey(edges[j])
	})
	outgoing := make(map[string][]Edge, len(program.Nodes))
	for _, edge := range edges {
		key := edge.From.Key()
		outgoing[key] = append(outgoing[key], edge)
	}
	initial := referenceState{}
	for index := range program.Identities {
		initial.aliases[index] = Identity(index)
		initial.validated[index] = program.InitialFactFor(Identity(index)) == InitialFactValidated
	}
	rootEntry := referenceEntryFact{node: program.Entry, state: initial}
	pathEdges := make(map[string]referencePathEdge)
	queue := make([]string, 0)
	dependencies := make(map[string]referenceDependency)
	dependenciesByEntry := make(map[string][]string)
	summaries := make(map[string]referenceSummary)
	summariesByEntry := make(map[string][]string)
	outcome := OutcomeNone
	uncertainty := uint8(0)
	var witness []string
	var refinedWitnesses []string
	summaryReuses := 0
	workUnits := 0
	budgetExceeded := false
	reserve := func() bool {
		workUnits++
		if workUnits <= maxStates {
			return true
		}
		budgetExceeded = true
		return false
	}
	enqueue := func(edge referencePathEdge) bool {
		key := edge.key()
		if _, exists := pathEdges[key]; exists {
			return true
		}
		if !reserve() {
			return false
		}
		pathEdges[key] = edge
		queue = append(queue, key)
		return true
	}
	applySummary := func(dependency referenceDependency, summary referenceSummary) bool {
		if dependency.calleeExit != summary.exit {
			return true
		}
		state := summary.state
		state.uncertainty |= dependency.callerState.uncertainty
		summaryReuses++
		return enqueue(referencePathEdge{
			entry:   dependency.callerEntry,
			node:    dependency.returnNode,
			state:   state,
			witness: appendWitness(dependency.callerWitness, summary.witness...),
		})
	}
	registerDependency := func(dependency referenceDependency) bool {
		key := dependency.key()
		if _, exists := dependencies[key]; exists {
			return true
		}
		if !reserve() {
			return false
		}
		dependencies[key] = dependency
		entryKey := dependency.calleeEntry.key()
		dependenciesByEntry[entryKey] = append(dependenciesByEntry[entryKey], key)
		sort.Strings(dependenciesByEntry[entryKey])
		for _, summaryKey := range summariesByEntry[entryKey] {
			if !applySummary(dependency, summaries[summaryKey]) {
				return false
			}
		}
		return true
	}
	publishSummary := func(summary referenceSummary) bool {
		key := summary.key()
		if _, exists := summaries[key]; exists {
			return true
		}
		if !reserve() {
			return false
		}
		summaries[key] = summary
		entryKey := summary.entry.key()
		summariesByEntry[entryKey] = append(summariesByEntry[entryKey], key)
		sort.Strings(summariesByEntry[entryKey])
		for _, dependencyKey := range dependenciesByEntry[entryKey] {
			if !applySummary(dependencies[dependencyKey], summary) {
				return false
			}
		}
		return true
	}
	_ = enqueue(referencePathEdge{entry: rootEntry, node: program.Entry, state: initial})

	for len(queue) > 0 && !budgetExceeded {
		key := queue[0]
		queue = queue[1:]
		path := pathEdges[key]
		node := nodes[path.node.Key()]
		if node.Constraint == ConstraintUNSAT {
			refinedWitnesses = append(refinedWitnesses, strings.Join(path.witness, " -> "))
			continue
		}
		path.state = applyReferenceNode(path.state, node, identity)
		if node.Terminal {
			nodeOutcome := referenceTerminalOutcome(path.state, identity)
			joined := joinOutcome(outcome, nodeOutcome)
			if joined != outcome || len(witness) == 0 {
				witness = append([]string(nil), path.witness...)
			}
			outcome = joined
			uncertainty |= path.state.uncertainty
			continue
		}

		returnEdges := referenceReturnEdgesFrom(outgoing[path.node.Key()])
		if len(returnEdges) > 0 {
			if !publishSummary(referenceSummary{entry: path.entry, exit: path.node, state: path.state, witness: path.witness}) {
				break
			}
			continue
		}
		for _, edge := range outgoing[path.node.Key()] {
			nextWitness := appendWitness(path.witness, referenceEdgeKey(edge))
			switch edge.Kind {
			case EdgeReturn:
				continue
			case EdgeCall:
				calleeEntry := referenceEntryFact{node: edge.To, state: path.state}
				for _, returnEdge := range referenceReturnEdgesForCall(edges, edge.CallSite, edge.To.Procedure) {
					if !registerDependency(referenceDependency{
						calleeEntry:   calleeEntry,
						calleeExit:    returnEdge.From,
						callerEntry:   path.entry,
						callSite:      edge.CallSite,
						returnNode:    returnEdge.To,
						callerState:   path.state,
						callerWitness: nextWitness,
					}) {
						break
					}
				}
				if budgetExceeded {
					break
				}
				_ = enqueue(referencePathEdge{entry: calleeEntry, node: edge.To, state: path.state, witness: nextWitness})
			default:
				_ = enqueue(referencePathEdge{entry: path.entry, node: edge.To, state: path.state, witness: nextWitness})
			}
		}
	}
	if budgetExceeded {
		outcome = joinOutcome(outcome, OutcomeInconclusive)
		uncertainty |= 1 << 4
	}
	sort.Strings(refinedWitnesses)
	return IdentityResult{
		Outcome:          outcome,
		Uncertainty:      referenceUncertaintyNames(uncertainty),
		Witness:          witness,
		RefinedWitnesses: refinedWitnesses,
		Summaries:        len(summaries),
		SummaryReuses:    summaryReuses,
	}
}

func applyReferenceNode(state referenceState, node Node, tracked Identity) referenceState {
	if int(node.Identity) >= len(state.aliases) || int(node.AliasSource) >= len(state.aliases) {
		state.uncertainty |= 1 << 5
		return state
	}
	switch node.AliasAction {
	case AliasActionNone:
		// Preserve the current must-alias environment.
	case AliasActionCopy:
		state.aliases[node.Identity] = state.aliases[node.AliasSource]
	case AliasActionKill:
		state.aliases[node.Identity] = node.Identity
		state.validated[node.Identity] = false
	}
	relevant := state.aliases[node.Identity] == state.aliases[tracked]
	if !relevant {
		return state
	}
	switch node.Operation {
	case OperationNoop:
		// A no-op does not change the tracked abstract state.
	case OperationConsume:
		if !state.validated[state.aliases[node.Identity]] {
			state.violated = true
		}
	case OperationEscape:
		state.uncertainty |= 1 << 3
	case OperationValidate:
		switch node.Condition {
		case ConditionalResultNone, ConditionalResultNonNil:
			// No successful validation fact is established.
		case ConditionalResultNil:
			state.validated[state.aliases[node.Identity]] = true
		case ConditionalResultUnknown:
			// Rechecking an already validated value does not revoke the
			// established fact merely because this later result is ignored.
			// An unvalidated value still carries an unresolved conditional
			// result because no successful validation edge was established.
			if !state.validated[state.aliases[node.Identity]] {
				state.uncertainty |= 1 << 0
			}
		}
	case OperationMutate, OperationReplace:
		state.validated[state.aliases[node.Identity]] = false
	case OperationUnresolved:
		state.uncertainty |= 1 << 1
	}
	switch node.UnknownEffect {
	case UnknownEffectNone:
		// No uncertainty contribution.
	case UnknownEffectUnresolved:
		state.uncertainty |= 1 << 1
	case UnknownEffectConcurrentMutation:
		state.uncertainty |= 1 << 2
	case UnknownEffectEscapedHeap:
		state.uncertainty |= 1 << 3
	}
	return state
}

func referenceTerminalOutcome(state referenceState, tracked Identity) Outcome {
	if state.violated || !state.validated[state.aliases[tracked]] {
		return OutcomeViolation
	}
	if state.uncertainty != 0 {
		return OutcomeInconclusive
	}
	return OutcomeNone
}

func referenceUncertaintyNames(bits uint8) []string {
	names := make([]string, 0)
	values := []struct {
		bit  uint8
		name string
	}{
		{bit: 1 << 0, name: "conditional-result"},
		{bit: 1 << 1, name: "unresolved"},
		{bit: 1 << 2, name: "concurrent-mutation"},
		{bit: 1 << 3, name: "escaped-heap-mutation"},
		{bit: 1 << 4, name: "state-budget"},
		{bit: 1 << 5, name: "identity-bound"},
	}
	for _, value := range values {
		if bits&value.bit != 0 {
			names = append(names, value.name)
		}
	}
	return names
}

func joinOutcome(left, right Outcome) Outcome {
	if left == OutcomeViolation || right == OutcomeViolation {
		return OutcomeViolation
	}
	if left == OutcomeInconclusive || right == OutcomeInconclusive {
		return OutcomeInconclusive
	}
	return OutcomeNone
}

func referenceEdgeKey(edge Edge) string {
	return fmt.Sprintf("%s|%s|%d|%s", edge.From.Key(), edge.Kind, edge.CallSite, edge.To.Key())
}

func referenceReturnEdgesFrom(edges []Edge) []Edge {
	out := make([]Edge, 0)
	for _, edge := range edges {
		if edge.Kind == EdgeReturn {
			out = append(out, edge)
		}
	}
	return out
}

func referenceReturnEdgesForCall(edges []Edge, callSite uint8, calleeProcedure uint8) []Edge {
	out := make([]Edge, 0)
	for _, edge := range edges {
		if edge.Kind == EdgeReturn && edge.CallSite == callSite && edge.From.Procedure == calleeProcedure {
			out = append(out, edge)
		}
	}
	sort.Slice(out, func(left, right int) bool {
		return strings.Compare(referenceEdgeKey(out[left]), referenceEdgeKey(out[right])) < 0
	})
	return out
}

func appendWitness(prefix []string, edges ...string) []string {
	result := make([]string, 0, len(prefix)+len(edges))
	result = append(result, prefix...)
	result = append(result, edges...)
	return result
}
