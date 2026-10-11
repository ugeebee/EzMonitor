# EzMonitor: Lightweight Metrics Monitoring System

## 📖 Project Description
EzMonitor is a multi-tenant, relational metrics monitoring platform designed to collect, store, and visualize time-series hardware measurements (CPU, GPU, RAM, Uptime, Network) from distributed infrastructure. 

Unlike traditional time-series database (TSDB) stacks, EzMonitor enforces state, consistency, and role-based access control (RBAC) directly within a unified PostgreSQL database using strict relational constraints and triggers. It is designed to handle high-frequency metric ingestion and automated threshold-based alerting without the overhead of specialized NoSQL engines.

### 🛠 Technology Stack
* **Database:** PostgreSQL (Version 16+)
* **Backend API / Ingestion:** Go (Golang)
* **Frontend:** React (TypeScript, Vite / Next.js)
* **Infrastructure:** Docker, Docker Compose, Caddy (Reverse Proxy)

## 📊 Current Progress (Mid-Sem Evaluation)
* **Database Design:** Finalized the 10-table relational schema (3NF/BCNF) including the RBAC implementation.
* **Documentation:** Completed the ER/EER diagram and the comprehensive Database Design Document mapping entities, cardinalities, and normalization justifications.
* **Database Scripts:** Drafted the initial Data Definition Language (DDL) scripts for table creation, primary/foreign keys, and data constraints.
* **Application Architecture:** Defined the end-to-end Go/React architecture and drafted the high-level API routing structure.

## 👥 Team Members & Mid-Sem Contributions
**Team 4 (P1)**


