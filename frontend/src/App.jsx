import { Routes, Route } from 'react-router-dom'
import { ThemeProvider } from './context/ThemeContext'
import { AuthProvider, useAuth } from './context/AuthContext'
import { ToastProvider } from './components/Toast'
import ErrorBoundary from './components/ErrorBoundary'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Images from './pages/Images'
import Compare from './pages/Compare'
import Notifications from './pages/Notifications'
import Settings from './pages/Settings'

function AppRoutes() {
  const { authenticated, checking } = useAuth()

  // 校验 cookie 期间避免登录页闪现
  if (checking) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-zinc-50 dark:bg-zinc-900">
        <span className="h-5 w-5 animate-spin rounded-full border-2 border-zinc-300 border-t-blue-500 dark:border-zinc-600 dark:border-t-blue-500" />
      </div>
    )
  }

  if (!authenticated) {
    return <Login />
  }

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="images" element={<Images />} />
        <Route path="compare" element={<Compare />} />
        <Route path="notifications" element={<Notifications />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Dashboard />} />
      </Route>
    </Routes>
  )
}

export default function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <ToastProvider>
          <AuthProvider>
            <AppRoutes />
          </AuthProvider>
        </ToastProvider>
      </ThemeProvider>
    </ErrorBoundary>
  )
}
