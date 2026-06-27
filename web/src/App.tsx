import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Overview from './pages/Overview'
import Operations from './pages/Operations'
import Logs from './pages/Logs'
import Models from './pages/Models'
import ModelDetail from './pages/ModelDetail'
import Keys from './pages/Keys'
import Distribution from './pages/Distribution'
import AutoPilot from './pages/AutoPilot'
import Settings from './pages/Settings'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Overview />} />
        <Route path="operations" element={<Operations />} />
        <Route path="logs" element={<Logs />} />
        <Route path="models" element={<Models />} />
        <Route path="models/:id" element={<ModelDetail />} />
        <Route path="keys" element={<Keys />} />
        <Route path="distribution" element={<Distribution />} />
        <Route path="autopilot" element={<AutoPilot />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}
