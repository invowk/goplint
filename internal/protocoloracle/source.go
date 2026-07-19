// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// GoProjectionTrace records the declared IR dimensions that were faithfully
// projected into one generated Go program. It is returned to differential
// tests so evidence can be derived from the executed projection instead of
// restating manifest dimensions after the analyzer has run.
type GoProjectionTrace struct {
	TrackedIdentity     Identity
	Identities          int
	InitialFacts        int
	Procedures          int
	Nodes               int
	IntraEdges          int
	CallEdges           int
	ReturnEdges         int
	AliasActions        int
	Constraints         int
	ValidationSemantics bool
}

// Dimensions returns the semantic dimensions actually present in this
// projection. Callers aggregate dimensions across a corpus before emitting
// semantic evidence.
func (trace GoProjectionTrace) Dimensions() []string {
	dimensions := make([]string, 0, 6)
	if trace.AliasActions > 0 {
		dimensions = append(dimensions, "aliases")
	}
	if trace.CallEdges > 0 {
		dimensions = append(dimensions, "call-sites")
	}
	if trace.ValidationSemantics {
		dimensions = append(dimensions, "cast-validation")
	}
	if trace.Constraints > 0 {
		dimensions = append(dimensions, "constraints")
	}
	if trace.InitialFacts > 0 {
		dimensions = append(dimensions, "facts")
	}
	if trace.ReturnEdges > 0 {
		dimensions = append(dimensions, "return-edges")
	}
	return dimensions
}

// GoSource renders an admitted program for its first tracked identity.
func GoSource(program Program) (string, error) {
	if len(program.Identities) == 0 {
		return "", errors.New("generated program has no tracked identity")
	}
	return GoSourceForIdentity(program, program.Identities[0])
}

// GoSourceForIdentity renders the complete generated graph while projecting
// diagnostics onto one tracked identity. Calling it for every admitted
// identity makes the end-to-end comparison cover the whole program without
// conflating diagnostics from distinct origins.
func GoSourceForIdentity(program Program, tracked Identity) (string, error) {
	source, _, err := GoSourceForIdentityWithTrace(program, tracked)
	return source, err
}

// GoSourceForIdentityWithTrace renders a generated program and returns the
// exact structural dimensions projected into that source.
func GoSourceForIdentityWithTrace(program Program, tracked Identity) (string, GoProjectionTrace, error) {
	if err := program.Validate(); err != nil {
		return "", GoProjectionTrace{}, err
	}
	if !containsIdentity(program.Identities, tracked) {
		return "", GoProjectionTrace{}, fmt.Errorf("tracked identity %d is not admitted", tracked)
	}
	trace, err := validateGoProjection(program, tracked)
	if err != nil {
		return "", GoProjectionTrace{}, err
	}
	nodesByProcedure := make(map[uint8][]Node, program.Shape.Procedures)
	callsByNode := make(map[string][]Edge)
	branchesByNode := make(map[string]bool)
	for _, node := range program.Nodes {
		nodesByProcedure[node.Ref.Procedure] = append(nodesByProcedure[node.Ref.Procedure], node)
	}
	for _, edge := range program.Edges {
		if edge.Kind == EdgeCall {
			callsByNode[edge.From.Key()] = append(callsByNode[edge.From.Key()], edge)
		}
		if edge.Kind == EdgeIntra && edge.From.Procedure == edge.To.Procedure && edge.To.Node > edge.From.Node+1 {
			branchesByNode[edge.From.Key()] = true
		}
	}
	for procedure := range nodesByProcedure {
		sort.Slice(nodesByProcedure[procedure], func(i, j int) bool {
			return nodesByProcedure[procedure][i].Ref.Node < nodesByProcedure[procedure][j].Ref.Node
		})
	}
	for key := range callsByNode {
		sort.Slice(callsByNode[key], func(i, j int) bool {
			return callsByNode[key][i].CallSite < callsByNode[key][j].CallSite
		})
	}
	directHelpers := make(map[uint8]bool)
	for procedure := 1; procedure < program.Shape.Procedures; procedure++ {
		directHelpers[uint8(procedure)] = directValidationHelper(
			nodesByProcedure[uint8(procedure)],
			callsByNode,
			tracked,
		)
	}

	var source strings.Builder
	source.WriteString("package generated\n\n")
	source.WriteString("type Name string\n\n")
	source.WriteString("func (Name) Validate() error { return nil }\n\n")
	source.WriteString("type mutator interface { Mutate(*Name) }\n\n")
	source.WriteString("var escaped *Name\n\n")
	source.WriteString("func consume(Name) {}\n\n")
	for procedure := 1; procedure < program.Shape.Procedures; procedure++ {
		writeProcedure(
			&source,
			uint8(procedure),
			nodesByProcedure[uint8(procedure)],
			callsByNode,
			branchesByNode,
			directHelpers,
			program.Identities,
			tracked,
		)
	}
	writeEntry(
		&source,
		nodesByProcedure[0],
		callsByNode,
		branchesByNode,
		directHelpers,
		program.Identities,
		program.InitialFacts,
		tracked,
		projectedInitialFactIdentities(program, tracked),
	)
	return source.String(), trace, nil
}

func writeProcedure(
	source *strings.Builder,
	procedure uint8,
	nodes []Node,
	callsByNode map[string][]Edge,
	branchesByNode map[string]bool,
	directHelpers map[uint8]bool,
	identities []Identity,
	tracked Identity,
) {
	parameters := identityParameters(identities)
	if directHelpers[procedure] {
		fmt.Fprintf(source, "func procedure%d(%s) error {\n", procedure, parameters)
		target := directValidationTarget(nodes, tracked)
		writeUnusedIdentitiesExcept(source, identities, target)
		fmt.Fprintf(source, "\treturn %s.Validate()\n}\n\n", identityVariable(target))
		return
	}
	fmt.Fprintf(source, "func procedure%d(%s, raw string, choose bool, effect mutator) error {\n", procedure, parameters)
	source.WriteString("\tvar err error\n")
	writeUnusedIdentities(source, identities)
	source.WriteString("\t_ = raw\n\t_ = choose\n\t_ = effect\n\t_ = err\n")
	aliases := initialProjectionAliases(identities)
	variables := initialProjectionVariables(identities)
	for _, node := range nodes {
		if node.Constraint == ConstraintUNSAT && nodeRelevantToIdentity(node, tracked) {
			fmt.Fprintf(
				source,
				"\tif raw == \"left\" && raw != \"left\" { consume(%s) }\n",
				variables[tracked],
			)
		} else {
			writeSourceNode(source, node, tracked, aliases, variables, "return err", "return nil")
		}
		for _, edge := range callsByNode[node.Ref.Key()] {
			if directHelpers[edge.To.Procedure] {
				fmt.Fprintf(source, "\terr = procedure%d(%s)\n", edge.To.Procedure, identityArguments(variables))
				source.WriteString("\tif err != nil { return err }\n")
				continue
			}
			if edge.To.Procedure == procedure {
				fmt.Fprintf(
					source,
					"\tif choose { err = procedure%d(%s, raw, false, effect); if err != nil { return err } }\n",
					edge.To.Procedure,
					identityArguments(variables),
				)
				continue
			}
			fmt.Fprintf(
				source,
				"\terr = procedure%d(%s, raw, choose, effect)\n",
				edge.To.Procedure,
				identityArguments(variables),
			)
			source.WriteString("\tif err != nil { return err }\n")
		}
		if branchesByNode[node.Ref.Key()] {
			source.WriteString("\tif choose { return nil }\n")
		}
	}
	source.WriteString("\treturn nil\n}\n\n")
}

func directValidationHelper(nodes []Node, callsByNode map[string][]Edge, tracked Identity) bool {
	validations := 0
	for _, node := range nodes {
		if len(callsByNode[node.Ref.Key()]) > 0 || node.Constraint != ConstraintNone || node.AliasAction != AliasActionNone ||
			node.UnknownEffect != UnknownEffectNone {
			return false
		}
		if !nodeRelevantToIdentity(node, tracked) || node.Operation == OperationNoop {
			continue
		}
		if node.Operation != OperationValidate || node.Condition != ConditionalResultNil {
			return false
		}
		validations++
	}
	return validations == 1
}

func writeEntry(
	source *strings.Builder,
	nodes []Node,
	callsByNode map[string][]Edge,
	branchesByNode map[string]bool,
	directHelpers map[uint8]bool,
	identities []Identity,
	initialFacts []InitialFact,
	tracked Identity,
	projectedInitialFacts map[Identity]bool,
) {
	source.WriteString("func Entry(raw string, choose bool, effect mutator) error {\n")
	if hasUNSATConstraint(nodes) {
		if bypass := unsatBypassPrefix(nodes, branchesByNode); len(bypass) > 0 {
			var bypassBody strings.Builder
			writeEntryBody(
				&bypassBody,
				bypass,
				callsByNode,
				nil,
				directHelpers,
				identities,
				initialFacts,
				tracked,
				projectedInitialFacts,
				false,
			)
			source.WriteString("\tif choose {\n")
			writeIndentedSource(source, bypassBody.String(), "\t")
			source.WriteString("\t}\n")
		}
		var constrainedBody strings.Builder
		writeEntryBody(
			&constrainedBody,
			nodes,
			callsByNode,
			branchesByNode,
			directHelpers,
			identities,
			initialFacts,
			tracked,
			projectedInitialFacts,
			true,
		)
		source.WriteString("\tif raw == \"left\" {\n\t\tif raw != \"left\" {\n")
		writeIndentedSource(source, constrainedBody.String(), "\t\t")
		source.WriteString("\t\t}\n\t}\n\treturn nil\n}\n")
		return
	}
	writeEntryBody(
		source,
		nodes,
		callsByNode,
		branchesByNode,
		directHelpers,
		identities,
		initialFacts,
		tracked,
		projectedInitialFacts,
		false,
	)
	source.WriteString("}\n")
}

func hasUNSATConstraint(nodes []Node) bool {
	return slices.ContainsFunc(nodes, func(node Node) bool {
		return node.Constraint == ConstraintUNSAT
	})
}

func unsatBypassPrefix(nodes []Node, branchesByNode map[string]bool) []Node {
	unsatIndex := slices.IndexFunc(nodes, func(node Node) bool {
		return node.Constraint == ConstraintUNSAT
	})
	if unsatIndex < 0 {
		return nil
	}
	for index, node := range nodes[:unsatIndex] {
		if branchesByNode[node.Ref.Key()] {
			return nodes[:index+1]
		}
	}
	return nil
}

func writeEntryBody(
	source *strings.Builder,
	nodes []Node,
	callsByNode map[string][]Edge,
	branchesByNode map[string]bool,
	directHelpers map[uint8]bool,
	identities []Identity,
	initialFacts []InitialFact,
	tracked Identity,
	projectedInitialFacts map[Identity]bool,
	insideUNSATWitness bool,
) {
	for index, identity := range identities {
		variable := identityVariable(identity)
		if !projectedInitialFacts[identity] {
			fmt.Fprintf(source, "\t%s := Name(%q)\n", variable, "seed-identity-"+strconv.Itoa(index))
			continue
		}
		fmt.Fprintf(source, "\t%s := Name(raw)\n", variable)
		if initialFacts[index] == InitialFactValidated {
			fmt.Fprintf(source, "\tif err := %s.Validate(); err != nil { return err }\n", variable)
		}
	}
	source.WriteString("\tvar err error\n")
	writeUnusedIdentities(source, identities)
	source.WriteString("\t_ = choose\n\t_ = effect\n\t_ = err\n")
	aliases := initialProjectionAliases(identities)
	variables := initialProjectionVariables(identities)
	for _, node := range nodes {
		if node.Constraint == ConstraintUNSAT && nodeRelevantToIdentity(node, tracked) && !insideUNSATWitness {
			fmt.Fprintf(
				source,
				"\tif raw == \"left\" && raw != \"left\" { consume(%s) }\n",
				variables[tracked],
			)
		} else {
			if insideUNSATWitness && node.Constraint == ConstraintUNSAT {
				node.Constraint = ConstraintNone
			}
			writeSourceNode(source, node, tracked, aliases, variables, "return err", "return nil")
		}
		for _, edge := range callsByNode[node.Ref.Key()] {
			if directHelpers[edge.To.Procedure] {
				fmt.Fprintf(source, "\terr = procedure%d(%s)\n", edge.To.Procedure, identityArguments(variables))
				source.WriteString("\tif err != nil { return err }\n")
				continue
			}
			fmt.Fprintf(
				source,
				"\terr = procedure%d(%s, raw, choose, effect)\n",
				edge.To.Procedure,
				identityArguments(variables),
			)
			source.WriteString("\tif err != nil { return err }\n")
		}
		if branchesByNode[node.Ref.Key()] {
			fmt.Fprintf(source, "\tif choose { consume(%s); return nil }\n", variables[tracked])
		}
	}
	fmt.Fprintf(source, "\tconsume(%s)\n\treturn nil\n", variables[tracked])
}

func writeIndentedSource(source *strings.Builder, body, prefix string) {
	for line := range strings.SplitSeq(body, "\n") {
		if line == "" {
			continue
		}
		source.WriteString(prefix)
		source.WriteString(line)
		source.WriteByte('\n')
	}
}

func writeSourceNode(
	source *strings.Builder,
	node Node,
	tracked Identity,
	aliases []Identity,
	variables []string,
	errorReturn string,
	successReturn string,
) {
	target := variables[node.Identity]
	switch node.AliasAction {
	case AliasActionNone:
	case AliasActionCopy:
		aliasVariable := projectionVariableName("alias", node)
		target = variables[node.AliasSource]
		fmt.Fprintf(source, "\t%s := %s\n", aliasVariable, target)
		fmt.Fprintf(source, "\t_ = %s\n", aliasVariable)
		variables[node.Identity] = target
		aliases[node.Identity] = aliases[node.AliasSource]
	case AliasActionKill:
		target = projectionVariableName("rebound", node)
		aliases[node.Identity] = node.Identity
		if aliases[node.Identity] == aliases[tracked] {
			fmt.Fprintf(source, "\t%s := Name(raw)\n", target)
		} else {
			fmt.Fprintf(source, "\t%s := Name(%q)\n", target, "seed-rebound-"+strconv.Itoa(int(node.Identity)))
		}
		fmt.Fprintf(source, "\t_ = %s\n", target)
		variables[node.Identity] = target
	}
	if aliases[node.Identity] != aliases[tracked] {
		return
	}
	switch node.Operation {
	case OperationNoop:
		// The graph node is semantically inert; emitting a helper call would
		// introduce an unsupported-call effect absent from the admitted IR.
	case OperationValidate:
		switch node.Condition {
		case ConditionalResultNone:
		case ConditionalResultNil:
			fmt.Fprintf(source, "\tif err := %s.Validate(); err != nil { %s }\n", target, errorReturn)
		case ConditionalResultNonNil:
			fmt.Fprintf(source, "\tif err := %s.Validate(); err != nil { consume(%s); %s }\n", target, target, successReturn)
		case ConditionalResultUnknown:
			fmt.Fprintf(source, "\terr = %s.Validate()\n", target)
			fmt.Fprintf(source, "\tif choose && err != nil { %s }\n", errorReturn)
		}
	case OperationConsume:
		fmt.Fprintf(source, "\tconsume(%s)\n", target)
	case OperationMutate, OperationReplace:
		fmt.Fprintf(source, "\t%s = Name(raw)\n", target)
	case OperationEscape:
		fmt.Fprintf(source, "\tescaped = &%s\n", target)
	case OperationUnresolved:
		fmt.Fprintf(source, "\teffect.Mutate(&%s)\n", target)
	}
	if node.Constraint == ConstraintSAT {
		fmt.Fprintf(source, "\tif raw == \"live\" { consume(%s) }\n", target)
	}
	switch node.UnknownEffect {
	case UnknownEffectNone:
	case UnknownEffectUnresolved:
		if node.Operation != OperationUnresolved {
			fmt.Fprintf(source, "\teffect.Mutate(&%s)\n", target)
		}
	case UnknownEffectConcurrentMutation:
		fmt.Fprintf(source, "\tgo effect.Mutate(&%s)\n", target)
	case UnknownEffectEscapedHeap:
		fmt.Fprintf(source, "\tescaped = &%s\n", target)
	}
}

func nodeRelevantToIdentity(node Node, tracked Identity) bool {
	switch node.AliasAction {
	case AliasActionCopy:
		return node.Identity == tracked || node.AliasSource == tracked
	case AliasActionKill:
		return node.Identity == tracked
	default:
		return node.Identity == tracked
	}
}

func projectedInitialFactIdentities(program Program, tracked Identity) map[Identity]bool {
	projected := map[Identity]bool{tracked: true}
	for _, node := range program.Nodes {
		switch node.AliasAction {
		case AliasActionNone:
		case AliasActionCopy:
			if node.Identity == tracked {
				if !program.Shape.BranchJoin {
					delete(projected, tracked)
				}
				projected[node.AliasSource] = true
			}
		case AliasActionKill:
			if node.Identity == tracked && !program.Shape.BranchJoin {
				delete(projected, tracked)
			}
		}
	}
	return projected
}

func initialProjectionAliases(identities []Identity) []Identity {
	aliases := make([]Identity, len(identities))
	copy(aliases, identities)
	return aliases
}

func identityVariable(identity Identity) string {
	return "n" + strconv.Itoa(int(identity))
}

func identityParameters(identities []Identity) string {
	parameters := make([]string, 0, len(identities))
	for _, identity := range identities {
		parameters = append(parameters, identityVariable(identity)+" Name")
	}
	return strings.Join(parameters, ", ")
}

func initialProjectionVariables(identities []Identity) []string {
	variables := make([]string, len(identities))
	for index, identity := range identities {
		variables[index] = identityVariable(identity)
	}
	return variables
}

func identityArguments(variables []string) string {
	return strings.Join(variables, ", ")
}

func projectionVariableName(prefix string, node Node) string {
	return fmt.Sprintf("%s%dp%dn%d", prefix, node.Identity, node.Ref.Procedure, node.Ref.Node)
}

func writeUnusedIdentities(source *strings.Builder, identities []Identity) {
	for _, identity := range identities {
		fmt.Fprintf(source, "\t_ = %s\n", identityVariable(identity))
	}
}

func writeUnusedIdentitiesExcept(source *strings.Builder, identities []Identity, used Identity) {
	for _, identity := range identities {
		if identity != used {
			fmt.Fprintf(source, "\t_ = %s\n", identityVariable(identity))
		}
	}
}

func directValidationTarget(nodes []Node, tracked Identity) Identity {
	for _, node := range nodes {
		if nodeRelevantToIdentity(node, tracked) && node.Operation == OperationValidate {
			return node.Identity
		}
	}
	return tracked
}

func validateGoProjection(program Program, tracked Identity) (GoProjectionTrace, error) {
	trace := GoProjectionTrace{
		TrackedIdentity:     tracked,
		Identities:          len(program.Identities),
		Procedures:          program.Shape.Procedures,
		Nodes:               len(program.Nodes),
		ValidationSemantics: true,
	}
	trace.InitialFacts = len(projectedInitialFactIdentities(program, tracked))
	expectedEdges := generatedEdges(program.Shape, program.Shape.NodesPerProcedure)
	if !sameProjectionEdges(program.Edges, expectedEdges) {
		return GoProjectionTrace{}, errors.New("program edge set is not faithfully representable by the generated Go projection")
	}
	terminalCount := 0
	aliasActionCount := 0
	for _, node := range program.Nodes {
		if node.Terminal {
			terminalCount++
			if node.Ref != (NodeRef{Procedure: 0, Node: uint8(program.Shape.NodesPerProcedure - 1)}) {
				return GoProjectionTrace{}, fmt.Errorf("terminal node %s is not representable by the generated entry return", node.Ref.Key())
			}
		}
		if node.AliasAction != AliasActionNone {
			aliasActionCount++
			trace.AliasActions++
			if node.Ref.Procedure != 0 {
				return GoProjectionTrace{}, fmt.Errorf("callee alias action at %s cannot be projected back to its caller", node.Ref.Key())
			}
		}
		if node.Constraint != ConstraintNone {
			trace.Constraints++
		}
		if node.Ref.Procedure != 0 && node.Operation != OperationNoop &&
			(node.Operation != OperationValidate || node.Condition != ConditionalResultNil ||
				node.UnknownEffect != UnknownEffectNone || node.Constraint != ConstraintNone) {
			return GoProjectionTrace{}, fmt.Errorf("callee effect at %s is not faithfully representable", node.Ref.Key())
		}
	}
	if aliasActionCount > 1 {
		return GoProjectionTrace{}, fmt.Errorf("program has %d alias actions, generated projection supports at most one", aliasActionCount)
	}
	if terminalCount != 1 {
		return GoProjectionTrace{}, fmt.Errorf("program has %d terminal nodes, want one representable entry terminal", terminalCount)
	}
	for _, edge := range program.Edges {
		switch edge.Kind {
		case EdgeIntra:
			trace.IntraEdges++
		case EdgeCall:
			trace.CallEdges++
		case EdgeReturn:
			trace.ReturnEdges++
		}
	}
	if trace.CallEdges != trace.ReturnEdges || trace.CallEdges != program.Shape.CallSites {
		return GoProjectionTrace{}, fmt.Errorf(
			"projected call/return edges = %d/%d, want %d matched pairs",
			trace.CallEdges,
			trace.ReturnEdges,
			program.Shape.CallSites,
		)
	}
	return trace, nil
}

func sameProjectionEdges(got, want []Edge) bool {
	gotKeys := make([]string, len(got))
	for index, edge := range got {
		gotKeys[index] = referenceEdgeKey(edge)
	}
	wantKeys := make([]string, len(want))
	for index, edge := range want {
		wantKeys[index] = referenceEdgeKey(edge)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	return slices.Equal(gotKeys, wantKeys)
}

func containsIdentity(identities []Identity, tracked Identity) bool {
	return slices.Contains(identities, tracked)
}
