import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import {
  createRule,
  createWebhook,
  deleteRule,
  deleteWebhook,
  getAlertHistory,
  getAlertSummary,
  listRules,
  listWebhooks,
  resolveAlert,
} from '../api/endpoints'
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
  const queryClient = useQueryClient()
  const { groups } = useAuth()
  const [now] = useState(() => Date.now())

  const [range, setRange] = useState<AlertRange>('7d')
  const [status, setStatus] = useState<AlertStatusFilter | 'all'>('all')

  const groupInfo = groups.find((g) => g.g_id === id)
  const userRole = groupInfo?.role_name ?? 'admin'
  const isAdmin = userRole === 'admin'

  const title =
    groupInfo?.label ??
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
  const webhooks = useQuery({
    queryKey: ['webhooks', id],
    queryFn: () => listWebhooks(id),
    enabled: !MOCK_QUERY_API,
  })

  // Rule creation modal state
  const [showRuleModal, setShowRuleModal] = useState(false)
  const [metricNameVal, setMetricNameVal] = useState('cpu_util')
  const [operatorVal, setOperatorVal] = useState('>')
  const [thresholdVal, setThresholdVal] = useState('85')
  const [ruleSubmitting, setRuleSubmitting] = useState(false)
  const [ruleError, setRuleError] = useState<string | null>(null)

  // Webhook modal state
  const [showWebhookModal, setShowWebhookModal] = useState(false)
  const [webhookUrl, setWebhookUrl] = useState('')
  const [webhookSubmitting, setWebhookSubmitting] = useState(false)
  const [webhookError, setWebhookError] = useState<string | null>(null)

  // Resolving alert state
  const [resolvingId, setResolvingId] = useState<number | null>(null)

  if (Number.isNaN(id)) return <p className="error">Invalid group id.</p>

  const rules = summary.data?.rules ?? []
  const alerts = history.data?.pages.flatMap((p) => p.alerts ?? []) ?? []

  const handleCreateRule = async (e: FormEvent) => {
    e.preventDefault()
    setRuleError(null)
    const val = Number(thresholdVal)
    if (Number.isNaN(val)) {
      setRuleError('Threshold must be a valid number')
      return
    }
    setRuleSubmitting(true)
    try {
      await createRule(id, {
        metric_name: metricNameVal,
        operator: operatorVal,
        threshold_value: val,
        enabled: true,
      })
      setShowRuleModal(false)
      await queryClient.invalidateQueries({ queryKey: ['alert-summary', id] })
    } catch (err) {
      setRuleError(err instanceof Error ? err.message : 'Failed to create alert rule')
    } finally {
      setRuleSubmitting(false)
    }
  }

  const handleDeleteRule = async (ruleId: number) => {
    if (!window.confirm('Are you sure you want to delete this alert rule?')) return
    try {
      await deleteRule(id, ruleId)
      await queryClient.invalidateQueries({ queryKey: ['alert-summary', id] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete rule')
    }
  }

  const handleResolveAlert = async (alertId: number) => {
    setResolvingId(alertId)
    try {
      await resolveAlert(alertId, id)
      await queryClient.invalidateQueries({ queryKey: ['alerts', id] })
      await queryClient.invalidateQueries({ queryKey: ['alert-summary', id] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to resolve alert')
    } finally {
      setResolvingId(null)
    }
  }

  const handleCreateWebhook = async (e: FormEvent) => {
    e.preventDefault()
    if (!webhookUrl.trim()) return
    setWebhookError(null)
    setWebhookSubmitting(true)
    try {
      await createWebhook(id, { webhook_url: webhookUrl.trim() })
      setWebhookUrl('')
      setShowWebhookModal(false)
      await queryClient.invalidateQueries({ queryKey: ['webhooks', id] })
    } catch (err) {
      setWebhookError(err instanceof Error ? err.message : 'Failed to create webhook')
    } finally {
      setWebhookSubmitting(false)
    }
  }

  const handleDeleteWebhook = async (webId: number) => {
    if (!window.confirm('Delete this notification webhook?')) return
    try {
      await deleteWebhook(id, webId)
      await queryClient.invalidateQueries({ queryKey: ['webhooks', id] })
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete webhook')
    }
  }

  return (
    <>
      <p className="crumbs">
        <Link to={`/groups/${id}`} state={{ label: title }}>
          ← {title}
        </Link>
      </p>
      <h2>Alerts & Monitoring Rules</h2>

      {MOCK_QUERY_API && (
        <p className="notice">
          Demo data: the read API isn&apos;t connected yet, so these alerts are synthetic.
        </p>
      )}

      {/* Rules Section */}
      <section className="section">
        <div className="range-row">
          <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
            <h3 style={{ margin: 0 }}>Rules</h3>
            {isAdmin && (
              <button className="secondary sm" onClick={() => { setShowRuleModal(true); setRuleError(null); }}>
                + Add Rule
              </button>
            )}
          </div>
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
        {summary.data && rules.length === 0 && <p className="muted">No alert rules configured for this group.</p>}
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
                  {isAdmin && <th>Actions</th>}
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
                    {isAdmin && (
                      <td>
                        <button className="danger sm" onClick={() => handleDeleteRule(r.rule_id)}>
                          Delete
                        </button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Webhooks Section */}
      <section className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
          <h3 style={{ margin: 0 }}>Notification Webhooks</h3>
          {isAdmin && (
            <button className="secondary sm" onClick={() => { setShowWebhookModal(true); setWebhookError(null); }}>
              + Add Webhook
            </button>
          )}
        </div>
        {webhooks.data && webhooks.data.length > 0 ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>URL</th>
                  <th>Status</th>
                  <th>Secret (HMAC)</th>
                  {isAdmin && <th>Actions</th>}
                </tr>
              </thead>
              <tbody>
                {webhooks.data.map((w) => (
                  <tr key={w.web_id}>
                    <td><code>{w.webhook_url}</code></td>
                    <td><span className="badge badge-up">{w.status}</span></td>
                    <td><span className="muted">{w.secret ? `${w.secret.substring(0, 8)}...` : '—'}</span></td>
                    {isAdmin && (
                      <td>
                        <button className="danger sm" onClick={() => handleDeleteWebhook(w.web_id)}>
                          Delete
                        </button>
                      </td>
                    )}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <p className="muted" style={{ fontSize: '0.9rem' }}>No webhook endpoints configured. Alerts will log to database only.</p>
        )}
      </section>

      {/* History Section */}
      <section className="section">
        <div className="range-row">
          <h3>Alert History</h3>
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
                  <th>Action</th>
                </tr>
              </thead>
              <tbody>
                {alerts.map((a) => {
                  const active = a.resolved_at === null
                  const isResolving = resolvingId === a.a_id
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
                      <td>
                        {active ? (
                          <button
                            className="secondary sm"
                            disabled={isResolving}
                            onClick={() => handleResolveAlert(a.a_id)}
                          >
                            {isResolving ? 'Resolving…' : 'Resolve'}
                          </button>
                        ) : (
                          <span className="muted" style={{ fontSize: '0.8rem' }}>Resolved</span>
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

      {/* Add Alert Rule Modal */}
      {showRuleModal && (
        <div className="modal-overlay" onClick={() => !ruleSubmitting && setShowRuleModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Create Alert Rule</h3>
              <button className="secondary sm" onClick={() => setShowRuleModal(false)} disabled={ruleSubmitting}>✕</button>
            </div>
            <form onSubmit={handleCreateRule}>
              {ruleError && <div className="error" style={{ marginBottom: '1rem' }}>{ruleError}</div>}
              <div style={{ display: 'grid', gap: '0.75rem' }}>
                <div>
                  <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Metric</label>
                  <select
                    value={metricNameVal}
                    onChange={(e) => setMetricNameVal(e.target.value)}
                    disabled={ruleSubmitting}
                  >
                    <option value="cpu_util">CPU Utilization (%)</option>
                    <option value="ram_util">RAM Utilization (%)</option>
                    <option value="gpu_util">GPU Utilization (%)</option>
                    <option value="net_up_kbps">Network Upload (KB/s)</option>
                    <option value="net_down_kbps">Network Download (KB/s)</option>
                  </select>
                </div>
                <div>
                  <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Condition</label>
                  <select
                    value={operatorVal}
                    onChange={(e) => setOperatorVal(e.target.value)}
                    disabled={ruleSubmitting}
                  >
                    <option value=">">Greater than (&gt;)</option>
                    <option value=">=">Greater or equal (&gt;=)</option>
                    <option value="<">Less than (&lt;)</option>
                    <option value="<=">Less or equal (&lt;=)</option>
                    <option value="=">Equals (=)</option>
                  </select>
                </div>
                <div>
                  <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Threshold Value</label>
                  <input
                    type="number"
                    step="any"
                    placeholder="e.g. 85 for 85% utilization"
                    value={thresholdVal}
                    onChange={(e) => setThresholdVal(e.target.value)}
                    required
                    disabled={ruleSubmitting}
                  />
                </div>
              </div>
              <div className="modal-actions">
                <button type="button" className="secondary" onClick={() => setShowRuleModal(false)} disabled={ruleSubmitting}>
                  Cancel
                </button>
                <button type="submit" disabled={ruleSubmitting || !thresholdVal.trim()}>
                  {ruleSubmitting ? 'Creating...' : 'Create Rule'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Add Webhook Modal */}
      {showWebhookModal && (
        <div className="modal-overlay" onClick={() => !webhookSubmitting && setShowWebhookModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Add Notification Webhook</h3>
              <button className="secondary sm" onClick={() => setShowWebhookModal(false)} disabled={webhookSubmitting}>✕</button>
            </div>
            <form onSubmit={handleCreateWebhook}>
              {webhookError && <div className="error" style={{ marginBottom: '1rem' }}>{webhookError}</div>}
              <div>
                <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Webhook Destination URL</label>
                <input
                  type="url"
                  placeholder="https://hooks.slack.com/... or https://your-domain.com/alert-hook"
                  value={webhookUrl}
                  onChange={(e) => setWebhookUrl(e.target.value)}
                  required
                  autoFocus
                  disabled={webhookSubmitting}
                />
              </div>
              <div className="modal-actions">
                <button type="button" className="secondary" onClick={() => setShowWebhookModal(false)} disabled={webhookSubmitting}>
                  Cancel
                </button>
                <button type="submit" disabled={webhookSubmitting || !webhookUrl.trim()}>
                  {webhookSubmitting ? 'Adding...' : 'Add Webhook'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  )
}
