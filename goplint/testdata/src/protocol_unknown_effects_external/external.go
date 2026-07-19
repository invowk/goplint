// SPDX-License-Identifier: MPL-2.0

package protocol_unknown_effects_external

// Apply represents an external operation whose mutation behavior is unknown.
func Apply[T any](value *T) {
	_ = value
}

// Replace represents an external operation that may replace a tracked value.
func Replace[T any](target *T, replacement T) {
	_, _ = target, replacement
}

// Retain represents an external operation that may retain a tracked pointer.
func Retain[T any](value *T) {
	_ = value
}
