# EzMonitor Frontend Dashboard

Modern, responsive web dashboard for EzMonitor built with **React 18**, **TypeScript**, **Vite**, and **TanStack Query**.

---

## Features

* **Fleet Overview:** Real-time multi-tenant view of all monitored instances, health status, and live CPU/RAM/GPU telemetry.
* **Interactive Metric Charts:** Time-series telemetry visualization powered by Recharts (1h, 6h, 24h, 7d ranges with rollup aggregation).
* **Hardware Pairing Modal:** Seamless onboarding of remote agent servers via 16-hex fingerprint and 6-digit OTP verification.
* **Alerts & Threshold Rules:** Group-level rule configuration, HMAC webhook management, and manual alert resolution.
* **Organization & RBAC Management:** Group and Subgroup hierarchy with role-based access control (Admin, Subadmin, Viewer).
* **Instance Control:** Runtime toggle to enable/disable ingestion per instance and custom label tagging.

---

## Development Setup

```bash
# Install dependencies
npm install

# Start Vite development server
npm run dev

# Build production bundle
npm run build
```

---

## Configuration

Environment configuration is read from `.env` or Vite environment variables:
* `VITE_API_URL`: Root backend URL (defaults to `window.location.origin`).
* `VITE_MOCK_QUERY_API`: Set to `false` in production to use the live PostgreSQL analytical query read engine.
