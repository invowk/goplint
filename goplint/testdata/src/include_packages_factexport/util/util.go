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

//goplint:validates-type=Server
func ValidateServer(s *Server) error { // want ValidateServer:"validates-type\\(Server\\)"
	return s.Validate()
}

func HelperNoDirective(s *Server) error {
	return s.Validate()
}
