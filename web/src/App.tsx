import { useState } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Dashboard } from './components/Dashboard'
import { ConfigPanel } from './components/ConfigPanel'
import { Sidebar } from './components/Sidebar'
import { AnimatedBackground } from './components/AnimatedBackground'
import { SettingsProvider, useSettings } from './contexts/SettingsContext'

type Page = 'dashboard' | 'config'

function AppInner() {
  const [page, setPage] = useState<Page>('dashboard')
  const { resolvedTheme } = useSettings()

  return (
    <div className={`min-h-screen bg-grid bg-glow relative overflow-hidden ${resolvedTheme === 'dark' ? 'bg-gray-950' : 'bg-[#f8f9fc]'}`}>
      <AnimatedBackground />

      <div className="relative z-10 flex min-h-screen">
        <Sidebar current={page} onNavigate={setPage} />

        <main className="flex-1 p-6 lg:p-8 overflow-y-auto">
          <AnimatePresence mode="wait">
            {page === 'dashboard' && (
              <motion.div
                key="dashboard"
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -20 }}
                transition={{ duration: 0.3, ease: 'easeOut' }}
              >
                <Dashboard />
              </motion.div>
            )}
            {page === 'config' && (
              <motion.div
                key="config"
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -20 }}
                transition={{ duration: 0.3, ease: 'easeOut' }}
              >
                <ConfigPanel />
              </motion.div>
            )}
          </AnimatePresence>
        </main>
      </div>
    </div>
  )
}

export default function App() {
  return (
    <SettingsProvider>
      <AppInner />
    </SettingsProvider>
  )
}
