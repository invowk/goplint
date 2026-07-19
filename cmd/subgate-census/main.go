// SPDX-License-Identifier: MPL-2.0

// Command subgate-census executes a manifest-declared Go test census and
// publishes exact populations derived from the current observations.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/invowk/invowk/tools/goplint/internal/soundnessgate"
	"github.com/invowk/invowk/tools/goplint/internal/subgatecensus"
)

func main() {
	manifestPath := flag.String("manifest", "", "subgate census manifest")
	flag.Parse()
	if *manifestPath == "" {
		fmt.Fprintln(os.Stderr, "goplint subgate census: -manifest is required")
		os.Exit(2)
	}
	manifest, err := subgatecensus.Load(*manifestPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint subgate census:", err)
		os.Exit(1)
	}
	ctx := context.Background()
	populations, err := subgatecensus.RunManifest(ctx, manifest, os.Stdout)
	if err == nil {
		_, err = soundnessgate.EmitReportFromEnvironment(ctx, populations)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goplint subgate census:", err)
		os.Exit(1)
	}
	fmt.Printf("goplint subgate census: recorded %d exact populations\n", len(populations))
}
