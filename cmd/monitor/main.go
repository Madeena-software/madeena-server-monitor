// Command monitor is the main entry point for the madeena-server-monitor daemon.
// It periodically collects system metrics, evaluates alert conditions, and sends
// email notifications when thresholds are exceeded or the daily heartbeat fires.
// It also serves a live web dashboard on the configured WEB_PORT.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Madeena-software/madeena-server-monitor/internal/checker"
	"github.com/Madeena-software/madeena-server-monitor/internal/config"
	"github.com/Madeena-software/madeena-server-monitor/internal/dashboard"
	"github.com/Madeena-software/madeena-server-monitor/internal/notifier"
)

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Println("INFO: madeena-server-monitor starting …")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("FATAL: failed to load configuration: %v", err)
	}

	// Validate required settings
	if cfg.SMTPUser == "" || cfg.SMTPPass == "" {
		log.Fatal("FATAL: SMTP_USER and SMTP_PASS must be set")
	}

	log.Printf("INFO: server=%s check_interval=%s disk_interval=%s cooldown=%s heartbeat_hour=%d web_port=%d",
		cfg.ServerName, cfg.CheckInterval, cfg.DiskInterval, cfg.AlertCooldown, cfg.HeartbeatHour, cfg.WebPort)

	// S.M.A.R.T. devices to monitor (configurable via DATA_PARTITIONS; default to common drives)
	smartDevices := []string{"/dev/sda", "/dev/sdb"}

	// Initialise components
	chk := checker.New(cfg.DataPartitions, smartDevices)
	emailer := notifier.NewEmailer(cfg)
	alertMgr := notifier.NewAlertManager(emailer, cfg)
	store := checker.NewMetricsStore()

	// Start live web dashboard in a background goroutine
	dash := dashboard.New(store, cfg.CheckInterval)
	go func() {
		addr := fmt.Sprintf(":%d", cfg.WebPort)
		if err := dash.ListenAndServe(addr); err != nil {
			log.Printf("ERROR: dashboard server stopped: %v", err)
		}
	}()

	// Tickers
	checkTicker := time.NewTicker(cfg.CheckInterval)
	diskTicker := time.NewTicker(cfg.DiskInterval)
	defer checkTicker.Stop()
	defer diskTicker.Stop()

	// Track the last day for which the heartbeat was sent so we only send once
	// per calendar day.
	lastHeartbeatDay := -1

	// Run a collection immediately on startup so the first heartbeat has data.
	runCheck(chk, alertMgr, store)

	// Signal handling for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	log.Println("INFO: monitor running – press Ctrl+C to stop")

	for {
		select {
		case <-checkTicker.C:
			runCheck(chk, alertMgr, store)
			maybeHeartbeat(chk, alertMgr, cfg.HeartbeatHour, &lastHeartbeatDay)

		case <-diskTicker.C:
			// Disk checks happen less frequently; we reuse the same collect path
			// but the alerting logic inside Evaluate handles root/data disks.
			log.Println("INFO: running scheduled disk check")
			runCheck(chk, alertMgr, store)

		case sig := <-sigs:
			log.Printf("INFO: received signal %s – shutting down", sig)
			return
		}
	}
}

// runCheck collects metrics, updates the MetricsStore, and passes stats to the
// alert manager for evaluation.
func runCheck(chk *checker.Checker, alertMgr *notifier.AlertManager, store *checker.MetricsStore) {
	stats, err := chk.Collect()
	if err != nil {
		log.Printf("ERROR: failed to collect stats: %v", err)
		return
	}
	log.Printf("INFO: collected stats – CPU=%.1f%% RAM=%.1f%% RootDisk=%.1f%% Uptime=%s",
		stats.CPUUsagePercent, stats.MemPercent, stats.RootDiskPercent,
		checker.FormatUptime(stats.UptimeDuration))

	store.Update(stats)
	alertMgr.Evaluate(stats)
}

// maybeHeartbeat sends the daily heartbeat email once per day at the configured hour.
func maybeHeartbeat(chk *checker.Checker, alertMgr *notifier.AlertManager, heartbeatHour int, lastDay *int) {
	now := time.Now()
	if now.Hour() == heartbeatHour && now.YearDay() != *lastDay {
		log.Println("INFO: sending daily heartbeat email")
		stats, err := chk.Collect()
		if err != nil {
			log.Printf("ERROR: failed to collect stats for heartbeat: %v", err)
			return
		}
		alertMgr.SendHeartbeat(stats)
		*lastDay = now.YearDay()
	}
}
