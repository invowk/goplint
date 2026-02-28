// SPDX-License-Identifier: MPL-2.0

package enumsync

import "fmt"

// --- Complete sync: all CUE members present in Go --- should NOT be flagged

//goplint:enum-cue=#RuntimeMode
type RuntimeMode string

func (m RuntimeMode) Validate() error {
	switch m {
	case "native", "virtual", "container":
		return nil
	default:
		return fmt.Errorf("invalid runtime mode %q", m)
	}
}

func (m RuntimeMode) String() string { return string(m) }

// --- Missing Go case: "light" is in CUE but missing from switch ---

//goplint:enum-cue=#ColorScheme
type ColorScheme string // want `type enumsync\.ColorScheme: CUE member "light" \(at #ColorScheme\) is missing from Validate\(\) switch cases`

func (c ColorScheme) Validate() error {
	switch c {
	case "auto", "dark": // "light" is missing!
		return nil
	default:
		return fmt.Errorf("invalid color scheme %q", c)
	}
}

func (c ColorScheme) String() string { return string(c) }

// --- Extra Go case: "legacy" is in switch but not in CUE disjunction ---

//goplint:enum-cue=#RuntimeMode
type RuntimeModeExtra string // want `type enumsync\.RuntimeModeExtra: Validate\(\) switch case "legacy" is not present in CUE disjunction at #RuntimeMode`

func (r RuntimeModeExtra) Validate() error {
	switch r {
	case "native", "virtual", "container", "legacy": // "legacy" is extra!
		return nil
	default:
		return fmt.Errorf("invalid mode %q", r)
	}
}

func (r RuntimeModeExtra) String() string { return string(r) }

// --- Nested path: #Flag.type disjunction ---

//goplint:enum-cue=#FlagType
type FlagType string

func (f FlagType) Validate() error {
	switch f {
	case "string", "bool", "int", "float":
		return nil
	default:
		return fmt.Errorf("invalid flag type %q", f)
	}
}

func (f FlagType) String() string { return string(f) }

// --- Named constant switch cases: constants resolve to string values ---

//goplint:enum-cue=#RuntimeMode
type ConstSwitchMode string

const (
	CSMNative    ConstSwitchMode = "native"
	CSMVirtual   ConstSwitchMode = "virtual"
	CSMContainer ConstSwitchMode = "container"
)

func (m ConstSwitchMode) Validate() error {
	switch m {
	case CSMNative, CSMVirtual, CSMContainer:
		return nil
	default:
		return fmt.Errorf("invalid mode %q", m)
	}
}

func (m ConstSwitchMode) String() string { return string(m) }

// --- Mixed literals and named constants ---

//goplint:enum-cue=#ColorScheme
type MixedCaseScheme string

const MCSLight MixedCaseScheme = "light"

func (c MixedCaseScheme) Validate() error {
	switch c {
	case "auto", "dark", MCSLight: // mix of literal + named constant
		return nil
	default:
		return fmt.Errorf("invalid color scheme %q", c)
	}
}

func (c MixedCaseScheme) String() string { return string(c) }

// --- Named constant with missing member: "light" is in CUE but not as constant ---

//goplint:enum-cue=#ColorScheme
type ConstMissingCase string // want `type enumsync\.ConstMissingCase: CUE member "light" \(at #ColorScheme\) is missing from Validate\(\) switch cases`

const (
	CMCAuto ConstMissingCase = "auto"
	CMCDark ConstMissingCase = "dark"
	// CMCLight is intentionally missing
)

func (c ConstMissingCase) Validate() error {
	switch c {
	case CMCAuto, CMCDark: // "light" missing!
		return nil
	default:
		return fmt.Errorf("invalid: %q", c)
	}
}

func (c ConstMissingCase) String() string { return string(c) }
