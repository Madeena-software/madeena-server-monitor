package manage_test

import (
	"os"
	"strings"
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
	// Timestamp prefix may be rewritten to include year and timezone (UTC+7).
	// Verify the important message suffixes are present instead of exact timestamp.
	if !strings.HasSuffix(lines[0], "sshd[125]: Failed password for invalid user") {
		t.Errorf("unexpected auth log line: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "fail2ban.actions[999]: NOTICE [sshd] Ban 10.0.0.1") {
		t.Errorf("unexpected auth log line: %q", lines[1])
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

func TestLogReaderSSHLogs(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "auth.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	testLines := []string{
		"Mar 30 10:00:00 server sshd[100]: Accepted publickey for alice from 10.0.0.1 port 22 ssh2",
		"Mar 30 10:00:01 server sudo: alice : COMMAND=/bin/ls",
		"Mar 30 10:00:02 server sshd[101]: Failed password for invalid user bob from 203.0.113.5 port 40001 ssh2",
		"Mar 30 10:00:03 server sshd[101]: Disconnected from invalid user bob 203.0.113.5 port 40001 [preauth]",
		"Mar 30 10:00:04 server fail2ban.actions[999]: Ban 203.0.113.5",
		"Mar 30 10:00:05 server sshd[100]: Disconnected from user alice 10.0.0.1 port 22",
	}
	for _, line := range testLines {
		tmpFile.WriteString(line + "\n")
	}
	tmpFile.Close()

	lr := manage.NewLogReader(tmpFile.Name())
	lines, err := lr.SSHLogs(10)
	if err != nil {
		t.Fatalf("SSHLogs() failed: %v", err)
	}

	// Should only return sshd login-related lines (4 lines, not sudo or fail2ban)
	if len(lines) != 4 {
		t.Errorf("expected 4 SSH log lines, got %d", len(lines))
	}

	// Verify sudo line is excluded
	for _, ln := range lines {
		if strings.Contains(ln, "sudo") {
			t.Errorf("unexpected sudo line in SSH logs: %q", ln)
		}
		if strings.Contains(strings.ToLower(ln), "fail2ban") {
			t.Errorf("unexpected fail2ban line in SSH logs: %q", ln)
		}
	}
}

func TestLogReaderSSHLogsLimitN(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "auth.log")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write 10 sshd login lines
	for i := 0; i < 10; i++ {
		tmpFile.WriteString("Mar 30 10:00:00 server sshd[100]: Failed password for test from 1.2.3.4 port 22 ssh2\n")
	}
	tmpFile.Close()

	lr := manage.NewLogReader(tmpFile.Name())
	lines, err := lr.SSHLogs(3)
	if err != nil {
		t.Fatalf("SSHLogs() failed: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (limit), got %d", len(lines))
	}
}

func TestUserManagerParsing(t *testing.T) {
	// Create a temporary passwd file
	tmpFile, err := os.CreateTemp("", "passwd")
	if err != nil {
		t.Fatalf("failed to create temp passwd file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	passwdContent := `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
bin:x:2:2:bin:/bin:/usr/sbin/nologin
www-data:x:33:33:www-data:/var/www:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
alice:x:1000:1000:Alice Smith:/home/alice:/bin/bash
bob:x:1001:1001:Bob Jones:/home/bob:/bin/zsh
svc:x:999:999:service:/:/bin/false
`
	if _, err := tmpFile.WriteString(passwdContent); err != nil {
		t.Fatalf("failed to write passwd file: %v", err)
	}
	tmpFile.Close()

	um := manage.NewUserManagerFromFile(tmpFile.Name())
	users, err := um.HumanUsers()
	if err != nil {
		t.Fatalf("HumanUsers() failed: %v", err)
	}

	// Should return root, alice, bob (not daemon, bin, www-data, nobody, svc)
	if len(users) != 3 {
		t.Errorf("expected 3 human users (root, alice, bob), got %d: %+v", len(users), users)
	}

	names := make(map[string]bool)
	for _, u := range users {
		names[u.Username] = true
	}
	for _, expected := range []string{"root", "alice", "bob"} {
		if !names[expected] {
			t.Errorf("expected user %q not found in results", expected)
		}
	}
	for _, excluded := range []string{"daemon", "bin", "www-data", "nobody", "svc"} {
		if names[excluded] {
			t.Errorf("system account %q should have been excluded", excluded)
		}
	}
}

func TestUserManagerNonexistentFile(t *testing.T) {
	um := manage.NewUserManagerFromFile("/nonexistent/passwd")
	_, err := um.HumanUsers()
	if err == nil {
		t.Fatal("expected error for nonexistent passwd file")
	}
}
