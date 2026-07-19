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
func ValidateServer(s *Server) error { // want ValidateServer:"protocol-summary:v5:constructorvalidates_cross/util:constructorvalidates_cross/util.ValidateServer:1"
	return s.Validate()
}

// HelperNoDirective proves summary extraction does not require annotations.
func HelperNoDirective(s *Server) error { // want HelperNoDirective:"protocol-summary:v5:constructorvalidates_cross/util:constructorvalidates_cross/util.HelperNoDirective:1"
	return s.Validate()
}
