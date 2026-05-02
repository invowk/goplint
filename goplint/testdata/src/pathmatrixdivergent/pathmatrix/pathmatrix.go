// SPDX-License-Identifier: MPL-2.0

// Package pathmatrix is a fixture-local stub of the real
// internal/testutil/pathmatrix package, with just enough surface area
// for the goplint pathmatrix-divergent check to type-resolve PassRelative
// calls, Expectations literals, and the three platform-divergent input
// constants. The check matches by package name, so this stub triggers
// the same code paths as production consumers.
package pathmatrix

const (
	InputUnixAbsolute       = "/absolute/path"
	InputWindowsDriveAbs    = `C:\absolute\path`
	InputWindowsRooted      = `\absolute\path`
	InputUNC                = `\\server\share`
	InputSlashTraversal     = "a/../../escape"
	InputBackslashTraversal = `a\..\..\escape`
)

type Outcome struct{ relative string }

type PlatformOverride struct {
	UnixAbsolute       *Outcome
	WindowsDriveAbs    *Outcome
	WindowsRooted      *Outcome
	UNC                *Outcome
	SlashTraversal     *Outcome
	BackslashTraversal *Outcome
	ValidRelative      *Outcome
}

type Expectations struct {
	OnWindows          *PlatformOverride
	OnLinux            *PlatformOverride
	OnDarwin           *PlatformOverride
	UnixAbsolute       Outcome
	WindowsDriveAbs    Outcome
	WindowsRooted      Outcome
	UNC                Outcome
	SlashTraversal     Outcome
	BackslashTraversal Outcome
	ValidRelative      Outcome
}

func PassRelative(segment string) Outcome { return Outcome{relative: segment} }

func PassHostNativeAbs(input string) Outcome { return Outcome{relative: input} }

func PassAny() Outcome { return Outcome{} }

func Pass(_ string) Outcome { return Outcome{} }
