import { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { translations, Lang, TranslationKey } from '../i18n'

export type Theme = 'light' | 'dark' | 'system'

interface SettingsContextType {
  lang: Lang
  setLang: (lang: Lang) => void
  theme: Theme
  setTheme: (theme: Theme) => void
  t: (key: TranslationKey) => string
  resolvedTheme: 'light' | 'dark'
}

const SettingsContext = createContext<SettingsContextType | null>(null)

function getStored<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(key)
    return v ? (JSON.parse(v) as T) : fallback
  } catch {
    return fallback
  }
}

function resolveSystemTheme(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function SettingsProvider({ children }: { children: ReactNode }) {
  const [lang, setLang] = useState<Lang>(() => getStored('lang', 'zh'))
  const [theme, setTheme] = useState<Theme>(() => getStored('theme', 'light'))
  const [systemTheme, setSystemTheme] = useState<'light' | 'dark'>(resolveSystemTheme)

  const resolvedTheme = theme === 'system' ? systemTheme : theme

  // Listen for system theme changes
  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const handler = () => setSystemTheme(resolveSystemTheme())
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [])

  // Apply theme class to html
  useEffect(() => {
    const root = document.documentElement
    root.classList.remove('light', 'dark')
    root.classList.add(resolvedTheme)
    root.setAttribute('data-theme', resolvedTheme)
  }, [resolvedTheme])

  // Persist
  useEffect(() => { localStorage.setItem('lang', JSON.stringify(lang)) }, [lang])
  useEffect(() => { localStorage.setItem('theme', JSON.stringify(theme)) }, [theme])

  const t = (key: TranslationKey): string => translations[lang][key]

  return (
    <SettingsContext.Provider value={{ lang, setLang, theme, setTheme, t, resolvedTheme }}>
      {children}
    </SettingsContext.Provider>
  )
}

export function useSettings() {
  const ctx = useContext(SettingsContext)
  if (!ctx) throw new Error('useSettings must be inside SettingsProvider')
  return ctx
}
