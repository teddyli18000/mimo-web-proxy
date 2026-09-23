import { motion } from 'framer-motion'
import { LayoutDashboard, Settings, Zap, Sun, Moon, Monitor, Globe } from 'lucide-react'
import { useSettings, Theme } from '../contexts/SettingsContext'

type Page = 'dashboard' | 'config'

interface SidebarProps {
  current: Page
  onNavigate: (page: Page) => void
}

export function Sidebar({ current, onNavigate }: SidebarProps) {
  const { lang, setLang, theme, setTheme, t, resolvedTheme } = useSettings()
  const isDark = resolvedTheme === 'dark'

  const navItems = [
    { id: 'dashboard' as Page, label: t('dashboard'), icon: LayoutDashboard },
    { id: 'config' as Page, label: t('config'), icon: Settings },
  ]

  const themeOptions: { value: Theme; icon: typeof Sun; label: string }[] = [
    { value: 'light', icon: Sun, label: t('light') },
    { value: 'dark', icon: Moon, label: t('dark') },
    { value: 'system', icon: Monitor, label: t('system') },
  ]

  return (
    <aside className={`w-16 lg:w-64 flex-shrink-0 flex flex-col ${
      isDark ? 'border-r border-white/[0.06]' : 'border-r border-gray-200'
    }`}>
      {/* Logo */}
      <div className={`h-16 flex items-center justify-center lg:justify-start lg:px-6 ${
        isDark ? 'border-b border-white/[0.06]' : 'border-b border-gray-200'
      }`}>
        <motion.div className="flex items-center gap-3" whileHover={{ scale: 1.02 }}>
          <div className="w-8 h-8 rounded-lg bg-gradient-to-br from-purple-500 to-pink-500 flex items-center justify-center glow-purple">
            <Zap className="w-4 h-4 text-white" />
          </div>
          <span className="hidden lg:block text-lg font-semibold gradient-text">
            MiMo Web Proxy
          </span>
        </motion.div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-2 lg:p-4 space-y-1">
        {navItems.map((item) => {
          const isActive = current === item.id
          return (
            <motion.button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={`w-full flex items-center gap-3 px-3 py-2.5 rounded-xl transition-all duration-200 ${
                isDark
                  ? isActive
                    ? 'bg-purple-500/10 text-purple-300 border border-purple-500/20'
                    : 'text-gray-400 hover:text-gray-200 hover:bg-white/[0.04]'
                  : isActive
                    ? 'bg-purple-500/10 text-purple-600 border border-purple-500/15'
                    : 'text-gray-500 hover:text-gray-900 hover:bg-black/[0.04]'
              }`}
              whileHover={{ x: 2 }}
              whileTap={{ scale: 0.98 }}
            >
              <item.icon className="w-5 h-5 flex-shrink-0" />
              <span className="hidden lg:block text-sm font-medium">{item.label}</span>
            </motion.button>
          )
        })}
      </nav>

      {/* Theme & Language Controls */}
      <div className={`p-3 lg:p-4 space-y-3 border-t ${
        isDark ? 'border-white/[0.06]' : 'border-gray-200'
      }`}>
        {/* Theme */}
        <div className="hidden lg:block">
          <p className={`text-xs mb-2 ${isDark ? 'text-gray-500' : 'text-gray-400'}`}>
            {isDark ? '🌙' : '☀️'} {isDark ? t('dark') : t('light')}
          </p>
          <div className={`flex rounded-lg p-0.5 ${isDark ? 'bg-white/[0.05]' : 'bg-gray-100'}`}>
            {themeOptions.map((opt) => (
              <button
                key={opt.value}
                onClick={() => setTheme(opt.value)}
                className={`flex-1 flex items-center justify-center gap-1 px-2 py-1.5 rounded-md text-xs transition-all ${
                  theme === opt.value
                    ? isDark
                      ? 'bg-purple-500/20 text-purple-300'
                      : 'bg-white text-purple-600 shadow-sm'
                    : isDark
                      ? 'text-gray-500 hover:text-gray-300'
                      : 'text-gray-400 hover:text-gray-600'
                }`}
                title={opt.label}
              >
                <opt.icon className="w-3.5 h-3.5" />
              </button>
            ))}
          </div>
        </div>

        {/* Language */}
        <div className="hidden lg:block">
          <div className={`flex rounded-lg p-0.5 ${isDark ? 'bg-white/[0.05]' : 'bg-gray-100'}`}>
            <button
              onClick={() => setLang('zh')}
              className={`flex-1 flex items-center justify-center gap-1 px-2 py-1.5 rounded-md text-xs transition-all ${
                lang === 'zh'
                  ? isDark ? 'bg-purple-500/20 text-purple-300' : 'bg-white text-purple-600 shadow-sm'
                  : isDark ? 'text-gray-500 hover:text-gray-300' : 'text-gray-400 hover:text-gray-600'
              }`}
            >
              中文
            </button>
            <button
              onClick={() => setLang('en')}
              className={`flex-1 flex items-center justify-center gap-1 px-2 py-1.5 rounded-md text-xs transition-all ${
                lang === 'en'
                  ? isDark ? 'bg-purple-500/20 text-purple-300' : 'bg-white text-purple-600 shadow-sm'
                  : isDark ? 'text-gray-500 hover:text-gray-300' : 'text-gray-400 hover:text-gray-600'
              }`}
            >
              EN
            </button>
          </div>
        </div>

        {/* Mobile: compact toggles */}
        <div className="lg:hidden flex flex-col items-center gap-2">
          <button
            onClick={() => setTheme(theme === 'dark' ? 'light' : theme === 'light' ? 'dark' : 'light')}
            className={`p-2 rounded-lg ${isDark ? 'text-gray-400 hover:bg-white/[0.05]' : 'text-gray-500 hover:bg-gray-100'}`}
          >
            {isDark ? <Moon className="w-4 h-4" /> : <Sun className="w-4 h-4" />}
          </button>
          <button
            onClick={() => setLang(lang === 'zh' ? 'en' : 'zh')}
            className={`p-2 rounded-lg ${isDark ? 'text-gray-400 hover:bg-white/[0.05]' : 'text-gray-500 hover:bg-gray-100'}`}
          >
            <Globe className="w-4 h-4" />
          </button>
        </div>

        {/* Version */}
        <p className={`hidden lg:block text-xs text-center ${isDark ? 'text-gray-600' : 'text-gray-400'}`}>
          v1.1.0 · MiMo Web Proxy
        </p>
      </div>
    </aside>
  )
}
