// SPDX-License-Identifier: MPL-2.0

package model

import "fmt"

type Server struct {
	Addr string // want `struct field model\.Server\.Addr uses primitive type string`
}

func (s *Server) Validate() error {
	if s.Addr == "" {
		return fmt.Errorf("empty addr")
	}
	return nil
}
