import { Link, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'

export function Layout() {
  const { user, logout } = useAuth()

  return (
    <div className="shell">
      <header className="topbar">
        <Link to="/" className="brand">
          EzMonitor
        </Link>
        <div className="topbar-right">
          <span className="muted">{user?.name}</span>
          <button type="button" className="secondary" onClick={logout}>
            Sign out
          </button>
        </div>
      </header>
      <main className="content">
        <Outlet />
      </main>
    </div>
  )
}
