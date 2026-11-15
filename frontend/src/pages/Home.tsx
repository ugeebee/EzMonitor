import { Link } from 'react-router-dom'
import { useAuth } from '../auth/useAuth'

export default function Home() {
  const { groups } = useAuth()

  return (
    <>
      <h2>Your groups</h2>
      {groups.length === 0 ? (
        <p className="muted">
          You aren&apos;t in any group yet. Create one, or ask an admin to add you by email.
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
    </>
  )
}
