// Package checker – MetricsStore tracks live, min, max, and history for all metrics.
package checker

import (
	"fmt"
	"sync"
	"time"
)

// HistorySize is the number of data points kept per metric series.
const HistorySize = 60

// MetricValue tracks the current, minimum, and maximum of a single metric.
type MetricValue struct {
	Value  float64
	Min    float64
	Max    float64
	inited bool
}

// Update incorporates a new sample into Value/Min/Max.
func (mv *MetricValue) Update(v float64) {
	mv.Value = v
	if !mv.inited {
		mv.Min = v
		mv.Max = v
		mv.inited = true
		return
	}
	if v < mv.Min {
		mv.Min = v
	}
	if v > mv.Max {
		mv.Max = v
	}
}

// MetricSeries extends MetricValue with a rolling history slice.
type MetricSeries struct {
	MetricValue
	History []float64
}

// Update incorporates a new sample and appends it to the rolling history.
func (ms *MetricSeries) Update(v float64) {
	ms.MetricValue.Update(v)
	ms.History = append(ms.History, v)
	if len(ms.History) > HistorySize {
		ms.History = ms.History[len(ms.History)-HistorySize:]
	}
}

// NamedMetric is a named MetricValue for dynamically-discovered sensors.
type NamedMetric struct {
	Name string
	MetricValue
}

// MetricsStore holds the global, thread-safe aggregation of all metric series.
// It is populated by calling Update after each Checker.Collect() call.
type MetricsStore struct {
	mu sync.RWMutex

	// Core time-series metrics (with rolling history for charting)
	CPUTotal MetricSeries
	RAM      MetricSeries
	RootDisk MetricSeries
	NetRx    MetricSeries
	NetTx    MetricSeries
	TempMain MetricSeries // highest CPU/core temperature

	// History timestamps (used as chart x-axis labels)
	Timestamps []time.Time

	// Per-core CPU utilization (indexed by logical core number)
	CPUCores []MetricValue

	// Dynamic sensors (ordered; index matches Stats.Temperatures etc.)
	TempSensors []NamedMetric
	FanSensors  []NamedMetric
	VoltSensors []NamedMetric

	// Latest raw snapshot (for SSE detailed payloads)
	Latest *SystemStats
}

// Update ingests a new SystemStats snapshot and updates all tracked metrics.
// Safe for concurrent use.
func (s *MetricsStore) Update(stats *SystemStats) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Latest = stats

	// Rolling timestamp list
	s.Timestamps = append(s.Timestamps, stats.CollectedAt)
	if len(s.Timestamps) > HistorySize {
		s.Timestamps = s.Timestamps[len(s.Timestamps)-HistorySize:]
	}

	// Core series
	s.CPUTotal.Update(stats.CPUUsagePercent)
	s.RAM.Update(stats.MemPercent)
	s.RootDisk.Update(stats.RootDiskPercent)
	s.NetRx.Update(stats.NetRxBytesPerSec)
	s.NetTx.Update(stats.NetTxBytesPerSec)
	s.TempMain.Update(stats.CPUTempCelsius)

	// Per-core CPU
	for i, pct := range stats.CPUCorePercents {
		if i >= len(s.CPUCores) {
			s.CPUCores = append(s.CPUCores, MetricValue{})
		}
		s.CPUCores[i].Update(pct)
	}

	// Temperature sensors
	s.TempSensors = updateNamedMetrics(s.TempSensors, len(stats.Temperatures),
		func(i int) string { return stats.Temperatures[i].Key },
		func(i int) float64 { return stats.Temperatures[i].Temperature })

	// Fan sensors
	s.FanSensors = updateNamedMetrics(s.FanSensors, len(stats.Fans),
		func(i int) string { return stats.Fans[i].Key },
		func(i int) float64 { return stats.Fans[i].RPM })

	// Voltage sensors
	s.VoltSensors = updateNamedMetrics(s.VoltSensors, len(stats.Voltages),
		func(i int) string { return stats.Voltages[i].Key },
		func(i int) float64 { return stats.Voltages[i].Voltage })
}

// updateNamedMetrics ensures the sensor slice has the right length and updates values.
// If the count changes (e.g., new hardware detected) the slice is rebuilt.
func updateNamedMetrics(existing []NamedMetric, count int, nameFn func(int) string, valFn func(int) float64) []NamedMetric {
	if len(existing) != count {
		existing = make([]NamedMetric, count)
		for i := range existing {
			existing[i].Name = nameFn(i)
		}
	}
	for i := range existing {
		existing[i].Update(valFn(i))
	}
	return existing
}

// NewMetricsStore returns an empty, ready-to-use MetricsStore.
func NewMetricsStore() *MetricsStore {
	return &MetricsStore{}
}

// HistoryResponse is the JSON payload returned by /api/metrics/history.
type HistoryResponse struct {
	Labels   []string  `json:"labels"`
	CPUTotal []float64 `json:"cpu_total"`
	RAM      []float64 `json:"ram"`
	TempMain []float64 `json:"temp_main"`
	NetRx    []float64 `json:"net_rx"`
	NetTx    []float64 `json:"net_tx"`
	RootDisk []float64 `json:"root_disk"`
}

// BuildHistoryResponse constructs the history payload under a read lock.
func (s *MetricsStore) BuildHistoryResponse() HistoryResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	labels := make([]string, len(s.Timestamps))
	for i, t := range s.Timestamps {
		labels[i] = t.Format("15:04:05")
	}
	return HistoryResponse{
		Labels:   labels,
		CPUTotal: cloneSlice(s.CPUTotal.History),
		RAM:      cloneSlice(s.RAM.History),
		TempMain: cloneSlice(s.TempMain.History),
		NetRx:    cloneSlice(s.NetRx.History),
		NetTx:    cloneSlice(s.NetTx.History),
		RootDisk: cloneSlice(s.RootDisk.History),
	}
}

// LiveSensor is a single sensor's live reading for the SSE payload.
type LiveSensor struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
}

// LiveResponse is the JSON payload pushed over SSE and returned by /api/metrics/live.
type LiveResponse struct {
	Timestamp    string       `json:"timestamp"`
	CPUTotal     LiveSensor   `json:"cpu_total"`
	CPUCores     []LiveSensor `json:"cpu_cores"`
	CPUClocksMHz []LiveSensor `json:"cpu_clocks_mhz"`
	RAM          LiveSensor   `json:"ram"`
	RootDisk     LiveSensor   `json:"root_disk"`
	DataDisks    []LiveSensor `json:"data_disks"`
	TempMain     LiveSensor   `json:"temp_main"`
	Temperatures []LiveSensor `json:"temperatures"`
	Fans         []LiveSensor `json:"fans"`
	Voltages     []LiveSensor `json:"voltages"`
	NetRx        LiveSensor   `json:"net_rx"`
	NetTx        LiveSensor   `json:"net_tx"`
	SmartHealth  map[string]string `json:"smart_health"`
	Uptime       string       `json:"uptime"`
	LoadAvg      [3]float64   `json:"load_avg"`
}

// BuildLiveResponse builds the SSE/REST live payload under a read lock.
func (s *MetricsStore) BuildLiveResponse() LiveResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r := LiveResponse{
		SmartHealth: make(map[string]string),
	}
	if s.Latest == nil {
		r.Timestamp = time.Now().Format(time.RFC3339)
		r.CPUTotal = mvToLive("CPU Total", MetricValue{})
		r.RAM = mvToLive("RAM", MetricValue{})
		r.RootDisk = mvToLive("Root Disk /", MetricValue{})
		r.TempMain = mvToLive("CPU Temp (max)", MetricValue{})
		r.NetRx = mvToLive("Network Rx", MetricValue{})
		r.NetTx = mvToLive("Network Tx", MetricValue{})
		return r
	}

	st := s.Latest
	r.Timestamp = st.CollectedAt.Format(time.RFC3339)
	r.Uptime = FormatUptime(st.UptimeDuration)
	r.LoadAvg = [3]float64{st.LoadAvg1, st.LoadAvg5, st.LoadAvg15}
	r.SmartHealth = st.SmartHealth

	r.CPUTotal = mvToLive("CPU Total", s.CPUTotal.MetricValue)
	r.RAM = mvToLive("RAM", s.RAM.MetricValue)
	r.RootDisk = mvToLive("Root Disk /", s.RootDisk.MetricValue)
	r.TempMain = mvToLive("CPU Temp (max)", s.TempMain.MetricValue)
	r.NetRx = mvToLive("Network Rx", s.NetRx.MetricValue)
	r.NetTx = mvToLive("Network Tx", s.NetTx.MetricValue)

	// Per-core CPU
	for i, c := range s.CPUCores {
		r.CPUCores = append(r.CPUCores, mvToLive(fmt.Sprintf("Core #%d", i), c))
	}

	// Per-core clocks (static from cpu.Info; only Value is meaningful)
	for i, mhz := range st.CPUCoreMHz {
		r.CPUClocksMHz = append(r.CPUClocksMHz, LiveSensor{
			Name:  fmt.Sprintf("Core #%d Clock", i),
			Value: mhz,
			Min:   mhz,
			Max:   mhz,
		})
	}

	// Data disks
	for mount, du := range st.DataDisks {
		r.DataDisks = append(r.DataDisks, LiveSensor{
			Name:  mount,
			Value: du.UsedPercent,
			Min:   du.UsedPercent,
			Max:   du.UsedPercent,
		})
	}

	// Dynamic sensors
	for _, nm := range s.TempSensors {
		r.Temperatures = append(r.Temperatures, nmToLive(nm))
	}
	for _, nm := range s.FanSensors {
		r.Fans = append(r.Fans, nmToLive(nm))
	}
	for _, nm := range s.VoltSensors {
		r.Voltages = append(r.Voltages, nmToLive(nm))
	}

	return r
}

// mvToLive converts a MetricValue to a LiveSensor.
func mvToLive(name string, mv MetricValue) LiveSensor {
	return LiveSensor{Name: name, Value: mv.Value, Min: mv.Min, Max: mv.Max}
}

// nmToLive converts a NamedMetric to a LiveSensor.
func nmToLive(nm NamedMetric) LiveSensor {
	return LiveSensor{Name: nm.Name, Value: nm.Value, Min: nm.Min, Max: nm.Max}
}

// cloneSlice returns a copy of a float64 slice to avoid data races.
func cloneSlice(s []float64) []float64 {
	if s == nil {
		return []float64{}
	}
	out := make([]float64, len(s))
	copy(out, s)
	return out
}
