// Package checker gathers system health metrics using gopsutil and os/exec.
package checker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

	// --- Temperature, Fan & Voltage Sensors (via 'sensors -j', fallback to sysfs) ---
	stats.Temperatures, stats.Fans, stats.Voltages = parseSensorsJSON()
	for _, t := range stats.Temperatures {
		lk := strings.ToLower(t.Key)
		if strings.Contains(lk, "cpu") || strings.Contains(lk, "core") || strings.Contains(lk, "package") {
			if t.Temperature > stats.CPUTempCelsius {
				stats.CPUTempCelsius = t.Temperature
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

// parseSensorsJSON runs `sensors -j` and parses all temperature, fan, and voltage
// sensors with human-readable names. Falls back to readHwmon() if sensors is
// unavailable or produces no usable output.
func parseSensorsJSON() (temps []TemperatureSensor, fans []FanSensor, voltages []VoltageSensor) {
	// #nosec G204 – fixed command with no user-controlled arguments.
	cmd := exec.Command("sensors", "-j")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	_ = cmd.Run() // non-zero exit is acceptable; some chips report N/A

	if out.Len() == 0 {
		fans, voltages = readHwmon()
		return
	}

	// sensors -j may inject bare "ERROR:" lines for unreadable sub-features, which
	// breaks JSON. Strip them before parsing.
	var cleaned strings.Builder
	for _, line := range strings.Split(out.String(), "\n") {
		if !strings.Contains(line, "ERROR:") {
			cleaned.WriteString(line)
			cleaned.WriteByte('\n')
		}
	}

	// Outer map: chip-name → raw chip JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned.String()), &raw); err != nil {
		fans, voltages = readHwmon()
		return
	}

	for chipName, chipRaw := range raw {
		var chipData map[string]json.RawMessage
		if err := json.Unmarshal(chipRaw, &chipData); err != nil {
			continue
		}
		prefix := friendlyChipName(chipName)

		for sensorLabel, sensorRaw := range chipData {
			if sensorLabel == "Adapter" {
				continue
			}
			var readings map[string]json.Number
			if err := json.Unmarshal(sensorRaw, &readings); err != nil {
				continue
			}

			var inputVal, highVal, critVal float64
			var sensorType string
			for key, numRaw := range readings {
				val, err := numRaw.Float64()
				if err != nil {
					continue
				}
				k := strings.ToLower(key)
				switch {
				case strings.HasPrefix(k, "temp") && strings.HasSuffix(k, "_input"):
					sensorType = "temp"
					inputVal = val
				case strings.HasPrefix(k, "temp") && strings.HasSuffix(k, "_max"):
					highVal = val
				case strings.HasPrefix(k, "temp") && strings.HasSuffix(k, "_crit"):
					critVal = val
				case strings.HasPrefix(k, "fan") && strings.HasSuffix(k, "_input"):
					sensorType = "fan"
					inputVal = val
				case strings.HasPrefix(k, "in") && strings.HasSuffix(k, "_input"):
					sensorType = "voltage"
					inputVal = val
				}
			}

			fullLabel := prefix + "/" + sensorLabel
			switch sensorType {
			case "temp":
				if inputVal < -50 { // skip bogus readings (e.g., –55 °C = unconnected probe)
					continue
				}
				temps = append(temps, TemperatureSensor{
					Key:         fullLabel,
					Temperature: inputVal,
					High:        highVal,
					Critical:    critVal,
				})
			case "fan":
				fans = append(fans, FanSensor{Key: fullLabel, RPM: inputVal})
			case "voltage":
				voltages = append(voltages, VoltageSensor{Key: fullLabel, Voltage: inputVal})
			}
		}
	}

	// If sensors produced nothing usable, fall back to direct sysfs read.
	if len(temps) == 0 && len(fans) == 0 && len(voltages) == 0 {
		fans, voltages = readHwmon()
	}
	return
}

// friendlyChipName converts a sensors chip identifier to a short human-readable prefix.
func friendlyChipName(chip string) string {
	switch {
	case strings.HasPrefix(chip, "coretemp"):
		return "CPU"
	case strings.HasPrefix(chip, "nouveau"), strings.HasPrefix(chip, "amdgpu"), strings.HasPrefix(chip, "radeon"):
		return "GPU"
	case strings.HasPrefix(chip, "it87"), strings.HasPrefix(chip, "it8792"),
		strings.HasPrefix(chip, "nct"), strings.HasPrefix(chip, "w836"),
		strings.HasPrefix(chip, "k10temp"):
		return "Motherboard"
	case strings.HasPrefix(chip, "iwlwifi"), strings.HasPrefix(chip, "ath"):
		return "WiFi"
	default:
		if idx := strings.Index(chip, "-"); idx > 0 {
			return chip[:idx]
		}
		return chip
	}
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
	output := runSmartctl(device)
	if output == "" {
		return "UNKNOWN"
	}
	if strings.Contains(output, "PASSED") {
		return "PASSED"
	}
	if strings.Contains(output, "FAILED") {
		return "FAILED"
	}
	return "UNKNOWN"
}

// runSmartctl executes smartctl using common absolute paths first, then PATH.
func runSmartctl(device string) string {
	cmds := [][]string{{"/usr/sbin/smartctl", "-H", device}, {"smartctl", "-H", device}}
	for _, args := range cmds {
		// #nosec G204 – device path comes from static config, not user input.
		cmd := exec.Command(args[0], args[1:]...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			// smartctl may return non-zero while still printing useful health text.
			if out.Len() > 0 {
				return out.String()
			}
			continue
		}
		if out.Len() > 0 {
			return out.String()
		}
	}
	return ""
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
