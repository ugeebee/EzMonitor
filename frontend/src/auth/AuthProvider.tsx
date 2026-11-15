import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { getMe, login as apiLogin, register as apiRegister } from '../api/endpoints'
import { getToken, setToken, setUnauthorizedHandler } from '../api/client'
import type { GroupWithRole, User } from '../types'
import { AuthContext, type AuthState } from './authContext'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [groups, setGroups] = useState<GroupWithRole[]>([])
  const [loading, setLoading] = useState<boolean>(() => getToken() !== null)

  const clear = useCallback(() => {
    setToken(null)
    setUser(null)
    setGroups([])
  }, [])

  const loadProfile = useCallback(async () => {
    const me = await getMe()
    setUser(me.user)
    setGroups(me.groups)
  }, [])

  // Restore a saved session on first load. State is only set from the
  // promise callbacks (not synchronously in the effect body), and a
  // cancelled flag ignores the result if the effect is torn down first.
  useEffect(() => {
    if (getToken() === null) return
    let cancelled = false
    getMe()
      .then((me) => {
        if (cancelled) return
        setUser(me.user)
        setGroups(me.groups)
      })
      .catch(() => {
        if (!cancelled) clear()
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [clear])

  // Any 401 on an authenticated call logs the user out.
  useEffect(() => {
    setUnauthorizedHandler(clear)
    return () => setUnauthorizedHandler(null)
  }, [clear])

  const login = useCallback(
    async (email: string, password: string) => {
      const res = await apiLogin(email, password)
      setToken(res.token)
      await loadProfile()
    },
    [loadProfile],
  )

  const register = useCallback(
    async (name: string, email: string, password: string) => {
      const res = await apiRegister(name, email, password)
      setToken(res.token)
      await loadProfile()
    },
    [loadProfile],
  )

  const value = useMemo<AuthState>(
    () => ({ user, groups, loading, login, register, logout: clear }),
    [user, groups, loading, login, register, clear],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
