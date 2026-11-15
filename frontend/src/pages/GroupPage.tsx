import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import { listInstances, listMembers, listSubgroups } from '../api/endpoints'
import { useAuth } from '../auth/useAuth'
import { StatusBadge } from '../components/StatusBadge'
import { timeAgo } from '../format'

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
  // Subgroups and members can be forbidden or empty depending on the group type; we just hide them then.
  const subgroups = useQuery({ queryKey: ['subgroups', id], queryFn: () => listSubgroups(id) })
  const members = useQuery({ queryKey: ['members', id], queryFn: () => listMembers(id) })

  if (Number.isNaN(id)) return <p className="error">Invalid group id.</p>

  return (
    <>
      <p className="crumbs">
        <Link to="/">← All groups</Link>
      </p>
      <h2>{title}</h2>

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

      <section className="section">
        <h3>Instances</h3>
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
                  <th>Last seen</th>
                </tr>
              </thead>
              <tbody>
                {instances.data.map((i) => (
                  <tr key={i.i_id}>
                    <td>
                      <Link to={`/subgroups/${id}/instances/${i.i_id}`} state={{ groupLabel: title }}>
                        {i.label}
                      </Link>
                    </td>
                    <td>
                      <StatusBadge status={i.status} enabled={i.enabled} />
                    </td>
                    <td>{i.cpu_model ?? '—'}</td>
                    <td>{i.ram_config ?? '—'}</td>
                    <td>{i.gpu_model ?? '—'}</td>
                    <td>{i.last_seen_at ? timeAgo(i.last_seen_at, now) : '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
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
