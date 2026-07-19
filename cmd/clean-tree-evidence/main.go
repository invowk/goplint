// SPDX-License-Identifier: MPL-2.0

// Command clean-tree-evidence records the reviewed soundness proof for an exact
// synthetic Git tree without staging through the caller's real index.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/cleantreeevidence"
)

func main() {
	root := flag.String("root", ".", "repository root")
	pathsPath := flag.String("paths", "", "reviewed newline-delimited path selection")
	planPath := flag.String("plan", "tools/goplint/testdata/gates/clean-tree-v3.json", "format-v3 command plan")
	evidencePath := flag.String(
		"evidence",
		"tools/goplint/testdata/gates/clean-tree-run.v3.json",
		"retained format-v3 evidence file",
	)
	flag.Parse()
	if *pathsPath == "" {
		fail(errors.New("-paths is required; implicit dirty-worktree capture is forbidden"))
	}
	if _, err := cleantreeevidence.Capture(context.Background(), cleantreeevidence.CaptureOptions{
		Root:         *root,
		PathsPath:    *pathsPath,
		PlanPath:     *planPath,
		EvidencePath: *evidencePath,
	}); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "goplint clean-tree evidence:", err)
	os.Exit(1)
}
