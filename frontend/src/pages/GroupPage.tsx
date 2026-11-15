import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  admitMember,
  createSubgroup,
  deleteGroup,
  deleteSubgroup,
  getFleet,
  listInstances,
  listMembers,
  listSubgroups,
  pairInstance,
} from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { StatusBadge } from '../components/StatusBadge'
import { MOCK_QUERY_API } from '../config'
import { formatKbps, formatPercent, timeAgo } from '../format'
import type { FleetRow } from '../types'

type FleetState = 'up' | 'silent' | 'disabled'

// The database `status` column stays "unknown" until something updates it, so derive health from
// whether the instance reported a sample recently.
function fleetState(r: FleetRow): FleetState {
  if (!r.enabled) return 'disabled'
  return r.latest ? 'up' : 'silent'
}

const STATE_LABEL: Record<FleetState, string> = { up: 'reporting', silent: 'no data', disabled: 'disabled' }

// Works for a top-level group and for a subgroup: both can hold instances.
export default function GroupPage() {
  const { gid } = useParams()
  const id = Number(gid)
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { groups, refreshProfile } = useAuth()
  const [now] = useState(() => Date.now())

  const groupInfo = groups.find((g) => g.g_id === id)
  const userRole = groupInfo?.role_name ?? 'admin'
  const isSubgroup = groupInfo?.parent_id != null
  const isAdmin = userRole === 'admin'
  const canManage = userRole === 'admin' || userRole === 'subadmin'

  const title =
    groupInfo?.label ??
    (location.state as { label?: string } | null)?.label ??
    `Group ${id}`

  const instances = useQuery({ queryKey: ['instances', id], queryFn: () => listInstances(id) })
  const fleet = useQuery({
    queryKey: ['fleet', id],
    queryFn: () => getFleet(id),
    refetchInterval: 30_000,
  })
  const subgroups = useQuery({ queryKey: ['subgroups', id], queryFn: () => listSubgroups(id) })
  const members = useQuery({ queryKey: ['members', id], queryFn: () => listMembers(id) })

  // Modal states
  const [showSubgroupModal, setShowSubgroupModal] = useState(false)
  const [subgroupLabel, setSubgroupLabel] = useState('')

  const [showPairModal, setShowPairModal] = useState(false)
  const [pairLabel, setPairLabel] = useState('')
  const [pairFingerprint, setPairFingerprint] = useState('')
  const [pairOtp, setPairOtp] = useState('')
  const [pairedSuccess, setPairedSuccess] = useState<string | null>(null)

  const [showMemberModal, setShowMemberModal] = useState(false)
  const [memberEmail, setMemberEmail] = useState('')
  const [memberRole, setMemberRole] = useState(3) // 3 viewer, 2 subadmin, 1 admin

  const [submitting, setSubmitting] = useState(false)
  const [modalError, setModalError] = useState<string | null>(null)

  if (Number.isNaN(id)) return <p className="error">Invalid group id.</p>

  const fleetRows = fleet.data?.instances ?? []
  const reporting = fleetRows.filter((r) => fleetState(r) === 'up').length
  const silent = fleetRows.filter((r) => fleetState(r) === 'silent').length
  const activeAlerts = fleetRows.reduce((sum, r) => sum + r.active_alerts, 0)

  const handleCreateSubgroup = async (e: FormEvent) => {
    e.preventDefault()
    if (!subgroupLabel.trim()) return
    setModalError(null)
    setSubmitting(true)
    try {
      await createSubgroup(id, subgroupLabel.trim())
      setSubgroupLabel('')
      setShowSubgroupModal(false)
      await queryClient.invalidateQueries({ queryKey: ['subgroups', id] })
      await refreshProfile()
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to create subgroup')
    } finally {
      setSubmitting(false)
    }
  }

  const handlePairInstance = async (e: FormEvent) => {
    e.preventDefault()
    if (!pairLabel.trim() || !pairFingerprint.trim() || !pairOtp.trim()) return
    setModalError(null)
    setSubmitting(true)
    try {
      const res = await pairInstance(id, {
        label: pairLabel.trim(),
        fingerprint: pairFingerprint.trim().toLowerCase(),
        otp: pairOtp.trim(),
      })
      setPairedSuccess(`Instance "${res.label}" (ID: ${res.i_id}) successfully paired and registered!`)
      setPairLabel('')
      setPairFingerprint('')
      setPairOtp('')
      await queryClient.invalidateQueries({ queryKey: ['fleet', id] })
      await queryClient.invalidateQueries({ queryKey: ['instances', id] })
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to pair instance')
    } finally {
      setSubmitting(false)
    }
  }

  const handleAdmitMember = async (e: FormEvent) => {
    e.preventDefault()
    if (!memberEmail.trim()) return
    setModalError(null)
    setSubmitting(true)
    try {
      await admitMember(id, memberEmail.trim(), memberRole)
      setMemberEmail('')
      setShowMemberModal(false)
      await queryClient.invalidateQueries({ queryKey: ['members', id] })
    } catch (err) {
      setModalError(err instanceof Error ? err.message : 'Failed to admit member')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeleteGroup = async () => {
    const confirmMsg = isSubgroup
      ? `Are you sure you want to delete subgroup "${title}"? All instances inside it will be permanently deleted.`
      : `Are you sure you want to delete group "${title}"? All subgroups, instances, and metrics will be permanently deleted.`
    if (!window.confirm(confirmMsg)) return

    try {
      if (isSubgroup) {
        await deleteSubgroup(id)
      } else {
        await deleteGroup(id)
      }
      await refreshProfile()
      navigate('/')
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to delete group')
    }
  }

  return (
    <>
      <p className="crumbs">
        <Link to="/">← All groups</Link>
      </p>
      <div className="title-row" style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem' }}>
          <h2>{title}</h2>
          <span className={`role role-${userRole}`}>{userRole}</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
          <Link to={`/groups/${id}/alerts`} state={{ label: title }} className="header-link">
            Alerts{fleet.data ? ` (${activeAlerts} active)` : ''} →
          </Link>
          {isAdmin && (
            <button className="danger sm" onClick={handleDeleteGroup}>
              Delete Group
            </button>
          )}
        </div>
      </div>

      {MOCK_QUERY_API && (
        <p className="notice">
          Demo data: the read API isn&apos;t connected yet, so live values and alerts below are synthetic.
        </p>
      )}

      {/* Subgroups Section */}
      <section className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
          <h3 style={{ margin: 0 }}>Subgroups</h3>
          {isAdmin && (
            <button className="secondary sm" onClick={() => { setShowSubgroupModal(true); setModalError(null); }}>
              + Add Subgroup
            </button>
          )}
        </div>
        {subgroups.data && subgroups.data.length > 0 ? (
          <ul className="group-list">
            {subgroups.data.map((s) => (
              <li key={s.g_id}>
                <Link to={`/groups/${s.g_id}`} state={{ label: s.label }}>
                  <span>{s.label}</span>
                  {s.instances_count !== undefined && (
                    <span className="muted">{s.instances_count} instances</span>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted" style={{ fontSize: '0.9rem' }}>No subgroups in this group.</p>
        )}
      </section>

      {fleet.data && fleetRows.length > 0 && (
        <div className="stats">
          <div className="stat">
            <span className="muted">Instances</span>
            <strong>{fleetRows.length}</strong>
          </div>
          <div className="stat">
            <span className="muted">Reporting</span>
            <strong>{reporting}</strong>
          </div>
          <div className="stat">
            <span className="muted">No data (5 min)</span>
            <strong>{silent}</strong>
          </div>
          <div className="stat">
            <span className="muted">Active alerts</span>
            <strong>{activeAlerts}</strong>
          </div>
        </div>
      )}

      {/* Instances Section */}
      <section className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
          <h3 style={{ margin: 0 }}>Instances</h3>
          {canManage && (
            <button className="secondary sm" onClick={() => { setShowPairModal(true); setPairedSuccess(null); setModalError(null); }}>
              + Pair Instance
            </button>
          )}
        </div>

        {fleet.data && fleetRows.length > 0 && (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Status</th>
                  <th>CPU</th>
                  <th>RAM</th>
                  <th>GPU</th>
                  <th>Network ↑ / ↓</th>
                  <th>Alerts</th>
                  <th>Last seen</th>
                </tr>
              </thead>
              <tbody>
                {fleetRows.map((r) => {
                  const state = fleetState(r)
                  const seen = r.latest?.recorded_at ?? r.last_seen_at
                  return (
                    <tr key={r.i_id}>
                      <td>
                        <Link to={`/subgroups/${id}/instances/${r.i_id}`}>{r.label}</Link>
                      </td>
                      <td>
                        <span className={`badge badge-${state}`}>{STATE_LABEL[state]}</span>
                      </td>
                      <td>{r.latest ? formatPercent(r.latest.cpu_util) : '—'}</td>
                      <td>{r.latest ? formatPercent(r.latest.ram_util) : '—'}</td>
                      <td>{r.latest?.gpu_util != null ? formatPercent(r.latest.gpu_util) : '—'}</td>
                      <td>
                        {r.latest
                          ? `${formatKbps(r.latest.net_up_kbps)} / ${formatKbps(r.latest.net_down_kbps)}`
                          : '—'}
                      </td>
                      <td>
                        {r.active_alerts > 0 ? (
                          <span className="badge badge-down">{r.active_alerts} active</span>
                        ) : (
                          <span className="muted">none</span>
                        )}
                      </td>
                      <td>{seen ? timeAgo(seen, now) : '—'}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}

        {/* Fallback while the read API is unavailable: the plain instance list. */}
        {!fleet.data && (
          <>
            {fleet.isError && (
              <p className="muted">Live metrics are unavailable right now, showing the instance list.</p>
            )}
            {instances.isPending && <p className="muted">Loading…</p>}
            {instances.isError && <p className="error">{instances.error.message}</p>}
            {instances.data && instances.data.length === 0 && (
              <p className="muted">No instances in this group yet.</p>
            )}
            {instances.data && instances.data.length > 0 && (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>Name</th>
                      <th>Status</th>
                      <th>CPU</th>
                      <th>RAM</th>
                      <th>GPU</th>
                    </tr>
                  </thead>
                  <tbody>
                    {instances.data.map((i) => (
                      <tr key={i.i_id}>
                        <td>
                          <Link to={`/subgroups/${id}/instances/${i.i_id}`}>{i.label}</Link>
                        </td>
                        <td>
                          <StatusBadge status={i.status} enabled={i.enabled} />
                        </td>
                        <td>{i.cpu_model ?? '—'}</td>
                        <td>{i.ram_config ?? '—'}</td>
                        <td>{i.gpu_model ?? '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </>
        )}
        {fleet.data && fleetRows.length === 0 && <p className="muted">No instances in this group yet.</p>}
      </section>

      {/* Members Section */}
      <section className="section">
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '0.5rem' }}>
          <h3 style={{ margin: 0 }}>Members</h3>
          {canManage && (
            <button className="secondary sm" onClick={() => { setShowMemberModal(true); setModalError(null); }}>
              + Invite Member
            </button>
          )}
        </div>
        {members.data && members.data.length > 0 ? (
          <ul className="group-list">
            {members.data.map((m) => (
              <li key={m.u_id}>
                <div className="row">
                  <span>
                    {m.name} <span className="muted">{m.email}</span>
                  </span>
                  <span className={`role role-${m.role_name}`}>{m.role_name}</span>
                </div>
              </li>
            ))}
          </ul>
        ) : (
          <p className="muted" style={{ fontSize: '0.9rem' }}>No members listed.</p>
        )}
      </section>

      {/* Subgroup Creation Modal */}
      {showSubgroupModal && (
        <div className="modal-overlay" onClick={() => !submitting && setShowSubgroupModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Create Subgroup</h3>
              <button className="secondary sm" onClick={() => setShowSubgroupModal(false)} disabled={submitting}>✕</button>
            </div>
            <form onSubmit={handleCreateSubgroup}>
              {modalError && <div className="error" style={{ marginBottom: '1rem' }}>{modalError}</div>}
              <div>
                <label style={{ fontSize: '0.9rem', color: 'var(--muted)' }}>Subgroup Name</label>
                <input
                  type="text"
                  placeholder="e.g. Frontend Tier, Database Workers"
                  value={subgroupLabel}
                  onChange={(e) => setSubgroupLabel(e.target.value)}
                  autoFocus
                  required
                  disabled={submitting}
                />
              </div>
              <div className="modal-actions">
                <button type="button" className="secondary" onClick={() => setShowSubgroupModal(false)} disabled={submitting}>
                  Cancel
                </button>
                <button type="submit" disabled={submitting || !subgroupLabel.trim()}>
                  {submitting ? 'Creating...' : 'Create Subgroup'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Instance Pairing Modal */}
      {showPairModal && (
        <div className="modal-overlay" onClick={() => !submitting && setShowPairModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Pair New Instance</h3>
              <button className="secondary sm" onClick={() => setShowPairModal(false)} disabled={submitting}>✕</button>
            </div>
            {pairedSuccess ? (
              <div>
                <p style={{ color: '#15803d', fontWeight: 500, margin: '0.5rem 0 1rem' }}>{pairedSuccess}</p>
                <p className="muted" style={{ fontSize: '0.85rem' }}>
                  The agent CLI on your target machine will now receive authentication credentials and begin streaming telemetry metrics.
                </p>
                <div className="modal-actions">
                  <button type="button" onClick={() => { setShowPairModal(false); setPairedSuccess(null); }}>
                    Done
                  </button>
                </div>
              </div>
            ) : (
              <form onSubmit={handlePairInstance}>
                {modalError && <div className="error" style={{ marginBottom: '1rem' }}>{modalError}</div>}
                <p className="muted" style={{ fontSize: '0.85rem', margin: '0 0 1rem' }}>
                  Run <code>ezmonitor agent pair</code> on your server to obtain its 16-character hardware fingerprint and 6-digit OTP handshake code.
                </p>
                <div style={{ display: 'grid', gap: '0.75rem' }}>
                  <div>
                    <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Instance Friendly Name</label>
                    <input
                      type="text"
                      placeholder="e.g. prod-api-01"
                      value={pairLabel}
                      onChange={(e) => setPairLabel(e.target.value)}
                      required
                      disabled={submitting}
                    />
                  </div>
                  <div>
                    <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Hardware Fingerprint (16 hex characters)</label>
                    <input
                      type="text"
                      placeholder="e.g. 7f8a9b0c1d2e3f4a"
                      value={pairFingerprint}
                      onChange={(e) => setPairFingerprint(e.target.value)}
                      maxLength={16}
                      required
                      disabled={submitting}
                    />
                  </div>
                  <div>
                    <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>6-Digit OTP Code</label>
                    <input
                      type="text"
                      placeholder="e.g. 849201"
                      value={pairOtp}
                      onChange={(e) => setPairOtp(e.target.value)}
                      maxLength={6}
                      required
                      disabled={submitting}
                    />
                  </div>
                </div>
                <div className="modal-actions">
                  <button type="button" className="secondary" onClick={() => setShowPairModal(false)} disabled={submitting}>
                    Cancel
                  </button>
                  <button type="submit" disabled={submitting || !pairLabel.trim() || !pairFingerprint.trim() || !pairOtp.trim()}>
                    {submitting ? 'Pairing...' : 'Verify & Pair Instance'}
                  </button>
                </div>
              </form>
            )}
          </div>
        </div>
      )}

      {/* Member Invite Modal */}
      {showMemberModal && (
        <div className="modal-overlay" onClick={() => !submitting && setShowMemberModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Invite Member</h3>
              <button className="secondary sm" onClick={() => setShowMemberModal(false)} disabled={submitting}>✕</button>
            </div>
            <form onSubmit={handleAdmitMember}>
              {modalError && <div className="error" style={{ marginBottom: '1rem' }}>{modalError}</div>}
              <div style={{ display: 'grid', gap: '0.75rem' }}>
                <div>
                  <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>User Email</label>
                  <input
                    type="email"
                    placeholder="user@example.com"
                    value={memberEmail}
                    onChange={(e) => setMemberEmail(e.target.value)}
                    required
                    autoFocus
                    disabled={submitting}
                  />
                </div>
                <div>
                  <label style={{ fontSize: '0.85rem', color: 'var(--muted)' }}>Role</label>
                  <select
                    value={memberRole}
                    onChange={(e) => setMemberRole(Number(e.target.value))}
                    disabled={submitting}
                  >
                    <option value={3}>Viewer (Inspect telemetry & alerts only)</option>
                    <option value={2}>Subadmin (Add/pair instances & admit members)</option>
                    <option value={1}>Admin (Full control & subgroup creation)</option>
                  </select>
                </div>
              </div>
              <div className="modal-actions">
                <button type="button" className="secondary" onClick={() => setShowMemberModal(false)} disabled={submitting}>
                  Cancel
                </button>
                <button type="submit" disabled={submitting || !memberEmail.trim()}>
                  {submitting ? 'Adding...' : 'Add Member'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  )
}
