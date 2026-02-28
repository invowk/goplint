// SPDX-License-Identifier: MPL-2.0

// Package auditexceptions_pkga contains a Server.Name field that matches the
// shared exception config. When auditing exceptions in THIS package,
// "Server.Name" is NOT stale — but "NonExistent.Field" is.
package auditexceptions_pkga // want `stale exception: pattern "NonExistent.Field" matched no diagnostics`

// Server has a Name field excepted via shared config.
type Server struct {
	Name string // no diagnostic — excepted via "Server.Name"
	Port int    // want `struct field auditexceptions_pkga\.Server\.Port uses primitive type int`
}
