// Package manage provides server operations: security logs, service management, and Fail2Ban.
package manage

import (
"bufio"
"fmt"
"log"
"os"
"strings"
"time"
)

// LogReader provides access to system security logs.
type LogReader struct {
logFile string
}

// NewLogReader creates a new LogReader for the specified file.
func NewLogReader(logFile string) *LogReader {
return &LogReader{logFile: logFile}
}

// Tail reads the last n lines from the security log file.
// Returns up to n lines in chronological order (oldest first).
func (lr *LogReader) Tail(n int) ([]string, error) {
if n <= 0 {
n = 80
}

file, err := os.Open(lr.logFile)
if err != nil {
log.Printf("ERROR: failed to open log file %s: %v", lr.logFile, err)
return nil, fmt.Errorf("cannot open log file: %w", err)
}
defer file.Close()

// Read lines into a buffer
var lines []string
scanner := bufio.NewScanner(file)
for scanner.Scan() {
lines = append(lines, scanner.Text())
}

if err := scanner.Err(); err != nil {
log.Printf("ERROR: failed to scan log file: %v", err)
return nil, fmt.Errorf("cannot read log file: %w", err)
}

// Return only the last n lines
start := len(lines) - n
if start < 0 {
start = 0
}

convertTimestamps(lines, start, len(lines))

return lines[start:], nil
}

// SSHLogs returns the last n sshd login-related log entries.
// It filters for lines from the sshd process that contain login activity
// keywords (Accepted, Failed, Disconnected, Invalid user, session opened/closed).
func (lr *LogReader) SSHLogs(n int) ([]string, error) {
if n <= 0 {
n = 80
}

// Keywords that indicate relevant sshd login activity
loginKeywords := []string{
"accepted",
"failed",
"disconnected",
"invalid user",
"session opened",
"session closed",
}

file, err := os.Open(lr.logFile)
if err != nil {
log.Printf("ERROR: failed to open log file %s: %v", lr.logFile, err)
return nil, fmt.Errorf("cannot open log file: %w", err)
}
defer file.Close()

var matching []string
scanner := bufio.NewScanner(file)
for scanner.Scan() {
line := scanner.Text()
lower := strings.ToLower(line)
// Must be from sshd
if !strings.Contains(lower, "sshd") {
continue
}
// Must contain at least one login activity keyword
for _, kw := range loginKeywords {
if strings.Contains(lower, kw) {
matching = append(matching, line)
break
}
}
}

if err := scanner.Err(); err != nil {
log.Printf("ERROR: failed to scan log file: %v", err)
return nil, fmt.Errorf("cannot read log file: %w", err)
}

// Return only the last n matching lines
start := len(matching) - n
if start < 0 {
start = 0
}

convertTimestamps(matching, start, len(matching))

return matching[start:], nil
}

// Search filters log lines containing all the given keywords.
func (lr *LogReader) Search(keywords []string, limit int) ([]string, error) {
if limit <= 0 {
limit = 500
}

file, err := os.Open(lr.logFile)
if err != nil {
return nil, fmt.Errorf("cannot open log file: %w", err)
}
defer file.Close()

var results []string
scanner := bufio.NewScanner(file)
for scanner.Scan() && len(results) < limit {
line := scanner.Text()
matches := true
for _, kw := range keywords {
if !strings.Contains(strings.ToLower(line), strings.ToLower(kw)) {
matches = false
break
}
}
if matches {
results = append(results, line)
}
}

return results, scanner.Err()
}

// convertTimestamps rewrites syslog-style timestamps (e.g. "Mar 30 10:42:11")
// in lines[from:to] to UTC+7 with year attached.
func convertTimestamps(lines []string, from, to int) {
targetTZ := time.FixedZone("UTC+7", 7*3600)
now := time.Now()
for i := from; i < to; i++ {
ln := lines[i]
fields := strings.Fields(ln)
if len(fields) >= 3 {
ts := fields[0] + " " + fields[1] + " " + fields[2]
parsed, err := time.ParseInLocation("Jan _2 15:04:05", ts, time.Local)
if err == nil {
parsed = time.Date(now.Year(), parsed.Month(), parsed.Day(),
parsed.Hour(), parsed.Minute(), parsed.Second(), 0, parsed.Location())
converted := parsed.In(targetTZ)
newTs := converted.Format("2006-01-02 15:04:05 -0700")
lines[i] = strings.Replace(ln, ts, newTs, 1)
}
}
}
}
