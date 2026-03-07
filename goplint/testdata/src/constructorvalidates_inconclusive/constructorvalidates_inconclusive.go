// SPDX-License-Identifier: MPL-2.0

package constructorvalidates_inconclusive

type Server struct{}

func (s *Server) Validate() error {
	return nil
}

func NewServer() *Server { // want `constructor constructorvalidates_inconclusive\.NewServer returns constructorvalidates_inconclusive\.Server with inconclusive Validate\(\) path analysis`
	srv := &Server{}
	if true {
		_ = srv
	}
	return srv
}
