// SPDX-License-Identifier: MPL-2.0

package pathmatrixdivergent

import (
	"pathmatrixdivergent/pathmatrix"
)

// FLAGGED — PassRelative on the three platform-divergent input
// constants without an OnWindows override.

func divergentUNCMissingOverride() pathmatrix.Expectations {
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassAny(),
		WindowsRooted:      pathmatrix.PassAny(),
		UNC:                pathmatrix.PassRelative(pathmatrix.InputUNC), // want `pathmatrix\.PassRelative on platform-divergent vector UNC`
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
	}
}

func divergentDriveAbsMissingOverride() pathmatrix.Expectations {
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassRelative(pathmatrix.InputWindowsDriveAbs), // want `pathmatrix\.PassRelative on platform-divergent vector WindowsDriveAbs`
		WindowsRooted:      pathmatrix.PassAny(),
		UNC:                pathmatrix.PassAny(),
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
	}
}

func divergentRootedConcatMissingOverride() pathmatrix.Expectations {
	// BinaryExpr argument: `pathmatrix.InputWindowsRooted + ".sh"` should
	// also be flagged — the analyzer walks through binary concatenation.
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassAny(),
		WindowsRooted:      pathmatrix.PassRelative(pathmatrix.InputWindowsRooted + ".sh"), // want `pathmatrix\.PassRelative on platform-divergent vector WindowsRooted`
		UNC:                pathmatrix.PassAny(),
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
	}
}

// NOT FLAGGED — OnWindows override present for the divergent field.

func divergentUNCWithOverride() pathmatrix.Expectations {
	pass := pathmatrix.PassAny()
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassAny(),
		WindowsRooted:      pathmatrix.PassAny(),
		UNC:                pathmatrix.PassRelative(pathmatrix.InputUNC),
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
		OnWindows: &pathmatrix.PlatformOverride{
			UNC: &pass,
		},
	}
}

// NOT FLAGGED — PassHostNativeAbs is the recommended outcome.

func divergentUseHostNativeAbs() pathmatrix.Expectations {
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassHostNativeAbs(pathmatrix.InputWindowsDriveAbs),
		WindowsRooted:      pathmatrix.PassHostNativeAbs(pathmatrix.InputWindowsRooted),
		UNC:                pathmatrix.PassHostNativeAbs(pathmatrix.InputUNC),
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
	}
}

// NOT FLAGGED — PassRelative on a non-divergent vector.

func nonDivergentSlashTraversal() pathmatrix.Expectations {
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassAny(),
		WindowsRooted:      pathmatrix.PassAny(),
		UNC:                pathmatrix.PassAny(),
		SlashTraversal:     pathmatrix.PassRelative(pathmatrix.InputSlashTraversal),
		BackslashTraversal: pathmatrix.PassRelative(pathmatrix.InputBackslashTraversal),
		ValidRelative:      pathmatrix.PassAny(),
	}
}

// NOT FLAGGED — //goplint:ignore on the function suppresses.

//goplint:ignore -- this resolver explicitly normalizes everything so PassRelative is correct everywhere.
func ignoredFunction() pathmatrix.Expectations {
	return pathmatrix.Expectations{
		UnixAbsolute:       pathmatrix.PassAny(),
		WindowsDriveAbs:    pathmatrix.PassAny(),
		WindowsRooted:      pathmatrix.PassAny(),
		UNC:                pathmatrix.PassRelative(pathmatrix.InputUNC),
		SlashTraversal:     pathmatrix.PassAny(),
		BackslashTraversal: pathmatrix.PassAny(),
		ValidRelative:      pathmatrix.PassAny(),
	}
}
