// SPDX-License-Identifier: MPL-2.0

package goplint

// runIFDSPropagation preserves the concise solver setup used by focused tests.
// Production callers use runIFDSPropagationControlled and provide every
// dependency explicitly.
func runIFDSPropagation(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	callChain []string,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	edgeTransfers ...interprocEdgeTransferFn,
) interprocPathResult {
	if len(edgeTransfers) > 1 {
		panic("test solver accepts at most one edge transfer")
	}
	var edgeTransfer interprocEdgeTransferFn
	if len(edgeTransfers) == 1 {
		edgeTransfer = edgeTransfers[0]
	}
	return runIFDSPropagationControlled(
		graph,
		start,
		maxStates,
		callChain,
		dischargedWitnesses,
		witnessHash,
		transfer,
		terminalUnsafe,
		unresolvedCallRelevant,
		nil,
		edgeTransfer,
	)
}

func runIFDSPropagationWithSink(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	callChain []string,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	obligationSink interprocObligationSinkFn,
	sinkPolicy interprocSinkPolicy,
	edgeTransfers ...interprocEdgeTransferFn,
) interprocPathResult {
	if len(edgeTransfers) > 1 {
		panic("test solver accepts at most one edge transfer")
	}
	var edgeTransfer interprocEdgeTransferFn
	if len(edgeTransfers) == 1 {
		edgeTransfer = edgeTransfers[0]
	}
	return runIFDSPropagationWithSinkControlled(
		graph,
		start,
		maxStates,
		callChain,
		dischargedWitnesses,
		witnessHash,
		transfer,
		terminalUnsafe,
		unresolvedCallRelevant,
		obligationSink,
		sinkPolicy,
		nil,
		edgeTransfer,
	)
}
