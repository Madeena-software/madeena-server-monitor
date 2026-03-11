// Package notifier implements alert management including debounce/cooldown logic.
package notifier

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/Madeena-software/madeena-server-monitor/internal/checker"
	"github.com/Madeena-software/madeena-server-monitor/internal/config"
)

// AlertManager tracks alert state and enforces cooldown windows so the same
// alert is not sent more than once per cooldown period.
type AlertManager struct {
	mu      sync.Mutex
	emailer *Emailer
	cfg     *config.Config

	// lastAlertTime maps an alert key (e.g., "cpu", "ram", "disk:/") to the
	// time the last alert was sent for that key.
	lastAlertTime map[string]time.Time

	// consecutiveCPUHighChecks counts how many consecutive checks had CPU above
	// the threshold.
	consecutiveCPUHighChecks int
}

// NewAlertManager creates a new AlertManager.
func NewAlertManager(emailer *Emailer, cfg *config.Config) *AlertManager {
	return &AlertManager{
		emailer:       emailer,
		cfg:           cfg,
		lastAlertTime: make(map[string]time.Time),
	}
}

// Evaluate inspects the provided stats and sends alert emails for any
// threshold violations, respecting the configured cooldown period.
// It also performs a cumulative OR-logic check: if ANY critical metric
// exceeds its threshold (Temp, CPU, RAM, or Disk) a combined alert is sent.
func (am *AlertManager) Evaluate(stats *checker.SystemStats) {
	// --- CPU ---
	if stats.CPUUsagePercent >= am.cfg.CPUThreshold {
		am.consecutiveCPUHighChecks++
	} else {
		am.consecutiveCPUHighChecks = 0
	}
	if am.consecutiveCPUHighChecks >= am.cfg.CPUConsecutiveChecks {
		subject := fmt.Sprintf("[ALERT][%s] CPU Usage Critical: %.1f%%",
			am.cfg.ServerName, stats.CPUUsagePercent)
		body := fmt.Sprintf(
			"CPU usage has been above %.1f%% for %d consecutive checks.\n"+
				"Current usage: %.1f%%\n"+
				"Load Average: %.2f / %.2f / %.2f (1m/5m/15m)\n",
			am.cfg.CPUThreshold,
			am.consecutiveCPUHighChecks,
			stats.CPUUsagePercent,
			stats.LoadAvg1, stats.LoadAvg5, stats.LoadAvg15,
		)
		if am.shouldAlert("cpu") {
			am.sendAlert("cpu", subject, body)
		}
	}

	// --- RAM ---
	if stats.MemPercent >= am.cfg.RAMThreshold {
		subject := fmt.Sprintf("[ALERT][%s] Memory Usage Critical: %.1f%%",
			am.cfg.ServerName, stats.MemPercent)
		body := fmt.Sprintf(
			"Memory usage is %.1f%% (Used: %d MB / Total: %d MB, Free: %d MB).\n",
			stats.MemPercent, stats.MemUsedMB, stats.MemTotalMB, stats.MemFreeMB,
		)
		if am.shouldAlert("ram") {
			am.sendAlert("ram", subject, body)
		}
	}

	// --- Root Disk ---
	if stats.RootDiskPercent >= am.cfg.RootDiskThreshold {
		key := "disk:/"
		subject := fmt.Sprintf("[ALERT][%s] Root Disk Space Critical: %.1f%%",
			am.cfg.ServerName, stats.RootDiskPercent)
		body := fmt.Sprintf(
			"Root disk (/) usage is %.1f%% (Used: %.1f GB / Total: %.1f GB, Free: %.1f GB).\n",
			stats.RootDiskPercent, stats.RootDiskUsedGB, stats.RootDiskTotalGB, stats.RootDiskFreeGB,
		)
		if am.shouldAlert(key) {
			am.sendAlert(key, subject, body)
		}
	}

	// --- Data Partitions ---
	for mount, du := range stats.DataDisks {
		if du.UsedPercent >= am.cfg.RootDiskThreshold {
			key := "disk:" + mount
			subject := fmt.Sprintf("[ALERT][%s] Disk Space Critical on %s: %.1f%%",
				am.cfg.ServerName, mount, du.UsedPercent)
			body := fmt.Sprintf(
				"Disk usage on %s is %.1f%% (Used: %.1f GB / Total: %.1f GB, Free: %.1f GB).\n",
				mount, du.UsedPercent, du.UsedGB, du.TotalGB, du.FreeGB,
			)
			if am.shouldAlert(key) {
				am.sendAlert(key, subject, body)
			}
		}
	}

	// --- S.M.A.R.T. Health ---
	for dev, status := range stats.SmartHealth {
		if status == "FAILED" {
			key := "smart:" + dev
			subject := fmt.Sprintf("[ALERT][%s] S.M.A.R.T. FAILURE on %s",
				am.cfg.ServerName, dev)
			body := fmt.Sprintf(
				"S.M.A.R.T. health check FAILED for device %s.\n"+
					"Please inspect the drive immediately to avoid data loss.\n",
				dev,
			)
			if am.shouldAlert(key) {
				am.sendAlert(key, subject, body)
			}
		}
	}

	// --- Cumulative OR-logic anomaly alert ---
	// Fires when ANY of the critical thresholds is breached, regardless of
	// individual per-metric cooldowns, so operators get a holistic summary.
	am.evaluateCumulative(stats)
}

// shouldAlert returns true if enough time has elapsed since the last alert for
// the given key. It is NOT safe to call while holding am.mu.
func (am *AlertManager) shouldAlert(key string) bool {
	am.mu.Lock()
	defer am.mu.Unlock()
	last, ok := am.lastAlertTime[key]
	if !ok {
		return true
	}
	return time.Since(last) >= am.cfg.AlertCooldown
}

// sendAlert sends an alert email and records the time it was sent.
func (am *AlertManager) sendAlert(key, subject, body string) {
	if err := am.emailer.SendAlert(subject, body); err != nil {
		log.Printf("ERROR: failed to send alert email (%s): %v", key, err)
		return
	}
	am.mu.Lock()
	am.lastAlertTime[key] = time.Now()
	am.mu.Unlock()
	log.Printf("INFO: alert sent for key=%s subject=%q", key, subject)
}

// evaluateCumulative performs an OR-logic check across all critical metrics.
// If any single metric (temperature, CPU, RAM, or disk) breaches its threshold,
// a combined summary alert email is sent (subject to its own cooldown key).
func (am *AlertManager) evaluateCumulative(stats *checker.SystemStats) {
	var triggers []string

	if am.cfg.TempThreshold > 0 && stats.CPUTempCelsius >= am.cfg.TempThreshold {
		triggers = append(triggers, fmt.Sprintf("Suhu CPU=%.1f°C (threshold %.1f°C)",
			stats.CPUTempCelsius, am.cfg.TempThreshold))
	}
	if stats.CPUUsagePercent >= am.cfg.CPUThreshold {
		triggers = append(triggers, fmt.Sprintf("CPU Usage=%.1f%% (threshold %.1f%%)",
			stats.CPUUsagePercent, am.cfg.CPUThreshold))
	}
	if stats.MemPercent >= am.cfg.RAMThreshold {
		triggers = append(triggers, fmt.Sprintf("RAM Usage=%.1f%% (threshold %.1f%%)",
			stats.MemPercent, am.cfg.RAMThreshold))
	}
	if stats.RootDiskPercent >= am.cfg.RootDiskThreshold {
		triggers = append(triggers, fmt.Sprintf("Disk /=%.1f%% (threshold %.1f%%)",
			stats.RootDiskPercent, am.cfg.RootDiskThreshold))
	}
	for mount, du := range stats.DataDisks {
		if du.UsedPercent >= am.cfg.RootDiskThreshold {
			triggers = append(triggers, fmt.Sprintf("Disk %s=%.1f%% (threshold %.1f%%)",
				mount, du.UsedPercent, am.cfg.RootDiskThreshold))
		}
	}

	if len(triggers) == 0 {
		return
	}

	if !am.shouldAlert("cumulative") {
		return
	}

	subject := fmt.Sprintf("[PERINGATAN KRITIS][%s] Anomali Sistem Terdeteksi", am.cfg.ServerName)
	body := fmt.Sprintf(
		"Peringatan Kritis Server – satu atau lebih metrik melebihi batas kritis.\n\n"+
			"Sensor yang memicu peringatan:\n",
	)
	for _, t := range triggers {
		body += "  • " + t + "\n"
	}
	body += fmt.Sprintf(
		"\nSnapshot saat ini:\n"+
			"  CPU  : %.1f%% | Load: %.2f / %.2f / %.2f\n"+
			"  RAM  : %.1f%% (%d MB used / %d MB total)\n"+
			"  Disk : %.1f%% (/ partition)\n"+
			"  Temp : %.1f°C\n",
		stats.CPUUsagePercent, stats.LoadAvg1, stats.LoadAvg5, stats.LoadAvg15,
		stats.MemPercent, stats.MemUsedMB, stats.MemTotalMB,
		stats.RootDiskPercent,
		stats.CPUTempCelsius,
	)
	am.sendAlert("cumulative", subject, body)
}

// SendHeartbeat sends the daily summary heartbeat email.
func (am *AlertManager) SendHeartbeat(stats *checker.SystemStats) {
	if err := am.emailer.SendHeartbeat(stats); err != nil {
		log.Printf("ERROR: failed to send heartbeat email: %v", err)
		return
	}
	log.Println("INFO: daily heartbeat email sent")
}
