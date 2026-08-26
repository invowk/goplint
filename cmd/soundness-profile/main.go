// SPDX-License-Identifier: MPL-2.0

// Command soundness-profile conservatively selects a goplint assurance profile.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/invowk/goplint/internal/profileownership"
)

type pathFlags []string

func (paths *pathFlags) String() string { return strings.Join(*paths, ",") }
func (paths *pathFlags) Set(value string) error {
	*paths = append(*paths, value)
	return nil
}

func main() {
	root := flag.String("root", ".", "repository root")
	manifestPath := flag.String("manifest", "spec/soundness-ownership.v2.json", "ownership manifest")
	event := flag.String("event", "pre_commit", "event context")
	base := flag.String("base", "", "base revision for changed-path discovery")
	head := flag.String("head", "HEAD", "head revision for changed-path discovery")
	staged := flag.Bool("staged", false, "classify staged paths")
	format := flag.String("format", "json", "output format: json or profile")
	var explicitPaths pathFlags
	flag.Var(&explicitPaths, "path", "explicit changed path (repeatable)")
	flag.Parse()

	manifest, err := profileownership.Load(*manifestPath)
	if err != nil {
		fatal(err)
	}
	paths := []string(explicitPaths)
	mergeBaseAvailable := len(paths) != 0
	ctx := context.Background()
	if *staged {
		paths, err = gitPaths(ctx, *root, "diff", "--cached", "--name-only", "-z", "--diff-filter=ACMRD")
		mergeBaseAvailable = true
	} else if *base != "" {
		var mergeBase []byte
		mergeBase, err = gitOutput(ctx, *root, "merge-base", *base, *head)
		if err == nil {
			mergeBaseAvailable = true
			paths, err = gitPaths(ctx, *root, "diff", "--name-only", "-z", "--diff-filter=ACMRD", strings.TrimSpace(string(mergeBase)), *head)
		}
	}
	shallow := false
	if err == nil {
		var shallowOutput []byte
		shallowOutput, err = gitOutput(ctx, *root, "rev-parse", "--is-shallow-repository")
		shallow = strings.TrimSpace(string(shallowOutput)) == "true"
	}
	if err != nil {
		mergeBaseAvailable = false
		paths = nil
	}
	decision, routeErr := manifest.Route(profileownership.Context{
		Event: *event, ChangedPaths: paths, MergeBaseAvailable: mergeBaseAvailable, ShallowRepository: shallow,
	})
	if routeErr != nil {
		fatal(routeErr)
	}
	switch *format {
	case "profile":
		if _, err := fmt.Fprintln(os.Stdout, decision.Profile); err != nil {
			fatal(fmt.Errorf("write selected profile: %w", err))
		}
	case "json":
		if encodeErr := json.NewEncoder(os.Stdout).Encode(decision); encodeErr != nil {
			fatal(encodeErr)
		}
	default:
		fatal(fmt.Errorf("format %q is invalid; want json or profile", *format))
	}
}

func gitPaths(ctx context.Context, root string, arguments ...string) ([]string, error) {
	output, err := gitOutput(ctx, root, arguments...)
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) != 0 {
			paths = append(paths, string(part))
		}
	}
	return paths, nil
}

func gitOutput(ctx context.Context, root string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w", arguments, err)
	}
	return output, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "select goplint soundness profile:", err)
	os.Exit(1)
}
