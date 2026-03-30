package manage

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
)

// NetworkManager handles network operations like IP detection and firewall rules.
type NetworkManager struct{}

// NewNetworkManager creates a new NetworkManager.
func NewNetworkManager() *NetworkManager {
	return &NetworkManager{}
}

// CurrentPublicIP attempts to detect the current public IP by making an HTTP request.
func (nm *NetworkManager) CurrentPublicIP() (string, error) {
	// Try multiple public IP detection services for redundancy
	services := []string{
		"https://api.ipify.org",
		"https://icanhazip.com",
		"https://checkip.amazonaws.com",
	}

	for _, service := range services {
		resp, err := http.Get(service)
		if err != nil {
			log.Printf("WARNING: failed to query %s: %v", service, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Printf("WARNING: %s returned status %d", service, resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			log.Printf("WARNING: failed to read response from %s: %v", service, err)
			continue
		}

		ip := strings.TrimSpace(string(body))
		if isValidIP(ip) {
			return ip, nil
		}
	}

	return "", fmt.Errorf("all IP detection services failed")
}

// isValidIP does a basic check that a string looks like an IP address.
func isValidIP(ip string) bool {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return false
	}
	return true
}

// WhitelistIP adds an IP to the SSH allowlist in UFW (if installed).
// This is a best-effort operation; it logs errors but doesn't fail if UFW is unavailable.
func (nm *NetworkManager) WhitelistIP(ip string, port int, protocol string) error {
	if !isValidIP(ip) {
		return fmt.Errorf("invalid IP format: %s", ip)
	}

	// Attempt to run: ufw allow from <ip> to any port <port> proto <protocol>
	// #nosec G204 – ip and port are user-provided but validated
	cmd := exec.Command("ufw", "allow", "from", ip, "to", "any", "port", fmt.Sprintf("%d", port), "proto", protocol)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// UFW might not be installed or running; log as warning
		log.Printf("WARNING: failed to whitelist %s in UFW: %v (%s)", ip, err, strings.TrimSpace(string(output)))
		return fmt.Errorf("ufw whitelist failed: %w", err)
	}
	log.Printf("INFO: whitelisted IP %s on port %d proto %s", ip, port, protocol)
	return nil
}

// RemoveIPWhitelist removes an IP from the SSH allowlist in UFW.
func (nm *NetworkManager) RemoveIPWhitelist(ip string, port int, protocol string) error {
	if !isValidIP(ip) {
		return fmt.Errorf("invalid IP format: %s", ip)
	}

	// #nosec G204 – ip and port are user-provided but validated
	cmd := exec.Command("ufw", "delete", "allow", "from", ip, "to", "any", "port", fmt.Sprintf("%d", port), "proto", protocol)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("WARNING: failed to remove whitelist for %s: %v (%s)", ip, err, strings.TrimSpace(string(output)))
		return fmt.Errorf("ufw delete failed: %w", err)
	}
	return nil
}
