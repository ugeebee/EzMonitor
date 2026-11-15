import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getInstanceMetrics, listInstances } from '../api/endpoints'
import { MetricChart, type ChartRow } from '../components/MetricChart'
import { StatusBadge } from '../components/StatusBadge'
import {
  formatDateTime,
  formatKbps,
  formatPercent,
  formatUptime,
  timeAgo,
} from '../format'

const LIMITS = [60, 240, 1000] as const
const REFRESH_MS = 15_000
const STALE_AFTER_MS = 2 * 60_000

export default function InstancePage() {
  const { sid, iid } = useParams()
  const subgroupId = Number(sid)
  const instanceId = Number(iid)

  const [limit, setLimit] = useState<number>(240)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), REFRESH_MS)
    return () => clearInterval(t)
  }, [])

  const instances = useQuery({
    queryKey: ['instances', subgroupId],
    queryFn: () => listInstances(subgroupId),
  })
  const metrics = useQuery({
    queryKey: ['metrics', subgroupId, instanceId, limit],
    queryFn: () => getInstanceMetrics(subgroupId, instanceId, limit),
    refetchInterval: REFRESH_MS,
  })

  const instance = instances.data?.find((i) => i.i_id === instanceId)
  const samples = metrics.data?.samples

  // API returns newest first; charts need oldest first.
  const rows = useMemo<ChartRow[]>(
    () =>
      [...(samples ?? [])].reverse().map((s) => ({
        t: s.recorded_at,
        cpu: s.cpu_util,
        ram: s.ram_util,
        gpu: s.gpu_util,
        up: s.net_up_kbps,
        down: s.net_down_kbps,
      })),
    [samples],
  )

  const latest = samples?.[0]
  const hasGpu = rows.some((r) => r.gpu !== null)
  const isStale = latest ? now - new Date(latest.recorded_at).getTime() > STALE_AFTER_MS : false

  if (Number.isNaN(subgroupId) || Number.isNaN(instanceId)) {
    return <p className="error">Invalid instance.</p>
  }

  return (
    <>
      <p className="crumbs">
        <Link to={`/groups/${subgroupId}`}>← Instances</Link>
      </p>

      <div className="title-row">
        <h2>{instance?.label ?? `Instance ${instanceId}`}</h2>
        {instance && <StatusBadge status={instance.status} enabled={instance.enabled} />}
      </div>
      {instance && (
        <p className="muted">
          {[instance.cpu_model, instance.ram_config, instance.gpu_model].filter(Boolean).join(' · ') ||
            'No hardware details reported yet'}
        </p>
      )}

      {metrics.isPending && <p className="muted">Loading metrics…</p>}
      {metrics.isError && <p className="error">{metrics.error.message}</p>}

      {metrics.data && rows.length === 0 && (
        <p className="muted">No metrics recorded for this instance yet.</p>
      )}

      {latest && (
        <>
          {isStale && (
            <p className="notice">
              No new data for {timeAgo(latest.recorded_at, now)}. Showing the latest stored samples.
            </p>
          )}

          <div className="stats">
            <div className="stat">
              <span className="muted">CPU</span>
              <strong>{formatPercent(latest.cpu_util)}</strong>
            </div>
            <div className="stat">
              <span className="muted">RAM</span>
              <strong>{formatPercent(latest.ram_util)}</strong>
            </div>
            <div className="stat">
              <span className="muted">GPU</span>
              <strong>{latest.gpu_util === null ? 'n/a' : formatPercent(latest.gpu_util)}</strong>
            </div>
            <div className="stat">
              <span className="muted">Uptime</span>
              <strong>{formatUptime(latest.uptime)}</strong>
            </div>
            <div className="stat">
              <span className="muted">Network ↑ / ↓</span>
              <strong>
                {formatKbps(latest.net_up_kbps)} / {formatKbps(latest.net_down_kbps)}
              </strong>
            </div>
          </div>

          <div className="range-row">
            <span className="muted">
              {rows.length} samples · {formatDateTime(rows[0].t)} to {formatDateTime(rows[rows.length - 1].t)}
            </span>
            <div className="range-buttons">
              {LIMITS.map((l) => (
                <button
                  key={l}
                  type="button"
                  className={l === limit ? 'range active' : 'range'}
                  onClick={() => setLimit(l)}
                >
                  Last {l}
                </button>
              ))}
            </div>
          </div>

          <div className="charts">
            <MetricChart
              title="CPU utilisation"
              data={rows}
              series={[{ key: 'cpu', label: 'CPU', color: '#0e9f8a' }]}
              format={formatPercent}
              yMax={100}
            />
            <MetricChart
              title="RAM utilisation"
              data={rows}
              series={[{ key: 'ram', label: 'RAM', color: '#3b82f6' }]}
              format={formatPercent}
              yMax={100}
            />
            {hasGpu && (
              <MetricChart
                title="GPU utilisation"
                data={rows}
                series={[{ key: 'gpu', label: 'GPU', color: '#8b5cf6' }]}
                format={formatPercent}
                yMax={100}
              />
            )}
            <MetricChart
              title="Network throughput"
              data={rows}
              series={[
                { key: 'up', label: 'Upload', color: '#d97706' },
                { key: 'down', label: 'Download', color: '#0ea5e9' },
              ]}
              format={formatKbps}
            />
          </div>
        </>
      )}
    </>
  )
}
