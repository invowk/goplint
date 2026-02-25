// SPDX-License-Identifier: MPL-2.0

package funcoptions

// --- Detection: too many non-option parameters ---

// TooManyParams has a constructor with 4 params — should suggest functional options.
type TooManyParams struct {
	a int // want `struct field funcoptions\.TooManyParams\.a uses primitive type int`
	b int // want `struct field funcoptions\.TooManyParams\.b uses primitive type int`
	c int // want `struct field funcoptions\.TooManyParams\.c uses primitive type int`
	d int // want `struct field funcoptions\.TooManyParams\.d uses primitive type int`
}

func NewTooManyParams(a, b, c, d int) *TooManyParams { return &TooManyParams{a: a, b: b, c: c, d: d} } // want `constructor NewTooManyParams\(\) for funcoptions\.TooManyParams has 4 non-option parameters; consider using functional options` `parameter "a" of funcoptions\.NewTooManyParams uses primitive type int` `parameter "b" of funcoptions\.NewTooManyParams uses primitive type int` `parameter "c" of funcoptions\.NewTooManyParams uses primitive type int` `parameter "d" of funcoptions\.NewTooManyParams uses primitive type int`

// ThreeParams has exactly 3 params — at the threshold, no flag.
type ThreeParams struct {
	x int // want `struct field funcoptions\.ThreeParams\.x uses primitive type int`
	y int // want `struct field funcoptions\.ThreeParams\.y uses primitive type int`
	z int // want `struct field funcoptions\.ThreeParams\.z uses primitive type int`
}

func NewThreeParams(x, y, z int) *ThreeParams { return &ThreeParams{x: x, y: y, z: z} } // want `parameter "x" of funcoptions\.NewThreeParams uses primitive type int` `parameter "y" of funcoptions\.NewThreeParams uses primitive type int` `parameter "z" of funcoptions\.NewThreeParams uses primitive type int`

// --- Completeness: already has options, all wired correctly ---

// HasOptions is fully wired — no diagnostics for func-options.
type HasOptions struct {
	shell string // want `struct field funcoptions\.HasOptions\.shell uses primitive type string`
	args  string // want `struct field funcoptions\.HasOptions\.args uses primitive type string`
}

// HasOptionsOption is the functional option type for HasOptions.
type HasOptionsOption func(*HasOptions)

// WithShell sets the shell.
func WithShell(s string) HasOptionsOption { return func(h *HasOptions) { h.shell = s } } // want `parameter "s" of funcoptions\.WithShell uses primitive type string`

// WithArgs sets the args.
func WithArgs(a string) HasOptionsOption { return func(h *HasOptions) { h.args = a } } // want `parameter "a" of funcoptions\.WithArgs uses primitive type string`

// NewHasOptions creates a HasOptions with options.
func NewHasOptions(opts ...HasOptionsOption) *HasOptions {
	h := &HasOptions{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// --- Completeness: missing a WithXxx function ---

// Incomplete has an option type but lacks WithTimeout (for the timeout field).
type Incomplete struct {
	host    string // want `struct field funcoptions\.Incomplete\.host uses primitive type string`
	timeout int    // want `struct field funcoptions\.Incomplete\.timeout uses primitive type int` `struct funcoptions\.Incomplete has IncompleteOption type but field "timeout" has no WithTimeout\(\) function`
}

// IncompleteOption is the functional option type for Incomplete.
type IncompleteOption func(*Incomplete)

// WithHost satisfies the host field.
func WithHost(h string) IncompleteOption { return func(i *Incomplete) { i.host = h } } // want `parameter "h" of funcoptions\.WithHost uses primitive type string`

// NewIncomplete creates an Incomplete.
func NewIncomplete(opts ...IncompleteOption) *Incomplete {
	i := &Incomplete{}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// --- Completeness: constructor not variadic ---

// NoVariadic has an option type but constructor doesn't accept it.
type NoVariadic struct {
	name string // want `struct field funcoptions\.NoVariadic\.name uses primitive type string` `struct funcoptions\.NoVariadic has NoVariadicOption type but field "name" has no WithName\(\) function`
}

// NoVariadicOption is the functional option type for NoVariadic.
type NoVariadicOption func(*NoVariadic)

func NewNoVariadic(name string) *NoVariadic { return &NoVariadic{name: name} } // want `constructor NewNoVariadic\(\) for funcoptions\.NoVariadic does not accept variadic \.\.\.NoVariadicOption` `parameter "name" of funcoptions\.NewNoVariadic uses primitive type string`

// --- Completeness: internal state fields excluded via //plint:internal ---

// HasInternalState has an option type with one internal-state field excluded
// from the WithXxx() completeness check.
type HasInternalState struct {
	addr string // want `struct field funcoptions\.HasInternalState\.addr uses primitive type string`
	//plint:internal -- computed cache, not user-configurable
	cache string // want `struct field funcoptions\.HasInternalState\.cache uses primitive type string`
}

// HasInternalStateOption is the functional option type for HasInternalState.
type HasInternalStateOption func(*HasInternalState)

// WithAddr sets the addr on HasInternalState.
func WithAddr(a string) HasInternalStateOption { return func(s *HasInternalState) { s.addr = a } } // want `parameter "a" of funcoptions\.WithAddr uses primitive type string`

// NewHasInternalState creates a HasInternalState with options.
func NewHasInternalState(opts ...HasInternalStateOption) *HasInternalState {
	s := &HasInternalState{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// No WithCache expected — cache has //plint:internal.
// WithAddr satisfies the addr field. No missing-func-options diagnostics.

// --- Combined directive: ignore,internal suppresses both primitive and func-options ---

// HasCombinedDirective has a field with both ignore and internal via combined directive.
// The combined form suppresses the primitive finding AND the WithXxx completeness check.
type HasCombinedDirective struct {
	label string // want `struct field funcoptions\.HasCombinedDirective\.label uses primitive type string`
	//plint:ignore,internal -- both: suppress primitive finding and exclude from func-options
	state string
}

// HasCombinedDirectiveOption is the functional option type for HasCombinedDirective.
type HasCombinedDirectiveOption func(*HasCombinedDirective)

// WithLabel sets the label on HasCombinedDirective.
func WithLabel(l string) HasCombinedDirectiveOption { // want `parameter "l" of funcoptions\.WithLabel uses primitive type string`
	return func(s *HasCombinedDirective) { s.label = l }
}

// NewHasCombinedDirective creates a HasCombinedDirective with options.
func NewHasCombinedDirective(opts ...HasCombinedDirectiveOption) *HasCombinedDirective {
	s := &HasCombinedDirective{}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// No WithState expected — state has //plint:ignore,internal (combined directive).
// WithLabel satisfies the label field. No missing-func-options diagnostics.

// --- Variant constructor only: exactCtor == nil, anyCtor != nil ---

// VariantOnly has only a variant constructor (NewVariantOnlyFromConfig),
// testing the fallback path where no exact NewVariantOnly exists.
type VariantOnly struct {
	host string // want `struct field funcoptions\.VariantOnly\.host uses primitive type string` `struct funcoptions\.VariantOnly has VariantOnlyOption type but field "host" has no WithHost\(\) function`
}

// VariantOnlyOption is the functional option type for VariantOnly.
type VariantOnlyOption func(*VariantOnly)

func NewVariantOnlyFromConfig(cfg string) *VariantOnly { return &VariantOnly{host: cfg} } // want `constructor NewVariantOnly\(\) for funcoptions\.VariantOnly does not accept variadic \.\.\.VariantOnlyOption` `parameter "cfg" of funcoptions\.NewVariantOnlyFromConfig uses primitive type string`

// --- Completeness: variadic option targets a different struct ---

// WrongTarget has an option type, but NewWrongTarget accepts options for OtherTarget.
type WrongTarget struct {
	wrongTargetName string // want `struct field funcoptions\.WrongTarget\.wrongTargetName uses primitive type string`
}

// WrongTargetOption is the functional option type for WrongTarget.
type WrongTargetOption func(*WrongTarget)

// OtherTarget is unrelated to WrongTarget.
type OtherTarget struct{}

// OtherTargetOption is an option type for OtherTarget.
type OtherTargetOption func(*OtherTarget)

// WithWrongTargetName satisfies the WrongTarget field completeness check.
func WithWrongTargetName(name string) WrongTargetOption { // want `parameter "name" of funcoptions\.WithWrongTargetName uses primitive type string`
	return func(w *WrongTarget) { w.wrongTargetName = name }
}

// NewWrongTarget incorrectly accepts OtherTargetOption instead of WrongTargetOption.
func NewWrongTarget(opts ...OtherTargetOption) *WrongTarget { // want `constructor NewWrongTarget\(\) for funcoptions\.WrongTarget does not accept variadic \.\.\.WrongTargetOption`
	w := &WrongTarget{}
	for _, opt := range opts {
		_ = opt
	}
	return w
}
