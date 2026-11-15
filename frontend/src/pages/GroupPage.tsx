import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { getFleet, listInstances, listMembers, listSubgroups } from '../api/endpoints'
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
  const { groups } = useAuth()
  const [now] = useState(() => Date.now())

  const title =
    groups.find((g) => g.g_id === id)?.label ??
    (location.state as { label?: string } | null)?.label ??
    `Group ${id}`

  const instances = useQuery({ queryKey: ['instances', id], queryFn: () => listInstances(id) })
  const fleet = useQuery({
    queryKey: ['fleet', id],
    queryFn: () => getFleet(id),
    refetchInterval: 30_000,
  })
  // Subgroups and members can be forbidden or empty depending on the group type; we just hide them then.
  const subgroups = useQuery({ queryKey: ['subgroups', id], queryFn: () => listSubgroups(id) })
  const members = useQuery({ queryKey: ['members', id], queryFn: () => listMembers(id) })

  if (Number.isNaN(id)) return <p className="error">Invalid group id.</p>

  const fleetRows = fleet.data?.instances ?? []
  const reporting = fleetRows.filter((r) => fleetState(r) === 'up').length
  const silent = fleetRows.filter((r) => fleetState(r) === 'silent').length
  const activeAlerts = fleetRows.reduce((sum, r) => sum + r.active_alerts, 0)

  return (
    <>
      <p className="crumbs">
        <Link to="/">← All groups</Link>
      </p>
      <div className="title-row">
        <h2>{title}</h2>
        <Link to={`/groups/${id}/alerts`} state={{ label: title }} className="header-link">
          Alerts{fleet.data ? ` (${activeAlerts} active)` : ''} →
        </Link>
      </div>

      {MOCK_QUERY_API && (
        <p className="notice">
          Demo data: the read API isn&apos;t connected yet, so live values and alerts below are synthetic.
        </p>
      )}

      {subgroups.data && subgroups.data.length > 0 && (
        <section className="section">
          <h3>Subgroups</h3>
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
        </section>
      )}

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

      <section className="section">
        <h3>Instances</h3>

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

      {members.data && members.data.length > 0 && (
        <section className="section">
          <h3>Members</h3>
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
        </section>
      )}
    </>
  )
}
