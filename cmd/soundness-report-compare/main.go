// SPDX-License-Identifier: MPL-2.0

// Command soundness-report-compare verifies byte-identical normalized
// findings, observations, populations, and verdicts between two retained runs.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessevidence"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	referencePath := flag.String("reference", "", "retained serial-reference report")
	candidatePath := flag.String("candidate", "", "retained optimized report")
	flag.Parse()
	if err := compareReportFiles(context.Background(), *referencePath, *candidatePath); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness report comparison:", err)
		os.Exit(1)
	}
}

func compareReportFiles(ctx context.Context, referencePath, candidatePath string) error {
	if referencePath == "" || candidatePath == "" {
		return errors.New("reference and candidate report paths are required")
	}
	reference, err := soundnessgate.LoadRunReport(ctx, referencePath)
	if err != nil {
		return fmt.Errorf("load reference soundness report: %w", err)
	}
	candidate, err := soundnessgate.LoadRunReport(ctx, candidatePath)
	if err != nil {
		return fmt.Errorf("load candidate soundness report: %w", err)
	}
	referenceDigest, candidateDigest, identical, err := compareNormalizedReports(reference, candidate)
	if err != nil {
		return err
	}
	if !identical {
		return fmt.Errorf(
			"normalized reports differ: reference=%s candidate=%s",
			referenceDigest, candidateDigest,
		)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Status string `json:"status"`
		Digest string `json:"normalized_report_digest"`
	}{Status: "identical", Digest: referenceDigest}); err != nil {
		return fmt.Errorf("encode soundness report comparison: %w", err)
	}
	return nil
}

func compareNormalizedReports(reference, candidate soundnessgate.RunReport) (string, string, bool, error) {
	referenceJSON, err := soundnessgate.NormalizedRunReportJSON(reference)
	if err != nil {
		return "", "", false, fmt.Errorf("normalize reference soundness report: %w", err)
	}
	candidateJSON, err := soundnessgate.NormalizedRunReportJSON(candidate)
	if err != nil {
		return "", "", false, fmt.Errorf("normalize candidate soundness report: %w", err)
	}
	referenceDigest := soundnessevidence.DigestBytes(referenceJSON)
	candidateDigest := soundnessevidence.DigestBytes(candidateJSON)
	return referenceDigest, candidateDigest, bytes.Equal(referenceJSON, candidateJSON), nil
}
