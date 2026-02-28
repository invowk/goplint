// SPDX-License-Identifier: MPL-2.0

// Package auditreviewdates is a fixture for testing the --audit-review-dates mode.
// The diagnostics are driven by the TOML config, not by this source code.
// The "want" annotations are on the package line because reportOverdueExceptions
// anchors diagnostics to pass.Files[0].Package.
package auditreviewdates // want `exception pattern "overdue.pattern" is past its review date 2020-01-01` `exception pattern "overdue.with.blocked" is past its review date 2020-06-15 \(blocked by: upstream type proposal\)` `exception pattern "invalid.date.pattern" has invalid review_after date "not-a-date"`

// Example is a minimal struct to make this a non-empty package.
type Example struct {
	name string // want `struct field auditreviewdates\.Example\.name uses primitive type string`
}
