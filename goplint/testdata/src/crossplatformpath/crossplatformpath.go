// SPDX-License-Identifier: MPL-2.0

package crossplatformpath

import (
	"path/filepath"
	"strings"
)

// --- Named string types so the always-on primitive analyzer is silent ---

type WorkDir string

type ResolvedPath string

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
