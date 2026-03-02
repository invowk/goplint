// SPDX-License-Identifier: MPL-2.0

package myapp

import (
	"constructorvalidates_cross/util"
)

// NewServerWithDirective calls util.ValidateServer which has the
// //goplint:validates-type=Server directive. This cross-package call
// satisfies the constructor-validates check via fact propagation.
// Should NOT be flagged.
func NewServerWithDirective(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewServerWithDirective uses primitive type string`
	s := &util.Server{Addr: addr}
	return s, util.ValidateServer(s)
}

// NewServerNoDirective calls util.HelperNoDirective which lacks the
// directive. The cross-package call does NOT satisfy the check.
// SHOULD be flagged.
func NewServerNoDirective(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewServerNoDirective uses primitive type string` `constructor myapp\.NewServerNoDirective returns util\.Server which has Validate\(\) but never calls it`
	s := &util.Server{Addr: addr}
	return s, util.HelperNoDirective(s)
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
