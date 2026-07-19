// SPDX-License-Identifier: MPL-2.0

// Command soundness-gate executes the canonical aggregate goplint soundness
// manifest and validates its fresh evidence.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/goplint"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	root := flag.String("root", "../..", "repository root")
	manifest := flag.String("manifest", "tools/goplint/spec/soundness-gate.v1.json", "root-relative aggregate manifest")
	profile := flag.String("profile", string(soundnessgate.ProfileCore), "reviewed manifest profile: core or complete")
	flag.Parse()
	result, err := soundnessgate.Run(context.Background(), soundnessgate.Options{
		Root:         *root,
		ManifestPath: *manifest,
		Profile:      soundnessgate.ProfileID(*profile),
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate:", err)
		os.Exit(1)
	}
	var census bytes.Buffer
	if err := goplint.WriteSemanticCoverageCensus(&census, result.Registry, result.Report.Observations); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: semantic census:", err)
		os.Exit(1)
	}
	if _, err := census.WriteTo(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: write semantic census:", err)
		os.Exit(1)
	}
	summary := struct {
		Profile          soundnessgate.ProfileID `json:"profile"`
		RunID            string                  `json:"run_id"`
		WorkspaceDigest  string                  `json:"workspace_digest"`
		ManifestDigest   string                  `json:"manifest_digest"`
		SubgateCount     int                     `json:"subgate_count"`
		ObservationCount int                     `json:"observation_count"`
	}{
		Profile:          result.Profile,
		RunID:            result.RunID,
		WorkspaceDigest:  result.WorkspaceDigest,
		ManifestDigest:   result.ManifestDigest,
		SubgateCount:     result.SubgateCount,
		ObservationCount: result.ObservationCount,
	}
	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(summary); err != nil {
		fmt.Fprintln(os.Stderr, "goplint soundness gate: encode summary:", err)
		os.Exit(1)
	}
}
