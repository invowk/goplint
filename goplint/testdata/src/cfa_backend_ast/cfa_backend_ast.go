// SPDX-License-Identifier: MPL-2.0

package cfa_backend_ast

import (
	"fmt"
	"os"
)

type CommandName string

func (c CommandName) Validate() error {
	if c == "" {
		return fmt.Errorf("invalid command")
	}
	return nil
}

// ASTBackendTreatsExitAsMayReturn is intentionally conservative under
// cfg-backend=ast: os.Exit is treated as may-return and the cast is reported.
func ASTBackendTreatsExitAsMayReturn(raw string) { // want `parameter "raw" of cfa_backend_ast\.ASTBackendTreatsExitAsMayReturn uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	if x == "" {
		os.Exit(1)
	}
	os.Exit(2)
}

// ASTBackendTreatsExitAliasAsMayReturn is also conservative under ast backend:
// function-value aliases are not no-return-special-cased.
func ASTBackendTreatsExitAliasAsMayReturn(raw string) { // want `parameter "raw" of cfa_backend_ast\.ASTBackendTreatsExitAliasAsMayReturn uses primitive type string`
	x := CommandName(raw) // want `type conversion to CommandName from non-constant without Validate\(\) check`
	exit := os.Exit
	if x == "" {
		exit(2)
	}
	exit(3)
}
