// SPDX-License-Identifier: MPL-2.0

// Package protocoloracle defines a test-only normalized protocol model.
// It intentionally depends only on the Go standard library.
package protocoloracle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Identity identifies one independently tracked value in a generated program.
type Identity uint8

// InitialFact is the protocol fact attached to an identity at program entry.
type InitialFact string

const (
	InitialFactNeedsValidation InitialFact = "needs-validation"
	InitialFactValidated       InitialFact = "validated"
)

// NodeRef identifies a node by procedure and procedure-local index.
type NodeRef struct {
	Procedure uint8 `json:"procedure"`
	Node      uint8 `json:"node"`
}

// Key returns the fully qualified stable node key.
func (ref NodeRef) Key() string {
	return fmt.Sprintf("p%d:n%d", ref.Procedure, ref.Node)
}

// Operation is a protocol effect performed at a generated node.
type Operation string

const (
	OperationNoop       Operation = "noop"
	OperationValidate   Operation = "validate"
	OperationConsume    Operation = "consume"
	OperationMutate     Operation = "mutate"
	OperationReplace    Operation = "replace"
	OperationEscape     Operation = "escape"
	OperationUnresolved Operation = "unresolved"
)

// ConditionalResult is the result fact controlling a validation effect.
type ConditionalResult string

const (
	ConditionalResultNone    ConditionalResult = "none"
	ConditionalResultNil     ConditionalResult = "nil"
	ConditionalResultNonNil  ConditionalResult = "non-nil"
	ConditionalResultUnknown ConditionalResult = "unknown"
)

// AliasAction changes the generated must-alias environment.
type AliasAction string

const (
	AliasActionNone AliasAction = "none"
	AliasActionCopy AliasAction = "copy"
	AliasActionKill AliasAction = "kill"
)

// UnknownEffect identifies a conservative effect that production cannot resolve precisely.
type UnknownEffect string

const (
	UnknownEffectNone               UnknownEffect = "none"
	UnknownEffectUnresolved         UnknownEffect = "unresolved"
	UnknownEffectConcurrentMutation UnknownEffect = "concurrent-mutation"
	UnknownEffectEscapedHeap        UnknownEffect = "escaped-heap-mutation"
)

// ConstraintKind is the bounded refinement predicate attached to a node.
type ConstraintKind string

const (
	ConstraintNone  ConstraintKind = "none"
	ConstraintSAT   ConstraintKind = "sat"
	ConstraintUNSAT ConstraintKind = "unsat"
)

// Topology identifies the generated control-flow family.
type Topology string

const (
	TopologyLinear     Topology = "linear"
	TopologyBranchJoin Topology = "branch-join"
	TopologyCallReturn Topology = "call-return"
	TopologyRecursive  Topology = "recursive"
)

// Node is one operation in the normalized protocol program.
type Node struct {
	Ref           NodeRef           `json:"ref"`
	Operation     Operation         `json:"operation"`
	Identity      Identity          `json:"identity"`
	Condition     ConditionalResult `json:"condition,omitempty"`
	AliasAction   AliasAction       `json:"alias_action,omitempty"`
	AliasSource   Identity          `json:"alias_source,omitempty"`
	UnknownEffect UnknownEffect     `json:"unknown_effect,omitempty"`
	Constraint    ConstraintKind    `json:"constraint,omitempty"`
	Terminal      bool              `json:"terminal,omitempty"`
}

// EdgeKind identifies an intra-procedural, call, or matching-return edge.
type EdgeKind string

const (
	EdgeIntra  EdgeKind = "intra"
	EdgeCall   EdgeKind = "call"
	EdgeReturn EdgeKind = "return"
)

// Edge is a fully qualified generated control-flow edge.
type Edge struct {
	From     NodeRef  `json:"from"`
	To       NodeRef  `json:"to"`
	Kind     EdgeKind `json:"kind"`
	CallSite uint8    `json:"call_site,omitempty"`
}

// Shape records every manifest dimension that admitted a generated program.
type Shape struct {
	Procedures        int      `json:"procedures"`
	NodesPerProcedure int      `json:"nodes_per_procedure"`
	Identities        int      `json:"identities"`
	CallSites         int      `json:"call_sites"`
	CallDepth         int      `json:"call_depth"`
	Topology          Topology `json:"topology"`
	BranchJoin        bool     `json:"branch_join"`
	Recursive         bool     `json:"recursive"`
}

// ProgramMetrics records dimensions derived from the admitted graph rather
// than copied from its manifest declaration.
type ProgramMetrics struct {
	Procedures        int
	NodesPerProcedure int
	Identities        int
	ValidatedFacts    int
	CallSites         int
	CallDepth         int
	BranchJoin        bool
	Recursive         bool
}

// Program is one admitted normalized program and its source-generation dimensions.
type Program struct {
	CaseID       string        `json:"case_id"`
	Shape        Shape         `json:"shape"`
	Entry        NodeRef       `json:"entry"`
	Nodes        []Node        `json:"nodes"`
	Edges        []Edge        `json:"edges"`
	Identities   []Identity    `json:"identities"`
	InitialFacts []InitialFact `json:"initial_facts"`
}

// Validate checks normalized graph and dimension well-formedness.
func (program Program) Validate() error {
	if program.CaseID == "" || len(program.Nodes) == 0 || len(program.Identities) == 0 {
		return errors.New("program requires a case ID, nodes, and identities")
	}
	if program.Shape.Procedures < 1 || program.Shape.NodesPerProcedure < 1 ||
		program.Shape.Identities < 1 || program.Shape.CallSites < 0 || program.Shape.CallDepth < 0 {
		return errors.New("program shape contains an invalid bound")
	}
	switch program.Shape.Topology {
	case TopologyLinear, TopologyBranchJoin, TopologyCallReturn, TopologyRecursive:
	default:
		return fmt.Errorf("program shape has unsupported topology %q", program.Shape.Topology)
	}
	if len(program.Identities) != program.Shape.Identities {
		return fmt.Errorf("program admits %d identities, want %d", len(program.Identities), program.Shape.Identities)
	}
	if len(program.InitialFacts) != len(program.Identities) {
		return fmt.Errorf("program admits %d initial facts, want one for each of %d identities", len(program.InitialFacts), len(program.Identities))
	}
	for index, identity := range program.Identities {
		if identity != Identity(index) {
			return fmt.Errorf("identity %d is %d, want a contiguous admitted identity", index, identity)
		}
		switch program.InitialFacts[index] {
		case InitialFactNeedsValidation, InitialFactValidated:
		default:
			return fmt.Errorf("identity %d has unsupported initial fact %q", identity, program.InitialFacts[index])
		}
	}
	nodes := make(map[string]bool, len(program.Nodes))
	nodesByProcedure := make(map[uint8]int)
	for _, node := range program.Nodes {
		key := node.Ref.Key()
		if nodes[key] {
			return fmt.Errorf("duplicate node %s", key)
		}
		nodes[key] = true
		nodesByProcedure[node.Ref.Procedure]++
		if int(node.Ref.Procedure) >= program.Shape.Procedures {
			return fmt.Errorf("node %s references procedure outside the admitted set", key)
		}
		if int(node.Ref.Node) >= program.Shape.NodesPerProcedure {
			return fmt.Errorf("node %s references node index outside the admitted set", key)
		}
		if int(node.Identity) >= len(program.Identities) || int(node.AliasSource) >= len(program.Identities) {
			return fmt.Errorf("node %s references identity outside the admitted set", key)
		}
		if err := validateNodeEnums(node); err != nil {
			return fmt.Errorf("node %s: %w", key, err)
		}
	}
	for procedure := range program.Shape.Procedures {
		count := nodesByProcedure[uint8(procedure)]
		if count != program.Shape.NodesPerProcedure {
			return fmt.Errorf("procedure %d contains %d nodes, want %d", procedure, count, program.Shape.NodesPerProcedure)
		}
	}
	if !nodes[program.Entry.Key()] {
		return fmt.Errorf("entry %s is missing", program.Entry.Key())
	}
	callSites := make(map[uint8]uint8)
	returns := make(map[uint8]uint8)
	for _, edge := range program.Edges {
		if !nodes[edge.From.Key()] || !nodes[edge.To.Key()] {
			return fmt.Errorf("edge %s -> %s has missing endpoint", edge.From.Key(), edge.To.Key())
		}
		switch edge.Kind {
		case EdgeIntra, EdgeCall, EdgeReturn:
		default:
			return fmt.Errorf("edge %s -> %s has unsupported kind %q", edge.From.Key(), edge.To.Key(), edge.Kind)
		}
		if (edge.Kind == EdgeCall || edge.Kind == EdgeReturn) && edge.CallSite == 0 {
			return fmt.Errorf("%s edge requires call site", edge.Kind)
		}
		if edge.Kind == EdgeCall {
			callSites[edge.CallSite]++
		}
		if edge.Kind == EdgeReturn {
			returns[edge.CallSite]++
		}
	}
	for callSite, count := range callSites {
		if count != 1 || returns[callSite] != 1 {
			return fmt.Errorf("call site %d has %d calls and %d returns, want one matched pair", callSite, count, returns[callSite])
		}
	}
	for callSite := range returns {
		if callSites[callSite] == 0 {
			return fmt.Errorf("return edge for call site %d has no matching call", callSite)
		}
	}
	metrics := program.Metrics()
	if metrics.Procedures != program.Shape.Procedures || metrics.NodesPerProcedure != program.Shape.NodesPerProcedure ||
		metrics.Identities != program.Shape.Identities || metrics.CallSites != program.Shape.CallSites ||
		metrics.CallDepth != program.Shape.CallDepth || metrics.BranchJoin != program.Shape.BranchJoin ||
		metrics.Recursive != program.Shape.Recursive {
		return fmt.Errorf("declared shape %+v does not match derived graph metrics %+v", program.Shape, metrics)
	}
	return nil
}

func validateNodeEnums(node Node) error {
	switch node.Operation {
	case OperationNoop, OperationValidate, OperationConsume, OperationMutate, OperationReplace,
		OperationEscape, OperationUnresolved:
	default:
		return fmt.Errorf("unsupported operation %q", node.Operation)
	}
	switch node.Condition {
	case "", ConditionalResultNone, ConditionalResultNil, ConditionalResultNonNil, ConditionalResultUnknown:
	default:
		return fmt.Errorf("unsupported conditional result %q", node.Condition)
	}
	switch node.AliasAction {
	case "", AliasActionNone, AliasActionCopy, AliasActionKill:
	default:
		return fmt.Errorf("unsupported alias action %q", node.AliasAction)
	}
	switch node.UnknownEffect {
	case "", UnknownEffectNone, UnknownEffectUnresolved, UnknownEffectConcurrentMutation, UnknownEffectEscapedHeap:
	default:
		return fmt.Errorf("unsupported unknown effect %q", node.UnknownEffect)
	}
	switch node.Constraint {
	case "", ConstraintNone, ConstraintSAT, ConstraintUNSAT:
	default:
		return fmt.Errorf("unsupported constraint %q", node.Constraint)
	}
	return nil
}

// Metrics derives the enumerable graph dimensions used by cardinality,
// sensitivity, and feature-census checks.
func (program Program) Metrics() ProgramMetrics {
	procedures := make(map[uint8]int)
	callSites := make(map[uint8]bool)
	callGraph := make(map[uint8]map[uint8]bool)
	branchJoin := false
	recursive := false
	intraIncoming := make(map[string]int)
	for _, node := range program.Nodes {
		procedures[node.Ref.Procedure]++
	}
	validatedFacts := 0
	for _, fact := range program.InitialFacts {
		if fact == InitialFactValidated {
			validatedFacts++
		}
	}
	for _, edge := range program.Edges {
		switch edge.Kind {
		case EdgeIntra:
			intraIncoming[edge.To.Key()]++
			if intraIncoming[edge.To.Key()] > 1 {
				branchJoin = true
			}
		case EdgeCall:
			callSites[edge.CallSite] = true
			if callGraph[edge.From.Procedure] == nil {
				callGraph[edge.From.Procedure] = make(map[uint8]bool)
			}
			callGraph[edge.From.Procedure][edge.To.Procedure] = true
			if edge.From.Procedure == edge.To.Procedure {
				recursive = true
			}
		case EdgeReturn:
			// Return edges contribute to call-site validation, not call depth.
		}
	}
	nodesPerProcedure := 0
	for _, count := range procedures {
		if nodesPerProcedure == 0 || count < nodesPerProcedure {
			nodesPerProcedure = count
		}
	}
	return ProgramMetrics{
		Procedures:        len(procedures),
		NodesPerProcedure: nodesPerProcedure,
		Identities:        len(program.Identities),
		ValidatedFacts:    validatedFacts,
		CallSites:         len(callSites),
		CallDepth:         callGraphDepth(callGraph),
		BranchJoin:        branchJoin,
		Recursive:         recursive,
	}
}

// Fingerprint returns a stable identity for the exact admitted program.
func (program Program) Fingerprint() string {
	normalized := program
	normalized.Nodes = append([]Node(nil), program.Nodes...)
	normalized.Edges = append([]Edge(nil), program.Edges...)
	sort.Slice(normalized.Nodes, func(i, j int) bool {
		return normalized.Nodes[i].Ref.Key() < normalized.Nodes[j].Ref.Key()
	})
	sort.Slice(normalized.Edges, func(i, j int) bool {
		left, right := normalized.Edges[i], normalized.Edges[j]
		leftKey := fmt.Sprintf("%s|%s|%d|%s", left.From.Key(), left.Kind, left.CallSite, left.To.Key())
		rightKey := fmt.Sprintf("%s|%s|%d|%s", right.From.Key(), right.Kind, right.CallSite, right.To.Key())
		return leftKey < rightKey
	})
	encoded, err := json.Marshal(normalized)
	if err != nil {
		panic("marshal normalized protocol program: " + err.Error())
	}
	digest := sha256.Sum256(encoded)
	return "poracle3_" + hex.EncodeToString(digest[:])
}

// InitialFactFor returns the entry fact for an admitted identity.
func (program Program) InitialFactFor(identity Identity) InitialFact {
	if int(identity) >= len(program.InitialFacts) {
		return ""
	}
	return program.InitialFacts[identity]
}

func callGraphDepth(graph map[uint8]map[uint8]bool) int {
	var visit func(uint8, map[uint8]bool) int
	visit = func(procedure uint8, active map[uint8]bool) int {
		best := 0
		active[procedure] = true
		for callee := range graph[procedure] {
			depth := 1
			if !active[callee] {
				depth += visit(callee, active)
			}
			best = max(best, depth)
		}
		delete(active, procedure)
		return best
	}
	best := 0
	for procedure := range graph {
		best = max(best, visit(procedure, make(map[uint8]bool)))
	}
	return best
}

// Outcome is the normalized analyzer result for one tracked identity.
type Outcome string

const (
	OutcomeNone         Outcome = "none"
	OutcomeViolation    Outcome = "violation"
	OutcomeInconclusive Outcome = "inconclusive"
)

// IdentityResult records the outcome and independently derived evidence for one identity.
type IdentityResult struct {
	Outcome          Outcome  `json:"outcome"`
	Uncertainty      []string `json:"uncertainty,omitempty"`
	Witness          []string `json:"witness,omitempty"`
	RefinedWitnesses []string `json:"refined_witnesses,omitempty"`
	Summaries        int      `json:"summaries,omitempty"`
	SummaryReuses    int      `json:"summary_reuses,omitempty"`
}

// Result records the independently interpreted result by identity.
type Result struct {
	ByIdentity map[Identity]Outcome        `json:"by_identity"`
	Evidence   map[Identity]IdentityResult `json:"evidence"`
}
