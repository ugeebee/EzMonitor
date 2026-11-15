import { createContext } from 'react'
import type { GroupWithRole, User } from '../types'

export interface AuthState {
  user: User | null
  groups: GroupWithRole[]
  loading: boolean // true while restoring a saved session
  login: (email: string, password: string) => Promise<void>
  register: (name: string, email: string, password: string) => Promise<void>
  refreshProfile: () => Promise<void>
  logout: () => void
}

export const AuthContext = createContext<AuthState | null>(null)
