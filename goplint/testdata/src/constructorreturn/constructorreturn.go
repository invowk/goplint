// SPDX-License-Identifier: MPL-2.0

// Package constructorreturn provides test fixtures for the
// --check-constructor-return-error mode. This mode detects constructors
// for types with Validate() that do not include error in their return
// signature.
package constructorreturn

import "fmt"

// --- Validatable type with constructor that returns error (GOOD) ---

// Config has Validate(). NewConfig returns (*Config, error) — NOT flagged.
type Config struct {
	name string // want `struct field constructorreturn\.Config\.name uses primitive type string`
}

func (c *Config) Validate() error {
	if c.name == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func NewConfig(name string) (*Config, error) { // want `parameter "name" of constructorreturn\.NewConfig uses primitive type string`
	c := &Config{name: name}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// --- Validatable type with constructor that does NOT return error (BAD) ---

// Widget has Validate(). NewWidget returns *Widget without error — FLAGGED.
type Widget struct {
	label string // want `struct field constructorreturn\.Widget\.label uses primitive type string`
}

func (w *Widget) Validate() error {
	if w.label == "" {
		return fmt.Errorf("empty label")
	}
	return nil
}

func NewWidget(label string) *Widget { // want `parameter "label" of constructorreturn\.NewWidget uses primitive type string` `constructor constructorreturn\.NewWidget returns constructorreturn\.Widget which has Validate\(\) but constructor does not return error`
	return &Widget{label: label}
}

// --- Type without Validate() — constructor should NOT be flagged ---

type Plain struct {
	value string // want `struct field constructorreturn\.Plain\.value uses primitive type string`
}

func NewPlain(value string) *Plain { // want `parameter "value" of constructorreturn\.NewPlain uses primitive type string`
	return &Plain{value: value}
}

// --- Constructor returning interface — should NOT be flagged ---

type Engine interface {
	Run() error
}

type engineImpl struct {
	name string // want `struct field constructorreturn\.engineImpl\.name uses primitive type string`
}

func (e *engineImpl) Run() error     { return nil }
func (e *engineImpl) Validate() error { return nil }

func NewEngine(name string) Engine { // want `parameter "name" of constructorreturn\.NewEngine uses primitive type string`
	return &engineImpl{name: name}
}

// --- Constant-only type — constructor NOT flagged ---

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

// NewSeverity does not return error — but Severity is constant-only,
// so this constructor is exempt.
func NewSeverity(s string) *Severity { // want `parameter "s" of constructorreturn\.NewSeverity uses primitive type string`
	sev := Severity(s)
	return &sev
}

// --- Multiple return values with error last (GOOD) ---

// Server has Validate(). NewServer returns (*Server, error) — NOT flagged.
type Server struct {
	addr string // want `struct field constructorreturn\.Server\.addr uses primitive type string`
}

func (s *Server) Validate() error {
	if s.addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

func NewServer(addr string) (*Server, error) { // want `parameter "addr" of constructorreturn\.NewServer uses primitive type string`
	s := &Server{addr: addr}
	return s, s.Validate()
}

// --- Constructor with only error return (unusual but valid) ---

// Watcher has Validate(). NewWatcher returns error without the type value.
// This is an unusual pattern but the constructor does return error — NOT flagged.
type Watcher struct {
	path string // want `struct field constructorreturn\.Watcher\.path uses primitive type string`
}

func (w *Watcher) Validate() error {
	if w.path == "" {
		return fmt.Errorf("empty path")
	}
	return nil
}

func NewWatcher(path string) error { // want `parameter "path" of constructorreturn\.NewWatcher uses primitive type string`
	w := &Watcher{path: path}
	return w.Validate()
}
