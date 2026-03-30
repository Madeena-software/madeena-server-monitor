// Package manage provides server operations: security logs, service management, and Fail2Ban.
package manage

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
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
	return lines[start:], nil
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
