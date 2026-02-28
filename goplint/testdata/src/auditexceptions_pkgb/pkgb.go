// SPDX-License-Identifier: MPL-2.0

// Package auditexceptions_pkgb has NO Server.Name field. When auditing
// exceptions in THIS package, BOTH "Server.Name" and "NonExistent.Field"
// are reported as stale — they matched nothing in this package's analysis.
package auditexceptions_pkgb // want `stale exception: pattern "Server.Name" matched no diagnostics` `stale exception: pattern "NonExistent.Field" matched no diagnostics`

// Client has no Name field — the "Server.Name" exception is stale here.
type Client struct {
	URL string // want `struct field auditexceptions_pkgb\.Client\.URL uses primitive type string`
}
