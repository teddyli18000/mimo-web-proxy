import { useSettings } from '../contexts/SettingsContext'

export function AnimatedBackground() {
  const { resolvedTheme } = useSettings()

  if (resolvedTheme === 'light') {
    return (
      <div className="fixed inset-0 pointer-events-none z-0 overflow-hidden">
        <div className="absolute top-0 right-0 w-[600px] h-[600px] bg-gradient-to-br from-blue-100/40 to-purple-100/30 rounded-full blur-3xl -translate-y-1/2 translate-x-1/4" />
        <div className="absolute bottom-0 left-0 w-[500px] h-[500px] bg-gradient-to-tr from-cyan-100/30 to-blue-100/20 rounded-full blur-3xl translate-y-1/3 -translate-x-1/4" />
      </div>
    )
  }

  return (
    <div className="fixed inset-0 pointer-events-none z-0 overflow-hidden">
      <div className="absolute top-0 right-0 w-[600px] h-[600px] bg-gradient-to-br from-purple-900/20 to-pink-900/10 rounded-full blur-3xl -translate-y-1/2 translate-x-1/4 animate-pulse" style={{ animationDuration: '8s' }} />
      <div className="absolute bottom-0 left-0 w-[500px] h-[500px] bg-gradient-to-tr from-blue-900/15 to-cyan-900/10 rounded-full blur-3xl translate-y-1/3 -translate-x-1/4 animate-pulse" style={{ animationDuration: '12s' }} />
    </div>
  )
}
