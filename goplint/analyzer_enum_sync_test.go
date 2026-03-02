// SPDX-License-Identifier: MPL-2.0

package goplint

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

// TestLookupWithOptional verifies that lookupWithOptional can navigate CUE
// values including optional fields that LookupPath cannot find directly.
func TestLookupWithOptional(t *testing.T) {
	t.Parallel()

	t.Run("required definition field", func(t *testing.T) {
		t.Parallel()
		ctx := cuecontext.New()
		v := ctx.CompileString(`#Config: { mode: "a" | "b" }`)
		if v.Err() != nil {
			t.Fatalf("compile: %v", v.Err())
		}
		result, err := lookupWithOptional(v, "#Config.mode")
		if err != nil {
			t.Fatalf("lookupWithOptional: %v", err)
		}
		if result.Err() != nil {
			t.Fatalf("result error: %v", result.Err())
		}
	})

	t.Run("optional field", func(t *testing.T) {
		t.Parallel()
		ctx := cuecontext.New()
		v := ctx.CompileString(`#Config: { engine?: "docker" | "podman" }`)
		if v.Err() != nil {
			t.Fatalf("compile: %v", v.Err())
		}
		result, err := lookupWithOptional(v, "#Config.engine")
		if err != nil {
			t.Fatalf("lookupWithOptional: %v", err)
		}
		if result.Err() != nil {
			t.Fatalf("result error: %v", result.Err())
		}
	})

	t.Run("nested definition", func(t *testing.T) {
		t.Parallel()
		ctx := cuecontext.New()
		v := ctx.CompileString(`#Outer: { #Inner: { mode: "x" | "y" } }`)
		if v.Err() != nil {
			t.Fatalf("compile: %v", v.Err())
		}
		result, err := lookupWithOptional(v, "#Outer.#Inner.mode")
		if err != nil {
			t.Fatalf("lookupWithOptional: %v", err)
		}
		if result.Err() != nil {
			t.Fatalf("result error: %v", result.Err())
		}
	})

	t.Run("missing field returns error", func(t *testing.T) {
		t.Parallel()
		ctx := cuecontext.New()
		v := ctx.CompileString(`#Config: { mode: "a" }`)
		if v.Err() != nil {
			t.Fatalf("compile: %v", v.Err())
		}
		_, err := lookupWithOptional(v, "#Config.nonexistent")
		if err == nil {
			t.Error("expected error for missing field, got nil")
		}
	})
}
