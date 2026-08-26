// SPDX-License-Identifier: MPL-2.0

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestVerificationErrorPreservesCauseAndNamesGenerationCommand(t *testing.T) {
	t.Parallel()

	cause := errors.New("retained record is missing or stale")
	const evidencePath = "testdata/gates/clean-tree-run.v5.json"
	err := verificationError(evidencePath, cause)
	if !errors.Is(err, cause) {
		t.Errorf("verificationError() does not preserve cause: %v", err)
	}
	if !strings.Contains(err.Error(), evidencePath) {
		t.Errorf("verificationError() = %q, want evidence path", err)
	}
	if !strings.Contains(err.Error(), cleanTreeGenerationCommand) {
		t.Errorf("verificationError() = %q, want generation command", err)
	}
}
