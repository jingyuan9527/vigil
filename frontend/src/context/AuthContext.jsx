import { createContext, useContext, useState } from 'react'

const AuthCtx = createContext({ token: null, isAuthenticated: false, login: () => {}, logout: () => {} })

export function AuthProvider({ children }) {
  const [token, setToken] = useState(() => localStorage.getItem('dockmon_token'))

  const login = (t) => {
    localStorage.setItem('dockmon_token', t)
    setToken(t)
  }
  const logout = () => {
    localStorage.removeItem('dockmon_token')
    setToken(null)
  }

  return (
    <AuthCtx.Provider value={{ token, isAuthenticated: !!token, login, logout }}>
      {children}
    </AuthCtx.Provider>
  )
}

export function useAuth() {
  return useContext(AuthCtx)
}
