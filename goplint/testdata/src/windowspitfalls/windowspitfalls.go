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

	ContainerVolumeMount struct {
		HostPath      HostFilesystemPath
		ContainerPath ContainerPath
	}

	ContainerVolumeMountCustomWriter struct {
		HostPath HostFilesystemPath
	}

	ContainerVolumeMountTwoArgWriter struct {
		HostPath      HostFilesystemPath
		ContainerPath ContainerPath
	}

	OtherMount struct {
		HostPath HostFilesystemPath
	}

	hostWriter struct{}

	twoArgWriter struct{}

	commandRunner func(*exec.Cmd) error

	//goplint:cue-fed-path
	WorkDir string // want WorkDir:"cue-fed-path"

	//goplint:path-domain=container
	PortableContainerPath string // want PortableContainerPath:"path-domain=container"

	ContainerishName string
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

func missingWaitDelayRunner(ctx context.Context, command CommandName, runner commandRunner) error {
	cmd := exec.CommandContext(ctx, string(command))
	return runner(cmd) // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelayRunner is used without setting Cmd\.WaitDelay`
}

func missingWaitDelayDoubleRun(ctx context.Context, command CommandName) error {
	cmd := exec.CommandContext(ctx, string(command))
	if err := cmd.Run(); err != nil { // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelayDoubleRun is used without setting Cmd\.WaitDelay`
		return err
	}
	return cmd.Wait()
}

func missingWaitDelayRunnerTwice(ctx context.Context, command CommandName, runner commandRunner) error {
	cmd := exec.CommandContext(ctx, string(command))
	if err := runner(cmd); err != nil { // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelayRunnerTwice is used without setting Cmd\.WaitDelay`
		return err
	}
	return runner(cmd)
}

func missingWaitDelayPreparedPair(ctx context.Context, command CommandName) (*PreparedCommand, *PreparedCommand) {
	cmd := exec.CommandContext(ctx, string(command))
	return &PreparedCommand{Cmd: cmd}, &PreparedCommand{Cmd: cmd} // want `exec\.CommandContext command in windowspitfalls\.missingWaitDelayPreparedPair is used without setting Cmd\.WaitDelay`
}

func nonExecutionMethodIgnored(ctx context.Context, command CommandName) CommandName {
	cmd := exec.CommandContext(ctx, string(command))
	return CommandName(cmd.String())
}

func preparedWaitDelaySet(ctx context.Context, command CommandName) *PreparedCommand {
	cmd := exec.CommandContext(ctx, string(command))
	cmd.WaitDelay = commandWaitDelay
	return &PreparedCommand{Cmd: cmd}
}

func runnerWaitDelaySet(ctx context.Context, command CommandName, runner commandRunner) error {
	cmd := exec.CommandContext(ctx, string(command))
	cmd.WaitDelay = commandWaitDelay
	return runner(cmd)
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

func (v ContainerVolumeMount) String() string {
	var b strings.Builder
	b.WriteString(string(v.HostPath)) // want `container volume mount host path in windowspitfalls\.ContainerVolumeMount\.String is formatted before filepath\.ToSlash`
	b.WriteString(":")
	b.WriteString(string(v.ContainerPath))
	return b.String()
}

func (v OtherMount) String() string {
	var b strings.Builder
	b.WriteString(string(v.HostPath))
	return b.String()
}

func (w hostWriter) WriteHostPath(host HostFilesystemPath) {
	_ = host
}

func (w twoArgWriter) WriteString(host HostFilesystemPath, container ContainerPath) {
	_, _ = host, container
}

func (v ContainerVolumeMountCustomWriter) String() string {
	var w hostWriter
	w.WriteHostPath(v.HostPath)
	return ""
}

func (v ContainerVolumeMountTwoArgWriter) String() string {
	var w twoArgWriter
	w.WriteString(v.HostPath, v.ContainerPath)
	return ""
}

func cobraBackground(cmd *cobra.Command) context.Context {
	return context.Background() // want `Cobra command handler windowspitfalls\.cobraBackground calls context\.Background`
}

func cobraCommandContext(cmd *cobra.Command) context.Context {
	return cmd.Context()
}

//goplint:render
func badContainerJoin(p PortableContainerPath) string {
	return filepath.Join("/workspace", string(p)) // want `filepath\.Join called on container path-domain value PortableContainerPath`
}

//goplint:render
func badContainerClean(p PortableContainerPath) string {
	clean := string(p)
	return filepath.Clean(clean) // want `filepath\.Clean called on container path-domain value PortableContainerPath`
}

func badContainerIsAbs(p PortableContainerPath) bool {
	return filepath.IsAbs(string(p)) // want `filepath\.IsAbs called on container path-domain value PortableContainerPath`
}

//goplint:render
func goodContainerJoin(p PortableContainerPath) string {
	return path.Join("/workspace", string(p))
}

//goplint:render
func unannotatedContainerishName(p ContainerishName) string {
	return filepath.Clean(string(p))
}
