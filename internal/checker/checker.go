// Package checker gathers system health metrics using gopsutil and os/exec.
package checker

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// TemperatureSensor represents a single hardware temperature reading.
type TemperatureSensor struct {
	Key         string
	Temperature float64
	High        float64
	Critical    float64
}

// FanSensor represents a single hardware fan speed reading.
type FanSensor struct {
	Key string
	RPM float64
}

// VoltageSensor represents a single voltage reading.
type VoltageSensor struct {
	Key     string
	Voltage float64
}

// SystemStats holds a complete snapshot of all monitored metrics.
type SystemStats struct {
	// CPU – aggregate
	CPUUsagePercent float64
	LoadAvg1        float64
	LoadAvg5        float64
	LoadAvg15       float64
	CPUTempCelsius  float64 // max CPU temp, 0 if unavailable

	// CPU – per-core detail
	CPUCorePercents []float64 // per-core usage percentages (from cpu.Percent with percpu=true)
	CPUCoreMHz      []float64 // per-core clock speed in MHz (from cpu.Info)

	// All temperature sensors reported by the host
	Temperatures []TemperatureSensor

	// Fan speeds from /sys/class/hwmon
	Fans []FanSensor

	// Voltages from /sys/class/hwmon (in Volts)
	Voltages []VoltageSensor

	// Memory
	MemTotalMB uint64
	MemUsedMB  uint64
	MemFreeMB  uint64
	MemPercent float64

	// Disk – root partition
	RootDiskTotalGB float64
	RootDiskUsedGB  float64
	RootDiskFreeGB  float64
	RootDiskPercent float64

	// Disk – extra data partitions (mount point → usage)
	DataDisks map[string]DiskUsage

	// Disk health (S.M.A.R.T.)
	SmartHealth map[string]string // device → "PASSED" / "FAILED" / "UNKNOWN"

	// Network (bytes/sec since last check)
	NetRxBytesPerSec float64
	NetTxBytesPerSec float64

	// System uptime
	UptimeDuration time.Duration

	// Timestamp of collection
	CollectedAt time.Time
}

// DiskUsage holds usage stats for a single partition.
type DiskUsage struct {
	TotalGB     float64
	UsedGB      float64
	FreeGB      float64
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

	// --- CPU Usage (aggregate) ---
	percents, err := cpu.Percent(500*time.Millisecond, false)
	if err == nil && len(percents) > 0 {
		stats.CPUUsagePercent = percents[0]
	}

	// --- CPU Usage per-core ---
	corePercents, err := cpu.Percent(0, true)
	if err == nil {
		stats.CPUCorePercents = corePercents
	}

	// --- CPU Clock Speeds per-core (from cpu.Info) ---
	infos, err := cpu.Info()
	if err == nil {
		for _, info := range infos {
			stats.CPUCoreMHz = append(stats.CPUCoreMHz, info.Mhz)
		}
	}

	// --- Load Average ---
	avg, err := load.Avg()
	if err == nil {
		stats.LoadAvg1 = avg.Load1
		stats.LoadAvg5 = avg.Load5
		stats.LoadAvg15 = avg.Load15
	}

	// --- Temperature Sensors (all available sensors) ---
	temps, err := host.SensorsTemperatures()
	if err == nil {
		for _, t := range temps {
			stats.Temperatures = append(stats.Temperatures, TemperatureSensor{
				Key:         t.SensorKey,
				Temperature: t.Temperature,
				High:        t.High,
				Critical:    t.Critical,
			})
			// Track max CPU temperature for threshold evaluation
			lk := strings.ToLower(t.SensorKey)
			if strings.Contains(lk, "cpu") || strings.Contains(lk, "core") || strings.Contains(lk, "coretemp") {
				if t.Temperature > stats.CPUTempCelsius {
					stats.CPUTempCelsius = t.Temperature
				}
			}
		}
	}

	// --- Fan Speeds & Voltages from /sys/class/hwmon ---
	stats.Fans, stats.Voltages = readHwmon()

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

// readHwmon reads fan speeds and voltages from /sys/class/hwmon.
// Returns nil slices when the directory is unavailable (non-Linux hosts).
func readHwmon() (fans []FanSensor, voltages []VoltageSensor) {
	hwmonDirs, err := filepath.Glob("/sys/class/hwmon/hwmon*")
	if err != nil || len(hwmonDirs) == 0 {
		return
	}
	for _, dir := range hwmonDirs {
		name := readSysString(filepath.Join(dir, "name"))
		if name == "" {
			name = filepath.Base(dir)
		}

		// Fan speeds: fan*_input (values in RPM)
		fanFiles, _ := filepath.Glob(filepath.Join(dir, "fan*_input"))
		for _, f := range fanFiles {
			rpm, err := readSysFloat(f)
			if err != nil {
				continue
			}
			base := strings.TrimSuffix(f, "_input")
			label := readSysString(base + "_label")
			if label == "" {
				label = name + " " + filepath.Base(base)
			}
			fans = append(fans, FanSensor{Key: label, RPM: rpm})
		}

		// Voltages: in*_input (values in mV; convert to V)
		voltFiles, _ := filepath.Glob(filepath.Join(dir, "in*_input"))
		for _, f := range voltFiles {
			mv, err := readSysFloat(f)
			if err != nil {
				continue
			}
			base := strings.TrimSuffix(f, "_input")
			label := readSysString(base + "_label")
			if label == "" {
				label = name + " " + filepath.Base(base)
			}
			voltages = append(voltages, VoltageSensor{Key: label, Voltage: mv / 1000.0})
		}
	}
	return
}

// readSysFloat reads a sysfs file and parses its content as float64.
func readSysFloat(path string) (float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
}

// readSysString reads a sysfs file and returns its trimmed string content.
func readSysString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
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
