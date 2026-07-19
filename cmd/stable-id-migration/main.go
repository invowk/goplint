// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/stableidmigration"
)

const populationReviewSchemaVersion = 1

type populationReviewManifest struct {
	SchemaVersion int                                        `json:"schema_version"`
	Reviews       []stableidmigration.PopulationChangeReview `json:"reviews"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("stable-id-migration", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	oldPath := flags.String("old", "", "old canonical findings JSONL")
	newPath := flags.String("new", "", "new canonical findings JSONL")
	repeatPath := flags.String("repeat", "", "independent repeated new findings JSONL")
	populationReviewPath := flags.String("population-review", "", "reviewed added/removed population manifest JSON")
	outPath := flags.String("out", "", "report output path (stdout when empty)")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse stable-ID migration flags: %w", err)
	}
	if *oldPath == "" || *newPath == "" || *repeatPath == "" {
		return errors.New("stable-ID migration requires -old, -new, and -repeat")
	}

	oldData, err := os.ReadFile(*oldPath)
	if err != nil {
		return fmt.Errorf("read old scan: %w", err)
	}
	newData, err := os.ReadFile(*newPath)
	if err != nil {
		return fmt.Errorf("read new scan: %w", err)
	}
	repeatData, err := os.ReadFile(*repeatPath)
	if err != nil {
		return fmt.Errorf("read repeat scan: %w", err)
	}
	reviews, err := readPopulationReviews(*populationReviewPath)
	if err != nil {
		return err
	}
	report, err := stableidmigration.BuildReviewed(oldData, newData, repeatData, reviews)
	if err != nil {
		return fmt.Errorf("build reviewed stable-ID migration: %w", err)
	}
	encoded, err := stableidmigration.Marshal(report)
	if err != nil {
		return fmt.Errorf("marshal stable-ID migration report: %w", err)
	}
	if *outPath == "" {
		if _, err := stdout.Write(encoded); err != nil {
			return fmt.Errorf("write stable-ID migration report: %w", err)
		}
	} else if err := os.WriteFile(*outPath, encoded, 0o644); err != nil {
		return fmt.Errorf("write stable-ID migration report: %w", err)
	}
	if !report.Accepted {
		return errors.New("stable-ID migration rejected; inspect the generated report")
	}
	return nil
}

func readPopulationReviews(path string) ([]stableidmigration.PopulationChangeReview, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read population review: %w", err)
	}
	var manifest populationReviewManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("decode population review: %w", err)
	}
	if manifest.SchemaVersion != populationReviewSchemaVersion {
		return nil, fmt.Errorf(
			"population review schema_version = %d, want %d",
			manifest.SchemaVersion,
			populationReviewSchemaVersion,
		)
	}
	return manifest.Reviews, nil
}
