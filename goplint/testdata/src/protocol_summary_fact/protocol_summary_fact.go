// SPDX-License-Identifier: MPL-2.0

package protocol_summary_fact

type Value string

func (v Value) Validate() error { return nil }

func ValidateValue(value Value) error { // want ValidateValue:"protocol-summary:v5:protocol_summary_fact:protocol_summary_fact.ValidateValue:1"
	return value.Validate()
}
