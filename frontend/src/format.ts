export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

export function formatClock(iso: string): string {
  return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString([], {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function timeAgo(iso: string, now: number): string {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 60) return `${s}s ago`
  const m = Math.round(s / 60)
  if (m < 60) return `${m} min ago`
  const h = Math.round(m / 60)
  if (h < 48) return `${h} h ago`
  return `${Math.round(h / 24)} days ago`
}

export function formatKbps(v: number): string {
  return v >= 1024 ? `${(v / 1024).toFixed(1)} Mbps` : `${v.toFixed(0)} kbps`
}

export function formatPercent(v: number): string {
  return `${v.toFixed(1)}%`
}

export function formatDuration(seconds: number): string {
  const s = Math.round(seconds)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ${s % 60}s`
  const h = Math.floor(m / 60)
  if (h < 48) return `${h}h ${m % 60}m`
  return `${Math.floor(h / 24)}d ${h % 24}h`
}

const METRIC_NAMES: Record<string, string> = {
  cpu_util: 'CPU',
  gpu_util: 'GPU',
  ram_util: 'RAM',
  uptime: 'Uptime',
  net_up_kbps: 'Network up',
  net_down_kbps: 'Network down',
}

export function metricName(key: string): string {
  return METRIC_NAMES[key] ?? key
}

export function metricValue(key: string, v: number): string {
  if (key.endsWith('_util')) return formatPercent(v)
  if (key.endsWith('_kbps')) return formatKbps(v)
  if (key === 'uptime') return formatUptime(v)
  return String(v)
}
