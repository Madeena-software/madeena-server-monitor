// Package notifier provides email notification capabilities via SMTP.
package notifier

import (
	"crypto/tls"
	"fmt"
	"strings"

	"github.com/Madeena-software/madeena-server-monitor/internal/checker"
	"github.com/Madeena-software/madeena-server-monitor/internal/config"
	gomail "gopkg.in/gomail.v2"
)

// Emailer sends alert and summary emails via SMTP.
type Emailer struct {
	cfg *config.Config
}

// NewEmailer creates a new Emailer with the given configuration.
func NewEmailer(cfg *config.Config) *Emailer {
	return &Emailer{cfg: cfg}
}

// SendAlert sends a critical alert email with the given subject and body.
func (e *Emailer) SendAlert(subject, body string) error {
	return e.send(subject, body)
}

// SendHeartbeat sends the daily heartbeat summary email.
func (e *Emailer) SendHeartbeat(stats *checker.SystemStats) error {
	subject := fmt.Sprintf("[%s] Daily Health Summary – %s",
		e.cfg.ServerName, stats.CollectedAt.Format("2006-01-02 15:04"))
	body := buildSummaryBody(e.cfg.ServerName, stats)
	return e.send(subject, body)
}

// send is the internal helper that dials the SMTP server and sends the email.
func (e *Emailer) send(subject, body string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", e.cfg.AlertFrom)
	m.SetHeader("To", e.cfg.AlertTo...)
	m.SetHeader("Subject", subject)
	m.SetBody("text/plain", body)

	d := gomail.NewDialer(e.cfg.SMTPHost, e.cfg.SMTPPort, e.cfg.SMTPUser, e.cfg.SMTPPass)
	// Use TLS for port 465, STARTTLS for 587 (gomail handles this automatically
	// for port 465 via DialAndSend). For port 587 we set TLSConfig explicitly.
	if e.cfg.SMTPPort == 465 {
		d.SSL = true
	} else {
		d.TLSConfig = &tls.Config{
			ServerName: e.cfg.SMTPHost,
			MinVersion: tls.VersionTLS12,
		}
	}

	return d.DialAndSend(m)
}

// buildSummaryBody formats a plain-text daily heartbeat email body.
func buildSummaryBody(serverName string, s *checker.SystemStats) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== Daily Health Report: %s ===\n", serverName))
	sb.WriteString(fmt.Sprintf("Collected at : %s\n\n", s.CollectedAt.Format("2006-01-02 15:04:05")))

	sb.WriteString("--- System ---\n")
	sb.WriteString(fmt.Sprintf("Uptime       : %s\n\n", checker.FormatUptime(s.UptimeDuration)))

	sb.WriteString("--- CPU ---\n")
	sb.WriteString(fmt.Sprintf("Usage        : %.1f%%\n", s.CPUUsagePercent))
	sb.WriteString(fmt.Sprintf("Load Avg     : %.2f / %.2f / %.2f (1m/5m/15m)\n", s.LoadAvg1, s.LoadAvg5, s.LoadAvg15))
	if s.CPUTempCelsius > 0 {
		sb.WriteString(fmt.Sprintf("Temperature  : %.1f°C\n", s.CPUTempCelsius))
	} else {
		sb.WriteString("Temperature  : N/A\n")
	}
	sb.WriteString("\n")

	sb.WriteString("--- Memory ---\n")
	sb.WriteString(fmt.Sprintf("Total        : %d MB\n", s.MemTotalMB))
	sb.WriteString(fmt.Sprintf("Used         : %d MB (%.1f%%)\n", s.MemUsedMB, s.MemPercent))
	sb.WriteString(fmt.Sprintf("Free         : %d MB\n\n", s.MemFreeMB))

	sb.WriteString("--- Disk (Root /) ---\n")
	sb.WriteString(fmt.Sprintf("Total        : %.1f GB\n", s.RootDiskTotalGB))
	sb.WriteString(fmt.Sprintf("Used         : %.1f GB (%.1f%%)\n", s.RootDiskUsedGB, s.RootDiskPercent))
	sb.WriteString(fmt.Sprintf("Free         : %.1f GB\n\n", s.RootDiskFreeGB))

	if len(s.DataDisks) > 0 {
		sb.WriteString("--- Data Partitions ---\n")
		for mount, du := range s.DataDisks {
			sb.WriteString(fmt.Sprintf("%-14s : %.1f GB used / %.1f GB total (%.1f%%)\n",
				mount, du.UsedGB, du.TotalGB, du.UsedPercent))
		}
		sb.WriteString("\n")
	}

	if len(s.SmartHealth) > 0 {
		sb.WriteString("--- S.M.A.R.T. Health ---\n")
		for dev, status := range s.SmartHealth {
			sb.WriteString(fmt.Sprintf("%-10s : %s\n", dev, status))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("--- Network ---\n")
	sb.WriteString(fmt.Sprintf("Rx Rate      : %.1f KB/s\n", s.NetRxBytesPerSec/1024))
	sb.WriteString(fmt.Sprintf("Tx Rate      : %.1f KB/s\n\n", s.NetTxBytesPerSec/1024))

	sb.WriteString("---\nThis email was sent automatically by madeena-server-monitor.\n")
	return sb.String()
}
