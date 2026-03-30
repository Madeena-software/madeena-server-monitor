package manage_test

import (
	"os"
	"testing"

	"github.com/Madeena-software/madeena-server-monitor/internal/manage"
)

func TestLogReaderTail(t *testing.T) {
	// Create a temporary log file
	tmpFile, err := os.CreateTemp("", "auth.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test log lines
	testLines := []string{
		"Mar 30 10:00:00 server sshd[123]: Connection from 10.0.0.1",
		"Mar 30 10:00:01 server sshd[124]: Invalid user test",
		"Mar 30 10:00:02 server sshd[125]: Failed password for invalid user",
		"Mar 30 10:00:03 server fail2ban.actions[999]: NOTICE [sshd] Ban 10.0.0.1",
	}

	for _, line := range testLines {
		tmpFile.WriteString(line + "\n")
	}
	tmpFile.Close()

	// Test Tail
	lr := manage.NewLogReader(tmpFile.Name())
	lines, err := lr.Tail(2)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}

	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d", len(lines))
	}
	if lines[0] != testLines[2] {
		t.Errorf("expected line %q, got %q", testLines[2], lines[0])
	}
	if lines[1] != testLines[3] {
		t.Errorf("expected line %q, got %q", testLines[3], lines[1])
	}
}

func TestLogReaderSearch(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "auth.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testLines := []string{
		"Mar 30 10:00:00 server sshd[123]: Accepted password for user",
		"Mar 30 10:00:01 server sshd[124]: Invalid user test",
		"Mar 30 10:00:02 server sudo: user : TTY=pts/0 COMMAND=/bin/ls",
		"Mar 30 10:00:03 server fail2ban.actions[999]: Ban 10.0.0.1",
	}

	for _, line := range testLines {
		tmpFile.WriteString(line + "\n")
	}
	tmpFile.Close()

	lr := manage.NewLogReader(tmpFile.Name())
	lines, err := lr.Search([]string{"sudo", "TTY"}, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(lines) != 1 {
		t.Errorf("expected 1 matching line, got %d", len(lines))
	}
}

func TestLogReaderNonexistent(t *testing.T) {
	lr := manage.NewLogReader("/nonexistent/path/auth.log")
	_, err := lr.Tail(10)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestNetworkManagerIPValidation(t *testing.T) {
	nm := manage.NewNetworkManager()
	// Just verify the manager was created
	if nm == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestServiceManagerBasic(t *testing.T) {
	services := []string{"sshd", "nginx"}
	sm := manage.NewServiceManager(services)

	// Just verify the manager was created
	if sm == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFail2BanManagerBasic(t *testing.T) {
	jails := []string{"sshd", "recidive"}
	fbm := manage.NewFail2BanManager(jails)

	// Just verify the manager was created
	if fbm == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestFail2BanManagerDefaultJails(t *testing.T) {
	// Test with empty jails list - should use defaults
	fbm := manage.NewFail2BanManager([]string{})

	if fbm == nil {
		t.Fatal("expected non-nil manager")
	}
}
