import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/useAuth'

export default function Login() {
  const { user, login, register } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/'

  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (user) return <Navigate to={from} replace />

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      if (mode === 'login') await login(email, password)
      else await register(name, email, password)
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Something went wrong. Try again.')
    } finally {
      setBusy(false)
    }
  }

  const isRegister = mode === 'register'

  return (
    <main className="auth-page">
      <section className="auth-hero">
        <div className="brand-lockup">
          <span className="brand-mark" aria-hidden="true" />
          EzMonitor
        </div>
        <h1>
          See every machine.
          <br />
          <span>Catch problems before your users do.</span>
        </h1>
        <p>
          Multi-tenant metrics monitoring where the database does the heavy lifting: constraints, triggers,
          partitions and rollups, all in PostgreSQL.
        </p>
        <ul className="hero-points">
          <li>Live CPU, GPU, RAM and network telemetry from every paired instance</li>
          <li>In-database alert rules with signed webhook notifications</li>
          <li>Role-based access per group and subgroup (admin, subadmin, viewer)</li>
        </ul>
        <div className="hero-spark" aria-hidden="true">
          <svg viewBox="0 0 600 120" fill="none">
            <defs>
              <linearGradient id="sparkStroke" x1="0" x2="1" y1="0" y2="0">
                <stop offset="0" stopColor="#22d3ee" />
                <stop offset="1" stopColor="#8b5cf6" />
              </linearGradient>
              <linearGradient id="sparkFill" x1="0" x2="0" y1="0" y2="1">
                <stop offset="0" stopColor="#22d3ee" stopOpacity="0.28" />
                <stop offset="1" stopColor="#22d3ee" stopOpacity="0" />
              </linearGradient>
            </defs>
            <path
              d="M0 88 C40 80 60 96 100 70 S160 40 200 62 S260 104 300 66 S360 18 400 44 S470 84 510 50 S570 22 600 34 L600 120 L0 120 Z"
              fill="url(#sparkFill)"
            />
            <path
              d="M0 88 C40 80 60 96 100 70 S160 40 200 62 S260 104 300 66 S360 18 400 44 S470 84 510 50 S570 22 600 34"
              stroke="url(#sparkStroke)"
              strokeWidth="2.5"
              strokeLinecap="round"
            />
          </svg>
        </div>
      </section>

      <section className="auth-panel">
        <div className="auth-card">
          <h2>{isRegister ? 'Create your account' : 'Welcome back'}</h2>
          <p className="muted">
            {isRegister ? 'Create an account to start monitoring.' : 'Sign in to view your instances.'}
          </p>

          <form onSubmit={onSubmit}>
            {isRegister && (
              <label>
                Name
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  autoComplete="name"
                  placeholder="Ada Lovelace"
                  required
                />
              </label>
            )}
            <label>
              Email
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoComplete="email"
                placeholder="you@example.com"
                required
              />
            </label>
            <label>
              Password
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={isRegister ? 'new-password' : 'current-password'}
                minLength={isRegister ? 6 : undefined}
                placeholder="••••••••"
                required
              />
            </label>

            {error && (
              <p className="error" role="alert">
                {error}
              </p>
            )}

            <button type="submit" disabled={busy}>
              {busy ? 'Please wait…' : isRegister ? 'Create account' : 'Sign in'}
            </button>
          </form>

          <button
            type="button"
            className="link"
            onClick={() => {
              setMode(isRegister ? 'login' : 'register')
              setError(null)
            }}
          >
            {isRegister ? 'Already have an account? Sign in' : 'New here? Create an account'}
          </button>
        </div>
      </section>
    </main>
  )
}
