// SPDX-License-Identifier: MPL-2.0

package excludedprotocol

type Value string

func (value Value) Validate() error { return nil }

func Probe(raw string) {
	value := Value(raw)
	_ = value
}

var Stored = func(raw string) {
	value := Value(raw)
	_ = value
}
