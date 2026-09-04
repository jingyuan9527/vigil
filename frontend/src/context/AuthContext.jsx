import { createContext, useContext, useEffect, useState } from 'react'
import { api } from '../api/client'

const AuthCtx = createContext({
  authenticated: false,
  checking: true,
  login: () => {},
  logout: () => {},
  registerDashboardRefresh: () => {},
  dashboardRefresh: null,
})

export function AuthProvider({ children }) {
  const [authenticated, setAuthenticated] = useState(false)
  const [checking, setChecking] = useState(true)
  const [dashboardRefreshFn, setDashboardRefreshFn] = useState(null)

  // 启动时校验 httpOnly cookie 中的令牌（cookie 由后端维护，前端只问结果）
  useEffect(() => {
    api
      .authCheck()
      .then((r) => setAuthenticated(!!r.authenticated))
      .catch(() => setAuthenticated(false))
      .finally(() => setChecking(false))
  }, [])

  // 登录成功后端已写入 cookie，这里只切换本地状态
  const login = () => setAuthenticated(true)

  const logout = async () => {
    try {
      await api.authLogout()
    } catch {
      // 后端不可达也强制登出（清除本地状态）
    }
    setAuthenticated(false)
  }

  // 仪表盘轮询回调：Dashboard mount 时注册、unmount 时注销（null）。
  // 其他页面通过 dashboardRefresh 主动触发仪表盘刷新。
  const registerDashboardRefresh = (fn) => setDashboardRefreshFn(() => fn || null)

  return (
    <AuthCtx.Provider
      value={{ authenticated, checking, login, logout, registerDashboardRefresh, dashboardRefresh: dashboardRefreshFn }}
    >
      {children}
    </AuthCtx.Provider>
  )
}

export function useAuth() {
  return useContext(AuthCtx)
}