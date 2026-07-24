// SPDX-License-Identifier: MPL-2.0

// Command benchmark-policy validates a reviewed smoke or certification policy.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/invowk/invowk/tools/goplint/internal/benchmarkpolicy"
)

func main() {
	manifestPath := flag.String("manifest", "bench/thresholds.toml", "benchmark policy manifest")
	policy := flag.String("policy", benchmarkpolicy.PolicyCertification, "required policy identity")
	runnerClass := flag.String("runner-class", os.Getenv("GOPLINT_BENCH_RUNNER_CLASS"), "actual runner class when known")
	flag.Parse()
	if _, err := benchmarkpolicy.Load(*manifestPath, *policy, runtime.Version(), *runnerClass); err != nil {
		fmt.Fprintln(os.Stderr, "validate goplint benchmark policy:", err)
		os.Exit(1)
	}
}
