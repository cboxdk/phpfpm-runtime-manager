package autoscaler

import (
	"testing"
)

func TestCalculateOptimalWorkers(t *testing.T) {
	tests := []struct {
		name       string
		maxWorkers int
		wantErr    bool
		validate   func(max, start, minSpare, maxSpare int) bool
	}{
		{
			name:       "no limit",
			maxWorkers: 0,
			wantErr:    false,
			validate: func(max, start, minSpare, maxSpare int) bool {
				return max >= 2 && start >= 1 && minSpare >= 1 && maxSpare > minSpare
			},
		},
		{
			name:       "with limit",
			maxWorkers: 10,
			wantErr:    false,
			validate: func(max, start, minSpare, maxSpare int) bool {
				return max <= 10 && start >= 1 && minSpare >= 1 && maxSpare > minSpare
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			max, start, minSpare, maxSpare, err := CalculateOptimalWorkers(tt.maxWorkers)

			if (err != nil) != tt.wantErr {
				t.Errorf("CalculateOptimalWorkers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && !tt.validate(max, start, minSpare, maxSpare) {
				t.Errorf("CalculateOptimalWorkers() invalid values: max=%d, start=%d, minSpare=%d, maxSpare=%d",
					max, start, minSpare, maxSpare)
			}

			t.Logf("CalculateOptimalWorkers(%d) = max=%d, start=%d, minSpare=%d, maxSpare=%d",
				tt.maxWorkers, max, start, minSpare, maxSpare)
		})
	}
}

func TestGetMemoryBasedMaxWorkers(t *testing.T) {
	maxWorkers, err := GetMemoryBasedMaxWorkers()
	if err != nil {
		t.Errorf("GetMemoryBasedMaxWorkers() error = %v", err)
		return
	}

	if maxWorkers < 2 {
		t.Errorf("GetMemoryBasedMaxWorkers() = %d, want >= 2", maxWorkers)
	}

	t.Logf("GetMemoryBasedMaxWorkers() = %d", maxWorkers)
}

func TestGetSystemMemoryInfo(t *testing.T) {
	totalMB, availableMB, reservedMB, err := GetSystemMemoryInfo()
	if err != nil {
		t.Errorf("GetSystemMemoryInfo() error = %v", err)
		return
	}

	if totalMB < 512 {
		t.Errorf("GetSystemMemoryInfo() totalMB = %d, want >= 512", totalMB)
	}

	if availableMB <= 0 {
		t.Errorf("GetSystemMemoryInfo() availableMB = %d, want > 0", availableMB)
	}

	if reservedMB <= 0 {
		t.Errorf("GetSystemMemoryInfo() reservedMB = %d, want > 0", reservedMB)
	}

	if totalMB != availableMB+reservedMB {
		t.Errorf("GetSystemMemoryInfo() memory accounting mismatch: %d != %d + %d",
			totalMB, availableMB, reservedMB)
	}

	t.Logf("GetSystemMemoryInfo() = totalMB=%d, availableMB=%d, reservedMB=%d",
		totalMB, availableMB, reservedMB)
}

func TestWorkerCalculationConfig(t *testing.T) {
	config := DefaultWorkerCalculationConfig()

	// Test default values
	if config.AvgWorkerMemoryMB != 64 {
		t.Errorf("DefaultWorkerCalculationConfig() AvgWorkerMemoryMB = %d, want 64", config.AvgWorkerMemoryMB)
	}

	if config.MemoryReservePercent != 0.25 {
		t.Errorf("DefaultWorkerCalculationConfig() MemoryReservePercent = %f, want 0.25", config.MemoryReservePercent)
	}

	if config.MinWorkers != 2 {
		t.Errorf("DefaultWorkerCalculationConfig() MinWorkers = %d, want 2", config.MinWorkers)
	}

	// Test calculation with custom config
	customConfig := WorkerCalculationConfig{
		AvgWorkerMemoryMB:    32,   // Use less memory per worker
		MemoryReservePercent: 0.15, // Reserve less memory
		StartServersPercent:  0.30, // Start with more workers
		MinSparePercent:      0.10,
		MaxSparePercent:      0.40,
		MinWorkers:           3,
		MaxWorkers:           50,
	}

	max, start, minSpare, maxSpare, err := CalculateOptimalWorkersWithConfig(customConfig)
	if err != nil {
		t.Errorf("CalculateOptimalWorkersWithConfig() error = %v", err)
		return
	}

	if max < 3 {
		t.Errorf("CalculateOptimalWorkersWithConfig() max = %d, want >= 3", max)
	}

	if max > 50 {
		t.Errorf("CalculateOptimalWorkersWithConfig() max = %d, want <= 50", max)
	}

	t.Logf("CalculateOptimalWorkersWithConfig(custom) = max=%d, start=%d, minSpare=%d, maxSpare=%d",
		max, start, minSpare, maxSpare)
}
