// Package manage provides server operations: security logs, service management, and Fail2Ban.
package manage

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// SystemUser represents a human user account on the system.
type SystemUser struct {
	Username string `json:"username"`
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
	HomeDir  string `json:"home_dir"`
	Shell    string `json:"shell"`
}

// UserManager reads and filters system user accounts from /etc/passwd.
type UserManager struct {
	passwdFile string
}

// NewUserManager creates a new UserManager reading from /etc/passwd.
func NewUserManager() *UserManager {
	return &UserManager{passwdFile: "/etc/passwd"}
}

// NewUserManagerFromFile creates a UserManager that reads from the given file path.
// Useful for testing.
func NewUserManagerFromFile(path string) *UserManager {
	return &UserManager{passwdFile: path}
}

// systemShells is the set of shells that indicate a non-interactive (service) account.
var systemShells = map[string]bool{
	"/sbin/nologin":     true,
	"/usr/sbin/nologin": true,
	"/bin/false":        true,
	"/bin/nologin":      true,
	"/dev/null":         true,
}

// HumanUsers returns real human accounts: root (UID=0) and accounts with
// UID >= 1000 that have an interactive login shell. Standard service accounts
// (low-UID or nologin shell) are excluded.
func (um *UserManager) HumanUsers() ([]SystemUser, error) {
	f, err := os.Open(um.passwdFile)
	if err != nil {
		return nil, fmt.Errorf("cannot open passwd file: %w", err)
	}
	defer f.Close()

	var users []SystemUser
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// passwd format: username:password:uid:gid:gecos:home:shell
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}

		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}
		gid, err := strconv.Atoi(parts[3])
		if err != nil {
			continue
		}

		shell := parts[6]

		// Include only root (UID=0) and human accounts (UID >= 1000)
		if uid != 0 && uid < 1000 {
			continue
		}

		// Exclude accounts without an interactive shell
		if systemShells[shell] {
			continue
		}

		users = append(users, SystemUser{
			Username: parts[0],
			UID:      uid,
			GID:      gid,
			HomeDir:  parts[5],
			Shell:    shell,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read passwd file: %w", err)
	}

	return users, nil
}
