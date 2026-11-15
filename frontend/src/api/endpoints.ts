import { MOCK_QUERY_API, QUERY_PREFIX } from '../config'
import { request } from './client'
import { mockAlertHistory, mockAlertSummary, mockFleet } from './mockFleetAlerts'
import { mockSeries } from './mockSeries'
import type {
  AddInstanceRequest,
  AlertRange,
  AlertRule,
  AlertRow,
  AlertsResponse,
  AlertStatusFilter,
  AlertSummaryResponse,
  AuthResponse,
  CreateRuleRequest,
  CreateWebhookRequest,
  FleetResponse,
  GroupCreated,
  Instance,
  InstanceCreated,
  MeResponse,
  Member,
  MetricsResponse,
  PairInstanceRequest,
  PairInstanceResponse,
  SeriesRange,
  SeriesResponse,
  Subgroup,
  UpdateRuleRequest,
  Webhook,
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

// ---- Groups & Subgroups ----
export const createGroup = (label: string) =>
  request<GroupCreated>('/groups', { method: 'POST', body: { label } })

export const deleteGroup = (groupId: number) =>
  request<{ message: string }>(`/groups/${groupId}`, { method: 'DELETE' })

export const listSubgroups = (groupId: number) => request<Subgroup[]>(`/groups/${groupId}/subgroups`)

export const createSubgroup = (groupId: number, label: string) =>
  request<Subgroup>(`/groups/${groupId}/subgroups`, { method: 'POST', body: { label } })

export const deleteSubgroup = (subgroupId: number) =>
  request<{ message: string }>(`/subgroups/${subgroupId}`, { method: 'DELETE' })

export const listMembers = (groupId: number) => request<Member[]>(`/groups/${groupId}/members`)

export const admitMember = (groupId: number, email: string, role_id: number) =>
  request<{ message: string }>(`/groups/${groupId}/admit`, { method: 'POST', body: { email, role_id } })

export const admitSubgroupMember = (subgroupId: number, email: string, role_id: number) =>
  request<{ message: string }>(`/subgroups/${subgroupId}/admit`, { method: 'POST', body: { email, role_id } })

// ---- Instance Management & Pairing ----
export const pairInstance = (subgroupId: number, body: PairInstanceRequest) =>
  request<PairInstanceResponse>(`/subgroups/${subgroupId}/instances/pair`, { method: 'POST', body })

export const toggleInstance = (subgroupId: number, instanceId: number, enabled?: boolean) =>
  request<{ i_id: number; enabled: boolean; message: string }>(
    `/subgroups/${subgroupId}/instances/${instanceId}/toggle`,
    { method: 'PATCH', body: enabled !== undefined ? { enabled } : {} },
  )

export const listLabels = (subgroupId: number, instanceId: number) =>
  request<Record<string, string>>(`/subgroups/${subgroupId}/instances/${instanceId}/labels`)

export const addLabel = (subgroupId: number, instanceId: number, key: string, value: string) =>
  request<{ label_key: string; label_value: string }>(
    `/subgroups/${subgroupId}/instances/${instanceId}/labels`,
    { method: 'POST', body: { label_key: key, label_value: value } },
  )

export const deleteLabel = (subgroupId: number, instanceId: number, key: string) =>
  request<{ message: string }>(
    `/subgroups/${subgroupId}/instances/${instanceId}/labels/${encodeURIComponent(key)}`,
    { method: 'DELETE' },
  )

// ---- Alert Rules & Webhooks ----
export const listRules = (groupId: number) => request<AlertRule[]>(`/groups/${groupId}/rules`)

export const createRule = (groupId: number, body: CreateRuleRequest) =>
  request<AlertRule>(`/groups/${groupId}/rules`, { method: 'POST', body })

export const updateRule = (groupId: number, ruleId: number, body: UpdateRuleRequest) =>
  request<AlertRule>(`/groups/${groupId}/rules/${ruleId}`, { method: 'PUT', body })

export const deleteRule = (groupId: number, ruleId: number) =>
  request<{ message: string }>(`/groups/${groupId}/rules/${ruleId}`, { method: 'DELETE' })

export const resolveAlert = (alertId: number, groupId?: number) =>
  request<{ a_id: number; status: string; message: string }>(
    groupId ? `/groups/${groupId}/alerts/${alertId}/resolve` : `/alerts/${alertId}/resolve`,
    { method: 'PUT' },
  )

export const listWebhooks = (groupId: number) => request<Webhook[]>(`/groups/${groupId}/webhooks`)

export const createWebhook = (groupId: number, body: CreateWebhookRequest) =>
  request<Webhook>(`/groups/${groupId}/webhooks`, { method: 'POST', body })

export const deleteWebhook = (groupId: number, webId: number) =>
  request<{ message: string }>(`/groups/${groupId}/webhooks/${webId}`, { method: 'DELETE' })

export const linkRuleWebhook = (groupId: number, ruleId: number, webId: number) =>
  request<{ message: string }>(`/groups/${groupId}/rules/${ruleId}/webhooks/${webId}`, { method: 'POST' })

export const unlinkRuleWebhook = (groupId: number, ruleId: number, webId: number) =>
  request<{ message: string }>(`/groups/${groupId}/rules/${ruleId}/webhooks/${webId}`, { method: 'DELETE' })

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
