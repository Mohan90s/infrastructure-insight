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
                                          │ Port 5000 (/api/metrics)
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
| **`load-balancer`** | `192.168.56.10` | 512 MB | HAProxy L7 Load Balancer, Stats Dashboard (:8404) |
| **`web-01`** | `192.168.56.11` | 512 MB | Nginx container serving SPA & reverse proxy |
| **`web-02`** | `192.168.56.12` | 512 MB | Redundant Nginx container serving SPA & reverse proxy |
| **`app-server`** | `192.168.56.13` | 1024 MB | Go Backend API container parsing `/proc` telemetry |
| **`backup-server`** | `192.168.56.14` | 512 MB | SSH key-based Rsync automated backup host |

---

## Quick Start & Setup

### Prerequisites
* [VirtualBox](https://www.virtualbox.org/) (>= 7.0)
* [Vagrant](https://www.vagrantup.com/) (>= 2.3)
* [Git](https://git-scm.com/)

### Automated Provisioning
Clone the repository and launch the entire multi-VM infrastructure with a single command:

```powershell
git clone <repo-url> infrastructure-insight
cd infrastructure-insight
vagrant up
```

Vagrant automatically:
1. Provisions all 5 Ubuntu 22.04 LTS virtual machines with static private IPs.
2. Generates an SSH key pair on `backup-server` and registers it with `app-server`.
3. Compiles and starts the Go backend container on `app-server:5000`.
4. Builds and starts the Nginx frontend containers on `web-01:80` and `web-02:80`.
5. Deploys and configures HAProxy on `load-balancer:80`.
6. Enforces zero-trust UFW firewall rules on all machines.

---

## Verifying the Deployment

### 1. Web Dashboard
Open `http://192.168.56.10` in your web browser. 

* The dashboard auto-refreshes every 30 seconds via `/api/metrics`.
* Look at the **Responding Web Server** indicator: as you refresh, HAProxy alternates requests between `web-01` and `web-02` (`X-Web-Server` header).

### 2. HAProxy Administrative Stats
Access the password-protected statistics portal:
* **URL:** `http://192.168.56.10:8404`
* **Username:** `admin`
* **Password:** `ChangeMeInDotEnv123!`

### 3. Failover Verification
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

* **`load-balancer`**:
  * Accepts `80/tcp` (public web).
  * Accepts `8404/tcp` **only** from the administrator workstation (`192.168.56.1`).
* **`web-01` & `web-02`**:
  * Accepts `80/tcp` **only** from HAProxy (`192.168.56.10`). Direct external requests are dropped.
* **`app-server`**:
  * Accepts `5000/tcp` **only** from `192.168.56.11` and `192.168.56.12`.
  * Accepts `22/tcp` for backup sync **only** from `192.168.56.14`.
* **`backup-server`**:
  * Blocks all incoming ports except `22/tcp`.

---

## Backup & Disaster Recovery

### Automated Weekly Backup
A scheduled cron job runs every Sunday at 02:00 (`/etc/cron.d/weekly-devops-backup`) executing `/usr/local/bin/backup-devops.sh`:
* Synchronizes `/home/devops` from `app-server` using passwordless SSH keys (`id_rsa`).
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

HAProxy is currently configured with **Round-Robin** (`balance roundrobin`) as the default.

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
go test -v ./...
go vet ./...
```
All unit tests verify:
* Strict `LISTEN_ADDR`, `ALLOWED_ORIGINS`, and `TRUSTED_PROXIES` environment validation.
* Pure `/proc/meminfo`, `/proc/loadavg`, and `/proc/uptime` parsing.
* Right-to-left proxy chain traversal and spoofed header defense.
* Direct `/metrics` JSON contract schema validation.

