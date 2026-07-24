// SPDX-License-Identifier: MPL-2.0

// Command docs-guard statically validates that goplint documentation remains
// anchored to executable artifacts. It has no baseline, exception, or
// inline-ignore surface: it exits zero on success and one with exact
// document:line findings on any stale reference.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/docsguard"
)

func main() {
	root := flag.String("root", "../..", "repository root")
	flag.Parse()

	report, err := docsguard.Validate(*root)
	if err != nil {
		fatal(err)
	}
	if len(report.Findings) != 0 {
		for _, finding := range report.Findings {
			fmt.Fprintln(os.Stderr, finding.String())
		}
		os.Exit(1)
	}
	if _, err := fmt.Fprintf(os.Stdout, "docs-guard: %d documents checked, %d anchors resolved\n",
		report.DocumentsChecked, report.AnchorsResolved); err != nil {
		fatal(fmt.Errorf("write validation summary: %w", err))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "validate goplint documentation:", err)
	os.Exit(1)
}
