// SPDX-License-Identifier: MPL-2.0

package configexceptions

// ExceptedByConfig has fields excepted via TOML config.
type ExceptedByConfig struct {
	Name    string // no diagnostic — excepted via config "configexceptions.ExceptedByConfig.Name"
	Label   string // no diagnostic — excepted via wildcard "*.Label"
	Flagged string // want `struct field configexceptions\.ExceptedByConfig\.Flagged uses primitive type string`
}

// SkippedType has a field whose type is in skip_types.
type SkippedType struct {
	Timeout int64 // no diagnostic — int64 is in skip_types for this test config
}

// ExceptedFunc has a param excepted via TOML config.
func ExceptedFunc(name string) { // no diagnostic — excepted via config
	_ = name
}

// NotExceptedFunc is NOT in the config.
func NotExceptedFunc(value string) { // want `parameter "value" of configexceptions\.NotExceptedFunc uses primitive type string`
	_ = value
}

// --- New pattern exercises ---

// InvalidFooError has a Reason field excepted via "*.Reason" wildcard.
type InvalidFooError struct {
	Reason string // no diagnostic — excepted via "*.Reason" wildcard
}

// View return excepted via "*.View.return.0".
type fakeModel struct{}

func (f fakeModel) View() string { return "" } // no diagnostic — excepted via return wildcard
