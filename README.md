# EzMonitor: Lightweight Metrics Monitoring System

EzMonitor is a high-performance, multi-tenant relational metrics monitoring platform designed to collect, aggregate, evaluate, and visualize time-series hardware telemetry (CPU, RAM, GPU, Uptime, Network) across distributed infrastructure using PostgreSQL 16+ as the central source of truth.

---

## Architecture Overview

```
+--------------------+        +-----------------------+        +----------------------+
|  Monitored Node    |        |     Edge / Caddy      |        |   EzMonitor Core     |
|  (Agent CLI)       |        |    Reverse Proxy      |        |   Backend (Go Chi)   |
|                    |        |                       |        |                      |
|  1. WebSocket Pair |  WSS   |  :80 / :443           |  HTTP  |  - Ingest Pipeline   |
|     (OTP Handshake)| ------>|  - /api/v1/* -------->|------->|  - Query Read API    |
|  2. Ingest Stream  |  HTTP  |  - /* (Frontend) ---->|------->|  - Pairing Hub       |
|     (Batch POST)   | ------>|                       |        |  - RBAC Middleware   |
+--------------------+        +-----------------------+        +----------+-----------+
                                                                          |
                                                                          | SQL / LISTEN
                                                                          v
+--------------------+        +-----------------------+        +----------------------+
| React Dashboard    |        |  Alert Worker Daemon  |        |    PostgreSQL 16+    |
| (Vite/TypeScript)  |        |  (Go alertworker)     |        |                      |
|                    |        |                       |        |  - Metrics Partition |
|  - Fleet Table     |        |  - LISTEN alert_fired |<-------|  - Rollup (5m)       |
|  - Series Charts   |        |  - HMAC Webhook POST  |        |  - RBAC Tables       |
|  - Alerts & Rules  |        +-----------------------+        |  - Triggers & Alerts |
+--------------------+                                         +----------------------+
```

### Key Subsystems
1. **Relational Database & Triggers (`db/migrations`):**
   - 3NF/BCNF relational architecture with partitioned `metrics_data`.
   - Automated 5-minute rollup aggregation tables and refresh state.
   - Database triggers for business rules: rejecting disabled instances, deduplicating active alerts, and queueing webhook events.
2. **Backend API Server (`backend/cmd/server`):**
   - High-throughput ingestion endpoint (`POST /api/v1/ingest`).
   - Query engine with lateral joins, gap-filling, and window functions (`backend/internal/query`).
   - Dynamic WebSocket pairing hub (`/api/v1/agent/pair/ws`) for secure hardware onboarding.
   - Complete CRUD endpoints for Groups, Subgroups, RBAC Membership, Alert Rules, Webhooks, and Instance Labels.
3. **Alert Worker Daemon (`backend/cmd/alertworker`):**
   - PostgreSQL `LISTEN alert_fired` event-driven worker.
   - Dispatches HTTP webhooks with HMAC-SHA256 signatures (`X-EzMonitor-Signature`) and exponential backoff retry.
4. **Agent CLI (`backend/cmd/agent`):**
   - One-line hardware pairing (`ezmonitor agent pair`) with 6-digit OTP.
   - Lightweight system metrics collector and batch streamer.
5. **Frontend Dashboard (`frontend/`):**
   - Modern React / TypeScript / Vite interface.
   - Real-time fleet health status, interactive metric graphs, and alert management.

---

## Technology Stack

* **Database:** PostgreSQL (Version 16+) with native partitioning & triggers
* **Backend:** Go 1.22+ (Chi Router, pgx/v5 connection pooling, Gorilla WebSocket)
* **Frontend:** React 18, TypeScript, Vite, TanStack Query, Recharts
* **Infrastructure:** Docker Compose, Caddy Reverse Proxy, Cloudflare Tunnel

---

## Quick Start (Docker Deployment)

### 1. Environment Configuration
Copy `.env.example` to `.env` and configure passwords and secrets:
```bash
cp .env.example .env
```

### 2. Run Database Migrations
```bash
docker compose up migrate
```

### 3. Seed Development Fixtures (Optional)
```bash
docker compose run seed
```

### 4. Start Application Stack
```bash
docker compose --profile app up -d --build
```
The application will be accessible at:
* **Frontend Dashboard:** `http://localhost:80` (or configured `HTTP_PORT`)
* **API Endpoints:** `http://localhost:80/api/v1`

---

## Agent Pairing & Monitoring Workflow

1. From the Dashboard, navigate to a group or subgroup and click **+ Pair Instance**.
2. On your target server, launch the agent pairing command:
   ```bash
   ./agent pair --server https://ezmonitor.ugbhartariya.com
   ```
3. Enter the displayed 16-character hardware fingerprint and 6-digit OTP into the dashboard modal.
4. The backend verifies the handshake, issues an ingestion token, and the agent automatically begins streaming telemetry metrics.

---

## Team Members & Contributions
**Team 4 (P1)**
* **Utkarsh Gopal Bhartariya (2024A4PS0656H):** High-frequency ingestion pipeline, agent scraper daemon, WebSocket OTP pairing hub, and subgroups architecture.
* **Harshavardhan Marmamula (2024A4PS0560H):** Relational schema design, RBAC privilege isolation, and migration infrastructure.
* **Sanjay Varghese Sunil (2024A4PS0623H):** Frontend visualization dashboards, fleet monitoring, and alert management interfaces.
* **Diveshraju M (2024A4PS0727H):** Rollup aggregation engine, database index tuning, and analytical query read API.
* **Asnani Aayush Pratap (2024A4PS0757H):** Alerting engine, database trigger dispatchers, and HMAC webhook worker.
