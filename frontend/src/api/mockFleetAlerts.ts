import type {
  AlertRange,
  AlertRow,
  AlertsResponse,
  AlertStatusFilter,
  AlertSummaryResponse,
  FleetResponse,
  FleetRow,
  Instance,
  RuleSummary,
} from '../types'
import { mulberry32 } from './mockSeries'

const RULES = [
  { rule_id: 1, metric_name: 'cpu_util', operator: '>', threshold_value: 90 },
  { rule_id: 2, metric_name: 'ram_util', operator: '>', threshold_value: 85 },
  { rule_id: 3, metric_name: 'net_down_kbps', operator: '>', threshold_value: 1500 },
]
const RANGE_S: Record<AlertRange, number> = { '24h': 86400, '7d': 604800, '30d': 2592000 }

function buildAlerts(): AlertRow[] {
  const rand = mulberry32(4242)
  const base = Date.now()
  let t = base - 25 * 60_000
  const out: AlertRow[] = []
  for (let n = 0; n < 40; n++) {
    const rule = RULES[Math.floor(rand() * RULES.length)]
    const iid = 1 + Math.floor(rand() * 9)
    const active = n < 3
    const dur = (2 + rand() * 40) * 60_000
    out.push({
      a_id: 200 - n,
      rule_id: rule.rule_id,
      metric_name: rule.metric_name,
      operator: rule.operator,
      threshold_value: rule.threshold_value,
      i_id: iid,
      instance_label: `dev-box-${String(iid).padStart(2, '0')}`,
      alert_value: Math.round((rule.threshold_value + rand() * rule.threshold_value * 0.08) * 10) / 10,
      triggered_at: new Date(t).toISOString(),
      resolved_at: active ? null : new Date(t + dur).toISOString(),
      status: active ? 'Active' : 'Resolved',
    })
    t -= (30 + rand() * 240) * 60_000
  }
  return out
}

const ALERTS = buildAlerts()

export function mockFleet(instances: Instance[]): FleetResponse {
  const rows: FleetRow[] = instances.map((inst) => {
    const rand = mulberry32(inst.i_id * 31)
    const silent = inst.i_id % 7 === 0
    const recent = new Date(Date.now() - rand() * 20_000).toISOString()
    return {
      i_id: inst.i_id,
      g_id: inst.g_id,
      group_label: 'Team 4 Lab',
      label: inst.label,
      role_id: 1,
      enabled: inst.enabled,
      status: inst.status,
      last_seen_at: silent || !inst.enabled ? new Date(Date.now() - 3 * 3600_000).toISOString() : recent,
      labels: null,
      latest:
        silent || !inst.enabled
          ? null
          : {
              recorded_at: recent,
              cpu_util: Math.round((15 + rand() * 80) * 10) / 10,
              gpu_util: null,
              ram_util: Math.round((30 + rand() * 50) * 10) / 10,
              uptime: 800000 + Math.floor(rand() * 100000),
              net_up_kbps: Math.round(100 + rand() * 300),
              net_down_kbps: Math.round(300 + rand() * 1000),
            },
      active_alerts: ALERTS.filter((a) => a.i_id === inst.i_id && a.resolved_at === null).length,
    }
  })
  return { instances: rows }
}

export function mockAlertSummary(range: AlertRange): AlertSummaryResponse {
  const since = Date.now() - RANGE_S[range] * 1000
  const rules: RuleSummary[] = RULES.map((r) => {
    const mine = ALERTS.filter((a) => a.rule_id === r.rule_id && new Date(a.triggered_at).getTime() >= since)
    const resolved = mine.filter((a) => a.resolved_at)
    const mttr = resolved.length
      ? resolved.reduce(
          (sum, a) => sum + (new Date(a.resolved_at as string).getTime() - new Date(a.triggered_at).getTime()) / 1000,
          0,
        ) / resolved.length
      : null
    return {
      ...r,
      enabled: true,
      fired: mine.length,
      instances_affected: new Set(mine.map((a) => a.i_id)).size,
      mttr_s: mttr,
      worst_value: mine.length ? Math.max(...mine.map((a) => a.alert_value)) : null,
      last_fired: mine[0]?.triggered_at ?? null,
      active_now: mine.filter((a) => a.resolved_at === null).length,
    }
  })
  return { from: new Date(since).toISOString(), to: new Date().toISOString(), rules }
}

export function mockAlertHistory(
  status: AlertStatusFilter | undefined,
  limit: number,
  cursor: string | undefined,
): AlertsResponse {
  const filtered = ALERTS.filter((a) =>
    status === undefined ? true : status === 'active' ? a.resolved_at === null : a.resolved_at !== null,
  )
  const offset = cursor ? Number(cursor) : 0
  const page = filtered.slice(offset, offset + limit)
  const next = offset + limit < filtered.length ? String(offset + limit) : null
  return { alerts: page, next_cursor: next }
}
