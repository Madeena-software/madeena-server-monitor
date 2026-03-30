package manage

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// Fail2BanManager handles Fail2Ban operations.
type Fail2BanManager struct {
	jails []string // List of jails to monitor, e.g., ["sshd", "nginx-http-auth"]
}

// NewFail2BanManager creates a new Fail2BanManager.
func NewFail2BanManager(jails []string) *Fail2BanManager {
	if len(jails) == 0 {
		jails = []string{"sshd", "recidive"}
	}
	return &Fail2BanManager{jails: jails}
}

// BannedIP represents a single IP banned by Fail2Ban.
type BannedIP struct {
	IP    string `json:"ip"`
	Jail  string `json:"jail"`
	Since string `json:"since"` // approximate time added to ban list
}

// BannedIPs returns all currently banned IPs across all jails.
func (fbm *Fail2BanManager) BannedIPs() ([]BannedIP, error) {
	var results []BannedIP
	for _, jail := range fbm.jails {
		ips, err := fbm.bannedIPsInJail(jail)
		if err != nil {
			log.Printf("WARNING: failed to get banned IPs from jail %s: %v", jail, err)
			continue
		}
		results = append(results, ips...)
	}
	return results, nil
}

// bannedIPsInJail queries Fail2Ban for banned IPs in a specific jail.
func (fbm *Fail2BanManager) bannedIPsInJail(jail string) ([]BannedIP, error) {
	// Use 'fail2ban-client status <jail>' to get banned IPs
	// Output format is like:
	// Status for the jail: sshd
	// |- Filter
	// |  |- Currently failed: 5
	// ...
	// |- Actions
	// |  |- Currently banned: 2
	// |  |- Total banned: 10
	// |  `- IP list:  203.0.113.42 198.51.100.17

	// #nosec G204 – jail name is from internal config, not user input
	cmd := exec.Command("fail2ban-client", "status", jail)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("fail2ban-client failed: %w", err)
	}

	var results []BannedIP
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Look for the IP list line containing "IP list:"
		if strings.Contains(line, "IP list:") {
			// Extract IPs after "IP list:"
			parts := strings.Split(line, "IP list:")
			if len(parts) == 2 {
				ipStr := strings.TrimSpace(parts[1])
				ips := strings.Fields(ipStr)
				for _, ip := range ips {
					results = append(results, BannedIP{
						IP:    ip,
						Jail:  jail,
						Since: time.Now().Add(-time.Hour).Format("2006-01-02 15:04:05"), // Approximate
					})
				}
			}
		}
	}
	return results, nil
}

// Unban removes an IP from the ban list in a specific jail.
func (fbm *Fail2BanManager) Unban(ip, jail string) error {
	// Use 'fail2ban-client set <jail> unbanip <ip>'
	// #nosec G204 – IP and jail are from internal config/dashboard input
	cmd := exec.Command("fail2ban-client", "set", jail, "unbanip", ip)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to unban %s from jail %s: %v", ip, jail, err)
		return fmt.Errorf("unban failed: %w", err)
	}
	return nil
}

// Ban adds an IP to the ban list in a specific jail.
func (fbm *Fail2BanManager) Ban(ip, jail string) error {
	// #nosec G204 – IP and jail are from internal config/dashboard input
	cmd := exec.Command("fail2ban-client", "set", jail, "banip", ip)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to ban %s in jail %s: %v", ip, jail, err)
		return fmt.Errorf("ban failed: %w", err)
	}
	return nil
}
