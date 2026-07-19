// SPDX-License-Identifier: MPL-2.0

package myapp

import (
	"constructorvalidates_cross/util"
)

// NewServerWithDirective calls util.ValidateServer. Its extracted conditional
// protocol summary satisfies the constructor-validates check.
// Should NOT be flagged.
func NewServerWithDirective(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewServerWithDirective uses primitive type string`
	s := &util.Server{Addr: addr}
	return s, util.ValidateServer(s)
}

// NewServerNoDirective calls util.HelperNoDirective. The package-qualified
// protocol summary captures the real validation effect without a directive.
func NewServerNoDirective(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewServerNoDirective uses primitive type string`
	s := &util.Server{Addr: addr}
	return s, util.HelperNoDirective(s)
}

// NewRecursiveElseServer proves successful-return classification preserves the
// cross-package object obligation through recursion and the nil-error else edge.
func NewRecursiveElseServer(addr string, err error, depth int) (*util.Server, error) { // want `parameter "addr" of myapp\.NewRecursiveElseServer uses primitive type string` `parameter "depth" of myapp\.NewRecursiveElseServer uses primitive type int` `constructor myapp\.NewRecursiveElseServer returns util\.Server which has Validate\(\) but never calls it`
	if depth > 0 {
		return NewRecursiveElseServer(addr, err, depth-1)
	}

	server := &util.Server{Addr: addr}
	if err != nil {
		return nil, err
	} else {
		return server, err
	}
}

// --- Cross-package constructor-return-error tests ---

// NewServerNoErrorCross returns a cross-package type (util.Server)
// which has Validate(), but the constructor does not return error.
// SHOULD be flagged by --check-constructor-return-error AND
// --check-constructor-validates (never calls Validate).
func NewServerNoErrorCross(addr string) *util.Server { // want `parameter "addr" of myapp\.NewServerNoErrorCross uses primitive type string` `constructor myapp\.NewServerNoErrorCross returns util\.Server which has Validate\(\) but never calls it` `constructor myapp\.NewServerNoErrorCross returns util\.Server which has Validate\(\) but constructor does not return error`
	return &util.Server{Addr: addr}
}

// NewServerWithErrorCross returns a cross-package type (util.Server)
// and already returns error. Should NOT be flagged.
func NewServerWithErrorCross(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewServerWithErrorCross uses primitive type string` `constructor myapp\.NewServerWithErrorCross returns util\.Server which has Validate\(\) but never calls it`
	return &util.Server{Addr: addr}, nil
}
