// SPDX-License-Identifier: MPL-2.0

// Package cfa_ifds_repo_patterns captures repo-shaped IFDS precision cases that
// should stay clean without Phase C refinement.
package cfa_ifds_repo_patterns

import "path/filepath"

type LocalPath string

func (p LocalPath) Validate() error { return nil }

type LocalMode string

func (m LocalMode) Validate() error { return nil }

type LocalMetadata struct {
	mode  LocalMode
	items []string
}

func (m LocalMetadata) Validate() error { return nil }

func PathFromJoin(raw string) (LocalPath, error) {
	path := LocalPath(filepath.Join(raw, "cfg"))
	if err := path.Validate(); err != nil {
		return "", err
	}
	return path, nil
}

func ModeFromString(raw string) (LocalMode, error) {
	mode := LocalMode(raw)
	if err := mode.Validate(); err != nil {
		return "", err
	}
	return mode, nil
}

func NewLocalMetadata(mode LocalMode, items []string) (*LocalMetadata, error) {
	meta := &LocalMetadata{mode: mode}
	if len(items) > 0 {
		meta.items = make([]string, len(items))
		copy(meta.items, items)
	}
	if err := meta.Validate(); err != nil {
		return nil, err
	}
	return meta, nil
}
