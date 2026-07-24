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
	planPath := flag.String("plan", "tools/goplint/testdata/gates/clean-tree-v4.json", "format-v4 command plan")
	evidencePath := flag.String(
		"evidence",
		"tools/goplint/testdata/gates/clean-tree-run.v4.json",
		"retained format-v4 evidence file",
	)
	rebind := flag.Bool(
		"rebind",
		false,
		"re-bind the retained record after prose-only drift without executing any assurance profile; "+
			"semantic-content drift fails closed",
	)
	flag.Parse()
	if *pathsPath == "" {
		fail(errors.New("-paths is required; implicit dirty-worktree capture is forbidden"))
	}
	options := cleantreeevidence.CaptureOptions{
		Root:         *root,
		PathsPath:    *pathsPath,
		PlanPath:     *planPath,
		EvidencePath: *evidencePath,
	}
	if *rebind {
		if _, err := cleantreeevidence.Rebind(context.Background(), options); err != nil {
			fail(err)
		}
		return
	}
	if _, err := cleantreeevidence.Capture(context.Background(), options); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "goplint clean-tree evidence:", err)
	os.Exit(1)
}
