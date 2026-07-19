// SPDX-License-Identifier: MPL-2.0

package constructorvalidates

import "fmt"

// --- Types with Validate() ---

type Config struct {
	name string // want `struct field constructorvalidates\.Config\.name uses primitive type string`
}

func (c *Config) Validate() error {
	if c.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewConfig calls Validate() — should NOT be flagged by constructor-validates.
func NewConfig(name string) (*Config, error) { // want `parameter "name" of constructorvalidates\.NewConfig uses primitive type string`
	c := &Config{name: name}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// --- Constructor that does NOT call Validate() ---

type Server struct {
	addr string // want `struct field constructorvalidates\.Server\.addr uses primitive type string`
}

func (s *Server) Validate() error {
	if s.addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

// NewServer does NOT call Validate() — should be flagged.
func NewServer(addr string) (*Server, error) { // want `parameter "addr" of constructorvalidates\.NewServer uses primitive type string` `constructor constructorvalidates\.NewServer returns constructorvalidates\.Server which has Validate\(\) but never calls it`
	return &Server{addr: addr}, nil
}

// NewServerNamedResultBareReturn uses named results and bare return, but does
// NOT validate the returned Server. This must be flagged.
func NewServerNamedResultBareReturn(addr string) (srv *Server, err error) { // want `parameter "addr" of constructorvalidates\.NewServerNamedResultBareReturn uses primitive type string` `constructor constructorvalidates\.NewServerNamedResultBareReturn returns constructorvalidates\.Server which has Validate\(\) but never calls it`
	srv = &Server{addr: addr}
	return
}

// --- Non-validating factory (with ignore directive) ---

type Options struct {
	debug bool
}

func (o *Options) Validate() error {
	return nil
}

//goplint:ignore -- non-validating factory for tests
func NewOptionsFromDefaults() *Options {
	return &Options{}
}

// --- Type WITHOUT Validate() — constructors should not be flagged ---

type SimpleType struct {
	value string // want `struct field constructorvalidates\.SimpleType\.value uses primitive type string`
}

func NewSimpleType(value string) *SimpleType { // want `parameter "value" of constructorvalidates\.NewSimpleType uses primitive type string`
	return &SimpleType{value: value}
}

// --- Constructor returning interface — should not be flagged ---

type Engine interface {
	Run() error
}

type engineImpl struct {
	name string // want `struct field constructorvalidates\.engineImpl\.name uses primitive type string`
}

func (e *engineImpl) Run() error { // want Run:"protocol-summary:v5:constructorvalidates:\\(\\*constructorvalidates.engineImpl\\).Run:1"
	return nil
}
func (e *engineImpl) Validate() error { return nil }

func NewEngine(name string) Engine { // want `parameter "name" of constructorvalidates\.NewEngine uses primitive type string`
	return &engineImpl{name: name}
}

// --- Variant constructor calling Validate() — should NOT be flagged ---

type Resolver struct {
	path string // want `struct field constructorvalidates\.Resolver\.path uses primitive type string`
}

func (r *Resolver) Validate() error {
	if r.path == "" {
		return fmt.Errorf("empty path")
	}
	return nil
}

func NewResolverFromPath(path string) (*Resolver, error) { // want `parameter "path" of constructorvalidates\.NewResolverFromPath uses primitive type string`
	r := &Resolver{path: path}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return r, nil
}

// --- Single-return constructor (no error return) ---
// Even without an error return, the constructor could call Validate()
// and panic or log. The mode flags missing Validate() regardless.

type Widget struct {
	label string // want `struct field constructorvalidates\.Widget\.label uses primitive type string`
}

func (w *Widget) Validate() error {
	if w.label == "" {
		return fmt.Errorf("empty label")
	}
	return nil
}

func NewWidget(label string) *Widget { // want `parameter "label" of constructorvalidates\.NewWidget uses primitive type string` `constructor constructorvalidates\.NewWidget returns constructorvalidates\.Widget which has Validate\(\) but never calls it`
	return &Widget{label: label}
}

// --- False-negative test: validates parameter, not return type ---

// Handler has Validate().
type Handler struct {
	config Config
}

func (h *Handler) Validate() error {
	return h.config.Validate()
}

// NewHandler validates Config (the parameter type) but NOT Handler (the return type).
// The old heuristic would accept this because cfg.Validate() exists in the body.
// The receiver-aware check correctly flags this.
func NewHandler(cfg Config) (*Handler, error) { // want `constructor constructorvalidates\.NewHandler returns constructorvalidates\.Handler which has Validate\(\) but never calls it`
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Handler{config: cfg}, nil
}

// --- Factory delegation: helper validates, outer does not ---
// Documents the inter-procedural gap: the check only looks at the
// direct constructor body, not called functions.

type Processor struct {
	name string // want `struct field constructorvalidates\.Processor\.name uses primitive type string`
}

func (p *Processor) Validate() error {
	if p.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func newProcessorInternal(name string) (*Processor, error) { // want `parameter "name" of constructorvalidates\.newProcessorInternal uses primitive type string`
	p := &Processor{name: name}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// NewProcessor delegates to newProcessorInternal which calls Validate().
// NOT flagged — transitive factory tracking sees through the private call.
func NewProcessor(name string) (*Processor, error) { // want `parameter "name" of constructorvalidates\.NewProcessor uses primitive type string`
	return newProcessorInternal(name)
}

// --- Factory delegation without Validate() — should still be flagged ---

type Builder struct {
	path string // want `struct field constructorvalidates\.Builder\.path uses primitive type string`
}

func (b *Builder) Validate() error {
	if b.path == "" {
		return fmt.Errorf("empty path")
	}
	return nil
}

func buildBuilder(path string) *Builder { // want `parameter "path" of constructorvalidates\.buildBuilder uses primitive type string`
	return &Builder{path: path}
}

// NewBuilder delegates to buildBuilder which does NOT call Validate() — flagged.
func NewBuilder(path string) (*Builder, error) { // want `parameter "path" of constructorvalidates\.NewBuilder uses primitive type string` `constructor constructorvalidates\.NewBuilder returns constructorvalidates\.Builder which has Validate\(\) but never calls it`
	return buildBuilder(path), nil
}

// --- Helper with partial validation coverage ---

type Partial struct {
	name string // want `struct field constructorvalidates\.Partial\.name uses primitive type string`
}

func (p *Partial) Validate() error {
	if p.name == "" {
		return fmt.Errorf("empty partial name")
	}
	return nil
}

func maybeInitPartial(name string, validate bool) (*Partial, error) { // want `parameter "name" of constructorvalidates\.maybeInitPartial uses primitive type string`
	p := &Partial{name: name}
	if !validate {
		return p, nil
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

// NewPartial delegates to helper that validates only on one branch — flagged.
func NewPartial(name string, validate bool) (*Partial, error) { // want `parameter "name" of constructorvalidates\.NewPartial uses primitive type string` `constructor constructorvalidates\.NewPartial returns constructorvalidates\.Partial which has Validate\(\) but never calls it`
	return maybeInitPartial(name, validate)
}

// --- Deep transitive chain: NewPipeline → buildStages → initStage → stage.Validate() ---
// Conditional procedure summaries preserve the validation effect across the
// complete finite helper chain.

type Pipeline struct {
	name string // want `struct field constructorvalidates\.Pipeline\.name uses primitive type string`
}

func (p *Pipeline) Validate() error {
	if p.name == "" {
		return fmt.Errorf("empty pipeline name")
	}
	return nil
}

func initStage(p *Pipeline) error {
	return p.Validate()
}

func buildStages(p *Pipeline) error {
	return initStage(p)
}

func assemblePipeline(name string) (*Pipeline, error) { // want `parameter "name" of constructorvalidates\.assemblePipeline uses primitive type string`
	p := &Pipeline{name: name}
	if err := buildStages(p); err != nil {
		return nil, err
	}
	return p, nil
}

// NewPipeline delegates through assemblePipeline → buildStages → initStage → p.Validate().
// It is not flagged because the fixed-point summaries preserve the effect.
func NewPipeline(name string) (*Pipeline, error) { // want `parameter "name" of constructorvalidates\.NewPipeline uses primitive type string`
	return assemblePipeline(name)
}

// --- Five-hop finite summary chain ---
// This chain passes through five functions before reaching Validate() and is
// accepted without a semantic call-depth cutoff.

type DepthBoundary struct {
	name string // want `struct field constructorvalidates\.DepthBoundary\.name uses primitive type string`
}

func (d *DepthBoundary) Validate() error {
	if d.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func boundaryL4(d *DepthBoundary) error { return d.Validate() }
func boundaryL3(d *DepthBoundary) error { return boundaryL4(d) }
func boundaryL2(d *DepthBoundary) error { return boundaryL3(d) }
func boundaryL1(d *DepthBoundary) error { return boundaryL2(d) }

func boundaryAssemble(name string) (*DepthBoundary, error) { // want `parameter "name" of constructorvalidates\.boundaryAssemble uses primitive type string`
	d := &DepthBoundary{name: name}
	if err := boundaryL1(d); err != nil {
		return nil, err
	}
	return d, nil
}

// NewDepthBoundary delegates through boundaryAssemble → L1 → L2 → L3 → L4 → Validate().
// It is not flagged because summary convergence is independent of stack depth.
func NewDepthBoundary(name string) (*DepthBoundary, error) { // want `parameter "name" of constructorvalidates\.NewDepthBoundary uses primitive type string`
	return boundaryAssemble(name)
}

// --- Deep finite chain beyond the historical DFS cutoff ---
// The canonical fixed-point solver follows the complete finite call chain
// without a semantic depth cutoff.

type DepthBeyond struct {
	name string // want `struct field constructorvalidates\.DepthBeyond\.name uses primitive type string`
}

func (d *DepthBeyond) Validate() error {
	if d.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func beyondL5(d *DepthBeyond) error { return d.Validate() }
func beyondL4(d *DepthBeyond) error { return beyondL5(d) }
func beyondL3(d *DepthBeyond) error { return beyondL4(d) }
func beyondL2(d *DepthBeyond) error { return beyondL3(d) }
func beyondL1(d *DepthBeyond) error { return beyondL2(d) }

func beyondAssemble(name string) (*DepthBeyond, error) { // want `parameter "name" of constructorvalidates\.beyondAssemble uses primitive type string`
	d := &DepthBeyond{name: name}
	if err := beyondL1(d); err != nil {
		return nil, err
	}
	return d, nil
}

// NewDepthBeyond delegates through beyondAssemble → L1 → L2 → L3 → L4 → L5 → Validate().
// NOT flagged — the canonical summary reaches Validate() and converges.
func NewDepthBeyond(name string) (*DepthBeyond, error) { // want `parameter "name" of constructorvalidates\.NewDepthBeyond uses primitive type string`
	return beyondAssemble(name)
}

// --- Constant-only type (//goplint:constant-only) — constructor NOT flagged ---

//goplint:constant-only
type Severity string

func (s Severity) Validate() error {
	switch s {
	case "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("invalid severity: %s", string(s))
	}
}

func (s Severity) String() string { return string(s) }

// NewSeverity does NOT call Validate() — but Severity is constant-only,
// so this constructor is exempt from --check-constructor-validates.
func NewSeverity(s string) (*Severity, error) { // want `parameter "s" of constructorvalidates\.NewSeverity uses primitive type string`
	sev := Severity(s)
	return &sev, nil
}

// --- Method-call transitive tracking: r.init() → r.Validate() ---

type Registry struct {
	prefix string // want `struct field constructorvalidates\.Registry\.prefix uses primitive type string`
}

func (r *Registry) Validate() error {
	if r.prefix == "" {
		return fmt.Errorf("empty prefix")
	}
	return nil
}

func (r *Registry) init() error {
	return r.Validate()
}

// NewRegistry calls r.init() which transitively calls r.Validate().
// NOT flagged — method-call transitive tracking recognizes this.
func NewRegistry(prefix string) (*Registry, error) { // want `parameter "prefix" of constructorvalidates\.NewRegistry uses primitive type string`
	r := &Registry{prefix: prefix}
	if err := r.init(); err != nil {
		return nil, err
	}
	return r, nil
}

// --- Method-call that does NOT call Validate() ---

type Store struct {
	name string // want `struct field constructorvalidates\.Store\.name uses primitive type string`
}

func (s *Store) Validate() error {
	if s.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func (s *Store) prepare() {
	// Does not call Validate()
}

// NewStore calls s.prepare() but prepare() does NOT call Validate() — flagged.
func NewStore(name string) (*Store, error) { // want `parameter "name" of constructorvalidates\.NewStore uses primitive type string` `constructor constructorvalidates\.NewStore returns constructorvalidates\.Store which has Validate\(\) but never calls it`
	s := &Store{name: name}
	s.prepare()
	return s, nil
}

// --- Closure-variable validation path should count ---

type Session struct {
	addr string // want `struct field constructorvalidates\.Session\.addr uses primitive type string`
}

func (s *Session) Validate() error {
	if s.addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

// NewSession uses a closure variable call to invoke Validate(). This should
// satisfy canonical constructor validation.
func NewSession(addr string) (*Session, error) { // want `parameter "addr" of constructorvalidates\.NewSession uses primitive type string`
	s := &Session{addr: addr}
	validateFn := func() error {
		return s.Validate()
	}
	if err := validateFn(); err != nil {
		return nil, err
	}
	return s, nil
}

// --- Method-call on wrong type — should NOT satisfy the check ---

type GatewayConfig struct {
	host string // want `struct field constructorvalidates\.GatewayConfig\.host uses primitive type string`
}

func (gc *GatewayConfig) Validate() error {
	if gc.host == "" {
		return fmt.Errorf("empty host")
	}
	return nil
}

type Gateway struct {
	config GatewayConfig
}

func (g *Gateway) Validate() error {
	return g.config.Validate()
}

// NewGateway calls gc.Validate() on GatewayConfig, not on Gateway — flagged.
func NewGateway(gc GatewayConfig) (*Gateway, error) { // want `constructor constructorvalidates\.NewGateway returns constructorvalidates\.Gateway which has Validate\(\) but never calls it`
	if err := gc.Validate(); err != nil {
		return nil, err
	}
	return &Gateway{config: gc}, nil
}

// --- Multi-path constructor: path analysis detects partial validation ---

type MultiPath struct {
	name string // want `struct field constructorvalidates\.MultiPath\.name uses primitive type string`
}

func (m *MultiPath) Validate() error {
	if m.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewMultiPath validates on only one path — analysis flags this because the
// "fast" path returns without calling Validate().
func NewMultiPath(name string, fast bool) (*MultiPath, error) { // want `parameter "name" of constructorvalidates\.NewMultiPath uses primitive type string` `constructor constructorvalidates\.NewMultiPath returns constructorvalidates\.MultiPath which has Validate\(\) but never calls it`
	m := &MultiPath{name: name}
	if fast {
		return m, nil // unvalidated return
	}
	return m, m.Validate()
}

// NewMultiPathAllPaths validates on all paths and is not flagged.
func NewMultiPathAllPaths(name string, mode bool) (*MultiPath, error) { // want `parameter "name" of constructorvalidates\.NewMultiPathAllPaths uses primitive type string`
	m := &MultiPath{name: name}
	if mode {
		if err := m.Validate(); err != nil {
			return nil, err
		}
	} else {
		if err := m.Validate(); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// --- Early error return should not require Validate() on nil return paths ---

type EarlyReturn struct {
	name string // want `struct field constructorvalidates\.EarlyReturn\.name uses primitive type string`
}

func (e *EarlyReturn) Validate() error {
	if e.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewEarlyReturn validates on the success path. The early nil/error return
// must not force a constructor-validates finding.
func NewEarlyReturn(name string, fail bool) (*EarlyReturn, error) { // want `parameter "name" of constructorvalidates\.NewEarlyReturn uses primitive type string`
	if fail {
		return nil, fmt.Errorf("forced failure")
	}
	e := &EarlyReturn{name: name}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return e, nil
}

// NewElseBranchSuccess returns an unvalidated value from the nil-error edge.
// The enclosing non-nil check must not make its else branch unsuccessful.
func NewElseBranchSuccess(name string, err error) (*EarlyReturn, error) { // want `parameter "name" of constructorvalidates\.NewElseBranchSuccess uses primitive type string` `constructor constructorvalidates\.NewElseBranchSuccess returns constructorvalidates\.EarlyReturn which has Validate\(\) but never calls it`
	e := &EarlyReturn{name: name}
	if err != nil {
		return nil, err
	} else {
		return e, err
	}
}

// --- Path-sensitive transitive validation ---

type PathSensitive struct {
	name string // want `struct field constructorvalidates\.PathSensitive\.name uses primitive type string`
}

func (p *PathSensitive) Validate() error {
	if p.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func maybeValidatePathSensitive(p *PathSensitive) {
	if false {
		_ = p.Validate()
	}
}

// NewPathSensitive only calls a helper that validates on a dead branch.
// SHOULD be flagged by constructor-validates.
func NewPathSensitive(name string) (*PathSensitive, error) { // want `parameter "name" of constructorvalidates\.NewPathSensitive uses primitive type string` `constructor constructorvalidates\.NewPathSensitive returns constructorvalidates\.PathSensitive which has Validate\(\) but never calls it`
	p := &PathSensitive{name: name}
	maybeValidatePathSensitive(p)
	return p, nil
}

// --- Deferred closure validation should count on constructor paths ---

type DeferredValidated struct {
	name string // want `struct field constructorvalidates\.DeferredValidated\.name uses primitive type string`
}

func (d *DeferredValidated) Validate() error {
	if d.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewDeferredValidated validates via a deferred closure that sets the named
// error return before function exit.
func NewDeferredValidated(name string) (d *DeferredValidated, err error) { // want `parameter "name" of constructorvalidates\.NewDeferredValidated uses primitive type string`
	d = &DeferredValidated{name: name}
	defer func() {
		validateErr := d.Validate()
		if err == nil {
			err = validateErr
		}
	}()
	return d, nil
}

// --- Mixed direct + helper validation across paths ---

type MixedPathValidated struct {
	name string // want `struct field constructorvalidates\.MixedPathValidated\.name uses primitive type string`
}

func (m *MixedPathValidated) Validate() error {
	if m.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func validateMixedPath(m *MixedPathValidated) error {
	return m.Validate()
}

// NewMixedPathValidated validates directly on one branch and via helper on the
// other branch. All return paths validate.
func NewMixedPathValidated(name string, fast bool) (*MixedPathValidated, error) { // want `parameter "name" of constructorvalidates\.NewMixedPathValidated uses primitive type string`
	m := &MixedPathValidated{name: name}
	if fast {
		if err := m.Validate(); err != nil {
			return nil, err
		}
		return m, nil
	}
	if err := validateMixedPath(m); err != nil {
		return nil, err
	}
	return m, nil
}

// --- Decoy same-type Validate call must not satisfy constructor-validates ---

type DecoyValidated struct {
	name string // want `struct field constructorvalidates\.DecoyValidated\.name uses primitive type string`
}

func (d *DecoyValidated) Validate() error {
	if d.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

// NewDecoyValidated validates a different instance than the one returned.
// This must still be flagged.
func NewDecoyValidated(name string) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewDecoyValidated uses primitive type string` `constructor constructorvalidates\.NewDecoyValidated returns constructorvalidates\.DecoyValidated which has Validate\(\) but never calls it`
	decoy := &DecoyValidated{name: name}
	if err := decoy.Validate(); err != nil {
		return nil, err
	}
	real := &DecoyValidated{name: name}
	return real, nil
}

// NewHistoricallyRebound validates the value held by result before rebinding
// the same variable to a distinct allocation. Historical assignment closure
// must not transfer validation to the allocation that reaches the return.
func NewHistoricallyRebound(name string) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewHistoricallyRebound uses primitive type string` `constructor constructorvalidates\.NewHistoricallyRebound returns constructorvalidates\.DecoyValidated which has Validate\(\) but never calls it`
	first := &DecoyValidated{name: name}
	result := first
	if err := result.Validate(); err != nil {
		return nil, err
	}
	second := &DecoyValidated{name: name}
	result = second
	return result, nil
}

// NewFreshLiteralAfterValidation validates one allocation and then returns a
// distinct same-typed literal. Equal Go types do not establish identity.
func NewFreshLiteralAfterValidation(name string) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewFreshLiteralAfterValidation uses primitive type string` `constructor constructorvalidates\.NewFreshLiteralAfterValidation returns constructorvalidates\.DecoyValidated which has Validate\(\) but never calls it`
	validated := &DecoyValidated{name: name}
	if err := validated.Validate(); err != nil {
		return nil, err
	}
	return &DecoyValidated{name: name}, nil
}

// NewCrossPathMismatched validates one candidate unconditionally, but a
// different same-typed allocation reaches one return. Each return identity
// carries an independent obligation through the join.
func NewCrossPathMismatched(name string, first bool) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewCrossPathMismatched uses primitive type string` `constructor constructorvalidates\.NewCrossPathMismatched returns constructorvalidates\.DecoyValidated which has Validate\(\) but never calls it`
	a := &DecoyValidated{name: name}
	b := &DecoyValidated{name: name}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if first {
		return a, nil
	}
	return b, nil
}

// NewCrossPathMatched validates exactly the allocation returned by each
// branch. The independent identity obligations are both discharged.
func NewCrossPathMatched(name string, first bool) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewCrossPathMatched uses primitive type string`
	a := &DecoyValidated{name: name}
	b := &DecoyValidated{name: name}
	if first {
		if err := a.Validate(); err != nil {
			return nil, err
		}
		return a, nil
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return b, nil
}

// NewConditionallyRebound may return either allocation after validating only
// the conditionally assigned one. The reaching identity at the join is
// ambiguous, so analysis must block with an inconclusive result.
func NewConditionallyRebound(name string, replace bool) (*DecoyValidated, error) { // want `parameter "name" of constructorvalidates\.NewConditionallyRebound uses primitive type string` `constructor constructorvalidates\.NewConditionallyRebound returns constructorvalidates\.DecoyValidated with inconclusive Validate\(\) path analysis`
	result := &DecoyValidated{name: name}
	if replace {
		result = &DecoyValidated{name: name}
	}
	if err := result.Validate(); err != nil {
		return nil, err
	}
	return result, nil
}

// --- Conditional helper results and inverse validation guards ---

type HelperResultValidated struct {
	name string // want `struct field constructorvalidates\.HelperResultValidated\.name uses primitive type string`
}

func (h *HelperResultValidated) Validate() error {
	if h.name == "" {
		return fmt.Errorf("name required")
	}
	return nil
}

func validateHelperResult(h *HelperResultValidated) error {
	return h.Validate()
}

// NewDiscardedHelperResult ignores the conditional helper result, so it does
// not establish that the returned value is valid.
func NewDiscardedHelperResult(name string) (*HelperResultValidated, error) { // want `parameter "name" of constructorvalidates\.NewDiscardedHelperResult uses primitive type string` `constructor constructorvalidates\.NewDiscardedHelperResult returns constructorvalidates\.HelperResultValidated which has Validate\(\) but never calls it`
	h := &HelperResultValidated{name: name}
	_ = validateHelperResult(h)
	return h, nil
}

// NewCheckedHelperResult checks the helper result before returning the value.
func NewCheckedHelperResult(name string) (*HelperResultValidated, error) { // want `parameter "name" of constructorvalidates\.NewCheckedHelperResult uses primitive type string`
	h := &HelperResultValidated{name: name}
	if err := validateHelperResult(h); err != nil {
		return nil, err
	}
	return h, nil
}

// NewInverseValidationGuard returns the candidate only together with the exact
// non-nil validation error, so that return is unsuccessful and has no pending
// constructor validation obligation.
func NewInverseValidationGuard(name string) (*HelperResultValidated, error) { // want `parameter "name" of constructorvalidates\.NewInverseValidationGuard uses primitive type string`
	h := &HelperResultValidated{name: name}
	if err := h.Validate(); err == nil {
		return nil, nil
	} else {
		return h, err
	}
}
