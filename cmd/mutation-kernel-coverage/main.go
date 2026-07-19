// SPDX-License-Identifier: MPL-2.0

// Command mutation-kernel-coverage verifies that the blocking causal mutation
// profile covers every semantic category whose catalog contract requires it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/mutationkernel"
	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
)

func main() {
	root := flag.String("root", ".", "goplint module root")
	manifestPath := flag.String(
		"manifest",
		"testdata/subgates/mutation-kernel-coverage.v1.json",
		"mutation kernel coverage manifest",
	)
	flag.Parse()
	if err := run(context.Background(), *root, *manifestPath, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "goplint mutation kernel coverage:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, root, manifestPath string, output io.Writer) error {
	result, err := mutationkernel.Load(ctx, root, manifestPath)
	if err != nil {
		return fmt.Errorf("load coverage contract: %w", err)
	}
	populations, err := result.Populations()
	if err != nil {
		return fmt.Errorf("derive soundness populations: %w", err)
	}
	if _, err := soundnessgate.EmitReportFromEnvironment(ctx, populations); err != nil {
		return fmt.Errorf("emit soundness report: %w", err)
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode mutation kernel coverage: %w", err)
	}
	return nil
}
