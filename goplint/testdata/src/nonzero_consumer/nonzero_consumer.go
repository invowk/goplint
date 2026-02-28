// SPDX-License-Identifier: MPL-2.0

// Package nonzero_consumer tests cross-package NonZeroFact propagation.
// It imports the nonzero package and uses its annotated types to verify
// that facts are correctly serialized and deserialized across packages.
package nonzero_consumer

import "nonzero"

// CrossPkgValueFields uses nonzero types from another package as value
// fields — should be flagged via cross-package fact propagation.
type CrossPkgValueFields struct {
	Name   nonzero.CommandName // want `struct field nonzero_consumer\.CrossPkgValueFields\.Name uses nonzero type CommandName as value`
	Module nonzero.ModuleID    // want `struct field nonzero_consumer\.CrossPkgValueFields\.Module uses nonzero type ModuleID as value`
}

// CrossPkgPointerFields uses *Type for optional nonzero fields — correct.
type CrossPkgPointerFields struct {
	Name   *nonzero.CommandName // NOT flagged — pointer is correct
	Module *nonzero.ModuleID    // NOT flagged — pointer is correct
}

// CrossPkgZeroOK uses a type WITHOUT nonzero annotation — not flagged.
type CrossPkgZeroOK struct {
	Value nonzero.ZeroValidType // NOT flagged — no nonzero annotation
}

// CrossPkgMixed has both correct and incorrect cross-package usage.
type CrossPkgMixed struct {
	Required nonzero.CommandName  // want `struct field nonzero_consumer\.CrossPkgMixed\.Required uses nonzero type CommandName as value`
	Optional *nonzero.CommandName // NOT flagged — pointer
	ZeroOK   nonzero.ZeroValidType // NOT flagged — no nonzero annotation
}

// CrossPkgEmbedded embeds a nonzero type from another package — should be flagged.
type CrossPkgEmbedded struct {
	nonzero.CommandName // want `struct field nonzero_consumer\.CrossPkgEmbedded\.\(embedded\) uses nonzero type CommandName as value`
}
