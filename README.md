# Infrastructure Insight

A highly available, production-grade Linux infrastructure monitoring and diagnostic telemetry dashboard.

---

## Architecture Overview

Infrastructure Insight is deployed across a 5-node virtual network (`192.168.56.0/24`) isolating concerns across load balancing, web delivery, backend computation, and disaster recovery:

```text
                                [ Public Web Browser ]
                                          │
                                          ▼ Port 80
                             ┌─────────────────────────┐
                             │      load-balancer      │
                             │      192.168.56.10      │
                             │  (HAProxy + Stats 8404) │
                             └────────────┬────────────┘
                                          │
                        ┌─────────────────┴─────────────────┐
                        ▼ Port 80                           ▼ Port 80
               ┌─────────────────┐                 ┌─────────────────┐
               │     web-01      │                 │     web-02      │
               │  192.168.56.11  │                 │  192.168.56.12  │
               │ (Nginx Frontend)│                 │ (Nginx Frontend)│
               └────────┬────────┘                 └────────┬────────┘
                        │                                   │
                        └─────────────────┬─────────────────┘
                                          │ Port 5000 (/metrics & /api/metrics)
                                          ▼
                               ┌─────────────────────┐
                               │     app-server      │ ◄──── Automated Rsync Backup
                               │    192.168.56.13    │       (Sundays at 02:00)
                               │   (Go Backend API)  │              │
                               └─────────────────────┘              ▼
                                                         ┌─────────────────────┐
                                                         │    backup-server    │
                                                         │    192.168.56.14    │
                                                         │   (Rsync + Storage) │
                                                         └─────────────────────┘
```

### Network & VM Inventory

| VM Name | IP Address | RAM | Role / Services |
| :--- | :--- | :--- | :--- |
| **`backup-server`** | `192.168.56.14` | 512 MB | SSH key generator, automated Rsync backup & restore host |
| **`load-balancer`** | `192.168.56.10` | 512 MB | HAProxy L7 Load Balancer, Stats Dashboard (:8404) |
| **`web-01`** | `192.168.56.11` | 512 MB | Docker, Nginx container serving SPA & reverse proxy |
| **`web-02`** | `192.168.56.12` | 512 MB | Redundant Docker, Nginx container serving SPA & reverse proxy |
| **`app-server`** | `192.168.56.13` | 1024 MB | Docker, Go Backend API container parsing `/proc` telemetry |

---

## Metrics API Contract

The Go backend serves live telemetry metrics via both `/metrics` and `/api/metrics` with the identical JSON structure:

```json
{
  "server": {
    "hostname": "app-server",
    "os": "linux",
    "architecture": "amd64",
    "uptimeSeconds": 12345.67
  },
  "cpu": {
    "cores": 1,
    "loadAverage1": 0.02,
    "loadAverage5": 0.05,
    "loadAverage15": 0.08
  },
  "memory": {
    "appUsedMB": 4,
    "systemTotalMB": 1024,
    "systemAvailableMB": 512,
    "usagePercent": 50.0
  },
  "clientIP": "192.168.56.1"
}
```

* **Server Identity**: Provided via HTTP response header `X-Web-Server: web-01` or `web-02`.
* **CPU Load**: Represents the 1-, 5-, and 15-minute Linux system run-queue length from `/proc/loadavg`.
* **Memory**: `appUsedMB` reflects active Go runtime heap allocations; `systemTotalMB` and `systemAvailableMB` reflect physical RAM from `/proc/meminfo`.
* **Client IP**: Derived safely through right-to-left proxy chain traversal.

---

## Quick Start & Setup

### Prerequisites
* [VirtualBox](https://www.virtualbox.org/) (>= 7.0)
* [Vagrant](https://www.vagrantup.com/) (>= 2.3)
* [Git](https://git-scm.com/)

### Starting the Infrastructure
From the project root directory, launch the entire multi-VM infrastructure with a single command:

```powershell
vagrant up
```

Vagrant automatically provisions machines in dependency order:
1. Generates an SSH key pair on `backup-server` and pre-stages it for `app-server`.
2. Starts `load-balancer`, `web-01`, and `web-02`.
3. Compiles and runs the Go backend container on `app-server:5000`.
4. Builds and runs the frontend containers on `web-01:80` and `web-02:80`.
5. Deploys and starts HAProxy on `load-balancer:80`.
6. Enforces zero-trust UFW firewall rules on all machines.

---

## Verifying the Deployment

### 1. Web Dashboard
Open `http://192.168.56.10` in your web browser. 

* The dashboard auto-refreshes every 30 seconds via `/api/metrics`.
* Look at the **Responding Web Server** indicator: as you refresh, HAProxy alternates requests between `web-01` and `web-02` (`X-Web-Server` header).

### 2. Metrics API Endpoints
Both endpoints are proxied through HAProxy directly to the Go backend and return identical telemetry JSON:
* **`http://192.168.56.10/metrics`**: Standard Prometheus-style endpoint.
* **`http://192.168.56.10/api/metrics`**: Frontend application route.
* **`http://192.168.56.13:5000/metrics`**: Direct backend API on `app-server`.

Test with `curl.exe`:
```powershell
curl.exe -i http://192.168.56.10/metrics
curl.exe -i http://192.168.56.10/api/metrics
```
Both return `HTTP/1.1 200 OK` with `Content-Type: application/json; charset=utf-8`.

### 3. HAProxy Administrative Stats
Access the statistics portal on dedicated port 8404:
* **URL:** `http://192.168.56.10:8404`
* **Access Control:** Protected by HTTP Basic Authentication via environment variables (`${HAPROXY_STATS_USER}` / `${HAPROXY_STATS_PASSWORD}`) and restricted in UFW strictly to the administrator workstation (`192.168.56.1`).

### 4. Failover Verification
Simulate an outage on `web-01`:
```powershell
vagrant ssh web-01 -c "sudo docker pause frontend"
```
Refresh `http://192.168.56.10`. HAProxy automatically detects the failed health check on `/health` and routes 100% of traffic to `web-02` with zero downtime.

Unpause to restore full cluster capacity:
```powershell
vagrant ssh web-01 -c "sudo docker unpause frontend"
```

---

## Security & Zero-Trust Firewall (UFW)

Every virtual machine implements a strict default-deny incoming policy (`ufw default deny incoming`):

* **`load-balancer` (`192.168.56.10`)**:
  * Accepts `80/tcp` (public web).
  * Accepts `8404/tcp` **only** from the administrator workstation (`192.168.56.1`).
* **`web-01` (`192.168.56.11`) & `web-02` (`192.168.56.12`)**:
  * Accepts `80/tcp` **only** from HAProxy (`192.168.56.10`). Direct external requests are dropped.
* **`app-server` (`192.168.56.13`)**:
  * Accepts `5000/tcp` **only** from `192.168.56.11` and `192.168.56.12`.
  * Accepts `22/tcp` for backup sync **only** from `192.168.56.14`.
* **`backup-server` (`192.168.56.14`)**:
  * Blocks all incoming ports except `22/tcp`.

---

## Backup & Disaster Recovery

### Automated Weekly Backup
A scheduled cron job runs every Sunday at 02:00 (`/etc/cron.d/weekly-devops-backup`) executing `/usr/local/bin/backup-devops.sh`:
* Synchronizes `/home/devops` and `/etc/hosts` from `app-server` using passwordless SSH keys (`id_rsa`).
* Includes `--delete` to purge stale files deleted from the source.
* Generates a compressed tarball: `/var/backups/devops/backup_YYYYMMDD_HHMMSS.tar.gz`.
* Prunes historical snapshots older than **30 days**.

### Manual Backup Execution
To trigger an immediate backup on demand:
```powershell
vagrant ssh backup-server -c "sudo /usr/local/bin/backup-devops.sh"
```

### Verified Disaster Recovery / Restore Procedure
To restore the latest backup back to `app-server`:
```powershell
vagrant ssh backup-server -c "sudo /usr/local/bin/restore-devops.sh"
```
Or run the raw restore command directly:
```bash
rsync -avz -e "ssh -i /home/devops/.ssh/id_rsa" /var/backups/devops/latest/devops/ devops@192.168.56.13:/home/devops/
```

---

## Load Balancing Comparison: Round-Robin vs. Least Connections

HAProxy is configured with **Round-Robin** (`balance roundrobin`) as the default.

### Live Test Evidence (Round-Robin)
Running six sequential curl requests against the load balancer demonstrates exact 50/50 alternating distribution between both backend nodes:

```text
Request 1: X-Web-Server: web-01
Request 2: X-Web-Server: web-02
Request 3: X-Web-Server: web-01
Request 4: X-Web-Server: web-02
Request 5: X-Web-Server: web-01
Request 6: X-Web-Server: web-02
```

### Comparative Analysis

| Criteria | Round-Robin (`roundrobin`) [Active] | Least Connections (`leastconn`) |
| :--- | :--- | :--- |
| **Routing Logic** | Distributes requests sequentially: $A \rightarrow B \rightarrow A \rightarrow B$. | Routes to the server with the lowest count of active TCP sessions. |
| **Ideal Workload** | Short, stateless, uniform HTTP requests (static assets, fast REST APIs). | Long-running transactions, WebSockets, variable database queries. |
| **Server Utilization** | Assumes nodes have equivalent capacity and processing times. | Dynamically adapts if one server slows down under heavier load. |
| **Recommendation for Infrastructure Insight** | **Optimal**: Dashboard metrics requests are uniform and complete in < 5ms. | Alternative: Recommended if backend shifts to streaming telemetry. |

To switch to **Least Connections**, edit `haproxy/haproxy.cfg`:
```haproxy
backend infrastructure_web_servers
    mode http
    balance leastconn
    ...
```
Then reload HAProxy:
```powershell
vagrant ssh load-balancer -c "sudo systemctl reload haproxy"
```

---

## Testing & Code Quality

Run the backend unit test suite locally:

```powershell
cd app/backend
go test -v -count=1 ./...
go vet ./...
```
All 42 unit tests verify:
* Strict `LISTEN_ADDR`, `ALLOWED_ORIGINS`, and `TRUSTED_PROXIES` environment validation.
* Pure `/proc/meminfo`, `/proc/loadavg`, and `/proc/uptime` parsing.
* Right-to-left proxy chain traversal and spoofed header defense.
* Direct `/metrics` JSON contract schema validation.
