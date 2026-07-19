// SPDX-License-Identifier: MPL-2.0

package cfa_no_return_terminator

import (
	"fmt"
	"log"
	"os"
)

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

// PanicTerminator should not be flagged: the function terminates via panic and
// has no return path that requires validated values.
func PanicTerminator(raw string) { // want `parameter "raw" of cfa_no_return_terminator\.PanicTerminator uses primitive type string`
	x := CommandName(raw)
	panic(x)
}

// ExitTerminator should not be flagged: all paths terminate via os.Exit.
func ExitTerminator(raw string) { // want `parameter "raw" of cfa_no_return_terminator\.ExitTerminator uses primitive type string`
	x := CommandName(raw)
	if x == "" {
		os.Exit(1)
	}
	os.Exit(2)
}

// FatalTerminator should not be flagged: log.Fatal is a no-return sink.
func FatalTerminator(raw string) { // want `parameter "raw" of cfa_no_return_terminator\.FatalTerminator uses primitive type string`
	x := CommandName(raw)
	log.Fatal(x)
}

// ExitAliasTerminator should not be flagged: aliasing os.Exit still terminates.
func ExitAliasTerminator(raw string) { // want `parameter "raw" of cfa_no_return_terminator\.ExitAliasTerminator uses primitive type string`
	x := CommandName(raw)
	exit := os.Exit
	if x == "" {
		exit(1)
	}
	exit(2)
}

// ConditionalExitAlias must retain the continuation because os.Exit reaches
// the call only on one branch; the original may-return value reaches it on the
// other branch.
func ConditionalExitAlias(raw string, terminate bool) { // want `parameter "raw" of cfa_no_return_terminator\.ConditionalExitAlias uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	exit := func(int) {}
	if terminate {
		exit = os.Exit
	}
	exit(1)
	_ = x
}
