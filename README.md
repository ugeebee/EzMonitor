# EzMonitor: Lightweight Relational Metrics Monitoring System

**CS F212 Database Systems — Final Course Project Submission**  
**Team 4 (Lab Section P1) — BITS Pilani**  
**GitHub Repository:** [https://github.com/ugeebee/EzMonitor](https://github.com/ugeebee/EzMonitor)  
**Final Submission Tag:** `final`  
**Final Commit Hash:** `c5ca97cd2e712e9492cffd7db4258155de640822`  

---

## Submission Deliverables

As required by the course submission guidelines, the following deliverables are included:
1. **GitHub Repository:** Complete source code (backend, frontend, database migrations, seed data, Docker deployment, and daemons).
2. **Database Design Document (PDF, 5 pages):** [`EzMonitor_Database_Design_Document.pdf`](./EzMonitor_Database_Design_Document.pdf)
3. **Completed Demo Sheet (PDF, 1 page):** [`EzMonitor_Demo_Sheet.pdf`](./EzMonitor_Demo_Sheet.pdf)

---

## Project Overview

EzMonitor is a high-performance, multi-tenant relational metrics monitoring platform designed to collect, aggregate, evaluate, and visualize time-series hardware telemetry (CPU, RAM, GPU, Uptime, Network) across distributed compute nodes using **PostgreSQL 16+** as the central single source of truth.

Unlike conventional monitoring stacks that require specialized NoSQL time-series databases (e.g. Prometheus, InfluxDB), EzMonitor proves that a relational database can handle high-frequency time-series fact data alongside multi-tenant configuration state while providing:
* **Strict ACID transaction guarantees** and foreign key referential integrity across tenants and nodes.
* **Declarative daily range partitioning** eliminating table bloat and enabling plan-time partition pruning.
* **In-database automated alerting** using transition-table statement triggers (`REFERENCING NEW TABLE`) and asynchronous `pg_notify` inter-process dispatch.
* **Controlled analytical rollup cache (`metrics_rollup_5m`)** storing exact additive aggregates (`SUM`, `n`, `MIN`, `MAX`) with trigger-based watermark invalidation and transactional advisory locks.
* **High-efficiency analytical queries** utilizing Common Table Expressions (CTEs), lateral joins, window functions (`lag()`, `dense_rank()`, `cume_dist()`, moving averages), gap-filling via `generate_series()`, and keyset pagination.

---

## Team Members & Contributions

**Team 4 (Lab Section P1)**
* **Utkarsh Gopal Bhartariya (2024A4PS0656H):** High-frequency ingestion pipeline (`POST /api/v1/ingest`), agent scraper daemon, WebSocket OTP pairing hub, and hierarchical subgroups architecture.
* **Harshavardhan Marmamula (2024A4PS0560H):** Relational schema design (3NF/BCNF normalization), RBAC privilege isolation (`ezm_ingest`, `ezm_api`, `ezm_readonly`), database migrations, and constraint infrastructure.
* **Sanjay Varghese Sunil (2024A4PS0623H):** React frontend visualization dashboards, real-time fleet monitoring, interactive Recharts time-series graphs, and alert management console.
* **Diveshraju M (2024A4PS0727H):** 5-minute rollup aggregation engine, database index tuning, query performance benchmarking, and analytical read API (`series`, `fleet_status`, `hotspots`, `events`, `alert_summary`, `alert_history`).
* **Asnani Aayush Pratap (2024A4PS0757H):** Alerting engine, in-database evaluation triggers, partial unique indexes, `pg_notify` event pipeline, and Go HMAC-SHA256 webhook worker daemon.

---

## System Architecture

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

---

## Technology Stack

* **Database Tier:** PostgreSQL 16+ (Declarative Partitioning, Triggers, Functions, Keyset Indexes, Advisory Locks)
* **Backend Tier:** Go 1.22+ (Chi Router, `pgx/v5` connection pool, Gorilla WebSocket, HMAC-SHA256 signing)
* **Frontend Tier:** React 18, TypeScript, Vite, TanStack Query, Recharts, Tailwind CSS
* **Infrastructure:** Docker Compose, Caddy Reverse Proxy (Automated SSL & HTTP/2 routing)

---

## Database Scope & Technical Specifications

* **Number of Tables:** 13 tables (11 core relational tables in 3NF/BCNF + 2 analytical rollup and cache state tables):
  1. `roles`: Catalog of RBAC permissions (1=Admin, 2=Subadmin, 3=Viewer).
  2. `users`: User identity with unique `citext` email and bcrypt credentials.
  3. `groups`: Organization tenant root with self-referencing `parent_id` for nested subgroups.
  4. `user_group_roles`: M:N tenant membership resolving user roles per group.
  5. `instances`: Enrolled compute instances with 16-hex fingerprints and SHA-256 token hashes.
  6. `instance_labels`: Operational metadata tags (`env=prod`, `rack=r1`).
  7. `metrics_data`: High-frequency telemetry fact table, declaratively partitioned by UTC day.
  8. `alert_rules`: Threshold configuration rules per group and metric kind.
  9. `alert_logs`: Incident history with status transitions (`active`, `resolved`).
  10. `webhooks`: External notification channels with HMAC signing secrets.
  11. `alert_rule_webhooks`: Composite M:N link enforcing that rules only notify webhooks in the same tenant.
  12. `metrics_rollup_5m`: Derived 5-minute pre-aggregated rollup rows (`n`, `SUM`, `MIN`, `MAX`).
  13. `metrics_rollup_state`: Singleton tracking cache `watermark` and `dirty_from` timestamps.
* **Database Records:**
  * **Default Development Seed:** > 403,250 records (403,200 metric samples spanning 7 days across 10 enabled instances at 15s intervals, plus users, groups, instances, rules, and alert logs).
  * **Benchmark Profile:** Capable of generating > 6,050,000 records (7 days at 1-second scrape interval).

---

## Application Workflows

EzMonitor implements four end-to-end integrated workflows:

### Workflow 1: Dynamic Hardware Agent Pairing & Fleet Onboarding
1. An administrator clicks **+ Pair Instance** in the dashboard and selects an organization group or subgroup.
2. On the remote server, the administrator launches:
   ```bash
   ./agent pair --server http://localhost:80
   ```
3. The agent initiates a secure WebSocket connection and displays a 16-hex hardware fingerprint and 6-digit one-time password (OTP).
4. The administrator enters the credentials into the dashboard modal.
5. The backend validates the handshake in memory, then executes an **atomic SQL transaction**:
   * Inserts the new instance into `instances` (`enabled = true`, `token_hash = SHA256(token)`).
   * Inserts default labels into `instance_labels`.
6. The WebSocket hub sends the ingestion token to the agent, which immediately begins automated telemetry collection.

### Workflow 2: High-Frequency Metric Ingest & Rollup Invalidation
1. Monitored nodes collect hardware telemetry (CPU, GPU, RAM, Uptime, Network) and submit batches via `POST /api/v1/ingest`.
2. The Go backend verifies the bearer token via SHA-256 hash lookup against `instances`.
3. An atomic batch transaction executes an idempotent upsert:
   ```sql
   INSERT INTO metrics_data (...) SELECT ... FROM unnest(...)
   ON CONFLICT (i_id, recorded_at) DO UPDATE SET ...;
   ```
4. **Trigger T1 (`t1_reject_disabled_instances`)** fires `BEFORE INSERT/UPDATE` on `metrics_data`: if the instance is disabled, it raises an exception (SQLSTATE 23514), rolling back the batch.
5. **Rollup Invalidation Trigger (`rollup_invalidate`)** fires `AFTER INSERT/UPDATE/DELETE`: if an inserted timestamp is older than the rollup cache watermark, it lowers `metrics_rollup_state.dirty_from` to ensure analytical accuracy.

### Workflow 3: In-Database Alert Evaluation & HMAC Webhook Dispatch
1. During batch ingestion into `metrics_data`, **Trigger T2 (`t2_evaluate_alerts_on_insert`)** executes `AFTER INSERT FOR EACH STATEMENT` using transition tables (`REFERENCING NEW TABLE`).
2. T2 joins incoming samples against `alert_rules` for that instance's tenant.
3. If a metric crosses a threshold (e.g. CPU > 90%), T2 inserts an active incident into `alert_logs`:
   ```sql
   INSERT INTO alert_logs (rule_id, i_id, g_id, alert_value, triggered_at, status)
   VALUES (...)
   ON CONFLICT (rule_id, i_id) WHERE status = 'active' DO NOTHING;
   ```
   *Partial unique index `alert_logs_one_active` strictly prevents duplicate active alerts for the same rule and instance (BR7).*
4. **Trigger T3 (`t3_notify_alert_fired`)** executes on `alert_logs` and broadcasts an event via PostgreSQL `pg_notify('alert_fired', payload)`.
5. The background `alertworker` daemon receives the notification, signs the payload with HMAC-SHA256 (`X-EzMonitor-Signature`), and dispatches it to configured external webhook endpoints with exponential backoff retry.

### Workflow 4: Multi-Resolution Analytical Querying & Dashboard
1. Users view the dashboard for fleet health, historical series, or anomaly hotspots.
2. The query engine (`backend/internal/query`) constructs optimized SQL:
   * **`series.sql`:** Reads pre-aggregated 5-minute rollup rows (`metrics_rollup_5m`) for data before `least(watermark, dirty_from)` and raw partitioned rows (`metrics_data`) for recent telemetry via `UNION ALL`. Applies moving averages and gap-filling with `generate_series()`.
   * **`fleet_status.sql`:** Executes `LEFT JOIN LATERAL ... ORDER BY recorded_at DESC LIMIT 1` (backward primary key scan) to fetch the latest scrape per instance in $O(\text{instances})$ time.
   * **`hotspots.sql`:** Computes 95th percentile metrics (`percentile_cont(0.95) WITHIN GROUP`) in a session with `SET LOCAL work_mem = '32MB'`.
   * **`alert_history.sql`:** Fetches incident logs using keyset cursor pagination `(triggered_at, a_id) < ($3, $4)` in $O(1)$ time.

---

## Quick Start & Reproducibility Guide

Follow these steps to run and verify the complete application and database stack from scratch:

### 1. Environment Configuration
```bash
cp .env.example .env
```
*(Default passwords and port configuration are pre-set for local development).*

### 2. Run Database Migrations
Applies all migrations (`000001` through `000005`) in monotonic sequence:
```bash
docker compose up migrate
```
*Verifies: 13 tables, partition functions, triggers T1/T2/T3, partial unique indexes, and RBAC roles.*

### 3. Seed Development Fixtures (> 403,000 Records)
```bash
docker compose run --rm seed
```
This script executes:
* `db/seed/dev_seed.sql`: Seeds default roles, users, tenants (`Team 4 Lab`, `Other Tenant`), 11 instances, alert rules, and webhooks.
* `db/seed/backfill_metrics.sql`: Generates **403,200 time-series samples** across 10 enabled instances spanning 7 days at 15-second intervals.
* `refresh_metrics_rollup()`: Pre-builds the 5-minute rollup cache.

**Default Login Credentials:**
* **Admin:** `admin@ezmonitor.dev` / `admin123`
* **Subadmin:** `editor@ezmonitor.dev` / `editor123`
* **Viewer:** `viewer@ezmonitor.dev` / `viewer123`

### 4. Start Full Application Stack
```bash
docker compose --profile app up -d --build
```
Access the services at:
* **Frontend Web Dashboard:** `http://localhost:80`
* **Backend API & Pairing Hub:** `http://localhost:80/api/v1`
* **PostgreSQL Database:** `localhost:5432` (`ezmonitor`)

---

## In-Lab Demonstration & Verification Guide

During the 7-minute presentation and viva, evaluators can inspect and verify the DBMS features using direct SQL or UI actions:

### 1. Verify Database Scope & Record Counts
Connect to PostgreSQL:
```bash
docker compose exec db psql -U ezm_owner -d ezmonitor
```
Inspect table row counts:
```sql
SELECT count(*) AS total_samples FROM metrics_data;
-- Output: > 403,200 rows

SELECT count(*) AS total_rollup_buckets FROM metrics_rollup_5m;
-- Output: ~20,160 rows

SELECT i_id, label, enabled, status FROM instances ORDER BY i_id;
-- Output: 11 instances (dev-box-10 is DISABLED to test Trigger T1)
```

### 2. Verify Trigger T1: Disabled Instance Rejection (BR5)
Attempt to insert telemetry for disabled instance `10`:
```sql
INSERT INTO metrics_data (i_id, recorded_at, cpu_util, ram_util, uptime, net_up_kbps, net_down_kbps)
VALUES (10, now(), 50.0, 50.0, 1000, 100.0, 100.0);
-- Expected result: ERROR: instance is disabled (SQLSTATE 23514)
```

### 3. Verify Trigger T2 & T3: In-Database Alert Trigger & `pg_notify` (BR7)
Listen for notifications and insert a threshold-breaching sample (> 90% CPU):
```sql
-- In terminal 1:
LISTEN alert_fired;

-- In terminal 2:
INSERT INTO metrics_data (i_id, recorded_at, cpu_util, ram_util, uptime, net_up_kbps, net_down_kbps)
VALUES (1, now(), 98.5, 60.0, 5000, 200.0, 500.0);

-- Output in terminal 1: Asynchronous notification "alert_fired" received with JSON payload!
```

### 4. Verify Analytical Rollup Performance (280x Speedup)
Compare raw vs rollup aggregation performance:
```sql
-- 1. Querying raw metrics_data over 7 days for 10 instances:
EXPLAIN ANALYZE
SELECT date_trunc('hour', recorded_at), avg(cpu_util)
FROM metrics_data WHERE recorded_at >= now() - interval '7 days' GROUP BY 1;
-- Runtime: ~780 ms - 6,960 ms (scans 498,000+ buffers)

-- 2. Querying pre-aggregated metrics_rollup_5m over the same window:
EXPLAIN ANALYZE
SELECT date_bin('1 hour', bucket, timestamptz 'epoch'), sum(cpu_sum)/sum(n)
FROM metrics_rollup_5m WHERE bucket >= now() - interval '7 days' GROUP BY 1;
-- Runtime: ~25 ms (scans ~3,500 buffers — 280x faster!)
```

---

## Repository Structure

```
EzMonitor/
├── backend/
│   ├── cmd/
│   │   ├── agent/            # Hardware metrics collector CLI
│   │   ├── alertworker/      # LISTEN alert_fired HMAC webhook daemon
│   │   └── server/           # Go Chi REST API & WebSocket pairing hub
│   └── internal/
│       ├── handlers/         # Ingest, pairing, groups, webhooks, rules APIs
│       └── query/            # Analytical query engine & embedded SQL
│           └── sql/          # series, fleet_status, hotspots, alert_summary
├── db/
│   ├── migrations/           # Monotonic migrations (000001 to 000005)
│   ├── seed/                 # dev_seed.sql and backfill_metrics.sql
│   ├── bench/                # EXPLAIN ANALYZE benchmark reproduction scripts
│   ├── QUERIES.md            # Query designs, explain plans, and measurements
│   └── README.md             # Database architecture and schema notes
├── frontend/                 # React 18 / TypeScript / Vite web dashboard
│   └── src/
│       ├── pages/            # Fleet, Group, Instance, Alerts, Hotspots pages
│       └── components/       # Metric charts, modals, instance tables
├── scripts/
│   ├── generate_demo_sheet.py   # PDF generator for Demo Sheet (1 page)
│   └── generate_design_doc.py   # PDF generator for Design Document (5 pages)
├── EzMonitor_Database_Design_Document.pdf  # Final Design Document (5 pages)
├── EzMonitor_Demo_Sheet.pdf                # Final Demo Sheet (1 page)
├── docker-compose.yaml       # Full-stack container orchestration
└── Caddyfile                 # Reverse proxy & automated routing
```
