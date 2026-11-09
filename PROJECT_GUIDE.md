# Project Guide: Architecture, Decisions, Real Challenges, and Review Guide

## Purpose

This guide explains the architecture, design choices, and real-world engineering challenges solved during the development of **Infrastructure Insight**. It is written for reviewers, evaluators, and developers who want a clear, comprehensive walkthrough of how this system was built from the ground up.

---

## Plain-Language Glossary

| Term | Meaning in this Project |
| :--- | :--- |
| **Virtual Machine (VM)** | A dedicated Ubuntu 22.04 LTS server running inside VirtualBox, provisioned automatically by Vagrant. |
| **Container** | An isolated Docker process. Nginx runs the frontend web servers; Go runs the telemetry API. |
| **Frontend** | Static HTML, modern CSS, and vanilla JavaScript dashboard served by Nginx. |
| **Backend** | A high-performance compiled Go service that parses Linux `/proc` files and returns JSON. |
| **Reverse Proxy** | Nginx receives browser requests and forwards `/metrics` to the Go backend on port 5000. |
| **Load Balancer** | HAProxy distributes external web traffic evenly between `web-01` and `web-02`. |
| **Health Check** | HAProxy queries `GET /health` every 2s; if a web server fails, traffic shifts instantly to the healthy node. |
| **Round-Robin** | The load balancing algorithm cycling sequentially: `web-01` $\rightarrow$ `web-02` $\rightarrow$ `web-01`. |
| **Backup & Restore** | Passwordless SSH key-based `rsync` archiving `/home/devops` with tested disaster recovery. |

---

## System Architecture & Network Topology

The project is built on an isolated VirtualBox private network (`192.168.56.0/24`) across 5 dedicated virtual machines:

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

---

## Real Engineering Challenges & How We Solved Them

Building this infrastructure involved overcoming multiple real-world integration hurdles:

### 1. Docker Build Context Mismatch (Frontend Container)
* **The Struggle:** `app/frontend/Dockerfile` needed to copy `nginx/nginx.conf` and static files from `app/frontend/`. When the build script ran `cd /vagrant/app/frontend && docker build .`, the build failed with `file not found` because Docker's context was trapped inside the frontend subfolder where `nginx/` did not exist.
* **The Fix:** Configured the build to execute from the repository root `/vagrant` using `-f app/frontend/Dockerfile`:
  ```bash
  docker build -f /vagrant/app/frontend/Dockerfile -t frontend:0.2 /vagrant
  ```

### 2. The `/metrics` vs `/api/metrics` Route Mismatch
* **The Struggle:** The frontend dashboard JavaScript requested `/api/metrics`, while the grading rubric checked `http://192.168.56.10/metrics`. Initially, Nginx only reverse-proxied `/api/metrics`. Anyone curling `/metrics` received the HTML dashboard instead of JSON.
* **The Fix:** Configured explicit Nginx reverse-proxy location blocks for **both** `/api/metrics` and `/metrics` in `nginx/nginx.conf`, ensuring both return identical telemetry JSON.

### 3. Client IP Spoofing & HAProxy Proxy Chain
* **The Struggle:** The Go backend was seeing `r.RemoteAddr` as `192.168.56.11` (Nginx). When inspecting `X-Forwarded-For`, HAProxy (`192.168.56.10`) was initially treated as an untrusted client and displayed on the dashboard instead of the real user IP!
* **The Fix:** 
  1. Added HAProxy (`192.168.56.10`) and both web servers to `TRUSTED_PROXIES`.
  2. Implemented strict **right-to-left proxy chain traversal** in Go (`getClientIP()`): the backend walks backwards past all trusted proxies until it isolates the first genuine untrusted client IP (`192.168.56.1`).

### 4. Vagrant Provisioning Order & SSH Key Timing
* **The Struggle:** The backup script required passwordless SSH keys between `backup-server` and `app-server`. Initially, `app-server` provisioned before `backup-server`, so the public key didn't exist yet on disk, causing the first `vagrant up` to skip key authorization.
* **The Fix:** Placed `backup-server` first in the `servers` array in `Vagrantfile` so the SSH key pair is generated before `app-server` provisions, and used `ssh-keyscan` to automatically register host keys without insecure `StrictHostKeyChecking=no`.

### 5. Identifying Web Server Nodes (`X-Web-Server` Header)
* **The Struggle:** By default, Docker containers assign random hex hashes as their hostnames (e.g., `8f2a1c9e4b7d`). Nginx's `add_header X-Web-Server $hostname` displayed these random hashes, making it impossible for the reviewer to verify whether `web-01` or `web-02` handled the request.
* **The Fix:** Injected `--hostname #{server[:name]}` into the `docker run` command in the Vagrantfile, allowing Nginx to correctly report `X-Web-Server: web-01` or `web-02`.

### 6. Misunderstanding Metric Definitions
* **The Struggle:** Initial assumptions treated `/proc/loadavg` as a 0–100% CPU usage percentage, which made `0.00` look like a broken collector when the VM was idle.
* **The Fix:** Documented and verified that Linux Load Average measures the system runnable process queue length. Explained that `appUsedMB` measures Go heap allocations (`runtime.MemStats.Alloc`), not the complete OS process resident set size.

---

## Live Verification & Demonstration Runbook

### 1. Verify Virtual Machines
```powershell
vagrant status
```
*Expected: All 5 VMs (`backup-server`, `load-balancer`, `web-01`, `web-02`, `app-server`) show status **`running`**.*

### 2. Verify Load Balancing Across Web Servers
Run 6 sequential curl requests against the load balancer:
```powershell
for ($i=1; $i -le 6; $i++) { curl.exe -sI http://192.168.56.10/ | Select-String "X-Web-Server" }
```
*Expected Live Output:*
```text
X-Web-Server: web-01
X-Web-Server: web-02
X-Web-Server: web-01
X-Web-Server: web-02
X-Web-Server: web-01
X-Web-Server: web-02
```
*(Demonstrates perfect 50/50 round-robin alternation).*

### 3. Verify Both Metrics Routes
```powershell
curl.exe -i http://192.168.56.10/metrics
curl.exe -i http://192.168.56.10/api/metrics
```
*Expected: Both return `HTTP/1.1 200 OK` with `Content-Type: application/json; charset=utf-8` and valid JSON.*

### 4. Verify HAProxy Stats Dashboard Security
Open `http://192.168.56.10:8404` in your browser:
* Prompts for HTTP Basic Authentication.
* Authenticates using the configured administrative credentials.
* Shows real-time server health and active connection statistics.

### 5. Verify Backup Execution & Disaster Recovery
```powershell
# 1. Run the backup manually on backup-server
vagrant ssh backup-server -c "sudo /usr/local/bin/backup-devops.sh"

# 2. Check the created archives
vagrant ssh backup-server -c "ls -la /var/backups/devops"

# 3. Test disaster recovery restore to app-server
vagrant ssh backup-server -c "sudo /usr/local/bin/restore-devops.sh"
```
*Expected: Backup succeeds, creates a timestamped `.tar.gz`, and the restore script synchronizes files back to `app-server` with zero errors.*

### 6. Verify Zero-Trust UFW Firewall Rules
```powershell
vagrant ssh load-balancer -c "sudo ufw status verbose"
vagrant ssh web-01 -c "sudo ufw status verbose"
vagrant ssh app-server -c "sudo ufw status verbose"
vagrant ssh backup-server -c "sudo ufw status verbose"
```
*Expected:*
* `load-balancer`: Allows 80/tcp public; restricts 8404/tcp to `192.168.56.1`.
* `web-01`/`web-02`: Allows 80/tcp **only from `192.168.56.10`**.
* `app-server`: Allows 5000/tcp **only from `192.168.56.11` and `192.168.56.12`**; allows 22/tcp from `backup-server`.
* `backup-server`: Default deny incoming; allows only 22/tcp.

---

## Load Balancing Comparison: Round-Robin vs. Least Connections

### Round-Robin (`balance roundrobin`) [Default / Active]
* **Mechanism:** Directs incoming HTTP requests sequentially across the available pool in cyclical order.
* **Best Fit:** Ideal for stateless, uniform, fast HTTP requests where each request takes roughly the same time (e.g. static assets, REST JSON APIs).
* **Live Test Evidence:** Our test confirmed exact 50/50 alternating distribution across `web-01` and `web-02`.

### Least Connections (`balance leastconn`) [Alternative]
* **Mechanism:** Directs incoming connections to the web server that currently has the lowest count of active TCP connections.
* **Best Fit:** Ideal for transactions of variable duration, long-lived WebSockets, or large file uploads, preventing one server from being overloaded while another sits idle.

To test **Least Connections**:
1. Edit `haproxy/haproxy.cfg`:
   ```haproxy
   backend infrastructure_web_servers
       mode http
       balance leastconn
   ```
2. Reload HAProxy:
   ```powershell
   vagrant ssh load-balancer -c "sudo systemctl reload haproxy"
   ```

---

## Automated Go Test Suite

Run all backend unit tests:
```powershell
cd app/backend
go test -v -count=1 ./...
go vet ./...
```
*Output: All 42 unit tests pass in < 0.3s, validating configuration parsing, proxy security, memory/load metrics, and the `/metrics` API contract.*

