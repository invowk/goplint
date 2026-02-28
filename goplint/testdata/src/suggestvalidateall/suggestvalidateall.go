// SPDX-License-Identifier: MPL-2.0

package suggestvalidateall

import "fmt"

// --- Type with Validate() and validatable fields, no directive → flagged ---

type Name string

func (n Name) Validate() error {
	if n == "" {
		return fmt.Errorf("empty name")
	}
	return nil
}

func (n Name) String() string { return string(n) }

type Config struct { // want `struct suggestvalidateall\.Config has Validate\(\) and 1 validatable field\(s\) but no //goplint:validate-all directive`
	name Name
}

func (c *Config) Validate() error {
	return c.name.Validate()
}

// --- Already has directive → NOT flagged ---

//goplint:validate-all
type Annotated struct {
	name Name
}

func (a *Annotated) Validate() error {
	return a.name.Validate()
}

// --- No Validate() method → NOT flagged ---

type Plain struct {
	name Name
}

// --- Has Validate() but no validatable fields → NOT flagged ---

type Simple struct {
	count int // want `struct field suggestvalidateall\.Simple\.count uses primitive type int`
}

func (s *Simple) Validate() error { return nil }

// --- Multiple validatable fields → flagged with correct count ---

type Mode string

func (m Mode) Validate() error {
	if m == "" {
		return fmt.Errorf("empty mode")
	}
	return nil
}

func (m Mode) String() string { return string(m) }

type Server struct { // want `struct suggestvalidateall\.Server has Validate\(\) and 2 validatable field\(s\) but no //goplint:validate-all directive`
	name Name
	mode Mode
}

func (s *Server) Validate() error {
	if err := s.name.Validate(); err != nil {
		return err
	}
	return s.mode.Validate()
}
