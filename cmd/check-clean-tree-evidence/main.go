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

	"github.com/invowk/goplint/internal/cleantreeevidence"
	"github.com/invowk/goplint/internal/soundnessgate"
)

const cleanTreeGenerationCommand = "make generate-goplint-clean-tree-evidence"

func main() {
	root := flag.String("root", ".", "repository root")
	pathsPath := flag.String("paths", "", "reviewed newline-delimited path selection")
	planPath := flag.String("plan", "testdata/gates/clean-tree-v5.json", "format-v4 command plan")
	evidencePath := flag.String(
		"evidence",
		"testdata/gates/clean-tree-run.v5.json",
		"retained format-v4 evidence file",
	)
	allowReusedAggregate := flag.Bool(
		cleantreeevidence.AllowReusedAggregateFlag,
		false,
		"accept a record whose aggregate command was reused from a caller-provided report instead of executed; "+
			"local iteration only, never a completion or release claim",
	)
	flag.Parse()
	if *pathsPath == "" {
		fail(errors.New("-paths is required; implicit dirty-worktree verification is forbidden"))
	}
	ctx := context.Background()
	if err := cleantreeevidence.Verify(ctx, cleantreeevidence.VerifyOptions{
		Root:                 *root,
		PathsPath:            *pathsPath,
		PlanPath:             *planPath,
		EvidencePath:         *evidencePath,
		AllowReusedAggregate: *allowReusedAggregate,
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
