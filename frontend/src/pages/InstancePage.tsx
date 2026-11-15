import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  addLabel,
  deleteInstance,
  deleteLabel,
  getInstanceSeries,
  listInstances,
  listLabels,
  toggleInstance,
} from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { MetricChart, type ChartRow } from '../components/MetricChart'
import { StatusBadge } from '../components/StatusBadge'
import { MOCK_QUERY_API } from '../config'
import {
  formatClock,
  formatDateTime,
  formatKbps,
  formatPercent,
  formatUptime,
  timeAgo,
} from '../format'
import type { SeriesPoint, SeriesRange } from '../types'

const RANGES: { value: SeriesRange; label: string }[] = [
  { value: '1h', label: '1 hour' },
  { value: '6h', label: '6 hours' },
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
]
const REFRESH_MS = 30_000
const MIN_STALE_MS = 2 * 60_000

const formatDay = (iso: string) =>
  new Date(iso).toLocaleDateString([], { day: 'numeric', month: 'short' })

export default function InstancePage() {
  const { sid, iid } = useParams()
  const subgroupId = Number(sid)
  const instanceId = Number(iid)
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { groups } = useAuth()

  const [range, setRange] = useState<SeriesRange>('24h')
  const [now, setNow] = useState(() => Date.now())
  const [toggling, setToggling] = useState(false)
  const [labelKey, setLabelKey] = useState('')
  const [labelVal, setLabelVal] = useState('')
  const [labelSubmitting, setLabelSubmitting] = useState(false)

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), REFRESH_MS)
    return () => clearInterval(t)
  }, [])

  const groupInfo = groups.find((g) => g.g_id === subgroupId)
  const canManage = groupInfo?.role_name === 'admin' || groupInfo?.role_name === 'subadmin' || !groupInfo

  const instances = useQuery({
    queryKey: ['instances', subgroupId],
    queryFn: () => listInstances(subgroupId),
  })
  const seriesQuery = useQuery({
    queryKey: ['series', instanceId, range],
    queryFn: () => getInstanceSeries(instanceId, range),
    refetchInterval: REFRESH_MS,
  })
  const labelsQuery = useQuery({
    queryKey: ['labels', subgroupId, instanceId],
    queryFn: () => listLabels(subgroupId, instanceId),
    enabled: !MOCK_QUERY_API,
  })

  const instance = instances.data?.find((i) => i.i_id === instanceId)
  const stepS = seriesQuery.data?.step_s ?? 0
  const points: SeriesPoint[] = useMemo(
    () => seriesQuery.data?.series[0]?.points ?? [],
    [seriesQuery.data],
  )

  // Keep empty buckets as nulls so charts show gaps, not zeros.
  const rows = useMemo<ChartRow[]>(
    () =>
      points.map((p) => ({
        t: p.t,
        cpu_avg: p.cpu_avg,
        cpu_max: p.cpu_max,
        ram_avg: p.ram_avg,
        ram_max: p.ram_max,
        gpu_avg: p.gpu_avg,
        gpu_max: p.gpu_max,
        up: p.net_up_avg,
        down: p.net_down_avg,
      })),
    [points],
  )

  const latest = useMemo(() => [...points].reverse().find((p) => p.samples > 0), [points])
  const hasGpu = points.some((p) => p.gpu_avg !== null)
  const isStale = latest ? now - (new Date(latest.t).getTime() + stepS * 1000) > MIN_STALE_MS : false
  const xFormat = range === '7d' ? formatDay : formatClock

  const handleToggle = async () => {
    if (!instance) return
    setToggling(true)
    try {
      await toggleInstance(subgroupId, instanceId, !instance.enabled)
      await queryClient.invalidateQueries({ queryKey: ['instances', subgroupId] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to toggle instance')
    } finally {
      setToggling(false)
    }
  }

  const handleDelete = async () => {
    if (!window.confirm(`Are you sure you want to delete instance "${instance?.label ?? instanceId}"?`)) return
    try {
      await deleteInstance(subgroupId, instanceId)
      navigate(`/groups/${subgroupId}`)
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete instance')
    }
  }

  const handleAddLabel = async (e: FormEvent) => {
    e.preventDefault()
    if (!labelKey.trim() || !labelVal.trim()) return
    setLabelSubmitting(true)
    try {
      await addLabel(subgroupId, instanceId, labelKey.trim(), labelVal.trim())
      setLabelKey('')
      setLabelVal('')
      await queryClient.invalidateQueries({ queryKey: ['labels', subgroupId, instanceId] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to add label')
    } finally {
      setLabelSubmitting(false)
    }
  }

  const handleDeleteLabel = async (key: string) => {
    try {
      await deleteLabel(subgroupId, instanceId, key)
      await queryClient.invalidateQueries({ queryKey: ['labels', subgroupId, instanceId] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete label')
    }
  }

  if (Number.isNaN(subgroupId) || Number.isNaN(instanceId)) {
    return <p className="error">Invalid instance.</p>
  }

  const currentLabels = labelsQuery.data ?? {}

  return (
    <>
      <p className="crumbs">
        <Link to={`/groups/${subgroupId}`}>← Instances</Link>
      </p>

      <div className="title-row" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <h2>{instance?.label ?? `Instance ${instanceId}`}</h2>
          {instance && <StatusBadge status={instance.status} enabled={instance.enabled} />}
        </div>
        {canManage && (
          <div style={{ display: 'flex', gap: '0.6rem' }}>
            {instance && (
              <button
                className="secondary sm"
                disabled={toggling}
                onClick={handleToggle}
              >
                {toggling ? 'Updating…' : instance.enabled ? 'Disable Ingestion' : 'Enable Ingestion'}
              </button>
            )}
            <button className="danger sm" onClick={handleDelete}>
              Delete Instance
            </button>
          </div>
        )}
      </div>

      {instance && (
        <p className="muted" style={{ margin: '0.25rem 0 1rem' }}>
          {[instance.cpu_model, instance.ram_config, instance.gpu_model].filter(Boolean).join(' · ') ||
            'No hardware details reported yet'}
        </p>
      )}

      {/* Metadata Labels Section */}
      <div style={{ margin: '1rem 0', padding: '0.75rem 1rem', background: 'var(--surface)', border: '1px solid var(--border)', borderRadius: '8px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: Object.keys(currentLabels).length > 0 ? '0.5rem' : '0' }}>
          <strong style={{ fontSize: '0.85rem', color: 'var(--muted)', textTransform: 'uppercase' }}>Instance Labels</strong>
        </div>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.4rem', alignItems: 'center' }}>
          {Object.entries(currentLabels).map(([k, v]) => (
            <span key={k} className="badge" style={{ display: 'inline-flex', alignItems: 'center', gap: '0.3rem', padding: '0.2rem 0.6rem', fontSize: '0.82rem' }}>
              <code>{k}={v}</code>
              {canManage && (
                <button
                  type="button"
                  style={{ background: 'none', border: 'none', padding: 0, cursor: 'pointer', color: 'var(--muted)', fontSize: '0.9rem', lineHeight: 1 }}
                  onClick={() => handleDeleteLabel(k)}
                  title="Remove label"
                >
                  ✕
                </button>
              )}
            </span>
          ))}
          {Object.keys(currentLabels).length === 0 && (
            <span className="muted" style={{ fontSize: '0.85rem' }}>No labels assigned yet.</span>
          )}
        </div>
        {canManage && (
          <form onSubmit={handleAddLabel} style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem', alignItems: 'center' }}>
            <input
              type="text"
              placeholder="Key (e.g. env)"
              value={labelKey}
              onChange={(e) => setLabelKey(e.target.value)}
              style={{ maxWidth: '140px', padding: '0.35rem 0.5rem', fontSize: '0.85rem' }}
              disabled={labelSubmitting}
            />
            <input
              type="text"
              placeholder="Value (e.g. prod)"
              value={labelVal}
              onChange={(e) => setLabelVal(e.target.value)}
              style={{ maxWidth: '140px', padding: '0.35rem 0.5rem', fontSize: '0.85rem' }}
              disabled={labelSubmitting}
            />
            <button
              type="submit"
              className="secondary sm"
              disabled={labelSubmitting || !labelKey.trim() || !labelVal.trim()}
            >
              + Add
            </button>
          </form>
        )}
      </div>

      {MOCK_QUERY_API && (
        <p className="notice">
          Demo data: the read API isn&apos;t connected yet, so these charts are synthetic.
        </p>
      )}

      <div className="range-row">
        <span className="muted">
          {points.length > 0 &&
            `${formatDateTime(points[0].t)} to ${formatDateTime(points[points.length - 1].t)} · ${
              stepS >= 3600 ? `${stepS / 3600} h` : stepS >= 60 ? `${stepS / 60} min` : `${stepS} s`
            } buckets`}
        </span>
        <div className="range-buttons">
          {RANGES.map((r) => (
            <button
              key={r.value}
              type="button"
              className={r.value === range ? 'range active' : 'range'}
              onClick={() => setRange(r.value)}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      {seriesQuery.isPending && <p className="muted">Loading metrics…</p>}
      {seriesQuery.isError && <p className="error">{seriesQuery.error.message}</p>}
      {seriesQuery.data && !latest && (
        <p className="muted">No samples recorded for this instance in this period.</p>
      )}

      {latest && (
        <>
          {isStale && (
            <p className="notice">
              No new data for {timeAgo(new Date(new Date(latest.t).getTime() + stepS * 1000).toISOString(), now)}.
              Showing the most recent stored samples.
            </p>
          )}

          <div className="stats">
            <div className="stat">
              <span className="muted">CPU (avg)</span>
              <strong>{latest.cpu_avg === null ? '—' : formatPercent(latest.cpu_avg)}</strong>
            </div>
            <div className="stat">
              <span className="muted">RAM (avg)</span>
              <strong>{latest.ram_avg === null ? '—' : formatPercent(latest.ram_avg)}</strong>
            </div>
            <div className="stat">
              <span className="muted">GPU (avg)</span>
              <strong>{latest.gpu_avg === null ? 'n/a' : formatPercent(latest.gpu_avg)}</strong>
            </div>
            <div className="stat">
              <span className="muted">Uptime</span>
              <strong>{latest.uptime === null ? '—' : formatUptime(latest.uptime)}</strong>
            </div>
            <div className="stat">
              <span className="muted">Network ↑ / ↓</span>
              <strong>
                {latest.net_up_avg === null ? '—' : formatKbps(latest.net_up_avg)} /{' '}
                {latest.net_down_avg === null ? '—' : formatKbps(latest.net_down_avg)}
              </strong>
            </div>
          </div>

          <div className="charts">
            <MetricChart
              title="CPU utilisation"
              data={rows}
              series={[
                { key: 'cpu_avg', label: 'Average', color: '#0e9f8a' },
                { key: 'cpu_max', label: 'Peak', color: '#0e9f8a', dashed: true },
              ]}
              format={formatPercent}
              yMax={100}
              xFormat={xFormat}
            />
            <MetricChart
              title="RAM utilisation"
              data={rows}
              series={[
                { key: 'ram_avg', label: 'Average', color: '#3b82f6' },
                { key: 'ram_max', label: 'Peak', color: '#3b82f6', dashed: true },
              ]}
              format={formatPercent}
              yMax={100}
              xFormat={xFormat}
            />
            {hasGpu && (
              <MetricChart
                title="GPU utilisation"
                data={rows}
                series={[
                  { key: 'gpu_avg', label: 'Average', color: '#8b5cf6' },
                  { key: 'gpu_max', label: 'Peak', color: '#8b5cf6', dashed: true },
                ]}
                format={formatPercent}
                yMax={100}
                xFormat={xFormat}
              />
            )}
            <MetricChart
              title="Network throughput (average)"
              data={rows}
              series={[
                { key: 'up', label: 'Upload', color: '#d97706' },
                { key: 'down', label: 'Download', color: '#0ea5e9' },
              ]}
              format={formatKbps}
              xFormat={xFormat}
            />
          </div>
        </>
      )}
    </>
  )
}
