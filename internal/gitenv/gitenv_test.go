// SPDX-License-Identifier: MPL-2.0

package gitenv

import (
	"slices"
	"testing"
)

func TestWithoutRepositoryLocalPreservesOnlyNonlocalEntries(t *testing.T) {
	t.Parallel()

	environment := []string{
		"PATH=/usr/bin",
		"GIT_DIR=/caller/.git",
		"GIT_INDEX_FILE=/caller/.git/index",
		"GIT_CONFIG_COUNT=1",
		"GOPLINT_SOUNDNESS_RUN_ID=run-test",
	}
	want := []string{
		"PATH=/usr/bin",
		"GOPLINT_SOUNDNESS_RUN_ID=run-test",
	}
	if got := WithoutRepositoryLocal(environment); !slices.Equal(got, want) {
		t.Fatalf("WithoutRepositoryLocal() = %q, want %q", got, want)
	}
}
