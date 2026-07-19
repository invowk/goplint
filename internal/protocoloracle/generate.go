// SPDX-License-Identifier: MPL-2.0

package protocoloracle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const (
	manifestFormatVersion = 3
	manifestGenerator     = "normalized-protocol-v3"
	manifestEnumeration   = "one-factor-plus-integrated-scheduled-v1"
)

// BoundsManifest is the complete versioned grammar for the bounded corpus.
type BoundsManifest struct {
	FormatVersion        int                `json:"format_version"`
	Generator            string             `json:"generator"`
	Enumeration          string             `json:"enumeration"`
	Dimensions           ManifestDimensions `json:"dimensions"`
	Scenarios            []string           `json:"scenarios"`
	Blocking             CorpusProfile      `json:"blocking"`
	Scheduled            CorpusProfile      `json:"scheduled"`
	RequiredFeatures     []string           `json:"required_features"`
	ExpectedProgramCount int                `json:"expected_program_count"`
}

// ManifestDimensions declares every independently enumerable program dimension.
type ManifestDimensions struct {
	ProcedureCounts    []int    `json:"procedure_counts"`
	NodesPerProcedure  []int    `json:"nodes_per_procedure"`
	IdentityCounts     []int    `json:"identity_counts"`
	CallSiteCounts     []int    `json:"call_site_counts"`
	CallDepths         []int    `json:"call_depths"`
	Topologies         []string `json:"topologies"`
	BranchJoins        []bool   `json:"branch_joins"`
	Recursion          []bool   `json:"recursion"`
	Operations         []string `json:"operations"`
	ConditionalResults []string `json:"conditional_results"`
	AliasActions       []string `json:"alias_actions"`
	UnknownEffects     []string `json:"unknown_effects"`
	Constraints        []string `json:"constraints"`
	InitialFacts       []string `json:"initial_facts"`
}

// CorpusProfile controls deterministic partitioning and resource bounds.
type CorpusProfile struct {
	Shards               int  `json:"shards"`
	MinimumPrograms      int  `json:"minimum_programs"`
	MaxStates            int  `json:"max_states"`
	PairwiseSemantics    bool `json:"pairwise_semantics"`
	ExpectedProgramCount int  `json:"expected_program_count"`
}

type generatedConfiguration struct {
	caseID            string
	shape             Shape
	operation         Operation
	condition         ConditionalResult
	aliasAction       AliasAction
	unknownEffect     UnknownEffect
	constraint        ConstraintKind
	initialFact       InitialFact
	validateFirst     bool
	operationInCallee bool
	protectedIdentity Identity
}

// LoadBoundsManifest loads and validates the exact bounded grammar.
func LoadBoundsManifest(path string) (BoundsManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BoundsManifest{}, fmt.Errorf("read protocol oracle bounds %s: %w", path, err)
	}
	var manifest BoundsManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return BoundsManifest{}, fmt.Errorf("decode protocol oracle bounds %s: %w", path, err)
	}
	if err := validateManifest(manifest); err != nil {
		return BoundsManifest{}, err
	}
	cardinality, err := Cardinality(manifest)
	if err != nil {
		return BoundsManifest{}, err
	}
	if manifest.ExpectedProgramCount != cardinality {
		return BoundsManifest{}, fmt.Errorf(
			"expected_program_count=%d, admitted grammar contains %d programs",
			manifest.ExpectedProgramCount,
			cardinality,
		)
	}
	if cardinality < manifest.Blocking.MinimumPrograms || cardinality != manifest.Blocking.ExpectedProgramCount {
		return BoundsManifest{}, fmt.Errorf("blocking corpus contains %d programs outside its reviewed bounds", cardinality)
	}
	scheduledCardinality, err := ProfileCardinality(manifest, "scheduled")
	if err != nil {
		return BoundsManifest{}, err
	}
	if scheduledCardinality < manifest.Scheduled.MinimumPrograms ||
		scheduledCardinality != manifest.Scheduled.ExpectedProgramCount {
		return BoundsManifest{}, fmt.Errorf("scheduled corpus contains %d programs outside its reviewed bounds", scheduledCardinality)
	}
	return manifest, nil
}

// Cardinality derives the exact number of unique admitted programs.
func Cardinality(manifest BoundsManifest) (int, error) {
	programs, err := generateAll(manifest)
	if err != nil {
		return 0, err
	}
	return len(programs), nil
}

// ProfileCardinality derives the exact number of programs admitted by a profile.
func ProfileCardinality(manifest BoundsManifest, profileName string) (int, error) {
	programs, err := generateProfilePrograms(manifest, profileName)
	if err != nil {
		return 0, err
	}
	return len(programs), nil
}

// Enumerate yields the complete corpus in stable fingerprint order.
func Enumerate(manifest BoundsManifest, yield func(Program) error) error {
	programs, err := generateAll(manifest)
	if err != nil {
		return err
	}
	for _, program := range programs {
		if err := yield(program); err != nil {
			return err
		}
	}
	return nil
}

// EnumerateProfile yields the complete deterministic corpus admitted by one
// reviewed profile. The scheduled profile includes its integrated semantic
// cross-product in addition to every blocking program.
func EnumerateProfile(manifest BoundsManifest, profileName string, yield func(Program) error) error {
	programs, err := generateProfilePrograms(manifest, profileName)
	if err != nil {
		return err
	}
	for _, program := range programs {
		if err := yield(program); err != nil {
			return err
		}
	}
	return nil
}

// EnumerateShard yields one deterministic fingerprint partition for a profile.
func EnumerateShard(
	manifest BoundsManifest,
	profileName string,
	shardIndex int,
	yield func(Program) error,
) error {
	profile, err := manifest.Profile(profileName)
	if err != nil {
		return err
	}
	if shardIndex < 0 || shardIndex >= profile.Shards {
		return fmt.Errorf("shard index %d is outside [0,%d)", shardIndex, profile.Shards)
	}
	programs, err := generateProfilePrograms(manifest, profileName)
	if err != nil {
		return err
	}
	for _, program := range programs {
		if programShard(program, profile.Shards) != shardIndex {
			continue
		}
		if err := yield(program); err != nil {
			return err
		}
	}
	return nil
}

// Profile returns the reviewed blocking or scheduled corpus profile.
func (manifest BoundsManifest) Profile(name string) (CorpusProfile, error) {
	switch name {
	case "blocking":
		return manifest.Blocking, nil
	case "scheduled":
		return manifest.Scheduled, nil
	default:
		return CorpusProfile{}, fmt.Errorf("unknown protocol oracle profile %q", name)
	}
}

// FeatureCensus derives feature counts from generated nodes and edges.
func FeatureCensus(programs []Program) map[string]int {
	census := make(map[string]int)
	for _, program := range programs {
		features := programFeatures(program)
		for feature := range features {
			census[feature]++
		}
	}
	return census
}

// Features returns the exact sorted semantic features derived from a program.
func Features(program Program) []string {
	features := programFeatures(program)
	result := make([]string, 0, len(features))
	for feature := range features {
		result = append(result, feature)
	}
	sort.Strings(result)
	return result
}

// ProfileShardCardinality derives the exact number of programs assigned to one
// deterministic profile shard.
func ProfileShardCardinality(manifest BoundsManifest, profileName string, shardIndex int) (int, error) {
	count := 0
	err := EnumerateShard(manifest, profileName, shardIndex, func(Program) error {
		count++
		return nil
	})
	return count, err
}

// CorpusFingerprint returns a stable digest over the ordered admitted corpus.
func CorpusFingerprint(programs []Program) string {
	fingerprints := make([]string, 0, len(programs))
	for _, program := range programs {
		fingerprints = append(fingerprints, program.Fingerprint())
	}
	sort.Strings(fingerprints)
	digest := sha256.Sum256([]byte(strings.Join(fingerprints, "\n")))
	return fmt.Sprintf("pcorpus3_%x", digest[:])
}

func validateManifest(manifest BoundsManifest) error {
	if manifest.FormatVersion != manifestFormatVersion || manifest.Generator != manifestGenerator ||
		manifest.Enumeration != manifestEnumeration {
		return errors.New("unsupported protocol oracle manifest format, generator, or enumeration")
	}
	if err := validateDimensions(manifest.Dimensions); err != nil {
		return err
	}
	if len(manifest.Scenarios) == 0 || len(manifest.RequiredFeatures) == 0 || manifest.ExpectedProgramCount <= 0 {
		return errors.New("manifest requires scenarios, required features, and expected cardinality")
	}
	if err := validateProfile("blocking", manifest.Blocking); err != nil {
		return err
	}
	if err := validateProfile("scheduled", manifest.Scheduled); err != nil {
		return err
	}
	if manifest.Scheduled.Shards < manifest.Blocking.Shards || manifest.Scheduled.MaxStates < manifest.Blocking.MaxStates {
		return errors.New("scheduled profile must be at least as strong as the blocking profile")
	}
	return nil
}

func validateDimensions(dimensions ManifestDimensions) error {
	integerDimensions := [][]int{
		dimensions.ProcedureCounts,
		dimensions.NodesPerProcedure,
		dimensions.IdentityCounts,
	}
	for _, values := range integerDimensions {
		if len(values) == 0 {
			return errors.New("every integer dimension must be enumerable")
		}
		for _, value := range values {
			if value <= 0 {
				return errors.New("integer dimensions must be positive")
			}
		}
	}
	for _, value := range dimensions.NodesPerProcedure {
		if value < 4 {
			return errors.New("nodes-per-procedure bounds must admit entry, operation, return, and terminal nodes")
		}
	}
	for _, values := range [][]int{dimensions.CallSiteCounts, dimensions.CallDepths} {
		if len(values) == 0 {
			return errors.New("call-site and call-depth dimensions must be enumerable")
		}
		for _, value := range values {
			if value < 0 {
				return errors.New("call-site and call-depth dimensions must be non-negative")
			}
		}
	}
	stringDimensions := [][]string{
		dimensions.Topologies,
		dimensions.Operations,
		dimensions.ConditionalResults,
		dimensions.AliasActions,
		dimensions.UnknownEffects,
		dimensions.Constraints,
		dimensions.InitialFacts,
	}
	for _, values := range stringDimensions {
		if len(values) == 0 {
			return errors.New("every string dimension must be enumerable")
		}
	}
	if len(dimensions.BranchJoins) == 0 || len(dimensions.Recursion) == 0 {
		return errors.New("branch and recursion dimensions must be enumerable")
	}
	return nil
}

func validateProfile(name string, profile CorpusProfile) error {
	if profile.Shards <= 0 || profile.MinimumPrograms <= 0 || profile.MaxStates <= 0 || profile.ExpectedProgramCount <= 0 {
		return fmt.Errorf("%s corpus profile contains a non-positive bound", name)
	}
	return nil
}

func generateAll(manifest BoundsManifest) ([]Program, error) {
	configurations, err := enumerateConfigurations(manifest)
	if err != nil {
		return nil, err
	}
	programs := make([]Program, 0, len(configurations))
	seen := make(map[string]bool, len(configurations))
	for _, configuration := range configurations {
		program, buildErr := generatedProgram(configuration)
		if buildErr != nil {
			return nil, buildErr
		}
		fingerprint := program.Fingerprint()
		if seen[fingerprint] {
			return nil, fmt.Errorf("duplicate generated program %s", fingerprint)
		}
		seen[fingerprint] = true
		programs = append(programs, program)
	}
	sort.Slice(programs, func(i, j int) bool {
		return programs[i].Fingerprint() < programs[j].Fingerprint()
	})
	return programs, nil
}

func generateProfilePrograms(manifest BoundsManifest, profileName string) ([]Program, error) {
	profile, err := manifest.Profile(profileName)
	if err != nil {
		return nil, err
	}
	programs, err := generateAll(manifest)
	if err != nil || !profile.PairwiseSemantics {
		return programs, err
	}
	base, err := baseConfiguration(manifest.Dimensions)
	if err != nil {
		return nil, err
	}
	base.shape.Topology = TopologyCallReturn
	base.shape.Procedures = max(base.shape.Procedures, 2)
	base.shape.Identities = max(base.shape.Identities, 2)
	base.shape.CallSites = max(base.shape.CallSites, 1)
	base.shape.CallDepth = max(base.shape.CallDepth, 1)
	base = normalizeConfiguration(base)
	seen := make(map[string]bool, profile.ExpectedProgramCount)
	for _, program := range programs {
		seen[program.Fingerprint()] = true
	}
	for _, operation := range manifest.Dimensions.Operations {
		for _, condition := range manifest.Dimensions.ConditionalResults {
			for _, aliasAction := range manifest.Dimensions.AliasActions {
				for _, unknownEffect := range manifest.Dimensions.UnknownEffects {
					for _, constraint := range manifest.Dimensions.Constraints {
						for _, initialFact := range manifest.Dimensions.InitialFacts {
							configuration := base
							configuration.caseID = fmt.Sprintf(
								"scheduled/integrated/%s/%s/%s/%s/%s/%s",
								operation,
								condition,
								aliasAction,
								unknownEffect,
								constraint,
								initialFact,
							)
							configuration.operation = Operation(operation)
							configuration.condition = ConditionalResult(condition)
							configuration.aliasAction = AliasAction(aliasAction)
							configuration.unknownEffect = UnknownEffect(unknownEffect)
							configuration.constraint = ConstraintKind(constraint)
							configuration.initialFact = InitialFact(initialFact)
							if configuration.aliasAction != AliasActionNone {
								configuration.shape.Identities = max(configuration.shape.Identities, 2)
							}
							program, buildErr := generatedProgram(configuration)
							if buildErr != nil {
								return nil, buildErr
							}
							fingerprint := program.Fingerprint()
							if seen[fingerprint] {
								continue
							}
							seen[fingerprint] = true
							programs = append(programs, program)
						}
					}
				}
			}
		}
	}
	sort.Slice(programs, func(i, j int) bool {
		return programs[i].Fingerprint() < programs[j].Fingerprint()
	})
	return programs, nil
}

func enumerateConfigurations(manifest BoundsManifest) ([]generatedConfiguration, error) {
	dimensions := manifest.Dimensions
	base, err := baseConfiguration(dimensions)
	if err != nil {
		return nil, err
	}
	configurations := []generatedConfiguration{base}
	appendConfig := func(dimension, value string, configuration generatedConfiguration) {
		configuration.caseID = "dimension/" + dimension + "/" + value
		configurations = append(configurations, normalizeConfiguration(configuration))
	}
	for _, value := range dimensions.ProcedureCounts[1:] {
		configuration := base
		configuration.shape.Procedures = value
		appendConfig("procedures", strconv.Itoa(value), configuration)
	}
	for _, value := range dimensions.NodesPerProcedure[1:] {
		configuration := base
		configuration.shape.NodesPerProcedure = value
		appendConfig("nodes", strconv.Itoa(value), configuration)
	}
	for _, value := range dimensions.IdentityCounts[1:] {
		configuration := base
		configuration.shape.Identities = value
		appendConfig("identities", strconv.Itoa(value), configuration)
	}
	for _, value := range dimensions.CallSiteCounts[1:] {
		configuration := base
		configuration.shape.CallSites = value
		appendConfig("call-sites", strconv.Itoa(value), configuration)
	}
	for _, value := range dimensions.CallDepths[1:] {
		configuration := base
		configuration.shape.CallDepth = value
		appendConfig("call-depth", strconv.Itoa(value), configuration)
	}
	for _, value := range dimensions.Topologies[1:] {
		configuration := base
		configuration.shape.Topology = Topology(value)
		appendConfig("topology", value, configuration)
	}
	for _, value := range dimensions.BranchJoins[1:] {
		configuration := base
		configuration.shape.BranchJoin = value
		appendConfig("branch-join", strconv.FormatBool(value), configuration)
	}
	for _, value := range dimensions.Recursion[1:] {
		configuration := base
		configuration.shape.Recursive = value
		appendConfig("recursion", strconv.FormatBool(value), configuration)
	}
	for _, value := range dimensions.Operations[1:] {
		configuration := base
		configuration.operation = Operation(value)
		appendConfig("operation", value, configuration)
	}
	for _, value := range dimensions.ConditionalResults[1:] {
		configuration := base
		configuration.condition = ConditionalResult(value)
		appendConfig("condition", value, configuration)
	}
	for _, value := range dimensions.AliasActions[1:] {
		configuration := base
		configuration.aliasAction = AliasAction(value)
		appendConfig("alias", value, configuration)
	}
	for _, value := range dimensions.UnknownEffects[1:] {
		configuration := base
		configuration.unknownEffect = UnknownEffect(value)
		appendConfig("unknown", value, configuration)
	}
	for _, value := range dimensions.Constraints[1:] {
		configuration := base
		configuration.constraint = ConstraintKind(value)
		appendConfig("constraint", value, configuration)
	}
	for _, value := range dimensions.InitialFacts[1:] {
		configuration := base
		configuration.initialFact = InitialFact(value)
		appendConfig("initial-fact", value, configuration)
	}
	for _, scenario := range manifest.Scenarios {
		configuration, scenarioErr := scenarioConfiguration(base, scenario)
		if scenarioErr != nil {
			return nil, scenarioErr
		}
		configurations = append(configurations, normalizeConfiguration(configuration))
	}
	return configurations, nil
}

func baseConfiguration(dimensions ManifestDimensions) (generatedConfiguration, error) {
	if err := validateDimensions(dimensions); err != nil {
		return generatedConfiguration{}, err
	}
	return normalizeConfiguration(generatedConfiguration{
		caseID: "baseline",
		shape: Shape{
			Procedures:        dimensions.ProcedureCounts[0],
			NodesPerProcedure: dimensions.NodesPerProcedure[0],
			Identities:        dimensions.IdentityCounts[0],
			CallSites:         dimensions.CallSiteCounts[0],
			CallDepth:         dimensions.CallDepths[0],
			Topology:          Topology(dimensions.Topologies[0]),
			BranchJoin:        dimensions.BranchJoins[0],
			Recursive:         dimensions.Recursion[0],
		},
		operation:     Operation(dimensions.Operations[0]),
		condition:     ConditionalResult(dimensions.ConditionalResults[0]),
		aliasAction:   AliasAction(dimensions.AliasActions[0]),
		unknownEffect: UnknownEffect(dimensions.UnknownEffects[0]),
		constraint:    ConstraintKind(dimensions.Constraints[0]),
		initialFact:   InitialFact(dimensions.InitialFacts[0]),
	}), nil
}

func normalizeConfiguration(configuration generatedConfiguration) generatedConfiguration {
	if configuration.shape.Procedures > 1 && configuration.shape.CallSites == 0 {
		configuration.shape.CallSites = configuration.shape.Procedures - 1
		configuration.shape.CallDepth = max(configuration.shape.CallDepth, configuration.shape.Procedures-1)
	}
	if configuration.shape.CallSites > 0 {
		configuration.shape.Procedures = max(configuration.shape.Procedures, 2)
		configuration.shape.CallDepth = max(configuration.shape.CallDepth, 1)
	}
	if configuration.shape.CallDepth > 0 {
		configuration.shape.Procedures = max(configuration.shape.Procedures, configuration.shape.CallDepth+1)
		configuration.shape.CallSites = max(configuration.shape.CallSites, configuration.shape.CallDepth)
	}
	switch configuration.shape.Topology {
	case TopologyLinear:
		// The base linear topology needs no well-formedness adjustment.
	case TopologyBranchJoin:
		configuration.shape.BranchJoin = true
	case TopologyCallReturn:
		configuration.shape.Procedures = max(configuration.shape.Procedures, 2)
		configuration.shape.CallSites = max(configuration.shape.CallSites, 1)
		configuration.shape.CallDepth = max(configuration.shape.CallDepth, 1)
	case TopologyRecursive:
		configuration.shape.Procedures = max(configuration.shape.Procedures, 2)
		configuration.shape.CallSites = max(configuration.shape.CallSites, 2)
		configuration.shape.CallDepth = max(configuration.shape.CallDepth, 2)
		configuration.shape.Recursive = true
	}
	if configuration.shape.BranchJoin && configuration.shape.Topology == TopologyLinear {
		configuration.shape.Topology = TopologyBranchJoin
	}
	if configuration.shape.Recursive && configuration.shape.Topology != TopologyRecursive {
		configuration.shape.Topology = TopologyRecursive
		configuration.shape.Procedures = max(configuration.shape.Procedures, 2)
		configuration.shape.CallSites = max(configuration.shape.CallSites, 2)
		configuration.shape.CallDepth = max(configuration.shape.CallDepth, 2)
	}
	if configuration.operation == OperationValidate && configuration.condition == ConditionalResultNone {
		configuration.condition = ConditionalResultNil
	}
	if configuration.condition != ConditionalResultNone {
		configuration.operation = OperationValidate
	}
	if configuration.aliasAction != AliasActionNone {
		configuration.shape.Identities = max(configuration.shape.Identities, 2)
	}
	return configuration
}

func scenarioConfiguration(base generatedConfiguration, name string) (generatedConfiguration, error) {
	configuration := base
	configuration.caseID = "scenario/" + name
	switch name {
	case "validated-nil":
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultNil
	case "validation-non-nil":
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultNonNil
	case "validation-unknown":
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultUnknown
	case "alias-copy":
		configuration.aliasAction = AliasActionCopy
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultNil
		configuration.shape.Identities = max(configuration.shape.Identities, 2)
	case "alias-kill":
		configuration.aliasAction = AliasActionKill
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultNil
		configuration.shape.Identities = max(configuration.shape.Identities, 2)
	case "post-validation-mutation":
		configuration.validateFirst = true
		configuration.operation = OperationMutate
	case "post-validation-unknown":
		configuration.validateFirst = true
		configuration.operation = OperationUnresolved
		configuration.unknownEffect = UnknownEffectUnresolved
	case "matched-return":
		configuration.shape.Topology = TopologyCallReturn
		configuration.operation = OperationValidate
		configuration.condition = ConditionalResultNil
		configuration.operationInCallee = true
	case "branch-join":
		configuration.shape.Topology = TopologyBranchJoin
	case "recursive-summary":
		configuration.shape.Topology = TopologyRecursive
	case "post-validation-escape":
		configuration.validateFirst = true
		configuration.operation = OperationEscape
	case "refinement-unsat":
		configuration.constraint = ConstraintUNSAT
	default:
		return generatedConfiguration{}, fmt.Errorf("unknown generated scenario %q", name)
	}
	return configuration, nil
}

func generatedProgram(configuration generatedConfiguration) (Program, error) {
	configuration = normalizeConfiguration(configuration)
	shape := configuration.shape
	mainNodes := shape.NodesPerProcedure
	nodes := make([]Node, 0, mainNodes*shape.Procedures)
	for procedure := range shape.Procedures {
		for nodeIndex := range mainNodes {
			nodes = append(nodes, Node{
				Ref:           NodeRef{Procedure: uint8(procedure), Node: uint8(nodeIndex)},
				Operation:     OperationNoop,
				Condition:     ConditionalResultNone,
				AliasAction:   AliasActionNone,
				UnknownEffect: UnknownEffectNone,
				Constraint:    ConstraintNone,
			})
		}
	}
	identities := make([]Identity, shape.Identities)
	initialFacts := make([]InitialFact, shape.Identities)
	for index := range identities {
		identities[index] = Identity(index)
		initialFacts[index] = configuration.initialFact
	}
	operationIndex := 1
	if configuration.operationInCallee {
		operationIndex = mainNodes + 1
	}
	nodes[operationIndex].Operation = configuration.operation
	nodes[operationIndex].Identity = configuration.protectedIdentity
	nodes[operationIndex].Condition = configuration.condition
	nodes[operationIndex].UnknownEffect = configuration.unknownEffect
	nodes[operationIndex].Constraint = configuration.constraint
	nodes[operationIndex].AliasAction = configuration.aliasAction
	if configuration.aliasAction != AliasActionNone {
		nodes[operationIndex].Identity = 1
		nodes[operationIndex].AliasSource = 0
	}
	if configuration.validateFirst {
		nodes[0].Operation = OperationValidate
		nodes[0].Condition = ConditionalResultNil
	}
	edges := generatedEdges(shape, mainNodes)
	terminalIndex := mainNodes - 1
	nodes[terminalIndex].Terminal = true
	program := Program{
		CaseID:       configuration.caseID,
		Shape:        shape,
		Entry:        NodeRef{Procedure: 0, Node: 0},
		Nodes:        nodes,
		Edges:        edges,
		Identities:   identities,
		InitialFacts: initialFacts,
	}
	return program, program.Validate()
}

func generatedEdges(shape Shape, nodesPerProcedure int) []Edge {
	edges := make([]Edge, 0, shape.Procedures*nodesPerProcedure+4)
	nonRecursiveSites := shape.CallSites
	if shape.Recursive {
		nonRecursiveSites--
	}
	callSources := make(map[int]bool, nonRecursiveSites)
	for site := 1; site <= nonRecursiveSites; site++ {
		callSources[min(site-1, shape.Procedures-2)] = true
	}
	for procedure := range shape.Procedures {
		for nodeIndex := range nodesPerProcedure - 1 {
			if nodeIndex == 1 && callSources[procedure] {
				continue
			}
			edges = append(edges, Edge{
				From: NodeRef{Procedure: uint8(procedure), Node: uint8(nodeIndex)},
				To:   NodeRef{Procedure: uint8(procedure), Node: uint8(nodeIndex + 1)},
				Kind: EdgeIntra,
			})
		}
	}
	if shape.BranchJoin {
		edges = append(edges, Edge{
			From: NodeRef{Procedure: 0, Node: 0},
			To:   NodeRef{Procedure: 0, Node: uint8(nodesPerProcedure - 1)},
			Kind: EdgeIntra,
		})
	}
	for site := 1; site <= nonRecursiveSites; site++ {
		caller := min(site-1, shape.Procedures-2)
		callee := min(caller+1, shape.Procedures-1)
		edges = append(edges,
			Edge{
				From: NodeRef{Procedure: uint8(caller), Node: 1},
				To:   NodeRef{Procedure: uint8(callee), Node: 0}, Kind: EdgeCall, CallSite: uint8(site),
			},
			Edge{
				From: NodeRef{Procedure: uint8(callee), Node: uint8(nodesPerProcedure - 1)},
				To:   NodeRef{Procedure: uint8(caller), Node: 2}, Kind: EdgeReturn, CallSite: uint8(site),
			},
		)
	}
	if shape.Recursive {
		procedure := uint8(min(max(nonRecursiveSites, 1), shape.Procedures-1))
		callSite := uint8(shape.CallSites)
		edges = append(edges,
			Edge{From: NodeRef{Procedure: procedure, Node: 1}, To: NodeRef{Procedure: procedure, Node: 0}, Kind: EdgeCall, CallSite: callSite},
			Edge{
				From: NodeRef{Procedure: procedure, Node: uint8(nodesPerProcedure - 1)},
				To:   NodeRef{Procedure: procedure, Node: 2}, Kind: EdgeReturn, CallSite: callSite,
			},
		)
	}
	return edges
}

func programFeatures(program Program) map[string]bool {
	metrics := program.Metrics()
	features := map[string]bool{
		"procedures-" + strconv.Itoa(metrics.Procedures): true,
		"identities-" + strconv.Itoa(metrics.Identities): true,
		"call-sites-" + strconv.Itoa(metrics.CallSites):  true,
		"call-depth-" + strconv.Itoa(metrics.CallDepth):  true,
	}
	for _, fact := range program.InitialFacts {
		features["initial-fact-"+string(fact)] = true
	}
	switch {
	case metrics.Recursive:
		features["topology-recursive"] = true
	case metrics.CallSites > 0:
		features["topology-call-return"] = true
	case metrics.BranchJoin:
		features["topology-branch-join"] = true
	default:
		features["topology-linear"] = true
	}
	if metrics.BranchJoin {
		features["branch-join"] = true
	}
	if metrics.Recursive {
		features["recursion"] = true
	}
	for _, node := range program.Nodes {
		if node.Operation != OperationNoop {
			features["operation-"+string(node.Operation)] = true
		}
		switch node.Operation {
		case OperationNoop, OperationValidate, OperationConsume, OperationEscape, OperationUnresolved:
			// These operations are represented by their operation-specific census key.
		case OperationMutate:
			features["mutation"] = true
		case OperationReplace:
			features["replacement"] = true
		}
		if node.Condition != ConditionalResultNone {
			features["condition-"+string(node.Condition)] = true
		}
		if node.AliasAction != AliasActionNone {
			features["alias-"+string(node.AliasAction)] = true
		}
		if node.UnknownEffect != UnknownEffectNone {
			features["unknown-"+string(node.UnknownEffect)] = true
		}
		if node.Constraint != ConstraintNone {
			features["constraint-"+string(node.Constraint)] = true
		}
	}
	for _, edge := range program.Edges {
		if edge.Kind != EdgeCall {
			continue
		}
		for _, candidate := range program.Edges {
			if candidate.Kind == EdgeReturn && candidate.CallSite == edge.CallSite &&
				candidate.From.Procedure == edge.To.Procedure {
				features["matched-return"] = true
			}
		}
	}
	return features
}

func programShard(program Program, shards int) int {
	digest := sha256.Sum256([]byte(program.Fingerprint()))
	return int(binary.BigEndian.Uint64(digest[:8]) % uint64(shards))
}
