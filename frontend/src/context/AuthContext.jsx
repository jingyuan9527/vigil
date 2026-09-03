import { createContext, useContext, useEffect, useState } from 'react'

const AuthCtx = createContext({ token: null, isAuthenticated: false, login: () => {}, logout: () => {} })

export function AuthProvider({ children }) {
  const [token, setToken] = useState(() => localStorage.getItem('dockmon_token'))

  useEffect(() => {
    if (token) {
      localStorage.setItem('dockmon_token', token)
    } else {
      localStorage.removeItem('dockmon_token')
    }
  }, [token])

  const login = (t) => setToken(t)
  const logout = () => setToken(null)

  return (
    <AuthCtx.Provider value={{ token, isAuthenticated: !!token, login, logout }}>
      {children}
    </AuthCtx.Provider>
  )
}

export function useAuth() {
  return useContext(AuthCtx)
}
