import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { lazy, Suspense } from 'react'
import './i18n'
import { ThemeProvider } from './components/theme-provider'
import { Layout } from './components/Layout'
import { TooltipProvider, Skeleton } from './components/ui'
import { ErrorBoundary } from './components/ErrorBoundary'

// v0.13：路由级代码分割。重依赖（recharts/framer-motion）随各自路由懒加载，
// 首屏（Overview）不再被 Models/Playground 等重页拖累，首屏 JS 显著瘦身。
const Overview = lazy(() => import('./pages/Overview'))
const Models = lazy(() => import('./pages/Models'))
const Playground = lazy(() => import('./pages/Playground'))
const Circuit = lazy(() => import('./pages/Circuit'))
const Operations = lazy(() => import('./pages/Operations'))
const Strategy = lazy(() => import('./pages/Strategy'))
const Keys = lazy(() => import('./pages/Keys'))
const Distribution = lazy(() => import('./pages/Distribution'))
const AutoPilot = lazy(() => import('./pages/AutoPilot'))
const Logs = lazy(() => import('./pages/Logs'))
const Backup = lazy(() => import('./pages/Backup'))
const Settings = lazy(() => import('./pages/Settings'))

const qc = new QueryClient({
  defaultOptions: { queries: { refetchOnWindowFocus: false, retry: 1, staleTime: 15_000 } },
})

function PageFallback() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <Skeleton className="h-8 w-48" />
    </div>
  )
}

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <TooltipProvider delayDuration={200}>
          <BrowserRouter>
            <Layout>
              {/* 全局错误边界：子树渲染错误不再白屏整页 */}
              <ErrorBoundary>
                <Suspense fallback={<PageFallback />}>
                  <Routes>
                    <Route path="/" element={<Overview />} />
                    <Route path="/models" element={<Models />} />
                    <Route path="/playground" element={<Playground />} />
                    <Route path="/circuit" element={<Circuit />} />
                    <Route path="/operations" element={<Operations />} />
                    <Route path="/strategy" element={<Strategy />} />
                    <Route path="/keys" element={<Keys />} />
                    <Route path="/distribution" element={<Distribution />} />
                    <Route path="/autopilot" element={<AutoPilot />} />
                    <Route path="/logs" element={<Logs />} />
                    <Route path="/backup" element={<Backup />} />
                    <Route path="/settings" element={<Settings />} />
                  </Routes>
                </Suspense>
              </ErrorBoundary>
            </Layout>
          </BrowserRouter>
        </TooltipProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
