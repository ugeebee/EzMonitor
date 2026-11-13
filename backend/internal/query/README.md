# Read API (dashboards)

All routes are `GET`, JSON, and scoped to the logged-in user's groups. Status codes: 401 when not
logged in, 404 for instances or groups the user cannot see (also for IDs that do not exist),
400 for bad parameters, 504 when a query exceeds its 5 s budget. Errors look like `{"error": "..."}`.

Times are RFC 3339 UTC. Time range parameters, shared by every route that takes a window:

- `range=24h` (default): ends now. Accepts Go durations (`15m`, `6h`) and days (`7d`).
- `from=...&to=...`: explicit bounds. `to` defaults to now. `from` and `range` are mutually exclusive.

## Routes

| route | params | notes |
|---|---|---|
| `/api/v1/fleet` | `group`, `lookback=5m` (≤ 24h) | one row per visible instance with its latest sample (`latest: null` if silent for `lookback`), labels, open alerts, your `role_id` (1 admin, 2 editor, 3 viewer) |
| `/api/v1/instances/{id}/series` | range, `step` | chart data for one instance |
| `/api/v1/groups/{id}/series` | range, `step`, `label=key:value` (repeatable, all must match) | one series per instance in the group |
| `/api/v1/groups/{id}/hotspots` | range (≤ 24h), `metric=cpu_util`, `limit=10` | instances ranked by p95 of `metric` |
| `/api/v1/instances/{id}/events` | range (≤ 7d), `max_gap=30s` | gaps and reboots, plus `availability` (0..1) |
| `/api/v1/groups/{id}/alerts/summary` | range | per rule: fired, instances affected, MTTR, worst value, active now |
| `/api/v1/groups/{id}/alerts` | `status=active\|resolved`, `limit=50` (≤ 500), `cursor` | newest first; pass `next_cursor` back as `cursor` for the next page (`null` = last page) |

`metric` is one of `cpu_util gpu_util ram_util uptime net_up_kbps net_down_kbps`.

### `step` for series

Omit it and the server picks a step that gives about 300 points (24h → 5m, 7d → 1h, 30d → 3h).
Otherwise give a whole number of seconds (`15s`, `5m`, `1h`). At most 2,000 points. A step that is a
multiple of 5 minutes is served from the 5-minute rollup (`"rollup": true`) and is fast for any range.
Other steps scan raw samples and are limited to 24 h windows. `from`/`to` are widened to step
boundaries, and the response echoes the aligned values.

```json
{
  "from": "2026-11-10T21:00:00Z", "to": "2026-11-11T21:05:00Z", "step_s": 300, "rollup": true,
  "series": [
    { "i_id": 3, "label": "dev-box-03",
      "points": [
        { "t": "2026-11-10T21:00:00Z", "samples": 300,
          "cpu_avg": 41.2, "cpu_max": 63.0, "ram_avg": 51.0, "ram_max": 52.9,
          "gpu_avg": null, "gpu_max": null,
          "net_up_avg": 210.4, "net_up_max": 401.2, "net_down_avg": 830.1, "net_down_max": 1502.7,
          "uptime": 3888000,
          "cpu_avg_smooth": 40.8, "fleet_cpu_avg": 38.9, "cpu_rank": 2 }
      ] }
  ]
}
```

Every bucket is present. A bucket with no data has `"samples": 0` and `null` metrics, so draw it
as a gap (Recharts: `connectNulls={false}`), not a zero. `gpu_*` is `null` for machines without an
NVIDIA GPU. `cpu_avg_smooth` is a 3-bucket moving average, `fleet_cpu_avg` the mean across the
instances in the response, and `cpu_rank` is 1 for the busiest instance in that bucket.

## Wiring (backend `main`)

```go
import (
    "github.com/jackc/pgx/v5/stdlib"
    "github.com/ugeebee/EzMonitor/backend/internal/query"
)

apiDB := stdlib.OpenDBFromPool(apiPool) // pool built from API_DATABASE_URL (ezm_api role)
h := &query.Handler{
    Store: query.NewStore(apiDB),
    User:  sessionUserID, // func(*http.Request) (int64, bool), from the auth middleware
}
h.Register(mux)
```

The store only uses `database/sql`, so it works with any PostgreSQL driver.

## Tests

```bash
export EZM_TEST_DATABASE_URL=postgres://ezm_owner:<pw>@localhost:5432/ezmonitor?sslmode=disable
export EZM_TEST_API_DATABASE_URL=postgres://ezm_api:<pw>@localhost:5432/ezmonitor?sslmode=disable  # optional
go test ./internal/query/
```

Without `EZM_TEST_DATABASE_URL` only the unit tests run. The integration tests create their own
users, groups and instances and delete them afterwards. They do advance the shared rollup watermark,
so use a dev database.
