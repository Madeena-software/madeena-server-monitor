// Package config handles loading and validating application configuration
// from environment variables (optionally loaded from a .env file).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration values for the monitor.
type Config struct {
	// SMTP settings
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string

	// Email addresses
	AlertFrom string
	AlertTo   []string

	// Thresholds (percentages 0–100)
	CPUThreshold      float64
	RAMThreshold      float64
	RootDiskThreshold float64

	// Number of consecutive high-CPU checks before alerting
	CPUConsecutiveChecks int

	// Temperature threshold in Celsius for the cumulative OR-logic alert
	TempThreshold float64

	// Alerting intervals
	CheckInterval    time.Duration // how often to check CPU/RAM
	DiskInterval     time.Duration // how often to check disk
	AlertCooldown    time.Duration // minimum time between repeated alerts
	HeartbeatHour    int           // hour of day (0–23) for daily heartbeat email

	// Extra disk mount points to monitor beyond root
	DataPartitions []string

	// Server name for email identification
	ServerName string

	// TCP port for the live web dashboard
	WebPort int
}

// Load reads configuration from environment variables.
// It first attempts to load a .env file from the current directory (non-fatal if absent).
func Load() (*Config, error) {
	// Load .env file if present (ignore error when missing)
	_ = godotenv.Load()

	cfg := &Config{}

	// SMTP
	cfg.SMTPHost = getEnv("SMTP_HOST", "smtp.gmail.com")
	port, err := strconv.Atoi(getEnv("SMTP_PORT", "587"))
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP_PORT: %w", err)
	}
	cfg.SMTPPort = port
	cfg.SMTPUser = mustEnv("SMTP_USER")
	cfg.SMTPPass = mustEnv("SMTP_PASS")

	// Email addresses
	cfg.AlertFrom = getEnv("ALERT_FROM", cfg.SMTPUser)
	toStr := mustEnv("ALERT_TO")
	for _, addr := range strings.Split(toStr, ",") {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			cfg.AlertTo = append(cfg.AlertTo, addr)
		}
	}
	if len(cfg.AlertTo) == 0 {
		return nil, fmt.Errorf("ALERT_TO must contain at least one email address")
	}

	// Thresholds
	cfg.CPUThreshold = parseFloat(getEnv("CPU_THRESHOLD", "95"))
	cfg.RAMThreshold = parseFloat(getEnv("RAM_THRESHOLD", "95"))
	cfg.RootDiskThreshold = parseFloat(getEnv("ROOT_DISK_THRESHOLD", "90"))

	// Consecutive CPU checks
	cfg.CPUConsecutiveChecks = parseInt(getEnv("CPU_CONSECUTIVE_CHECKS", "3"))

	// Temperature threshold for cumulative OR-logic alert
	cfg.TempThreshold = parseFloat(getEnv("TEMP_THRESHOLD", "85"))

	// Intervals
	cfg.CheckInterval = parseDuration(getEnv("CHECK_INTERVAL", "1m"))
	cfg.DiskInterval = parseDuration(getEnv("DISK_INTERVAL", "15m"))
	cfg.AlertCooldown = parseDuration(getEnv("ALERT_COOLDOWN", "3h"))

	// Daily heartbeat hour
	cfg.HeartbeatHour = parseInt(getEnv("HEARTBEAT_HOUR", "8"))
	if cfg.HeartbeatHour < 0 || cfg.HeartbeatHour > 23 {
		return nil, fmt.Errorf("HEARTBEAT_HOUR must be between 0 and 23")
	}

	// Data partitions (comma-separated mount points)
	partStr := getEnv("DATA_PARTITIONS", "")
	if partStr != "" {
		for _, p := range strings.Split(partStr, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.DataPartitions = append(cfg.DataPartitions, p)
			}
		}
	}

	cfg.ServerName = getEnv("SERVER_NAME", "madeena-server")

	// Web dashboard port
	webPort, err := strconv.Atoi(getEnv("WEB_PORT", "8080"))
	if err != nil {
		return nil, fmt.Errorf("invalid WEB_PORT: %w", err)
	}
	cfg.WebPort = webPort

	return cfg, nil
}

// getEnv returns the value of the environment variable or a default.
func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// mustEnv returns the value of a required environment variable or an error.
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		// Return empty string; caller checks for valid content
		return ""
	}
	return v
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseInt(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	return d
}
