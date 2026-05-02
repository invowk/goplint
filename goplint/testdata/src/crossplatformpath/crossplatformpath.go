// SPDX-License-Identifier: MPL-2.0

package crossplatformpath

import (
	"path/filepath"
	"strings"
)

// --- Named string types so the always-on primitive analyzer is silent ---

type WorkDir string

type ResolvedPath string

// CueFedPath is annotated with //goplint:cue-fed-path. V2 of the rule flags
// any filepath.IsAbs call whose argument resolves to this type without a
// preceding strings.HasPrefix(input, "/") guard.
//
//goplint:cue-fed-path
type CueFedPath string // want CueFedPath:"cue-fed-path"

// HostNativePath is intentionally NOT annotated. IsAbs calls on values of
// this type are NOT flagged by V2 — host-native semantics are correct here.
type HostNativePath string

// --- FLAGGED: canonical bug pattern (multi-line FromSlash + IsAbs) ---

func multiLineBug(workdir WorkDir) ResolvedPath {
	nativePath := filepath.FromSlash(string(workdir))
	if filepath.IsAbs(nativePath) { // want `filepath\.IsAbs called on filepath\.FromSlash result`
		return ResolvedPath(nativePath)
	}
	return ""
}

// --- FLAGGED: single-line bug (IsAbs(FromSlash(x))) ---

func singleLineBug(workdir WorkDir) bool {
	return filepath.IsAbs(filepath.FromSlash(string(workdir))) // want `filepath\.IsAbs called on filepath\.FromSlash result`
}

// --- NOT FLAGGED: HasPrefix("/") guard precedes FromSlash ---

func properGuard(workdir WorkDir) ResolvedPath {
	if strings.HasPrefix(string(workdir), "/") {
		return ResolvedPath(workdir)
	}
	nativePath := filepath.FromSlash(string(workdir))
	if filepath.IsAbs(nativePath) {
		return ResolvedPath(nativePath)
	}
	return ""
}

// --- NOT FLAGGED: IsAbs on a raw host string ---

func hostPathOnly(absHostPath WorkDir) ResolvedPath {
	if filepath.IsAbs(string(absHostPath)) {
		return ResolvedPath(absHostPath)
	}
	return ""
}

// --- NOT FLAGGED: FromSlash without subsequent IsAbs ---

func fromSlashOnly(input WorkDir) ResolvedPath {
	return ResolvedPath(filepath.FromSlash(string(input)))
}

// --- NOT FLAGGED: IsAbs in a function with //goplint:ignore ---

//goplint:ignore -- legacy path resolver kept for back-compat behind an
// explicit adapter boundary that documents the cross-platform issue.
func ignoredFunction(workdir WorkDir) bool {
	nativePath := filepath.FromSlash(string(workdir))
	return filepath.IsAbs(nativePath)
}

// --- NOT FLAGGED: variable reassigned from non-FromSlash source ---

func reassigned(workdir WorkDir) bool {
	nativePath := filepath.FromSlash(string(workdir))
	_ = nativePath
	nativePath = "/different/source"
	return filepath.IsAbs(nativePath)
}

// --- FLAGGED: nested in if-init statement ---

func ifInitBug(workdir WorkDir) ResolvedPath {
	if nativePath := filepath.FromSlash(string(workdir)); filepath.IsAbs(nativePath) { // want `filepath\.IsAbs called on filepath\.FromSlash result`
		return ResolvedPath(nativePath)
	}
	return ""
}

// --- V2 FLAGGED: IsAbs on a CUE-fed-path-typed value without slash guard ---

func v2DirectCueFed(p CueFedPath) ResolvedPath {
	if filepath.IsAbs(string(p)) { // want `filepath\.IsAbs called on CueFedPath value`
		return ResolvedPath(p)
	}
	return ""
}

// --- V2 NOT FLAGGED: HasPrefix("/") guard precedes IsAbs on a CUE-fed value ---

func v2GuardedCueFed(p CueFedPath) ResolvedPath {
	s := string(p)
	if strings.HasPrefix(s, "/") {
		return ResolvedPath(p)
	}
	if filepath.IsAbs(s) {
		return ResolvedPath(p)
	}
	return ""
}

// --- V2 NOT FLAGGED: IsAbs on a host-native (un-annotated) typed value ---

func v2HostNative(p HostNativePath) ResolvedPath {
	if filepath.IsAbs(string(p)) {
		return ResolvedPath(p)
	}
	return ""
}

// --- V2 FLAGGED: chained through TrimSpace + string conversion ---

func v2TrimSpaceChain(p CueFedPath) ResolvedPath {
	trimmed := strings.TrimSpace(string(p))
	if filepath.IsAbs(trimmed) { // want `filepath\.IsAbs called on CueFedPath value`
		return ResolvedPath(trimmed)
	}
	return ""
}

// --- V2 NOT FLAGGED: HasPrefix guard on the trimmed variable ---

func v2TrimSpaceChainGuarded(p CueFedPath) ResolvedPath {
	trimmed := strings.TrimSpace(string(p))
	if strings.HasPrefix(trimmed, "/") {
		return ResolvedPath(trimmed)
	}
	if filepath.IsAbs(trimmed) {
		return ResolvedPath(trimmed)
	}
	return ""
}

// --- V2 FLAGGED: IsAbs on a field selector of CUE-fed type ---

type cueFedHolder struct {
	Path CueFedPath
}

func v2FieldSelector(h *cueFedHolder) ResolvedPath {
	if filepath.IsAbs(string(h.Path)) { // want `filepath\.IsAbs called on CueFedPath value`
		return ResolvedPath(h.Path)
	}
	return ""
}

// --- V2 NOT FLAGGED: //goplint:ignore on the function ---

//goplint:ignore -- intentionally operates on CUE-fed value with documented host semantics.
func v2IgnoredFunction(p CueFedPath) ResolvedPath {
	if filepath.IsAbs(string(p)) {
		return ResolvedPath(p)
	}
	return ""
}
