import { Routes, Route } from 'react-router-dom'
import { ThemeProvider } from './context/ThemeContext'
import Layout from './components/Layout'
import Dashboard from './pages/Dashboard'
import Images from './pages/Images'
import Compare from './pages/Compare'
import Notifications from './pages/Notifications'

export default function App() {
  return (
    <ThemeProvider>
      <Routes>
        <Route element={<Layout />}>
          <Route index element={<Dashboard />} />
          <Route path="images" element={<Images />} />
          <Route path="compare" element={<Compare />} />
          <Route path="notifications" element={<Notifications />} />
          <Route path="*" element={<Dashboard />} />
        </Route>
      </Routes>
    </ThemeProvider>
  )
}
