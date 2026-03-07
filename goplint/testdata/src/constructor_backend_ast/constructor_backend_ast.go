// SPDX-License-Identifier: MPL-2.0

package constructor_backend_ast

import "os"

type Server struct{}

func (s *Server) Validate() error {
	return nil
}

func NewServer() *Server { // want `constructor constructor_backend_ast\.NewServer returns constructor_backend_ast\.Server which has Validate\(\) but never calls it`
	srv := &Server{}
	os.Exit(1)
	return srv
}
