// SPDX-License-Identifier: MPL-2.0

package soundnessgate

import (
	"errors"
	"testing"
)

func TestDiscoverResourceBudgetUsesEffectiveCPUAndMemoryHeadroom(t *testing.T) {
	t.Parallel()

	budget, err := discoverResourceBudget(ResourceOverrides{}, resourceDiscoveryDependencies{
		effectiveCPU:    func() int { return 24 },
		availableMemory: func() (int64, error) { return 96 * 1024 * 1024 * 1024, nil },
		lookupEnv:       func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("discoverResourceBudget() error = %v", err)
	}
	if budget.CPUUnits != 24 || budget.MaximumWorkers != 24 {
		t.Fatalf("CPU budget = %+v, want 24 CPUs and workers", budget)
	}
	if budget.MemoryBytes != 72*1024*1024*1024 {
		t.Fatalf("memory budget = %d, want 75%% headroom policy", budget.MemoryBytes)
	}
}

func TestDiscoverResourceBudgetOverridesAndFallback(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		EnvCPUUnits:       "8",
		EnvMemoryBytes:    "17179869184",
		EnvMaximumWorkers: "6",
	}
	budget, err := discoverResourceBudget(ResourceOverrides{CPUUnits: 4}, resourceDiscoveryDependencies{
		effectiveCPU:    func() int { return 24 },
		availableMemory: func() (int64, error) { return 0, errors.New("unavailable") },
		lookupEnv: func(name string) (string, bool) {
			value, exists := environment[name]
			return value, exists
		},
	})
	if err != nil {
		t.Fatalf("discoverResourceBudget() error = %v", err)
	}
	if budget.CPUUnits != 4 || budget.MemoryBytes != 16*1024*1024*1024 || budget.MaximumWorkers != 4 {
		t.Fatalf("override budget = %+v", budget)
	}

	fallback, err := discoverResourceBudget(ResourceOverrides{}, resourceDiscoveryDependencies{
		effectiveCPU:    func() int { return 2 },
		availableMemory: func() (int64, error) { return 0, errors.New("unavailable") },
		lookupEnv:       func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("fallback discoverResourceBudget() error = %v", err)
	}
	if fallback.MemoryBytes != conservativeMemoryFallbackBytes {
		t.Fatalf("fallback memory = %d, want %d", fallback.MemoryBytes, conservativeMemoryFallbackBytes)
	}
}

func TestDiscoverResourceBudgetRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	_, err := discoverResourceBudget(ResourceOverrides{}, resourceDiscoveryDependencies{
		effectiveCPU:    func() int { return 4 },
		availableMemory: func() (int64, error) { return 1024, nil },
		lookupEnv: func(name string) (string, bool) {
			if name == EnvCPUUnits {
				return "all", true
			}
			return "", false
		},
	})
	assertGateErrorContains(t, err, EnvCPUUnits+"=\"all\"")
}

func TestParseMemAvailable(t *testing.T) {
	t.Parallel()

	got, err := parseMemAvailable([]byte("MemTotal: 1024 kB\nMemAvailable: 768 kB\n"))
	if err != nil {
		t.Fatalf("parseMemAvailable() error = %v", err)
	}
	if got != 768*1024 {
		t.Fatalf("parseMemAvailable() = %d, want %d", got, 768*1024)
	}
}
