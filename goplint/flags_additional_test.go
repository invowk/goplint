// SPDX-License-Identifier: MPL-2.0

package goplint

import "testing"

func TestTrackedStringFlagString(t *testing.T) {
	t.Parallel()

	t.Run("nil value pointer", func(t *testing.T) {
		t.Parallel()

		flag := trackedStringFlag{}
		if got := flag.String(); got != "" {
			t.Fatalf("trackedStringFlag.String() = %q, want empty", got)
		}
	})

	t.Run("returns current value", func(t *testing.T) {
		t.Parallel()

		value := "exceptions.toml"
		flag := trackedStringFlag{value: &value}
		if got := flag.String(); got != "exceptions.toml" {
			t.Fatalf("trackedStringFlag.String() = %q, want %q", got, "exceptions.toml")
		}
	})
}
