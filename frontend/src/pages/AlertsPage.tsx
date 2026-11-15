import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { getAlertHistory, getAlertSummary } from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { MOCK_QUERY_API } from '../config'
import { formatDateTime, formatDuration, metricName, metricValue, timeAgo } from '../format'
import type { AlertRange, AlertStatusFilter } from '../types'

const RANGES: { value: AlertRange; label: string }[] = [
  { value: '24h', label: '24 hours' },
  { value: '7d', label: '7 days' },
  { value: '30d', label: '30 days' },
]
const STATUSES: { value: AlertStatusFilter | 'all'; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'active', label: 'Active' },
  { value: 'resolved', label: 'Resolved' },
]

export default function AlertsPage() {
  const { gid } = useParams()
  const id = Number(gid)
  const location = useLocation()
  const { groups } = useAuth()
  const [now] = useState(() => Date.now())

  const [range, setRange] = useState<AlertRange>('7d')
  const [status, setStatus] = useState<AlertStatusFilter | 'all'>('all')

  const title =
    groups.find((g) => g.g_id === id)?.label ??
    (location.state as { label?: string } | null)?.label ??
    `Group ${id}`

  const summary = useQuery({
    queryKey: ['alert-summary', id, range],
    queryFn: () => getAlertSummary(id, range),
  })
  const history = useInfiniteQuery({
    queryKey: ['alerts', id, status],
    queryFn: ({ pageParam }) =>
      getAlertHistory(id, { status: status === 'all' ? undefined : status, cursor: pageParam }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  })

  if (Number.isNaN(id)) return <p className="error">Invalid group id.</p>

  const rules = summary.data?.rules ?? []
  const alerts = history.data?.pages.flatMap((p) => p.alerts ?? []) ?? []

  return (
    <>
      <p className="crumbs">
        <Link to={`/groups/${id}`} state={{ label: title }}>
          ← {title}
        </Link>
      </p>
      <h2>Alerts</h2>

      {MOCK_QUERY_API && (
        <p className="notice">
          Demo data: the read API isn&apos;t connected yet, so these alerts are synthetic.
        </p>
      )}

      <section className="section">
        <div className="range-row">
          <h3>Rules</h3>
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
        {summary.isPending && <p className="muted">Loading…</p>}
        {summary.isError && <p className="error">{summary.error.message}</p>}
        {summary.data && rules.length === 0 && <p className="muted">No alert rules for this group.</p>}
        {rules.length > 0 && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Rule</th>
                  <th>Fired</th>
                  <th>Instances</th>
                  <th>Avg. time to resolve</th>
                  <th>Worst value</th>
                  <th>Active now</th>
                </tr>
              </thead>
              <tbody>
                {rules.map((r) => (
                  <tr key={r.rule_id}>
                    <td>
                      {metricName(r.metric_name)} {r.operator} {metricValue(r.metric_name, r.threshold_value)}
                      {!r.enabled && <span className="muted"> (disabled)</span>}
                    </td>
                    <td>{r.fired}</td>
                    <td>{r.instances_affected}</td>
                    <td>{r.mttr_s === null ? '—' : formatDuration(r.mttr_s)}</td>
                    <td>{r.worst_value === null ? '—' : metricValue(r.metric_name, r.worst_value)}</td>
                    <td>
                      {r.active_now > 0 ? (
                        <span className="badge badge-down">{r.active_now}</span>
                      ) : (
                        <span className="muted">0</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="section">
        <div className="range-row">
          <h3>History</h3>
          <div className="range-buttons">
            {STATUSES.map((s) => (
              <button
                key={s.value}
                type="button"
                className={s.value === status ? 'range active' : 'range'}
                onClick={() => setStatus(s.value)}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>
        {history.isPending && <p className="muted">Loading…</p>}
        {history.isError && <p className="error">{history.error.message}</p>}
        {history.data && alerts.length === 0 && <p className="muted">No alerts to show.</p>}
        {alerts.length > 0 && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Status</th>
                  <th>Instance</th>
                  <th>Rule</th>
                  <th>Value</th>
                  <th>Triggered</th>
                  <th>Duration</th>
                </tr>
              </thead>
              <tbody>
                {alerts.map((a) => {
                  const active = a.resolved_at === null
                  return (
                    <tr key={a.a_id}>
                      <td>
                        <span className={`badge ${active ? 'badge-down' : 'badge-up'}`}>
                          {active ? 'active' : 'resolved'}
                        </span>
                      </td>
                      <td>
                        <Link to={`/subgroups/${id}/instances/${a.i_id}`}>{a.instance_label}</Link>
                      </td>
                      <td>
                        {metricName(a.metric_name)} {a.operator} {metricValue(a.metric_name, a.threshold_value)}
                      </td>
                      <td>{metricValue(a.metric_name, a.alert_value)}</td>
                      <td title={formatDateTime(a.triggered_at)}>{timeAgo(a.triggered_at, now)}</td>
                      <td>
                        {active
                          ? 'ongoing'
                          : formatDuration(
                              (new Date(a.resolved_at as string).getTime() - new Date(a.triggered_at).getTime()) / 1000,
                            )}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {history.hasNextPage && (
          <p>
            <button
              type="button"
              className="secondary"
              disabled={history.isFetchingNextPage}
              onClick={() => history.fetchNextPage()}
            >
              {history.isFetchingNextPage ? 'Loading…' : 'Load more'}
            </button>
          </p>
        )}
      </section>
    </>
  )
}
