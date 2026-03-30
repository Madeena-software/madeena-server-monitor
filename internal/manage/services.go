package manage

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
)

// ServiceManager handles systemd service operations.
type ServiceManager struct {
	// List of services to manage (e.g., ["sshd", "nginx", "mysql"])
	services []string
}

// NewServiceManager creates a new ServiceManager.
func NewServiceManager(services []string) *ServiceManager {
	return &ServiceManager{services: services}
}

// ServiceStatus represents the status of a single service.
type ServiceStatus struct {
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"` // brief status string e.g., "active (running)"
}

// Status returns the current status of all monitored services.
func (sm *ServiceManager) Status() ([]ServiceStatus, error) {
	var results []ServiceStatus
	for _, svc := range sm.services {
		status, err := sm.statusOne(svc)
		if err != nil {
			log.Printf("WARNING: failed to get status for service %s: %v", svc, err)
			// Return a degraded status rather than failing completely
			results = append(results, ServiceStatus{
				Name:   svc,
				Active: false,
				Status: fmt.Sprintf("error: %v", err),
			})
			continue
		}
		results = append(results, status)
	}
	return results, nil
}

// statusOne queries the status of a single service using systemctl.
func (sm *ServiceManager) statusOne(name string) (ServiceStatus, error) {
	// #nosec G204 – service name is from internal config, not user input
	cmd := exec.Command("systemctl", "is-active", name)
	output, err := cmd.CombinedOutput()
	activeStr := strings.TrimSpace(string(output))

	// Query enabled status
	cmd2 := exec.Command("systemctl", "is-enabled", name)
	output2, _ := cmd2.CombinedOutput()
	enabledStr := strings.TrimSpace(string(output2))

	active := activeStr == "active"
	enabled := enabledStr == "enabled" || enabledStr == "enabled-runtime"

	return ServiceStatus{
		Name:    name,
		Active:  active,
		Enabled: enabled,
		Status:  activeStr,
	}, err
}

// Restart attempts to restart the given service.
// Returns the new status on success.
func (sm *ServiceManager) Restart(name string) (ServiceStatus, error) {
	// Validate that the service is in our list
	found := false
	for _, s := range sm.services {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		return ServiceStatus{}, fmt.Errorf("service %s is not in the managed list", name)
	}

	// #nosec G204 – service name is from internal config
	cmd := exec.Command("systemctl", "restart", name)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to restart service %s: %v", name, err)
		return ServiceStatus{}, fmt.Errorf("restart failed: %w", err)
	}

	// Return the new status
	return sm.statusOne(name)
}

// Start attempts to start the given service.
func (sm *ServiceManager) Start(name string) (ServiceStatus, error) {
	found := false
	for _, s := range sm.services {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		return ServiceStatus{}, fmt.Errorf("service %s is not in the managed list", name)
	}

	cmd := exec.Command("systemctl", "start", name)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to start service %s: %v", name, err)
		return ServiceStatus{}, fmt.Errorf("start failed: %w", err)
	}
	return sm.statusOne(name)
}

// Stop attempts to stop the given service.
func (sm *ServiceManager) Stop(name string) (ServiceStatus, error) {
	found := false
	for _, s := range sm.services {
		if s == name {
			found = true
			break
		}
	}
	if !found {
		return ServiceStatus{}, fmt.Errorf("service %s is not in the managed list", name)
	}

	cmd := exec.Command("systemctl", "stop", name)
	if err := cmd.Run(); err != nil {
		log.Printf("ERROR: failed to stop service %s: %v", name, err)
		return ServiceStatus{}, fmt.Errorf("stop failed: %w", err)
	}
	return sm.statusOne(name)
}
