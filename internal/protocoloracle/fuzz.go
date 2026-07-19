// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"crypto/sha256"
	"fmt"
)

// DecodeFuzzProgram deterministically maps bytes to one well-formed integrated
// protocol program. Every decoded program contains explicit entry facts,
// identities, procedures, a call site with a matching return, aliases,
// constraints, and node effects so differential fuzzing cannot validate those
// dimensions as disconnected component laws.
func DecodeFuzzProgram(data []byte) (Program, error) {
	topologies := []Topology{TopologyCallReturn, TopologyRecursive, TopologyBranchJoin}
	operations := []Operation{
		OperationNoop,
		OperationValidate,
		OperationConsume,
		OperationMutate,
		OperationReplace,
		OperationEscape,
		OperationUnresolved,
	}
	conditions := []ConditionalResult{
		ConditionalResultNone,
		ConditionalResultNil,
		ConditionalResultNonNil,
		ConditionalResultUnknown,
	}
	aliases := []AliasAction{AliasActionNone, AliasActionCopy, AliasActionKill}
	unknownEffects := []UnknownEffect{
		UnknownEffectNone,
		UnknownEffectUnresolved,
		UnknownEffectConcurrentMutation,
		UnknownEffectEscapedHeap,
	}
	constraints := []ConstraintKind{ConstraintNone, ConstraintSAT, ConstraintUNSAT}
	facts := []InitialFact{InitialFactNeedsValidation, InitialFactValidated}
	topology := topologies[fuzzByteAt(data, 6)%byte(len(topologies))]
	shape := Shape{
		Procedures:        2,
		NodesPerProcedure: 4,
		Identities:        2,
		CallSites:         1,
		CallDepth:         1,
		Topology:          topology,
		BranchJoin:        topology == TopologyBranchJoin,
		Recursive:         topology == TopologyRecursive,
	}
	if topology == TopologyRecursive {
		shape.CallSites = 2
		shape.CallDepth = 2
	}
	digest := sha256.Sum256(data)
	configuration := generatedConfiguration{
		caseID:            fmt.Sprintf("fuzz/%x", digest[:8]),
		shape:             shape,
		operation:         operations[fuzzByteAt(data, 1)%byte(len(operations))],
		condition:         conditions[fuzzByteAt(data, 2)%byte(len(conditions))],
		aliasAction:       aliases[fuzzByteAt(data, 3)%byte(len(aliases))],
		unknownEffect:     unknownEffects[fuzzByteAt(data, 4)%byte(len(unknownEffects))],
		constraint:        constraints[fuzzByteAt(data, 5)%byte(len(constraints))],
		initialFact:       facts[fuzzByteAt(data, 0)%byte(len(facts))],
		validateFirst:     fuzzByteAt(data, 7)&1 != 0,
		protectedIdentity: Identity(fuzzByteAt(data, 8) % 2),
	}
	program, err := generatedProgram(configuration)
	if err != nil {
		return Program{}, err
	}
	if err := program.Validate(); err != nil {
		return Program{}, err
	}
	return program, nil
}

func fuzzByteAt(data []byte, index int) byte {
	if len(data) == 0 {
		return 0
	}
	return data[index%len(data)]
}
