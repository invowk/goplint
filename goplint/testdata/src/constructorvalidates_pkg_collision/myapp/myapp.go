// SPDX-License-Identifier: MPL-2.0

package myapp

import (
	"constructorvalidates_pkg_collision/util"
	"fmt"
)

// Local Server has the same type name as util.Server, but is a different type.
//
//goplint:constant-only
type Server struct {
	addr string // want `struct field myapp\.Server\.addr uses primitive type string`
}

func (s *Server) Validate() error {
	if s.addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

func validateLocal(s *Server) error {
	return s.Validate()
}

// NewRemoteServer validates local myapp.Server, but returns util.Server.
// Should still be flagged because util.Server is never validated.
func NewRemoteServer(addr string) (*util.Server, error) { // want `parameter "addr" of myapp\.NewRemoteServer uses primitive type string` `constructor myapp\.NewRemoteServer returns util\.Server which has Validate\(\) but never calls it`
	local := &Server{addr: addr}
	if err := validateLocal(local); err != nil {
		return nil, err
	}
	return &util.Server{Addr: addr}, nil
}
