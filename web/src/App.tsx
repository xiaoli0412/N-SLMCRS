import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import './i18n'
import { ThemeProvider } from './components/theme-provider'
import { Layout } from './components/Layout'
import { TooltipProvider } from './components/ui'
import Overview from './pages/Overview'
import Models from './pages/Models'
import Operations from './pages/Operations'
import Keys from './pages/Keys'
import Distribution from './pages/Distribution'
import AutoPilot from './pages/AutoPilot'
import Logs from './pages/Logs'
import Backup from './pages/Backup'
import Settings from './pages/Settings'
import Circuit from './pages/Circuit'
import Playground from './pages/Playground'

const qc = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1, staleTime: 15_000 } },
})

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <TooltipProvider delayDuration={200}>
          <BrowserRouter>
            <Layout>
              <Routes>
                <Route path="/" element={<Overview />} />
                <Route path="/models" element={<Models />} />
                <Route path="/playground" element={<Playground />} />
                <Route path="/circuit" element={<Circuit />} />
                <Route path="/operations" element={<Operations />} />
                <Route path="/keys" element={<Keys />} />
                <Route path="/distribution" element={<Distribution />} />
                <Route path="/autopilot" element={<AutoPilot />} />
                <Route path="/logs" element={<Logs />} />
                <Route path="/backup" element={<Backup />} />
                <Route path="/settings" element={<Settings />} />
              </Routes>
            </Layout>
          </BrowserRouter>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
