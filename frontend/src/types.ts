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
