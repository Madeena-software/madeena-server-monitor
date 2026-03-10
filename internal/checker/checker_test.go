package checker_test

import (
	"testing"
	"time"

	"github.com/Madeena-software/madeena-server-monitor/internal/checker"
)

// TestCollect verifies that Collect returns a non-nil SystemStats with
// sensible values on the host where tests are running.
func TestCollect(t *testing.T) {
	chk := checker.New(nil, nil)
	stats, err := chk.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if stats == nil {
		t.Fatal("Collect() returned nil stats")
	}

	// CPU usage should be in [0, 100]
	if stats.CPUUsagePercent < 0 || stats.CPUUsagePercent > 100 {
		t.Errorf("CPUUsagePercent out of range: %.2f", stats.CPUUsagePercent)
	}

	// Memory should have positive total
	if stats.MemTotalMB == 0 {
		t.Error("MemTotalMB is 0; expected positive value")
	}

	// Used should not exceed total
	if stats.MemUsedMB > stats.MemTotalMB {
		t.Errorf("MemUsedMB (%d) > MemTotalMB (%d)", stats.MemUsedMB, stats.MemTotalMB)
	}

	// Root disk should have a positive total
	if stats.RootDiskTotalGB == 0 {
		t.Error("RootDiskTotalGB is 0; expected positive value")
	}

	// RootDiskPercent in [0, 100]
	if stats.RootDiskPercent < 0 || stats.RootDiskPercent > 100 {
		t.Errorf("RootDiskPercent out of range: %.2f", stats.RootDiskPercent)
	}

	// Uptime should be positive
	if stats.UptimeDuration <= 0 {
		t.Errorf("UptimeDuration non-positive: %v", stats.UptimeDuration)
	}

	// CollectedAt should be recent (within the last minute)
	if time.Since(stats.CollectedAt) > time.Minute {
		t.Errorf("CollectedAt is too old: %v", stats.CollectedAt)
	}
}

// TestNetworkRateSecondCall verifies that a second Collect call populates
// network rate fields (they are zero on the first call by design).
func TestNetworkRateSecondCall(t *testing.T) {
	chk := checker.New(nil, nil)

	// First call – rates will be zero because there is no previous baseline.
	_, err := chk.Collect()
	if err != nil {
		t.Fatalf("first Collect() error: %v", err)
	}

	// Wait briefly so the rate calculation has a non-zero elapsed time.
	time.Sleep(200 * time.Millisecond)

	stats2, err := chk.Collect()
	if err != nil {
		t.Fatalf("second Collect() error: %v", err)
	}

	// Rates must be non-negative (they can be zero on a quiet system).
	if stats2.NetRxBytesPerSec < 0 {
		t.Errorf("NetRxBytesPerSec negative: %.2f", stats2.NetRxBytesPerSec)
	}
	if stats2.NetTxBytesPerSec < 0 {
		t.Errorf("NetTxBytesPerSec negative: %.2f", stats2.NetTxBytesPerSec)
	}
}

// TestFormatUptime verifies the human-readable uptime formatter.
func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0d 0h 0m"},
		{time.Hour, "0d 1h 0m"},
		{25*time.Hour + 30*time.Minute, "1d 1h 30m"},
		{48*time.Hour + 3*time.Minute, "2d 0h 3m"},
	}
	for _, tt := range tests {
		got := checker.FormatUptime(tt.d)
		if got != tt.want {
			t.Errorf("FormatUptime(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

// TestDataPartitionMissing verifies that requesting a non-existent mount
// point does not cause Collect to crash; the entry should be present with
// zero values.
func TestDataPartitionMissing(t *testing.T) {
	chk := checker.New([]string{"/nonexistent-mount-point-xyzzy"}, nil)
	stats, err := chk.Collect()
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	du, ok := stats.DataDisks["/nonexistent-mount-point-xyzzy"]
	if !ok {
		t.Error("expected DataDisks entry for missing mount point")
	}
	if du.TotalGB != 0 || du.UsedGB != 0 {
		t.Errorf("expected zero values for missing mount, got %+v", du)
	}
}
