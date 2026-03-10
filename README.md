# madeena-server-monitor

A robust, production-ready **server monitoring daemon** written in Go. It periodically checks system health metrics and sends email alerts via SMTP when thresholds are exceeded. A daily heartbeat email is also sent as proof-of-life for the monitoring service.

---

## Features

| Category | Details |
|---|---|
| **CPU** | Usage %, load average (1m/5m/15m), temperature (if hardware sensors available) |
| **Memory** | Total, used, free (MB), usage % |
| **Disk – Root** | Total, used, free (GB), usage % for `/` |
| **Disk – Data** | Configurable extra mount points (e.g. `/mnt/data`) |
| **Disk Health** | S.M.A.R.T. check via `smartctl` (PASSED / FAILED / UNKNOWN) |
| **Network** | Rx/Tx bytes per second |
| **Uptime** | Days / hours / minutes since last boot |
| **Alerting** | Immediate email on threshold breach, with 3-hour cooldown to prevent spam |
| **Heartbeat** | Daily summary email at a configurable hour |

---

## Project Structure

```
madeena-server-monitor/
├── cmd/
│   └── monitor/
│       └── main.go             # Main entry point
├── internal/
│   ├── config/
│   │   └── config.go           # Environment variable configuration
│   ├── checker/
│   │   └── checker.go          # System metrics collection (gopsutil)
│   └── notifier/
│       ├── email.go            # SMTP email sending (gomail)
│       └── alertmanager.go     # Alert debounce / cooldown logic
├── proto/
│   └── monitor.proto           # gRPC skeleton (future Python client)
├── scripts/
│   └── madeena-monitor.service # systemd unit template
├── .env.example                # Example environment variables
├── .gitignore
├── Dockerfile                  # Multi-stage Docker build
├── docker-compose.yml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## Quick Start

### 1. Clone & configure

```bash
git clone https://github.com/Madeena-software/madeena-server-monitor.git
cd madeena-server-monitor
cp .env.example .env
# Edit .env with your SMTP credentials and alert settings
```

### 2. Build & run

```bash
make build      # Produces ./bin/monitor
make run        # Build + run (reads .env from current directory)
```

---

## Environment Variables

Copy `.env.example` to `.env` and fill in the values:

| Variable | Default | Description |
|---|---|---|
| `SMTP_HOST` | `smtp.gmail.com` | SMTP server hostname |
| `SMTP_PORT` | `587` | SMTP port (587 = STARTTLS, 465 = SSL) |
| `SMTP_USER` | *(required)* | SMTP username / email |
| `SMTP_PASS` | *(required)* | SMTP password or app password |
| `ALERT_FROM` | `SMTP_USER` | From address for alert emails |
| `ALERT_TO` | *(required)* | Comma-separated recipient email addresses |
| `CPU_THRESHOLD` | `95` | CPU usage % to trigger alert |
| `RAM_THRESHOLD` | `95` | RAM usage % to trigger alert |
| `ROOT_DISK_THRESHOLD` | `90` | Root disk usage % to trigger alert |
| `CPU_CONSECUTIVE_CHECKS` | `3` | Consecutive high-CPU checks before alerting |
| `CHECK_INTERVAL` | `1m` | How often to check CPU/RAM |
| `DISK_INTERVAL` | `15m` | How often to check disk space |
| `ALERT_COOLDOWN` | `3h` | Minimum time between repeated alerts for the same issue |
| `HEARTBEAT_HOUR` | `8` | Hour of day (0–23) for the daily heartbeat email |
| `DATA_PARTITIONS` | *(empty)* | Extra mount points to monitor, comma-separated |
| `SERVER_NAME` | `madeena-server` | Friendly name used in email subjects |

### Gmail app password

If using Gmail, create an [App Password](https://myaccount.google.com/apppasswords) and set `SMTP_PASS` to that value.

---

## Alerting Logic

* **Immediate alerts** are sent when:
  * CPU stays above `CPU_THRESHOLD` for `CPU_CONSECUTIVE_CHECKS` consecutive checks
  * RAM exceeds `RAM_THRESHOLD`
  * Root (or data) disk exceeds `ROOT_DISK_THRESHOLD`
  * S.M.A.R.T. reports `FAILED` for any monitored device

* **Cooldown**: Once an alert is sent for a specific condition, no further alert for that same condition is sent until `ALERT_COOLDOWN` has elapsed.

* **Daily heartbeat**: At `HEARTBEAT_HOUR` each day a summary email is sent containing all current metrics.

---

## Deployment with systemd (Ubuntu)

```bash
# 1. Build the binary
make build

# 2. Create a dedicated user and directory
sudo useradd -r -s /bin/false madeena-monitor
sudo mkdir -p /opt/madeena-monitor
sudo cp bin/monitor /opt/madeena-monitor/
sudo cp .env        /opt/madeena-monitor/
sudo chown -R madeena-monitor:madeena-monitor /opt/madeena-monitor
sudo chmod 600 /opt/madeena-monitor/.env

# 3. Install the systemd unit
sudo cp scripts/madeena-monitor.service /etc/systemd/system/

# 4. Enable and start
sudo systemctl daemon-reload
sudo systemctl enable --now madeena-monitor

# 5. Check status
sudo systemctl status madeena-monitor
sudo journalctl -u madeena-monitor -f
```

### S.M.A.R.T. device access

The monitor runs `smartctl` as the service user. To allow a non-root user to run `smartctl` without `sudo`:

```bash
sudo setcap cap_sys_rawio+ep /usr/sbin/smartctl
sudo usermod -aG disk madeena-monitor
```

---

## Docker

```bash
# Build image
docker build -t madeena-server-monitor .

# Run with docker compose
docker compose up -d
```

> **Note**: Full hardware access (CPU temperature, S.M.A.R.T.) requires `--privileged` or specific device/capability grants. Review `docker-compose.yml` and adjust as needed.

---

## Development

```bash
make build    # Compile binary
make run      # Build & run
make test     # Run unit tests
make clean    # Remove build artefacts
make protoc   # Generate gRPC stubs (requires protoc toolchain)
```

---

## Future: gRPC Integration (Python Client)

`proto/monitor.proto` defines the skeleton of a `ServerMonitor` gRPC service with a `GetSystemStats` RPC. The plan:

1. Run `make protoc` to generate Go server stubs.
2. Implement the gRPC server in a new `internal/grpcserver/` package.
3. Write a Python client using `grpcio` to query live metrics remotely.

---

## Dependencies

| Package | Purpose |
|---|---|
| [`github.com/shirou/gopsutil/v3`](https://github.com/shirou/gopsutil) | Cross-platform system metrics (CPU, RAM, disk, network, uptime) |
| [`github.com/joho/godotenv`](https://github.com/joho/godotenv) | `.env` file loading |
| [`gopkg.in/gomail.v2`](https://pkg.go.dev/gopkg.in/gomail.v2) | SMTP email sending |
