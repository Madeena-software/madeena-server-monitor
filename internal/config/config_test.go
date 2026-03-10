package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/Madeena-software/madeena-server-monitor/internal/config"
)

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

// TestLoadDefaults verifies that Load applies sensible defaults when only
// the required variables are set.
func TestLoadDefaults(t *testing.T) {
	setEnv(t, "SMTP_USER", "test@example.com")
	setEnv(t, "SMTP_PASS", "secret")
	setEnv(t, "ALERT_TO", "admin@example.com")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SMTPHost != "smtp.gmail.com" {
		t.Errorf("SMTPHost = %q, want smtp.gmail.com", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 587 {
		t.Errorf("SMTPPort = %d, want 587", cfg.SMTPPort)
	}
	if cfg.CPUThreshold != 95 {
		t.Errorf("CPUThreshold = %.1f, want 95", cfg.CPUThreshold)
	}
	if cfg.RAMThreshold != 95 {
		t.Errorf("RAMThreshold = %.1f, want 95", cfg.RAMThreshold)
	}
	if cfg.RootDiskThreshold != 90 {
		t.Errorf("RootDiskThreshold = %.1f, want 90", cfg.RootDiskThreshold)
	}
	if cfg.CheckInterval != time.Minute {
		t.Errorf("CheckInterval = %v, want 1m", cfg.CheckInterval)
	}
	if cfg.DiskInterval != 15*time.Minute {
		t.Errorf("DiskInterval = %v, want 15m", cfg.DiskInterval)
	}
	if cfg.AlertCooldown != 3*time.Hour {
		t.Errorf("AlertCooldown = %v, want 3h", cfg.AlertCooldown)
	}
	if cfg.HeartbeatHour != 8 {
		t.Errorf("HeartbeatHour = %d, want 8", cfg.HeartbeatHour)
	}
	if cfg.CPUConsecutiveChecks != 3 {
		t.Errorf("CPUConsecutiveChecks = %d, want 3", cfg.CPUConsecutiveChecks)
	}
	if cfg.ServerName != "madeena-server" {
		t.Errorf("ServerName = %q, want madeena-server", cfg.ServerName)
	}
	if len(cfg.AlertTo) != 1 || cfg.AlertTo[0] != "admin@example.com" {
		t.Errorf("AlertTo = %v, want [admin@example.com]", cfg.AlertTo)
	}
}

// TestLoadOverrides verifies that environment variables override defaults.
func TestLoadOverrides(t *testing.T) {
	setEnv(t, "SMTP_HOST", "mail.example.com")
	setEnv(t, "SMTP_PORT", "465")
	setEnv(t, "SMTP_USER", "user@example.com")
	setEnv(t, "SMTP_PASS", "pass123")
	setEnv(t, "ALERT_FROM", "from@example.com")
	setEnv(t, "ALERT_TO", "a@x.com, b@x.com")
	setEnv(t, "CPU_THRESHOLD", "80")
	setEnv(t, "RAM_THRESHOLD", "85")
	setEnv(t, "ROOT_DISK_THRESHOLD", "75")
	setEnv(t, "CHECK_INTERVAL", "30s")
	setEnv(t, "DISK_INTERVAL", "30m")
	setEnv(t, "ALERT_COOLDOWN", "1h")
	setEnv(t, "HEARTBEAT_HOUR", "6")
	setEnv(t, "CPU_CONSECUTIVE_CHECKS", "5")
	setEnv(t, "DATA_PARTITIONS", "/mnt/data, /mnt/backup")
	setEnv(t, "SERVER_NAME", "my-server")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.SMTPHost != "mail.example.com" {
		t.Errorf("SMTPHost = %q", cfg.SMTPHost)
	}
	if cfg.SMTPPort != 465 {
		t.Errorf("SMTPPort = %d", cfg.SMTPPort)
	}
	if cfg.CPUThreshold != 80 {
		t.Errorf("CPUThreshold = %.1f", cfg.CPUThreshold)
	}
	if cfg.RAMThreshold != 85 {
		t.Errorf("RAMThreshold = %.1f", cfg.RAMThreshold)
	}
	if cfg.CheckInterval != 30*time.Second {
		t.Errorf("CheckInterval = %v", cfg.CheckInterval)
	}
	if cfg.DiskInterval != 30*time.Minute {
		t.Errorf("DiskInterval = %v", cfg.DiskInterval)
	}
	if cfg.AlertCooldown != time.Hour {
		t.Errorf("AlertCooldown = %v", cfg.AlertCooldown)
	}
	if cfg.HeartbeatHour != 6 {
		t.Errorf("HeartbeatHour = %d", cfg.HeartbeatHour)
	}
	if cfg.CPUConsecutiveChecks != 5 {
		t.Errorf("CPUConsecutiveChecks = %d", cfg.CPUConsecutiveChecks)
	}
	if len(cfg.AlertTo) != 2 {
		t.Errorf("AlertTo = %v, want 2 entries", cfg.AlertTo)
	}
	if len(cfg.DataPartitions) != 2 {
		t.Errorf("DataPartitions = %v, want 2 entries", cfg.DataPartitions)
	}
	if cfg.ServerName != "my-server" {
		t.Errorf("ServerName = %q", cfg.ServerName)
	}
}

// TestLoadMissingRequired verifies that Load fails gracefully when ALERT_TO
// is empty.
func TestLoadMissingRequired(t *testing.T) {
	// Clear all relevant env vars
	for _, k := range []string{"SMTP_USER", "SMTP_PASS", "ALERT_TO"} {
		t.Setenv(k, "")
		os.Unsetenv(k) //nolint:errcheck
	}
	setEnv(t, "SMTP_USER", "u@example.com")
	setEnv(t, "SMTP_PASS", "pass")
	// ALERT_TO is empty → should fail

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should have returned an error for empty ALERT_TO")
	}
}

// TestLoadInvalidHeartbeatHour verifies that an out-of-range HEARTBEAT_HOUR
// is rejected.
func TestLoadInvalidHeartbeatHour(t *testing.T) {
	setEnv(t, "SMTP_USER", "u@example.com")
	setEnv(t, "SMTP_PASS", "pass")
	setEnv(t, "ALERT_TO", "a@example.com")
	setEnv(t, "HEARTBEAT_HOUR", "25")

	_, err := config.Load()
	if err == nil {
		t.Error("Load() should have returned an error for HEARTBEAT_HOUR=25")
	}
}
