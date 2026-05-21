// SPDX-License-Identifier: MPL-2.0

package testhomeenv

import (
	"os"
	"testing"
)

func TestDirectTestingHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // want `sets HOME directly`
}

func TestDirectOSHome(t *testing.T) {
	if err := os.Setenv("HOME", t.TempDir()); err != nil { // want `sets HOME directly`
		t.Fatal(err)
	}
}

func TestOtherEnv(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
}

func TestIgnoredHome(t *testing.T) {
	t.Setenv("HOMELESS", t.TempDir())
}
