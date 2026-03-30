package assets_test

import (
	"strings"
	"testing"

	"github.com/Madeena-software/madeena-server-monitor/internal/assets"
)

func readDashboardHTML(t *testing.T) string {
	t.Helper()
	content, err := assets.WebFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatalf("failed to read dashboard html: %v", err)
	}
	return string(content)
}

func TestManagementUIElementsPresent(t *testing.T) {
	html := readDashboardHTML(t)

	requiredIDs := []string{
		"id=\"whitelistIpBtn\"",
		"id=\"detectedIp\"",
		"id=\"serviceTable\"",
		"id=\"refreshServicesBtn\"",
		"id=\"bannedList\"",
		"id=\"refreshBansBtn\"",
		"id=\"authLogViewer\"",
		"id=\"refreshLogsBtn\"",
		"id=\"autoScrollLogs\"",
	}

	for _, id := range requiredIDs {
		if !strings.Contains(html, id) {
			t.Fatalf("expected dashboard to include element %s", id)
		}
	}
}

func TestManagementActionEndpointsPresent(t *testing.T) {
	html := readDashboardHTML(t)

	requiredEndpoints := []string{
		"/api/manage/network/current-ip",
		"/api/manage/firewall/whitelist-current-ip",
		"/api/manage/services/status",
		"/api/manage/fail2ban/banned",
		"/api/manage/fail2ban/unban",
		"/api/manage/logs/auth?lines=80",
	}

	for _, endpoint := range requiredEndpoints {
		if !strings.Contains(html, endpoint) {
			t.Fatalf("expected dashboard JS to reference endpoint %s", endpoint)
		}
	}
}

func TestMonitoringLogicStillPresent(t *testing.T) {
	html := readDashboardHTML(t)

	checks := []string{
		"/api/metrics/history",
		"/api/metrics/live",
		"connectSSE",
		"chartCPU",
		"chartRAM",
		"chartTemp",
		"chartDisk",
		"chartNetRx",
		"chartNetTx",
	}

	for _, token := range checks {
		if !strings.Contains(html, token) {
			t.Fatalf("expected existing monitor capability token %s", token)
		}
	}
}

func TestSensorTableIsScrollableAndBounded(t *testing.T) {
	html := readDashboardHTML(t)

	if !strings.Contains(html, "max-h-[500px]") {
		t.Fatal("expected sensor area to use max-h-[500px]")
	}
	if !strings.Contains(html, "overflow-y-auto") {
		t.Fatal("expected sensor area to use overflow-y-auto")
	}
}
