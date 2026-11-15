import { MOCK_QUERY_API, QUERY_PREFIX } from '../config'
import { request } from './client'
import { mockAlertHistory, mockAlertSummary, mockFleet } from './mockFleetAlerts'
import { mockSeries } from './mockSeries'
import type {
  AddInstanceRequest,
  AlertRange,
  AlertsResponse,
  AlertStatusFilter,
  AlertSummaryResponse,
  FleetResponse,
  AuthResponse,
  Instance,
  InstanceCreated,
  MeResponse,
  Member,
  MetricsResponse,
  SeriesRange,
  SeriesResponse,
  Subgroup,
} from '../types'

// ---- Auth ----
export const login = (email: string, password: string) =>
  request<AuthResponse>('/auth/login', { method: 'POST', body: { email, password }, auth: false })

export const register = (name: string, email: string, password: string) =>
  request<AuthResponse>('/auth/register', {
    method: 'POST',
    body: { name, email, password },
    auth: false,
  })

export const getMe = () => request<MeResponse>('/auth/me')

// ---- Instances (live under a subgroup) ----
export const listInstances = (subgroupId: number) =>
  request<Instance[]>(`/subgroups/${subgroupId}/instances`)

export const addInstance = (subgroupId: number, body: AddInstanceRequest) =>
  request<InstanceCreated>(`/subgroups/${subgroupId}/instances`, { method: 'POST', body })

export const renameInstance = (subgroupId: number, instanceId: number, label: string) =>
  request<Instance>(`/subgroups/${subgroupId}/instances/${instanceId}`, {
    method: 'PUT',
    body: { label },
  })

export const deleteInstance = (subgroupId: number, instanceId: number) =>
  request<{ message: string }>(`/subgroups/${subgroupId}/instances/${instanceId}`, {
    method: 'DELETE',
  })

// limit: 1..1000 (default 60 on the server). Newest sample first.
export const getInstanceMetrics = (subgroupId: number, instanceId: number, limit = 60) =>
  request<MetricsResponse>(`/subgroups/${subgroupId}/instances/${instanceId}/metrics`, {
    query: { limit },
  })

// ---- Groups ----
export const listSubgroups = (groupId: number) => request<Subgroup[]>(`/groups/${groupId}/subgroups`)

export const listMembers = (groupId: number) => request<Member[]>(`/groups/${groupId}/members`)

// ---- Read API (Divesh's query package) ----
// Time-bucketed series for one instance. The server picks the step (about 300 points) and serves
// multiples of 5 minutes from its rollup table, so 24h and 7d are fast.
export const getInstanceSeries = (instanceId: number, range: SeriesRange): Promise<SeriesResponse> =>
  MOCK_QUERY_API
    ? Promise.resolve(mockSeries(instanceId, range))
    : request<SeriesResponse>(`${QUERY_PREFIX}/instances/${instanceId}/series`, { query: { range } })

// One row per instance in the group with its latest sample (null if silent for `lookback`).
export const getFleet = async (groupId: number, lookback = '5m'): Promise<FleetResponse> => {
  if (MOCK_QUERY_API) return mockFleet(await listInstances(groupId))
  return request<FleetResponse>(`${QUERY_PREFIX}/fleet`, { query: { group: groupId, lookback } })
}

export const getAlertSummary = (groupId: number, range: AlertRange): Promise<AlertSummaryResponse> =>
  MOCK_QUERY_API
    ? Promise.resolve(mockAlertSummary(range))
    : request<AlertSummaryResponse>(`${QUERY_PREFIX}/groups/${groupId}/alerts/summary`, { query: { range } })

// Newest first. Pass the previous page's next_cursor as `cursor` to continue (null = last page).
export const getAlertHistory = (
  groupId: number,
  opts: { status?: AlertStatusFilter; limit?: number; cursor?: string } = {},
): Promise<AlertsResponse> => {
  const { status, limit = 20, cursor } = opts
  return MOCK_QUERY_API
    ? Promise.resolve(mockAlertHistory(status, limit, cursor))
    : request<AlertsResponse>(`${QUERY_PREFIX}/groups/${groupId}/alerts`, {
        query: { status, limit, cursor },
      })
}
