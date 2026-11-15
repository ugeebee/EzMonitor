import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './auth/AuthProvider'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { Layout } from './components/Layout'
import AlertsPage from './pages/AlertsPage'
import GroupPage from './pages/GroupPage'
import Home from './pages/Home'
import InstancePage from './pages/InstancePage'
import Login from './pages/Login'

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<Layout />}>
              <Route path="/" element={<Home />} />
              <Route path="/groups/:gid" element={<GroupPage />} />
              <Route path="/groups/:gid/alerts" element={<AlertsPage />} />
              <Route path="/subgroups/:sid/instances/:iid" element={<InstancePage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}
