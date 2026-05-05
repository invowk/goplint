// SPDX-License-Identifier: MPL-2.0

package windowspitfalls

import (
	"context"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const commandWaitDelay = 10 * time.Second

type (
	CommandName string

	HostFilesystemPath string

	ContainerPath string

	VolumeMountSpec string

	PreparedCommand struct {
		Cmd *exec.Cmd
	}

	VolumeMount struct {
		HostPath      HostFilesystemPath
		ContainerPath ContainerPath
	}

	//goplint:cue-fed-path
	WorkDir string // want WorkDir:"cue-fed-path"
)

func missingWaitDelay(ctx context.Context, command CommandName) error {
	cmd := exec.CommandContext(ctx, string(command))
	return cmd.Run() // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelay is used without setting Cmd\.WaitDelay`
}

func directMissingWaitDelay(ctx context.Context, command CommandName) error {
	return exec.CommandContext(ctx, string(command)).Run() // want `exec\.CommandContext command in windowspitfalls\.directMissingWaitDelay is used without setting Cmd\.WaitDelay`
}

//goplint:ignore -- command string is intentionally raw for this fixture.
func ignoredFunctionStillNeedsWaitDelay(ctx context.Context, command CommandName) error {
	cmd := exec.CommandContext(ctx, string(command))
	return cmd.Run() // want `exec\.CommandContext command in windowspitfalls\.ignoredFunctionStillNeedsWaitDelay is used without setting Cmd\.WaitDelay`
}

func waitDelaySet(ctx context.Context, command CommandName) error {
	cmd := exec.CommandContext(ctx, string(command))
	cmd.WaitDelay = commandWaitDelay
	return cmd.Run()
}

func missingWaitDelayPrepared(ctx context.Context, command CommandName) *PreparedCommand {
	cmd := exec.CommandContext(ctx, string(command))
	return &PreparedCommand{Cmd: cmd} // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelayPrepared is used without setting Cmd\.WaitDelay`
}

func preparedWaitDelaySet(ctx context.Context, command CommandName) *PreparedCommand {
	cmd := exec.CommandContext(ctx, string(command))
	cmd.WaitDelay = commandWaitDelay
	return &PreparedCommand{Cmd: cmd}
}

func ValidateWorkDirPath(p WorkDir) WorkDir {
	return WorkDir(filepath.Clean(string(p))) // want `CUE-fed or repo-relative path in windowspitfalls\.ValidateWorkDirPath flows into filepath\.Clean`
}

func ValidateWorkDirPathSlashClean(p WorkDir) WorkDir {
	return WorkDir(path.Clean(strings.ReplaceAll(string(p), "\\", "/")))
}

func badRelBoundary(base, target HostFilesystemPath) bool {
	rel, err := filepath.Rel(string(base), string(target))
	return err == nil && !strings.HasPrefix(rel, "..") // want `unsafe path boundary check in windowspitfalls\.badRelBoundary`
}

func goodRelBoundary(base, target HostFilesystemPath) bool {
	rel, err := filepath.Rel(string(base), string(target))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func badCleanBoundary(base, target HostFilesystemPath) bool {
	baseClean := filepath.Clean(string(base))
	targetClean := filepath.Clean(string(target))
	return strings.HasPrefix(targetClean, baseClean) // want `unsafe path boundary check in windowspitfalls\.badCleanBoundary`
}

func goodCleanBoundary(base, target HostFilesystemPath) bool {
	baseClean := filepath.Clean(string(base))
	targetClean := filepath.Clean(string(target))
	return targetClean == baseClean || strings.HasPrefix(targetClean, baseClean+string(filepath.Separator))
}

func badVolumeMount(host HostFilesystemPath) VolumeMountSpec {
	return VolumeMountSpec(string(host) + ":/workspace") // want `container volume mount host path in windowspitfalls\.badVolumeMount is formatted before filepath\.ToSlash`
}

func goodVolumeMount(host HostFilesystemPath) VolumeMountSpec {
	return VolumeMountSpec(filepath.ToSlash(string(host)) + ":/workspace")
}

func (v VolumeMount) String() string {
	return string(v.HostPath) + ":" + string(v.ContainerPath) // want `container volume mount host path in windowspitfalls\.VolumeMount\.String is formatted before filepath\.ToSlash`
}

func cobraBackground(cmd *cobra.Command) context.Context {
	return context.Background() // want `Cobra command handler windowspitfalls\.cobraBackground calls context\.Background`
}

func cobraCommandContext(cmd *cobra.Command) context.Context {
	return cmd.Context()
}
