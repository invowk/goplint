// SPDX-License-Identifier: MPL-2.0

package util

import "fmt"

type Server struct {
	Addr string
}

func (s *Server) Validate() error {
	if s.Addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}

func ValidateServer(s *Server) error { // want ValidateServer:"protocol-summary:v5:include_packages_factexport/util:include_packages_factexport/util.ValidateServer:1"
	return s.Validate()
}

func HelperNoDirective(s *Server) error { // want HelperNoDirective:"protocol-summary:v5:include_packages_factexport/util:include_packages_factexport/util.HelperNoDirective:1"
	return s.Validate()
}
