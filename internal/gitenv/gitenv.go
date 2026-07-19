// SPDX-License-Identifier: MPL-2.0

// Package gitenv isolates Git subprocesses from repository-local environment
// variables exported by callers such as commit hooks.
package gitenv

import (
	"slices"
	"strings"
)

var repositoryLocalVariables = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_CONFIG",
	"GIT_CONFIG_COUNT",
	"GIT_CONFIG_PARAMETERS",
	"GIT_DIR",
	"GIT_GRAFT_FILE",
	"GIT_IMPLICIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_NO_REPLACE_OBJECTS",
	"GIT_OBJECT_DIRECTORY",
	"GIT_PREFIX",
	"GIT_REPLACE_REF_BASE",
	"GIT_SHALLOW_FILE",
	"GIT_WORK_TREE",
}

// WithoutRepositoryLocal returns a copy without variables that bind Git to a
// caller's repository. The list matches `git rev-parse --local-env-vars`.
func WithoutRepositoryLocal(environment []string) []string {
	return slices.DeleteFunc(slices.Clone(environment), func(entry string) bool {
		key, _, _ := strings.Cut(entry, "=")
		return slices.Contains(repositoryLocalVariables, key)
	})
}
