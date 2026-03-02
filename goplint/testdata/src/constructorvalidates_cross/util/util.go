// SPDX-License-Identifier: MPL-2.0

package util

import "fmt"

// Server is a type with Validate() for cross-package testing.
type Server struct {
	Addr string // want `struct field util\.Server\.Addr uses primitive type string`
}

func (s *Server) Validate() error {
	if s.Addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

// ValidateServer validates a Server on behalf of a constructor.
// The directive tells goplint that this function satisfies the
// Validate() call requirement for the Server type.
//
//goplint:validates-type=Server
func ValidateServer(s *Server) error { // want ValidateServer:"validates-type\\(Server\\)"
	return s.Validate()
}

// HelperNoDirective also calls Validate but lacks the directive.
// Constructors delegating to this should still be flagged.
func HelperNoDirective(s *Server) error {
	return s.Validate()
}
