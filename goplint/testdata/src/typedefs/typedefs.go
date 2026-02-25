// SPDX-License-Identifier: MPL-2.0

package typedefs

// CommandName is a type definition wrapping string.
// This is a DDD Value Type — the "string" here is the definition itself,
// NOT a usage of bare string. Must NOT be flagged.
type CommandName string

// ExitCode wraps int.
type ExitCode int

// RuntimeMode is an enum-style type.
type RuntimeMode string

// MultiType block — none should be flagged.
type (
	FlagName  string
	FlagType  string
	PortValue uint16
)

// Alias type — type aliases ARE transparent, so a field using an alias
// of string would still be flagged (but the alias definition itself is not).
type StringAlias = string
