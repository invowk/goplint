// SPDX-License-Identifier: MPL-2.0

package cfa_phasec_constructor_cycle

import "fmt"

type Server struct {
	Addr string
}

func (s *Server) Validate() error {
	if s == nil || s.Addr == "" {
		return fmt.Errorf("invalid server")
	}
	return nil
}

func helperValidateA(s *Server) {
	helperValidateB(s)
}

func helperValidateB(s *Server) {
	helperValidateA(s)
}

func NewServer(addr string) (*Server, error) {
	s := &Server{Addr: addr}
	helperValidateA(s)
	return s, nil
}
