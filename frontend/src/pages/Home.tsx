import { useAuth } from '../auth/useAuth'

export default function Home() {
  const { user, groups, logout } = useAuth()

  return (
    <div className="shell">
      <header className="topbar">
        <strong>EzMonitor</strong>
        <div className="topbar-right">
          <span className="muted">{user?.name}</span>
          <button type="button" className="secondary" onClick={logout}>
            Sign out
          </button>
        </div>
      </header>

      <main className="content">
        <h2>Your groups</h2>
        {groups.length === 0 ? (
          <p className="muted">
            You aren&apos;t in any group yet. Create one, or ask an admin to add you by email.
          </p>
        ) : (
          <ul className="group-list">
            {groups.map((g) => (
              <li key={g.g_id}>
                <span>{g.label}</span>
                <span className={`role role-${g.role_name}`}>{g.role_name}</span>
              </li>
            ))}
          </ul>
        )}
      </main>
    </div>
  )
}
