// SPDX-License-Identifier: MPL-2.0

package constructor_backend_ssa

import "os"

type Server struct{}

func (s *Server) Validate() error {
	return nil
}

func NewServer() *Server {
	srv := &Server{}
	os.Exit(1)
	return srv
}
