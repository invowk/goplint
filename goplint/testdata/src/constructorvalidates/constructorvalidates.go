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

func (e *engineImpl) Run() error     { return nil }
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

// --- Deep transitive chain: NewPipeline → buildStages → initStage → stage.Validate() ---
// This is a 3-level delegation chain (depth 0→1→2→validate). Previously,
// len(visited) >= 3 would have stopped at the 3rd unique function, but
// the fix uses explicit depth tracking so chains up to maxTransitiveDepth (5) work.

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
// NOT flagged — the 4-level chain (depth 0→1→2→3) is within the maxTransitiveDepth limit.
func NewPipeline(name string) (*Pipeline, error) { // want `parameter "name" of constructorvalidates\.NewPipeline uses primitive type string`
	return assemblePipeline(name)
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
