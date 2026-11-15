// Types mirror the Go models in backend/internal/models (JSON tags).

export type RoleName = 'admin' | 'subadmin' | 'viewer'

export interface User {
  u_id: number
  name: string
  email: string
  date_created: string
}

export interface AuthResponse {
  token: string
  user: User
}

export interface GroupWithRole {
  g_id: number
  parent_id?: number
  label: string
  date_created: string
  role_id: number // 1 admin, 2 subadmin, 3 viewer
  role_name: RoleName
}

export interface MeResponse {
  user: User
  groups: GroupWithRole[]
}

export type InstanceStatus = 'unknown' | 'up' | 'down'

export interface Instance {
  i_id: number
  g_id: number
  label: string
  fingerprint: string
  cpu_model?: string
  ram_config?: string
  gpu_model?: string
  enabled: boolean
  status: InstanceStatus
  date_created: string
  last_seen_at?: string
}

export interface AddInstanceRequest {
  label: string
  fingerprint: string // 16 lowercase hex chars
  cpu_model?: string
  ram_config?: string
  gpu_model?: string
}

// The pairing token is only returned once, at creation time.
export interface InstanceCreated {
  i_id: number
  g_id: number
  label: string
  fingerprint: string
  token: string
  status: InstanceStatus
  enabled: boolean
  date_created: string
}

export interface MetricSample {
  recorded_at: string
  cpu_util: number
  gpu_util: number | null
  ram_util: number
  uptime: number
  net_up_kbps: number
  net_down_kbps: number
}

// Samples come back newest-first. Reverse before charting.
export interface MetricsResponse {
  i_id: number
  g_id: number
  count: number
  samples: MetricSample[]
}

// Returned by GET /groups/{id}/subgroups (verify once a group has real subgroups)
export interface Subgroup {
  g_id: number
  parent_id?: number
  label: string
  date_created: string
  instances_count?: number
  members_count?: number
}

export interface Member {
  u_id: number
  name: string
  email: string
  role_id: number
  role_name: RoleName
  // The backend currently returns a zero date here, so don't display it.
  date_joined?: string
}

// ---- Read API (backend/internal/query, GET /api/v1/instances/{id}/series) ----
// Every bucket is present. An empty bucket has samples = 0 and null metrics: draw a gap, not a zero.
export interface SeriesPoint {
  t: string
  samples: number
  cpu_avg: number | null
  cpu_max: number | null
  ram_avg: number | null
  ram_max: number | null
  gpu_avg: number | null
  gpu_max: number | null
  net_up_avg: number | null
  net_up_max: number | null
  net_down_avg: number | null
  net_down_max: number | null
  uptime: number | null
  cpu_avg_smooth: number | null
  fleet_cpu_avg: number | null
  cpu_rank: number | null
}

export interface InstanceSeries {
  i_id: number
  label: string
  points: SeriesPoint[]
}

export interface SeriesResponse {
  from: string
  to: string
  step_s: number
  rollup: boolean
  series: InstanceSeries[]
}

export type SeriesRange = '1h' | '6h' | '24h' | '7d'

// ---- Read API: fleet, alerts (backend/internal/query/store.go) ----
export interface FleetLatest {
  recorded_at: string
  cpu_util: number
  gpu_util: number | null
  ram_util: number
  uptime: number
  net_up_kbps: number
  net_down_kbps: number
}

export interface FleetRow {
  i_id: number
  g_id: number
  group_label: string
  label: string
  role_id: number
  enabled: boolean
  status: string
  last_seen_at: string | null
  labels: Record<string, string> | null
  latest: FleetLatest | null // null: no sample within the lookback
  active_alerts: number
}

export interface FleetResponse {
  instances: FleetRow[] | null
}

export interface RuleSummary {
  rule_id: number
  metric_name: string
  operator: string
  threshold_value: number
  enabled: boolean
  fired: number
  instances_affected: number
  mttr_s: number | null
  worst_value: number | null
  last_fired: string | null
  active_now: number
}

export interface AlertSummaryResponse {
  from: string
  to: string
  rules: RuleSummary[] | null
}

export interface AlertRow {
  a_id: number
  rule_id: number
  metric_name: string
  operator: string
  threshold_value: number
  i_id: number
  instance_label: string
  alert_value: number
  triggered_at: string
  resolved_at: string | null
  status: string // 'Active' | 'Resolved' (compare case-insensitively)
}

export interface AlertsResponse {
  alerts: AlertRow[] | null
  next_cursor: string | null
}

export type AlertStatusFilter = 'active' | 'resolved'
export type AlertRange = '24h' | '7d' | '30d'
