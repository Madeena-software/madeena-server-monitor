// Package dashboard provides the HTTP server for the live web monitoring dashboard.
// It exposes endpoints for metrics monitoring and server management.
package dashboard

import (
"crypto/subtle"
"encoding/json"
"fmt"
"io/fs"
"log"
"net/http"
"strconv"
"strings"
"time"

"github.com/Madeena-software/madeena-server-monitor/internal/assets"
"github.com/Madeena-software/madeena-server-monitor/internal/checker"
"github.com/Madeena-software/madeena-server-monitor/internal/manage"
)

// Server is the live-dashboard HTTP server.
type Server struct {
store           *checker.MetricsStore
interval        time.Duration
logReader       *manage.LogReader
serviceManager  *manage.ServiceManager
fail2banManager *manage.Fail2BanManager
networkManager  *manage.NetworkManager
userManager     *manage.UserManager
dashUser        string
dashPass        string
}

// New creates a new dashboard Server.
// store is the shared MetricsStore updated by the monitor loop.
// interval is how often SSE events are pushed to clients.
// services is the list of systemd services to manage.
// fail2banJails is the list of Fail2Ban jails to monitor.
// dashUser and dashPass are the HTTP Basic Auth credentials; if dashPass is
// empty, authentication is disabled.
func New(store *checker.MetricsStore, interval time.Duration, services []string, fail2banJails []string, dashUser, dashPass string) *Server {
return &Server{
store:           store,
interval:        interval,
logReader:       manage.NewLogReader("/var/log/auth.log"),
serviceManager:  manage.NewServiceManager(services),
fail2banManager: manage.NewFail2BanManager(fail2banJails),
networkManager:  manage.NewNetworkManager(),
userManager:     manage.NewUserManager(),
dashUser:        dashUser,
dashPass:        dashPass,
}
}

// basicAuth wraps h and enforces HTTP Basic Auth when a password is configured.
// Credentials are compared in constant time to prevent timing attacks.
func (s *Server) basicAuth(next http.Handler) http.Handler {
return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// Skip auth if no password is configured
if s.dashPass == "" {
next.ServeHTTP(w, r)
return
}
user, pass, ok := r.BasicAuth()
// Always compare both fields in constant time to prevent timing attacks.
userMatch := subtle.ConstantTimeCompare([]byte(user), []byte(s.dashUser))
passMatch := subtle.ConstantTimeCompare([]byte(pass), []byte(s.dashPass))
if !ok || userMatch != 1 || passMatch != 1 {
w.Header().Set("WWW-Authenticate", `Basic realm="Madeena Server Monitor", charset="UTF-8"`)
http.Error(w, "Unauthorized", http.StatusUnauthorized)
return
}
next.ServeHTTP(w, r)
})
}

// ListenAndServe registers routes and starts the HTTP server on the given addr
// (e.g., ":8080"). It blocks until the server fails.
func (s *Server) ListenAndServe(addr string) error {
mux := http.NewServeMux()

// Static files – serve from the embedded FS, stripping the "web/" prefix.
webFS, err := fs.Sub(assets.WebFS, "web")
if err != nil {
return fmt.Errorf("dashboard: unable to sub embed FS: %w", err)
}
mux.Handle("/", http.FileServer(http.FS(webFS)))

// REST – metrics endpoints
mux.HandleFunc("/api/metrics/history", s.handleHistory)
mux.HandleFunc("/api/metrics/live", s.handleLive)

// Management API endpoints
mux.HandleFunc("/api/manage/network/current-ip", s.handleCurrentIP)
mux.HandleFunc("/api/manage/firewall/whitelist-current-ip", s.handleWhitelistIP)
mux.HandleFunc("/api/manage/services/status", s.handleServicesStatus)
mux.HandleFunc("/api/manage/services/", s.handleServiceAction) // POST /api/manage/services/<name>/<action>
mux.HandleFunc("/api/manage/fail2ban/banned", s.handleBanned)
mux.HandleFunc("/api/manage/fail2ban/unban", s.handleUnban)
mux.HandleFunc("/api/manage/logs/auth", s.handleAuthLogs)
mux.HandleFunc("/api/manage/logs/ssh", s.handleSSHLogs)
mux.HandleFunc("/api/manage/users", s.handleUsers)

srv := &http.Server{
Addr:         addr,
Handler:      s.basicAuth(mux),
ReadTimeout:  10 * time.Second,
WriteTimeout: 0, // disabled for SSE streams (long-lived connections)
IdleTimeout:  120 * time.Second,
}

if s.dashPass == "" {
log.Printf("WARNING: dashboard authentication is disabled; set DASHBOARD_PASS to enable")
}
log.Printf("INFO: dashboard listening on %s", addr)
return srv.ListenAndServe()
}

// handleHistory returns the rolling metric history as a JSON object.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

resp := s.store.BuildHistoryResponse()
if err := json.NewEncoder(w).Encode(resp); err != nil {
log.Printf("ERROR: dashboard history encode: %v", err)
}
}

// handleLive streams SSE events with the current live sensor snapshot at the
// configured interval. Each event is a JSON-encoded LiveResponse.
func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
// Verify the client accepts SSE
flusher, ok := w.(http.Flusher)
if !ok {
http.Error(w, "SSE not supported", http.StatusInternalServerError)
return
}

w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("Access-Control-Allow-Origin", "*")

ctx := r.Context()
ticker := time.NewTicker(s.interval)
defer ticker.Stop()

// Send an initial event immediately so the browser doesn't wait
s.pushEvent(w, flusher)

for {
select {
case <-ctx.Done():
return
case <-ticker.C:
s.pushEvent(w, flusher)
}
}
}

// pushEvent serialises the current LiveResponse and writes a single SSE event.
func (s *Server) pushEvent(w http.ResponseWriter, flusher http.Flusher) {
resp := s.store.BuildLiveResponse()
data, err := json.Marshal(resp)
if err != nil {
log.Printf("ERROR: dashboard SSE marshal: %v", err)
return
}
fmt.Fprintf(w, "data: %s\n\n", data)
flusher.Flush()
}

// handleCurrentIP returns the current public IP of the server.
func (s *Server) handleCurrentIP(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

ip, err := s.networkManager.CurrentPublicIP()
if err != nil {
log.Printf("WARNING: failed to detect current IP: %v", err)
w.WriteHeader(http.StatusServiceUnavailable)
json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]string{"ip": ip})
}

// handleWhitelistIP adds the current IP to the SSH firewall allowlist.
func (s *Server) handleWhitelistIP(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

if r.Method != http.MethodPost {
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
return
}

var payload struct {
IP       string `json:"ip"`
Port     int    `json:"port"`
Protocol string `json:"protocol"`
}
if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
return
}

if payload.Port == 0 {
payload.Port = 22
}
if payload.Protocol == "" {
payload.Protocol = "tcp"
}

err := s.networkManager.WhitelistIP(payload.IP, payload.Port, payload.Protocol)
if err != nil {
log.Printf("ERROR: whitelist failed: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "ip": payload.IP})
}

// handleServicesStatus returns the current status of all managed services.
func (s *Server) handleServicesStatus(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

statuses, err := s.serviceManager.Status()
if err != nil {
log.Printf("ERROR: failed to get service status: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"services": statuses})
}

// handleServiceAction handles service control actions (restart, start, stop).
// Path: /api/manage/services/<name>/<action> (POST)
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

if r.Method != http.MethodPost {
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
return
}

// Parse path: /api/manage/services/<name>/<action>
parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/manage/services/"), "/")
if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]string{"error": "invalid path"})
return
}

serviceName := parts[0]
action := parts[1]

var status manage.ServiceStatus
var err error

switch action {
case "restart":
status, err = s.serviceManager.Restart(serviceName)
case "start":
status, err = s.serviceManager.Start(serviceName)
case "stop":
status, err = s.serviceManager.Stop(serviceName)
default:
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]string{"error": "unknown action"})
return
}

if err != nil {
log.Printf("ERROR: service action %s failed on %s: %v", action, serviceName, err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "service": serviceName})
return
}

json.NewEncoder(w).Encode(status)
}

// handleBanned returns all currently banned IPs from Fail2Ban.
func (s *Server) handleBanned(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

banned, err := s.fail2banManager.BannedIPs()
if err != nil {
log.Printf("ERROR: failed to get banned IPs: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"banned": banned})
}

// handleUnban removes an IP from the Fail2Ban ban list.
func (s *Server) handleUnban(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

if r.Method != http.MethodPost {
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
return
}

var payload struct {
IP   string `json:"ip"`
Jail string `json:"jail"` // optional; defaults to "sshd"
}
if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
w.WriteHeader(http.StatusBadRequest)
json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
return
}

if payload.Jail == "" {
payload.Jail = "sshd"
}

err := s.fail2banManager.Unban(payload.IP, payload.Jail)
if err != nil {
log.Printf("ERROR: unban failed for %s: %v", payload.IP, err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "ip": payload.IP, "jail": payload.Jail})
}

// handleAuthLogs returns the last n lines from /var/log/auth.log.
// Query parameter: ?lines=80 (default 80)
func (s *Server) handleAuthLogs(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

lines := parseLines(r, 80)

logLines, err := s.logReader.Tail(lines)
if err != nil {
log.Printf("ERROR: failed to read auth logs: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"lines": logLines})
}

// handleSSHLogs returns the last n sshd login-related entries from the auth log.
// Query parameter: ?lines=80 (default 80)
func (s *Server) handleSSHLogs(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

lines := parseLines(r, 80)

logLines, err := s.logReader.SSHLogs(lines)
if err != nil {
log.Printf("ERROR: failed to read SSH logs: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"lines": logLines})
}

// handleUsers returns the list of real human accounts on the system.
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
w.Header().Set("Access-Control-Allow-Origin", "*")

users, err := s.userManager.HumanUsers()
if err != nil {
log.Printf("ERROR: failed to read system users: %v", err)
w.WriteHeader(http.StatusInternalServerError)
json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
return
}

json.NewEncoder(w).Encode(map[string]interface{}{"users": users})
}

// parseLines reads the ?lines= query parameter with a given default.
func parseLines(r *http.Request, defaultVal int) int {
if s := r.URL.Query().Get("lines"); s != "" {
if n, err := strconv.Atoi(s); err == nil && n > 0 {
return n
}
}
return defaultVal
}
