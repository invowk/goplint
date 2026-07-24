// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const (
	// EnvCPUUnits overrides the auto-detected aggregate CPU budget.
	EnvCPUUnits = "GOPLINT_SOUNDNESS_CPU_UNITS"
	// EnvMemoryBytes overrides the auto-detected aggregate memory budget.
	EnvMemoryBytes = "GOPLINT_SOUNDNESS_MEMORY_BYTES"
	// EnvMaximumWorkers overrides the aggregate worker limit.
	EnvMaximumWorkers = "GOPLINT_SOUNDNESS_MAX_WORKERS"

	conservativeMemoryFallbackBytes int64 = 4 * 1024 * 1024 * 1024
	memoryBudgetNumerator                 = 3
	memoryBudgetDenominator               = 4
)

type (
	// ResourceOverrides contains optional positive resource limits. Zero selects
	// the corresponding environment override or automatic discovery.
	ResourceOverrides struct {
		CPUUnits       int
		MemoryBytes    int64
		MaximumWorkers int
		RunnerClass    string
	}

	resourceDiscoveryDependencies struct {
		effectiveCPU    func() int
		availableMemory func() (int64, error)
		lookupEnv       func(string) (string, bool)
	}
)

// DiscoverResourceBudget resolves explicit, environment, and automatic limits
// in that order and retains the exact effective policy in a plan-ready budget.
func DiscoverResourceBudget(overrides ResourceOverrides) (ResourceBudget, error) {
	return discoverResourceBudget(overrides, resourceDiscoveryDependencies{
		effectiveCPU:    func() int { return runtime.GOMAXPROCS(0) },
		availableMemory: availableMemoryBytes,
		lookupEnv:       os.LookupEnv,
	})
}

func discoverResourceBudget(
	overrides ResourceOverrides,
	dependencies resourceDiscoveryDependencies,
) (ResourceBudget, error) {
	cpuUnits, err := resolvePositiveIntOverride(EnvCPUUnits, overrides.CPUUnits, dependencies.lookupEnv)
	if err != nil {
		return ResourceBudget{}, err
	}
	if cpuUnits == 0 {
		cpuUnits = dependencies.effectiveCPU()
	}
	if cpuUnits <= 0 {
		cpuUnits = 1
	}
	memoryBytes, err := resolvePositiveInt64Override(EnvMemoryBytes, overrides.MemoryBytes, dependencies.lookupEnv)
	if err != nil {
		return ResourceBudget{}, err
	}
	if memoryBytes == 0 {
		available, discoveryErr := dependencies.availableMemory()
		if discoveryErr != nil || available <= 0 {
			memoryBytes = conservativeMemoryFallbackBytes
		} else {
			memoryBytes = available * memoryBudgetNumerator / memoryBudgetDenominator
		}
	}
	maximumWorkers, err := resolvePositiveIntOverride(EnvMaximumWorkers, overrides.MaximumWorkers, dependencies.lookupEnv)
	if err != nil {
		return ResourceBudget{}, err
	}
	if maximumWorkers == 0 {
		maximumWorkers = cpuUnits
	}
	maximumWorkers = min(maximumWorkers, cpuUnits)
	runnerClass := strings.TrimSpace(overrides.RunnerClass)
	if runnerClass == "" {
		runnerClass = fmt.Sprintf("local-%dcpu", cpuUnits)
	}
	budget := ResourceBudget{
		CPUUnits:       cpuUnits,
		MemoryBytes:    memoryBytes,
		MaximumWorkers: maximumWorkers,
		RunnerClass:    runnerClass,
	}
	if err := budget.validate(); err != nil {
		return ResourceBudget{}, fmt.Errorf("validate discovered soundness resource budget: %w", err)
	}
	return budget, nil
}

func resolvePositiveIntOverride(
	name string,
	explicit int,
	lookupEnv func(string) (string, bool),
) (int, error) {
	if explicit < 0 {
		return 0, fmt.Errorf("soundness resource override %s must be positive", name)
	}
	if explicit > 0 {
		return explicit, nil
	}
	value, exists := lookupEnv(name)
	if !exists {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("soundness resource override %s=%q must be a positive integer", name, value)
	}
	return parsed, nil
}

func resolvePositiveInt64Override(
	name string,
	explicit int64,
	lookupEnv func(string) (string, bool),
) (int64, error) {
	if explicit < 0 {
		return 0, fmt.Errorf("soundness resource override %s must be positive", name)
	}
	if explicit > 0 {
		return explicit, nil
	}
	value, exists := lookupEnv(name)
	if !exists {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("soundness resource override %s=%q must be a positive integer byte count", name, value)
	}
	return parsed, nil
}

func parseMemAvailable(data []byte) (int64, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "MemAvailable:" || fields[2] != "kB" {
			continue
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kilobytes <= 0 {
			return 0, errors.New("linux MemAvailable is not a positive integer")
		}
		return kilobytes * 1024, nil
	}
	return 0, errors.New("linux MemAvailable is unavailable")
}
