// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"
)

// interprocConditionalEdgeFunc is the ordered composition of canonical
// protocol-state transfers carried by a path edge.
type interprocConditionalEdgeFunc struct {
	required protocolAbstractState
	proven   protocolAbstractState
}

func identityInterprocConditionalEdgeFunc() interprocConditionalEdgeFunc {
	return interprocConditionalEdgeFunc{
		required: newProtocolRequiredState(),
		proven:   ideStateValidated,
	}
}

func (effect interprocConditionalEdgeFunc) apply(state protocolAbstractState) protocolAbstractState {
	transformed := effect.proven
	if state.validationRequired() {
		transformed = effect.required
	}
	state.Validation = transformed.Validation
	state.Hazards |= transformed.Hazards
	state.Result = transformed.Result
	return state
}

func (effect interprocConditionalEdgeFunc) then(tag ideEdgeFuncTag) interprocConditionalEdgeFunc {
	next := newIDEEdgeFunc(tag)
	return interprocConditionalEdgeFunc{
		required: next.Apply(effect.required),
		proven:   next.Apply(effect.proven),
	}
}

func (effect interprocConditionalEdgeFunc) thenSummary(next interprocConditionalEdgeFunc) interprocConditionalEdgeFunc {
	return interprocConditionalEdgeFunc{
		required: next.apply(effect.required),
		proven:   next.apply(effect.proven),
	}
}

func (effect interprocConditionalEdgeFunc) key() string {
	return effect.required.key() + ">" + effect.proven.key()
}

type interprocEntryFact struct {
	node  interprocNodeID
	state protocolAbstractState
}

func (fact interprocEntryFact) key() string {
	return strings.Join([]string{
		fact.node.Key(),
		"entry-fact=" + fact.state.key(),
	}, "|")
}

type interprocPathEdge struct {
	entry        interprocEntryFact
	node         interprocNodeID
	state        protocolAbstractState
	edgeFunction interprocConditionalEdgeFunc
	path         []int32
	witness      []interprocWitnessEdge
}

func (edge interprocPathEdge) key() string {
	return strings.Join([]string{
		edge.entry.key(),
		"procedure=" + edge.node.FuncKey,
		"node=" + edge.node.Key(),
		"current-fact=" + edge.state.key(),
		"edge-function=" + edge.edgeFunction.key(),
	}, "|")
}

type interprocCallDependency struct {
	calleeEntry   interprocEntryFact
	calleeExit    interprocNodeID
	callerEntry   interprocEntryFact
	callSite      string
	returnNode    interprocNodeID
	callerEffect  interprocConditionalEdgeFunc
	callerPath    []int32
	callerWitness []interprocWitnessEdge
}

func (dependency interprocCallDependency) key() string {
	return strings.Join([]string{
		dependency.calleeEntry.key(),
		"callee-exit=" + dependency.calleeExit.Key(),
		"caller-entry=" + dependency.callerEntry.key(),
		"call-site=" + dependency.callSite,
		"return-node=" + dependency.returnNode.Key(),
		"caller-effect=" + dependency.callerEffect.key(),
	}, "|")
}

type interprocProcedureSummary struct {
	entry           interprocEntryFact
	exit            interprocNodeID
	state           protocolAbstractState
	edgeFunction    interprocConditionalEdgeFunc
	path            []int32
	witness         []interprocWitnessEdge
	exitStateBefore protocolAbstractState
	exitTransferTag ideEdgeFuncTag
}

func (summary interprocProcedureSummary) key() string {
	return strings.Join([]string{
		summary.entry.key(),
		"exit=" + summary.exit.Key(),
		"exit-fact=" + summary.state.key(),
		"edge-function=" + summary.edgeFunction.key(),
	}, "|")
}

type interprocTabulationStats struct {
	PathEdges     int
	Dependencies  int
	Summaries     int
	SummaryReuses int
}

type protocolWorklistOrder uint8

const (
	protocolWorklistFIFO protocolWorklistOrder = iota
	protocolWorklistLIFO
)

// protocolWorklistOrderControl is an internal test seam for proving that the
// canonical result is independent of equivalent worklist schedules. Normal
// analyzer controls do not implement it, so production always uses FIFO.
type protocolWorklistOrderControl interface {
	protocolWorklistOrder() protocolWorklistOrder
}

type interprocTabulationOptions struct {
	InitialState *protocolAbstractState
	ObserveExit  func(interprocNodeID, protocolAbstractState)
	PruneEdge    func(interprocEdge, protocolAbstractState) bool
}

func takeProtocolWorklist(queue []string, control protocolAnalysisControl) (string, []string) {
	order := protocolWorklistFIFO
	if ordered, ok := control.(protocolWorklistOrderControl); ok {
		order = ordered.protocolWorklistOrder()
	}
	if order == protocolWorklistLIFO {
		last := len(queue) - 1
		return queue[last], queue[:last]
	}
	return queue[0], queue[1:]
}

func runIFDSPropagationWithStats(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	obligationSink interprocObligationSinkFn,
	sinkPolicy interprocSinkPolicy,
	edgeTransfer interprocEdgeTransferFn,
	control protocolAnalysisControl,
) (interprocPathResult, interprocTabulationStats) {
	return runIFDSPropagationWithStatsOptions(
		graph, start, maxStates, dischargedWitnesses, witnessHash, transfer,
		terminalUnsafe, unresolvedCallRelevant, obligationSink, sinkPolicy,
		edgeTransfer, control, interprocTabulationOptions{},
	)
}

func runIFDSPropagationWithStatsOptions(
	graph interprocSupergraph,
	start interprocNodeID,
	maxStates int,
	dischargedWitnesses map[string]bool,
	witnessHash interprocWitnessHashFunc,
	transfer interprocNodeTransferFn,
	terminalUnsafe interprocTerminalUnsafeFn,
	unresolvedCallRelevant interprocUnresolvedCallFn,
	obligationSink interprocObligationSinkFn,
	sinkPolicy interprocSinkPolicy,
	edgeTransfer interprocEdgeTransferFn,
	control protocolAnalysisControl,
	options interprocTabulationOptions,
) (interprocPathResult, interprocTabulationStats) {
	stats := interprocTabulationStats{}
	if maxStates <= 0 {
		maxStates = defaultCFGMaxStates
	}
	if feasibilityDeadlineReached(control) {
		return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonTimeout, nil), stats
	}
	if _, ok := graph.Nodes[start.Key()]; !ok {
		return interprocPathResultFromOutcome(pathOutcomeInconclusive, pathOutcomeReasonUnresolvedTarget, nil), stats
	}

	initialState := ideStateNeedsValidate
	if options.InitialState != nil {
		initialState = *options.InitialState
	}
	rootEntry := interprocEntryFact{node: start, state: initialState}
	pathEdges := make(map[string]interprocPathEdge)
	pathEdgeWitnesses := make(map[string]map[string]string)
	queue := make([]string, 0)
	dependencies := make(map[string]interprocCallDependency)
	dependenciesByEntry := make(map[string][]string)
	summaries := make(map[string]interprocProcedureSummary)
	summariesByEntry := make(map[string][]string)
	workUnits := 0
	var budgetResult *interprocPathResult
	var unsafeResult *interprocPathResult
	var inconclusiveResult *interprocPathResult

	setBudgetExceeded := func(reason pathOutcomeReason, path []int32, witness []interprocWitnessEdge) {
		if budgetResult != nil {
			return
		}
		result := interprocPathResultFromOutcome(pathOutcomeInconclusive, reason, path)
		result.WitnessEdges = cloneInterprocWitnessEdges(witness)
		result.WitnessTerminal = start
		if len(witness) > 0 {
			result.WitnessTerminal = witness[len(witness)-1].To
		}
		budgetResult = &result
	}
	checkpoint := func(path []int32, witness []interprocWitnessEdge) bool {
		if !feasibilityDeadlineReached(control) {
			return true
		}
		setBudgetExceeded(pathOutcomeReasonTimeout, path, witness)
		return false
	}
	reserveWorkUnit := func(path []int32, witness []interprocWitnessEdge) bool {
		if !checkpoint(path, witness) {
			return false
		}
		workUnits++
		if workUnits <= maxStates {
			return true
		}
		setBudgetExceeded(pathOutcomeReasonStateBudget, path, witness)
		return false
	}
	enqueue := func(edge interprocPathEdge) bool {
		if !checkpoint(edge.path, edge.witness) {
			return false
		}
		contributionKey := edge.key()
		witnessID := interprocWitnessIdentity(edge.witness)
		antichain := pathEdgeWitnesses[contributionKey]
		if antichain == nil {
			antichain = make(map[string]string)
			pathEdgeWitnesses[contributionKey] = antichain
		}
		var subsumedStorageKeys []string
		for retainedID, storageKey := range antichain {
			if !checkpoint(edge.path, edge.witness) {
				return false
			}
			retained := pathEdges[storageKey]
			retainedSubsumes := interprocWitnessSubsumes(retained.witness, edge.witness)
			candidateSubsumes := interprocWitnessSubsumes(edge.witness, retained.witness)
			if retainedSubsumes {
				if !candidateSubsumes || retainedID <= witnessID {
					return true
				}
			}
			if candidateSubsumes {
				subsumedStorageKeys = append(subsumedStorageKeys, storageKey)
			}
		}
		for _, storageKey := range subsumedStorageKeys {
			if !checkpoint(edge.path, edge.witness) {
				return false
			}
			retainedID := interprocWitnessIdentity(pathEdges[storageKey].witness)
			delete(antichain, retainedID)
			delete(pathEdges, storageKey)
		}
		if len(antichain) >= interprocWitnessAntichainLimit {
			setBudgetExceeded(pathOutcomeReasonWitnessBudget, edge.path, edge.witness)
			return false
		}
		if !reserveWorkUnit(edge.path, edge.witness) {
			return false
		}
		storageKey := contributionKey + "\x00witness=" + witnessID
		pathEdges[storageKey] = edge
		antichain[witnessID] = storageKey
		queue = append(queue, storageKey)
		stats.PathEdges++
		return true
	}

	applySummary := func(dependency interprocCallDependency, summary interprocProcedureSummary) bool {
		if !checkpoint(dependency.callerPath, dependency.callerWitness) {
			return false
		}
		if dependency.calleeExit.Key() != summary.exit.Key() {
			return true
		}
		path := mergeTabulationPaths(dependency.callerPath, summary.path, dependency.returnNode.BlockIndex)
		returnEdge := interprocEdge{
			From: dependency.calleeExit, To: dependency.returnNode,
			Kind: interprocEdgeReturn, CallSite: dependency.callSite,
		}
		combinedEffect := dependency.callerEffect.thenSummary(summary.edgeFunction)
		witness := mergeInterprocWitnessEdges(dependency.callerWitness, summary.witness)
		witness = appendInterprocWitnessEdge(
			witness,
			returnEdge,
			summary.exitStateBefore,
			summary.state,
			summary.exitTransferTag,
		)
		if !enqueue(interprocPathEdge{
			entry:        dependency.callerEntry,
			node:         dependency.returnNode,
			state:        summary.state,
			edgeFunction: combinedEffect,
			path:         path,
			witness:      witness,
		}) {
			return false
		}
		stats.SummaryReuses++
		return true
	}

	registerDependency := func(dependency interprocCallDependency) bool {
		if !checkpoint(dependency.callerPath, dependency.callerWitness) {
			return false
		}
		key := dependency.key()
		if _, exists := dependencies[key]; exists {
			return true
		}
		if !reserveWorkUnit(dependency.callerPath, dependency.callerWitness) {
			return false
		}
		dependencies[key] = dependency
		entryKey := dependency.calleeEntry.key()
		dependenciesByEntry[entryKey] = append(dependenciesByEntry[entryKey], key)
		slices.Sort(dependenciesByEntry[entryKey])
		stats.Dependencies++
		for _, summaryKey := range summariesByEntry[entryKey] {
			if !checkpoint(dependency.callerPath, dependency.callerWitness) {
				return false
			}
			if !applySummary(dependency, summaries[summaryKey]) {
				return false
			}
		}
		return true
	}

	publishSummary := func(summary interprocProcedureSummary) bool {
		if !checkpoint(summary.path, summary.witness) {
			return false
		}
		// The optional test seam runs before the summary key is admitted and
		// before any caller dependency aggregates the summary.
		summary = injectProtocolSummaryEvidence(control, summary)
		key := summary.key()
		if _, exists := summaries[key]; exists {
			return true
		}
		if !reserveWorkUnit(summary.path, summary.witness) {
			return false
		}
		summaries[key] = summary
		entryKey := summary.entry.key()
		summariesByEntry[entryKey] = append(summariesByEntry[entryKey], key)
		slices.Sort(summariesByEntry[entryKey])
		stats.Summaries++
		for _, dependencyKey := range dependenciesByEntry[entryKey] {
			if !checkpoint(summary.path, summary.witness) {
				return false
			}
			if !applySummary(dependencies[dependencyKey], summary) {
				return false
			}
		}
		return true
	}

	_ = enqueue(interprocPathEdge{
		entry:        rootEntry,
		node:         start,
		state:        initialState,
		edgeFunction: identityInterprocConditionalEdgeFunc(),
		path:         []int32{start.BlockIndex},
	})

	for len(queue) > 0 && budgetResult == nil {
		if !checkpoint(nil, nil) {
			break
		}
		key, remaining := takeProtocolWorklist(queue, control)
		queue = remaining
		snapshot, ok := pathEdges[key]
		if !ok {
			continue
		}

		node := graph.astNode(snapshot.node)
		nodeTag, reason := transfer(snapshot.node, node, snapshot.state)
		reason = injectProtocolReasonEvidence(control, reason)
		reasonEffects := protocolEffectsForUncertainty(reason)
		if reason != pathOutcomeReasonNone && reasonEffects == 0 {
			if witnessIsDischarged(witnessHash, snapshot.path, snapshot.witness, snapshot.node, string(reason), dischargedWitnesses) {
				continue
			}
			result := interprocPathResultFromOutcome(pathOutcomeInconclusive, reason, snapshot.path)
			result.WitnessEdges = cloneInterprocWitnessEdges(snapshot.witness)
			result.WitnessTerminal = snapshot.node
			inconclusiveResult = preferInterprocResult(inconclusiveResult, result)
			continue
		}
		nodeState := newIDEEdgeFunc(nodeTag).Apply(snapshot.state).withUncertainty(reason)
		nodeEffect := snapshot.edgeFunction.then(nodeTag)
		nodeReason := nodeState.pathOutcomeReason()
		if options.ObserveExit != nil && snapshot.node.FuncKey == start.FuncKey &&
			graph.isFunctionExitNode(snapshot.node) {
			options.ObserveExit(snapshot.node, nodeState)
		}

		returnEdges, expired := graph.returnEdgesFromWithControl(snapshot.node, control)
		if expired {
			setBudgetExceeded(pathOutcomeReasonTimeout, snapshot.path, snapshot.witness)
			break
		}
		sinkReached := obligationSinkReached(obligationSink, snapshot.node, node)
		queryIdentity := nodeState.Identity
		if sinkPolicy.UnresolvedIdentityAtSink && sinkReached {
			queryIdentity = protocolIdentityAtUnresolvedSink(nodeReason, queryIdentity)
		}
		if sinkPolicy.MustAliasUncertaintyAtSink && sinkReached &&
			nodeReason != pathOutcomeReasonNone && queryIdentity == protocolIdentityMustAlias {
			queryIdentity = protocolIdentityMayAlias
		}
		if !nodeState.validationProven() &&
			nodeState.Identity < protocolIdentityMayAlias &&
			terminalUnsafe(snapshot.node, node, nodeState) {
			if witnessIsDischarged(
				witnessHash,
				snapshot.path,
				snapshot.witness,
				snapshot.node,
				cfgRefinementTriggerUnsafeCandidate,
				dischargedWitnesses,
			) {
				continue
			}
			result := interprocPathResultFromOutcome(pathOutcomeUnsafe, pathOutcomeReasonNone, snapshot.path)
			result.WitnessEdges = cloneInterprocWitnessEdges(snapshot.witness)
			result.WitnessTerminal = snapshot.node
			unsafeResult = preferInterprocResult(unsafeResult, result)
			continue
		}
		query := protocolRelevanceQuery{
			ForwardReachable: nodeReason != pathOutcomeReasonNone,
			SinkReachable: sinkReached ||
				(sinkPolicy.TerminalCanObserve && len(returnEdges) == 0 && terminalCanObserveObligation(
					terminalUnsafe,
					snapshot.node,
					node,
				)),
			Identity:        queryIdentity,
			PossibleEffects: nodeState.PossibleEffects,
		}
		relevantUncertainty := nodeReason != pathOutcomeReasonNone && query.relevant(nodeState)
		if relevantUncertainty {
			if witnessIsDischarged(witnessHash, snapshot.path, snapshot.witness, snapshot.node, string(nodeReason), dischargedWitnesses) {
				continue
			}
			result := interprocPathResultFromOutcome(pathOutcomeInconclusive, nodeReason, snapshot.path)
			result.WitnessEdges = cloneInterprocWitnessEdges(snapshot.witness)
			result.WitnessTerminal = snapshot.node
			inconclusiveResult = preferInterprocResult(inconclusiveResult, result)
		}
		if !relevantUncertainty && terminalUnsafe(snapshot.node, node, nodeState) {
			if witnessIsDischarged(
				witnessHash,
				snapshot.path,
				snapshot.witness,
				snapshot.node,
				cfgRefinementTriggerUnsafeCandidate,
				dischargedWitnesses,
			) {
				continue
			}
			result := interprocPathResultFromOutcome(pathOutcomeUnsafe, pathOutcomeReasonNone, snapshot.path)
			result.WitnessEdges = cloneInterprocWitnessEdges(snapshot.witness)
			result.WitnessTerminal = snapshot.node
			unsafeResult = preferInterprocResult(unsafeResult, result)
			continue
		}

		if len(returnEdges) > 0 {
			if !publishSummary(interprocProcedureSummary{
				entry:           snapshot.entry,
				exit:            snapshot.node,
				state:           nodeState,
				edgeFunction:    nodeEffect,
				path:            snapshot.path,
				witness:         snapshot.witness,
				exitStateBefore: snapshot.state,
				exitTransferTag: nodeTag,
			}) {
				break
			}
			continue
		}

		outgoing, expired := graph.outgoingWithControl(snapshot.node, control)
		if expired {
			setBudgetExceeded(pathOutcomeReasonTimeout, snapshot.path, snapshot.witness)
			break
		}
		for _, edge := range outgoing {
			if !checkpoint(snapshot.path, snapshot.witness) {
				break
			}
			if edge.Kind == interprocEdgeReturn {
				continue
			}
			if options.PruneEdge != nil && options.PruneEdge(edge, nodeState) {
				continue
			}
			edgeTag := ideEdgeFuncIdentity
			edgeReason := pathOutcomeReasonNone
			if edgeTransfer != nil {
				edgeTag, edgeReason = edgeTransfer(edge, nodeState)
			}
			edgeReason = injectProtocolReasonEvidence(control, edgeReason)
			edgeState := newIDEEdgeFunc(edgeTag).Apply(nodeState).withUncertainty(edgeReason)
			edgeEffect := nodeEffect.then(edgeTag)
			nextPath := appendWitnessBlock(snapshot.path, edge.To.BlockIndex)
			nextWitness := appendInterprocWitnessEdge(
				snapshot.witness,
				edge,
				snapshot.state,
				edgeState,
				interprocTransitionTag(snapshot.state, edgeState),
			)
			if edge.Kind == interprocEdgeCall {
				calleeEntry := interprocEntryFact{node: edge.To, state: edgeState}
				matchingReturns, expired := graph.returnEdgesForCallWithControl(edge.CallSite, edge.To.FuncKey, control)
				if expired {
					setBudgetExceeded(pathOutcomeReasonTimeout, nextPath, nextWitness)
					break
				}
				for _, returnEdge := range matchingReturns {
					if !checkpoint(nextPath, nextWitness) {
						break
					}
					if !registerDependency(interprocCallDependency{
						calleeEntry:   calleeEntry,
						calleeExit:    returnEdge.From,
						callerEntry:   snapshot.entry,
						callSite:      edge.CallSite,
						returnNode:    returnEdge.To,
						callerEffect:  edgeEffect,
						callerPath:    nextPath,
						callerWitness: nextWitness,
					}) {
						break
					}
				}
				if budgetResult != nil {
					break
				}
				_ = enqueue(interprocPathEdge{
					entry:        calleeEntry,
					node:         edge.To,
					state:        edgeState,
					edgeFunction: identityInterprocConditionalEdgeFunc(),
					path:         []int32{edge.To.BlockIndex},
					witness:      nil,
				})
				continue
			}

			nextState := edgeState
			if edge.Kind == interprocEdgeCallToReturn && edge.Reason != pathOutcomeReasonNone {
				originNode := node
				if originNode == nil {
					originNode = graph.astNode(interprocNodeID{
						FuncKey:    edge.From.FuncKey,
						BlockIndex: edge.From.BlockIndex,
						NodeIndex:  edge.From.NodeIndex,
						Kind:       interprocNodeKindCFG,
					})
				}
				if unresolvedCallRelevant == nil || unresolvedCallRelevant(snapshot.node, originNode, nodeState) {
					injectedReason := injectProtocolReasonEvidence(control, edge.Reason)
					nextState = nextState.withUncertainty(injectedReason)
					if len(nextWitness) > 0 {
						nextWitness[len(nextWitness)-1].StateAfter = nextState
					}
				}
			}
			if !enqueue(interprocPathEdge{
				entry:        snapshot.entry,
				node:         edge.To,
				state:        nextState,
				edgeFunction: edgeEffect,
				path:         nextPath,
				witness:      nextWitness,
			}) {
				break
			}
		}
	}

	attachGraph := func(result interprocPathResult) interprocPathResult {
		if len(result.WitnessEdges) > 0 {
			result.witnessGraph = &graph
		}
		return result
	}
	if budgetResult == nil {
		_ = checkpoint(nil, nil)
	}
	if budgetResult != nil {
		return attachGraph(*budgetResult), stats
	}
	if unsafeResult != nil {
		return attachGraph(*unsafeResult), stats
	}
	if inconclusiveResult != nil {
		return attachGraph(*inconclusiveResult), stats
	}
	return interprocPathResultFromOutcome(pathOutcomeSafe, pathOutcomeReasonNone, nil), stats
}

func obligationSinkReached(
	obligationSink interprocObligationSinkFn,
	nodeID interprocNodeID,
	node ast.Node,
) bool {
	return obligationSink != nil && obligationSink(nodeID, node)
}

func interprocTransitionTag(before, after protocolAbstractState) ideEdgeFuncTag {
	if before == after {
		return ideEdgeFuncIdentity
	}
	switch {
	case before.validationRequired() && after.validationProven():
		return ideEdgeFuncValidate
	case before.validationRequired() && after.validationRequired() &&
		before.Result != protocolErrorResultNonNil && after.Result == protocolErrorResultNonNil:
		return ideEdgeFuncValidationFailed
	case before.validationProven() && after.validationRequired():
		return ideEdgeFuncInvalidate
	case !before.escapedBeforeValidation() && after.escapedBeforeValidation():
		return ideEdgeFuncEscape
	case !before.consumedBeforeValidation() && after.consumedBeforeValidation():
		return ideEdgeFuncConsume
	case before.DeferredError != after.DeferredError:
		switch after.DeferredError {
		case protocolDeferredErrorNil:
			return ideEdgeFuncDeferredErrorNil
		case protocolDeferredErrorValidation:
			return ideEdgeFuncDeferredErrorValidation
		case protocolDeferredErrorOther:
			return ideEdgeFuncDeferredErrorOther
		case protocolDeferredErrorUnknown:
			return ideEdgeFuncDeferredErrorUnknown
		}
	default:
		return ideEdgeFuncIdentity
	}
	return ideEdgeFuncIdentity
}

func mergeTabulationPaths(prefix, local []int32, returnBlock int32) []int32 {
	out := cloneCFGPath(prefix)
	for _, block := range local {
		if len(out) == 0 || out[len(out)-1] != block {
			out = append(out, block)
		}
	}
	return appendWitnessBlock(out, returnBlock)
}

func terminalCanObserveObligation(
	terminalUnsafe interprocTerminalUnsafeFn,
	nodeID interprocNodeID,
	node ast.Node,
) bool {
	if terminalUnsafe == nil {
		return false
	}
	for _, state := range []protocolAbstractState{
		ideStateNeedsValidate,
		ideStateValidationFailed,
		ideStateEscapedBeforeValidate,
		ideStateConsumedBeforeValidate,
	} {
		if terminalUnsafe(nodeID, node, state) {
			return true
		}
	}
	return false
}

func (graph interprocSupergraph) returnEdgesFromWithControl(
	node interprocNodeID,
	control protocolAnalysisControl,
) ([]interprocEdge, bool) {
	out := make([]interprocEdge, 0)
	outgoing, expired := graph.outgoingWithControl(node, control)
	if expired {
		return nil, true
	}
	for _, edge := range outgoing {
		if feasibilityDeadlineReached(control) {
			return nil, true
		}
		if edge.Kind == interprocEdgeReturn {
			out = append(out, edge)
		}
	}
	return out, feasibilityDeadlineReached(control)
}

func (graph interprocSupergraph) returnEdgesForCallWithControl(
	callSite, calleeFunc string,
	control protocolAnalysisControl,
) ([]interprocEdge, bool) {
	out := make([]interprocEdge, 0)
	for _, edge := range graph.Edges {
		if feasibilityDeadlineReached(control) {
			return nil, true
		}
		if edge.Kind != interprocEdgeReturn || edge.CallSite != callSite || edge.From.FuncKey != calleeFunc {
			continue
		}
		out = append(out, edge)
	}
	slices.SortFunc(out, func(left, right interprocEdge) int {
		leftKey := fmt.Sprintf("%s|%s", left.From.Key(), left.To.Key())
		rightKey := fmt.Sprintf("%s|%s", right.From.Key(), right.To.Key())
		return strings.Compare(leftKey, rightKey)
	})
	return out, feasibilityDeadlineReached(control)
}
