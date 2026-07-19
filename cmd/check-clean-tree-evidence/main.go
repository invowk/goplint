// SPDX-License-Identifier: MPL-2.0

// Command check-clean-tree-evidence rejects a retained soundness proof unless
// every identity still matches the exact reviewed synthetic tree.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/invowk/invowk/tools/goplint/internal/cleantreeevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

const cleanTreeGenerationCommand = "make generate-goplint-clean-tree-evidence"

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
		fail(errors.New("-paths is required; implicit dirty-worktree verification is forbidden"))
	}
	ctx := context.Background()
	if err := cleantreeevidence.Verify(ctx, cleantreeevidence.VerifyOptions{
		Root:         *root,
		PathsPath:    *pathsPath,
		PlanPath:     *planPath,
		EvidencePath: *evidencePath,
	}); err != nil {
		fail(verificationError(*evidencePath, err))
	}
	resolvedEvidencePath := *evidencePath
	if !filepath.IsAbs(resolvedEvidencePath) {
		resolvedEvidencePath = filepath.Join(*root, filepath.FromSlash(resolvedEvidencePath))
	}
	record, err := cleantreeevidence.LoadRecord(resolvedEvidencePath)
	if err != nil {
		fail(verificationError(*evidencePath, err))
	}
	populations, err := soundnessgate.PopulationsFromObservedMembers([]soundnessgate.ObservedMember{
		{PopulationID: "verified-clean-tree-records", MemberID: record.Repository.SyntheticTree},
	})
	if err != nil {
		fail(err)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(ctx, populations); err != nil {
		fail(err)
	}
}

func verificationError(evidencePath string, err error) error {
	return fmt.Errorf(
		"verify retained evidence %q: %w; regenerate the retained record with `%s`",
		evidencePath,
		err,
		cleanTreeGenerationCommand,
	)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "goplint clean-tree evidence verification:", err)
	os.Exit(1)
}
