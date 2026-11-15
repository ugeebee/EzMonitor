import { useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { createGroup } from '../api/endpoints'
import { useAuth } from '../auth/useAuth'

export default function Home() {
  const { groups, refreshProfile } = useAuth()
  const [showModal, setShowModal] = useState(false)
  const [label, setLabel] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault()
    if (!label.trim()) return
    setError(null)
    setSubmitting(true)
    try {
      await createGroup(label.trim())
      setLabel('')
      setShowModal(false)
      await refreshProfile()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create group')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
        <h2>Your groups</h2>
        <button onClick={() => setShowModal(true)}>+ Create Group</button>
      </div>

      {groups.length === 0 ? (
        <p className="muted">
          You aren&apos;t in any group yet. Click &quot;+ Create Group&quot; above, or ask an admin to add you by email.
        </p>
      ) : (
        <ul className="group-list">
          {groups.map((g) => (
            <li key={g.g_id}>
              <Link to={`/groups/${g.g_id}`} state={{ label: g.label }}>
                <span>{g.label}</span>
                <span className={`role role-${g.role_name}`}>{g.role_name}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {showModal && (
        <div className="modal-overlay" onClick={() => !submitting && setShowModal(false)}>
          <div className="modal-card" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>Create New Group</h3>
              <button className="secondary sm" onClick={() => setShowModal(false)} disabled={submitting}>✕</button>
            </div>
            <form onSubmit={handleCreate}>
              {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}
              <div>
                <label htmlFor="group-name" style={{ fontSize: '0.9rem', color: 'var(--muted)' }}>Group Name</label>
                <input
                  id="group-name"
                  type="text"
                  placeholder="e.g. Production Cluster, Infra East"
                  value={label}
                  onChange={(e) => setLabel(e.target.value)}
                  autoFocus
                  required
                  disabled={submitting}
                />
              </div>
              <div className="modal-actions">
                <button type="button" className="secondary" onClick={() => setShowModal(false)} disabled={submitting}>
                  Cancel
                </button>
                <button type="submit" disabled={submitting || !label.trim()}>
                  {submitting ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </>
  )
}
