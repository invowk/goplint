// SPDX-License-Identifier: MPL-2.0

package auditexceptions // want `stale exception: pattern "NonExistent.Field" matched no diagnostics`

// Server has a Name field that is excepted by config.
type Server struct {
	Name string // no diagnostic — excepted via "Server.Name"
	Port int    // want `struct field auditexceptions\.Server\.Port uses primitive type int`
}
