import { request } from './client'
import type {
  AddInstanceRequest,
  AuthResponse,
  Instance,
  InstanceCreated,
  MeResponse,
  MetricsResponse,
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
