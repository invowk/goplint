// SPDX-License-Identifier: MPL-2.0

package constructorusage

import "errors"

// --- DDD Value Type with constructor returning (T, error) ---

// Foo is a DDD type constructed via NewFoo.
type Foo struct {
	name string // want `struct field constructorusage\.Foo\.name uses primitive type string`
}

// ErrInvalidFoo is the sentinel error.
var ErrInvalidFoo = errors.New("invalid foo")

// NewFoo constructs a Foo, returning an error if invalid.
func NewFoo(name string) (*Foo, error) { // want `parameter "name" of constructorusage\.NewFoo uses primitive type string`
	if name == "" {
		return nil, ErrInvalidFoo
	}
	return &Foo{name: name}, nil
}

// --- Constructor returning single value (no error) ---

// Bar is constructed without error return.
type Bar struct {
	value string // want `struct field constructorusage\.Bar\.value uses primitive type string`
}

// NewBar returns only a *Bar, no error.
func NewBar(value string) *Bar { // want `parameter "value" of constructorusage\.NewBar uses primitive type string`
	return &Bar{value: value}
}

// --- Constructor returning (T, int) — not (T, error) ---

// Baz has a constructor returning (T, int), not (T, error).
type Baz struct {
	code int // want `struct field constructorusage\.Baz\.code uses primitive type int`
}

// NewBaz returns (*Baz, int) — NOT an error return.
func NewBaz(code int) (*Baz, int) { // want `parameter "code" of constructorusage\.NewBaz uses primitive type int` `return value of constructorusage\.NewBaz uses primitive type int`
	return &Baz{code: code}, code
}

// --- Non-New function returning (T, error) ---

// ParseFoo is not a constructor (doesn't start with New).
func ParseFoo(s string) (*Foo, error) { // want `parameter "s" of constructorusage\.ParseFoo uses primitive type string`
	return NewFoo(s)
}

// --- FLAGGED: error assigned to blank identifier ---

func errorBlankedShortDecl() {
	result, _ := NewFoo("test") // want `constructor NewFoo error return assigned to blank identifier`
	_ = result
}

func errorBlankedRegularAssign() {
	var result *Foo
	var err error
	_ = err
	result, _ = NewFoo("test") // want `constructor NewFoo error return assigned to blank identifier`
	_ = result
}

// --- FLAGGED: cross-package selector expression (simulated with local receiver func) ---

// pkg is a helper type to simulate cross-package calls within analysistest.
type pkg struct{}

// NewQux is a constructor on pkg to simulate pkg.NewQux().
func (pkg) NewQux(name string) (*Foo, error) { // want `parameter "name" of constructorusage\.pkg\.NewQux uses primitive type string`
	return NewFoo(name)
}

func crossPackageBlank() {
	var p pkg
	result, _ := p.NewQux("test") // want `constructor NewQux error return assigned to blank identifier`
	_ = result
}

// --- NOT FLAGGED: error captured ---

func errorCaptured() {
	result, err := NewFoo("test")
	if err != nil {
		return
	}
	_ = result
}

// --- NOT FLAGGED: value blanked but error captured ---

func valueBlankedErrorCaptured() {
	_, err := NewFoo("test")
	if err != nil {
		return
	}
}

// --- NOT FLAGGED: non-New function with blanked error ---

func nonNewBlankedError() {
	result, _ := ParseFoo("test")
	_ = result
}

// --- NOT FLAGGED: constructor with single return (no error) ---

func singleReturnConstructor() {
	result := NewBar("test")
	_ = result
}

// --- NOT FLAGGED: constructor returning (T, int) not (T, error) ---

func nonErrorSecondReturn() {
	result, _ := NewBaz(42)
	_ = result
}

// --- FLAGGED: closure body (now analyzed) ---

func closureAnalyzed() {
	fn := func() {
		result, _ := NewFoo("test") // want `constructor NewFoo error return assigned to blank identifier`
		_ = result
	}
	fn()
}

func closureGoroutine() {
	go func() {
		result, _ := NewFoo("test") // want `constructor NewFoo error return assigned to blank identifier`
		_ = result
	}()
}

func closureCorrectUsage() {
	go func() {
		result, err := NewFoo("test")
		if err != nil {
			return
		}
		_ = result
	}()
}

// --- Selector expression: free function via package import ---
// Note: In analysistest, cross-package imports require separate packages.
// The pkg receiver pattern above covers the selector expression path.

// --- Multi-value RHS with single LHS (not applicable to constructors) ---
// Constructors always return (T, error) consumed via `x, _ :=` pattern.
