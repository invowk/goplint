// SPDX-License-Identifier: MPL-2.0

package main

import "testing"

func TestResolveWorkerResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                             string
		effective, workers, cpuPerWorker int
		wantWorkers, wantCPUPerWorker    int
		wantError                        bool
	}{
		{name: "automatic", effective: 16, workers: 4, wantWorkers: 4, wantCPUPerWorker: 4},
		{name: "too many workers", effective: 4, workers: 64, wantError: true},
		{name: "explicit overcommit", effective: 8, workers: 4, cpuPerWorker: 3, wantError: true},
		{name: "explicit bound", effective: 8, workers: 4, cpuPerWorker: 2, wantWorkers: 4, wantCPUPerWorker: 2},
		{name: "zero workers", effective: 8, workers: 0, wantError: true},
		{name: "negative effective CPU", effective: -1, workers: 1, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			workers, cpuPerWorker, err := resolveWorkerResources(test.effective, test.workers, test.cpuPerWorker)
			if (err != nil) != test.wantError {
				t.Fatalf("resolveWorkerResources() error = %v, wantError %t", err, test.wantError)
			}
			if workers != test.wantWorkers || cpuPerWorker != test.wantCPUPerWorker {
				t.Fatalf("resolveWorkerResources() = %d/%d, want %d/%d", workers, cpuPerWorker, test.wantWorkers, test.wantCPUPerWorker)
			}
		})
	}
}
