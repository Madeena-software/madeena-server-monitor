// Package checker gathers system health metrics using gopsutil and os/exec.
package checker

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// SystemStats holds a complete snapshot of all monitored metrics.
type SystemStats struct {
	// CPU
	CPUUsagePercent float64
	LoadAvg1        float64
	LoadAvg5        float64
	LoadAvg15       float64
	CPUTempCelsius  float64 // 0 if unavailable

	// Memory
	MemTotalMB  uint64
	MemUsedMB   uint64
	MemFreeMB   uint64
	MemPercent  float64

	// Disk – root partition
	RootDiskTotalGB float64
	RootDiskUsedGB  float64
	RootDiskFreeGB  float64
	RootDiskPercent float64

	// Disk – extra data partitions (mount point → usage)
	DataDisks map[string]DiskUsage

	// Disk health (S.M.A.R.T.)
	SmartHealth map[string]string // device → "PASSED" / "FAILED" / "UNKNOWN"

	// Network (bytes since last check)
	NetRxBytesPerSec float64
	NetTxBytesPerSec float64

	// System uptime
	UptimeDuration time.Duration

	// Timestamp of collection
	CollectedAt time.Time
}

// DiskUsage holds usage stats for a single partition.
type DiskUsage struct {
	TotalGB   float64
	UsedGB    float64
	FreeGB    float64
	UsedPercent float64
}

// Checker provides methods to collect system metrics.
type Checker struct {
	// dataPartitions is the list of extra mount points to monitor beyond root.
	dataPartitions []string

	// smartDevices is the list of block devices to query via smartctl.
	smartDevices []string

	// netCounters tracks the previous network counters for rate calculation.
	prevNetCounters []net.IOCountersStat
	prevNetTime     time.Time
}

// New creates a new Checker instance.
// dataPartitions is a list of mount points (e.g., ["/mnt/data"]).
// smartDevices is a list of raw block device paths (e.g., ["/dev/sda", "/dev/sdb"]).
func New(dataPartitions, smartDevices []string) *Checker {
	return &Checker{
		dataPartitions: dataPartitions,
		smartDevices:   smartDevices,
	}
}

// Collect gathers all system metrics and returns a populated SystemStats.
func (c *Checker) Collect() (*SystemStats, error) {
	stats := &SystemStats{
		CollectedAt: time.Now(),
		DataDisks:   make(map[string]DiskUsage),
		SmartHealth: make(map[string]string),
	}

	// --- CPU Usage ---
	percents, err := cpu.Percent(500*time.Millisecond, false)
	if err == nil && len(percents) > 0 {
		stats.CPUUsagePercent = percents[0]
	}

	// --- Load Average ---
	avg, err := load.Avg()
	if err == nil {
		stats.LoadAvg1 = avg.Load1
		stats.LoadAvg5 = avg.Load5
		stats.LoadAvg15 = avg.Load15
	}

	// --- CPU Temperature (best-effort via host sensors) ---
	temps, err := host.SensorsTemperatures()
	if err == nil {
		for _, t := range temps {
			// Look for a CPU core temperature sensor
			if strings.Contains(strings.ToLower(t.SensorKey), "cpu") ||
				strings.Contains(strings.ToLower(t.SensorKey), "core") ||
				strings.Contains(strings.ToLower(t.SensorKey), "coretemp") {
				if t.Temperature > stats.CPUTempCelsius {
					stats.CPUTempCelsius = t.Temperature
				}
			}
		}
	}

	// --- Memory ---
	vmStat, err := mem.VirtualMemory()
	if err == nil {
		stats.MemTotalMB = vmStat.Total / 1024 / 1024
		stats.MemUsedMB = vmStat.Used / 1024 / 1024
		stats.MemFreeMB = vmStat.Available / 1024 / 1024
		stats.MemPercent = vmStat.UsedPercent
	}

	// --- Root Disk (/) ---
	rootDisk, err := disk.Usage("/")
	if err == nil {
		stats.RootDiskTotalGB = bytesToGB(rootDisk.Total)
		stats.RootDiskUsedGB = bytesToGB(rootDisk.Used)
		stats.RootDiskFreeGB = bytesToGB(rootDisk.Free)
		stats.RootDiskPercent = rootDisk.UsedPercent
	}

	// --- Data Partitions ---
	for _, mount := range c.dataPartitions {
		du, err := disk.Usage(mount)
		if err != nil {
			// Record the error in the map so callers know the check failed
			stats.DataDisks[mount] = DiskUsage{}
			continue
		}
		stats.DataDisks[mount] = DiskUsage{
			TotalGB:     bytesToGB(du.Total),
			UsedGB:      bytesToGB(du.Used),
			FreeGB:      bytesToGB(du.Free),
			UsedPercent: du.UsedPercent,
		}
	}

	// --- S.M.A.R.T. Health ---
	for _, dev := range c.smartDevices {
		stats.SmartHealth[dev] = querySmartHealth(dev)
	}

	// --- Network Rate ---
	c.collectNetworkRate(stats)

	// --- Uptime ---
	upSecs, err := host.Uptime()
	if err == nil {
		stats.UptimeDuration = time.Duration(upSecs) * time.Second
	}

	return stats, nil
}

// collectNetworkRate computes bytes/sec for all interfaces combined since the
// last call. On the very first call it stores counters and returns zero rates.
func (c *Checker) collectNetworkRate(stats *SystemStats) {
	counters, err := net.IOCounters(false) // false = aggregate all interfaces
	if err != nil || len(counters) == 0 {
		return
	}

	now := time.Now()
	if len(c.prevNetCounters) > 0 {
		elapsed := now.Sub(c.prevNetTime).Seconds()
		if elapsed > 0 {
			rxDiff := float64(counters[0].BytesRecv - c.prevNetCounters[0].BytesRecv)
			txDiff := float64(counters[0].BytesSent - c.prevNetCounters[0].BytesSent)
			stats.NetRxBytesPerSec = rxDiff / elapsed
			stats.NetTxBytesPerSec = txDiff / elapsed
		}
	}

	c.prevNetCounters = counters
	c.prevNetTime = now
}

// querySmartHealth runs `smartctl -H <device>` and returns the health status
// string ("PASSED", "FAILED", or "UNKNOWN"). This is a best-effort check; if
// smartctl is not installed or the device is not supported it returns "UNKNOWN".
func querySmartHealth(device string) string {
	// #nosec G204 – device path comes from configuration, not user input.
	cmd := exec.Command("smartctl", "-H", device)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		// Exit code 1 from smartctl is used for SMART test failures; still parse output.
		if out.Len() == 0 {
			return "UNKNOWN"
		}
	}

	output := out.String()
	if strings.Contains(output, "PASSED") {
		return "PASSED"
	}
	if strings.Contains(output, "FAILED") {
		return "FAILED"
	}
	return "UNKNOWN"
}

// FormatUptime formats a duration into a human-readable string.
func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
}

// bytesToGB converts bytes to gigabytes.
func bytesToGB(b uint64) float64 {
	return float64(b) / 1024 / 1024 / 1024
}
