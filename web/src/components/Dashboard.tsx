import { useState, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Activity, Zap, Cpu, Hash, BarChart3, Clock, List, Download, ChevronLeft, ChevronRight } from 'lucide-react'
import { ComposedChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Line, CartesianGrid } from 'recharts'
import { useSettings } from '../contexts/SettingsContext'
import { apiFetch } from '../lib/api'

interface UsageRecord {
  timestamp: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
}

interface DailyStats {
  date: string
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  reasoning_tokens: number
  total_tokens: number
  request_count: number
}

interface ModelStats {
  model: string
  prompt_tokens: number
  completion_tokens: number
  cached_tokens: number
  total_tokens: number
  request_count: number
}

interface StatsData {
  total: {
    prompt_tokens: number
    completion_tokens: number
    cached_tokens: number
    reasoning_tokens: number
    total_tokens: number
    request_count: number
    cache_hit_rate: number
  }
  by_day: DailyStats[]
  by_model: ModelStats[]
  concurrency: number
  recent: UsageRecord[]
}

type ViewMode = 'chart' | 'list'
type TimeRange = '7d' | '30d'

const MODEL_COLORS: Record<string, string> = {
  'mimo-v2.6-pro': '#3b82f6',                     // blue-500
  'mimo-v2.6-flash': '#22d3ee',                   // cyan-400
  'mimo-v2.6-pro-ultraspeed-studio': '#8b5cf6',   // violet-500
  'mimo-v2.5': '#10b981',                          // emerald-500
  'mimo-v2.5-pro': '#f59e0b',                      // amber-500
}
const FALLBACK_COLORS = ['#6366f1', '#ec4899', '#14b8a6', '#eab308', '#f97316']
function colorFor(model: string, idx: number): string {
  return MODEL_COLORS[model] ?? FALLBACK_COLORS[idx % FALLBACK_COLORS.length]
}
const COLORS = {
  total: '#6366f1',       // indigo-500
  grid: '#e5e7eb',
  text: '#6b7280',
  textDark: '#1f2937',
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M'
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K'
  return n.toLocaleString()
}

function formatNumber(n: number): string {
  return n.toLocaleString()
}


const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.06 } },
}
const item = {
  hidden: { opacity: 0, y: 16 },
  show: { opacity: 1, y: 0 },
}

export function Dashboard() {
  const { t, lang, resolvedTheme } = useSettings()
  const isDark = resolvedTheme === 'dark'
  const [stats, setStats] = useState<StatsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [viewMode, setViewMode] = useState<ViewMode>('chart')
  const [timeRange, setTimeRange] = useState<TimeRange>('30d')
  const [page, setPage] = useState(0)
  const PAGE_SIZE = 10

  const fetchStats = async () => {
    try {
      const resp = await apiFetch('/admin/api/stats')
      if (resp.ok) setStats(await resp.json())
    } catch (e) { console.error(e) }
    finally { setLoading(false) }
  }

  useEffect(() => {
    fetchStats()
    const iv = setInterval(fetchStats, 5000)
    return () => clearInterval(iv)
  }, [])

  const s = stats?.total

  // --- Chart data: group by day + model (dynamic model keys) ---
  const modelsInData: string[] = []
  const modelSeen = new Set<string>()
  const dailyModelMap: Record<string, any> = {}
  for (const r of stats?.recent ?? []) {
    if (r.model && !modelSeen.has(r.model)) { modelSeen.add(r.model); modelsInData.push(r.model) }
    const day = r.timestamp.slice(0, 10)
    if (!dailyModelMap[day]) dailyModelMap[day] = { date: day, total: 0 }
    if (!dailyModelMap[day][r.model]) dailyModelMap[day][r.model] = 0
    dailyModelMap[day][r.model] += r.total_tokens
    dailyModelMap[day].total += r.total_tokens
  }
  // Also inject by_day data for historical context
  for (const d of stats?.by_day ?? []) {
    if (!dailyModelMap[d.date]) dailyModelMap[d.date] = { date: d.date, total: 0 }
  }
  const chartData = Object.values(dailyModelMap).sort((a: any, b: any) => a.date.localeCompare(b.date))

  // --- List data: flat table rows by date+model ---
  const tableRows: { date: string; model: string; total: number; inputCached: number; inputMiss: number; output: number }[] = []
  const dateModelSet = new Set<string>()
  for (const r of stats?.recent ?? []) {
    const day = r.timestamp.slice(0, 10)
    const key = `${day}|${r.model}`
    if (!dateModelSet.has(key)) {
      dateModelSet.add(key)
      // Aggregate all records for this date+model
      const sameRecords = (stats?.recent ?? []).filter(x => x.timestamp.slice(0, 10) === day && x.model === r.model)
      const agg = sameRecords.reduce((acc, x) => ({
        total: acc.total + x.total_tokens,
        cached: acc.cached + x.cached_tokens,
        prompt: acc.prompt + x.prompt_tokens,
        completion: acc.completion + x.completion_tokens,
      }), { total: 0, cached: 0, prompt: 0, completion: 0 })
      tableRows.push({
        date: day,
        model: r.model,
        total: agg.total,
        inputCached: agg.cached,
        inputMiss: agg.prompt - agg.cached,
        output: agg.completion,
      })
    }
  }
  tableRows.sort((a, b) => b.date.localeCompare(a.date) || a.model.localeCompare(b.model))

  const totalPages = Math.max(1, Math.ceil(tableRows.length / PAGE_SIZE))
  const pagedRows = tableRows.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)

  const CustomTooltip = ({ active, payload, label }: any) => {
    if (!active || !payload?.length) return null
    return (
      <div className="bg-white rounded-lg shadow-lg border border-gray-200 p-3 text-xs">
        <p className="font-semibold text-gray-800 mb-1.5">{label}</p>
        {payload.map((p: any, i: number) => (
          <div key={i} className="flex items-center gap-2 py-0.5">
            <span className="w-2.5 h-2.5 rounded-full" style={{ background: p.color }} />
            <span className="text-gray-500">{p.name}</span>
            <span className="ml-auto font-mono font-medium text-gray-800">{formatNumber(p.value)}</span>
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <motion.h1 className={`text-2xl font-bold ${isDark ? 'text-white' : 'text-gray-800'}`} initial={{ opacity: 0, x: -20 }} animate={{ opacity: 1, x: 0 }}>
            {t('dashboardTitle')}
          </motion.h1>
          <motion.p className="text-gray-400 text-sm mt-0.5" initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ delay: 0.1 }}>
            {t('dashboardSubtitle')}
          </motion.p>
        </div>
        {/* Stats Cards */}
        <motion.div className="flex gap-3" variants={container} initial="hidden" animate="show">
          {[
            { label: t('totalRequests'), value: s?.request_count ?? 0, icon: Activity, color: 'text-blue-500' },
            { label: t('totalTokens'), value: formatTokens(s?.total_tokens ?? 0), icon: Hash, color: 'text-indigo-500' },
            { label: t('cacheHitRate'), value: s ? s.cache_hit_rate.toFixed(1) + '%' : '0%', icon: Zap, color: 'text-emerald-500' },
            { label: t('concurrency'), value: stats?.concurrency ?? 0, icon: Cpu, color: 'text-orange-500' },
          ].map((stat) => (
            <motion.div key={stat.label} variants={item} className={`rounded-xl border px-4 py-3 min-w-[120px] ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`}>
              <div className="flex items-center gap-2 mb-1">
                <stat.icon className={`w-4 h-4 ${stat.color}`} />
                <span className="text-xs text-gray-400">{stat.label}</span>
              </div>
              <p className={`text-xl font-bold ${isDark ? 'text-white' : 'text-gray-800'}`}>{loading ? '...' : stat.value}</p>
            </motion.div>
          ))}
        </motion.div>
      </div>

      {/* Controls Bar */}
      <motion.div className="flex items-center justify-between" initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.2 }}>
        {/* Left: View toggle */}
        <div className="flex items-center gap-2">
          <span className="text-sm text-gray-400 mr-1">{t('totalTokens')}</span>
          <div className={`flex rounded-lg p-0.5 ${isDark ? 'bg-gray-700' : 'bg-gray-100'}`}>
            {[
              { id: 'chart' as ViewMode, icon: BarChart3, label: lang === 'zh' ? '图表' : 'Chart' },
              { id: 'list' as ViewMode, icon: List, label: lang === 'zh' ? '列表' : 'List' },
            ].map(v => (
              <button
                key={v.id}
                onClick={() => { setViewMode(v.id); setPage(0) }}
                className={`flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium transition-all ${
                  viewMode === v.id
                    ? 'bg-white text-blue-600 shadow-sm'
                    : 'text-gray-400 hover:text-gray-600'
                }`}
              >
                <v.icon className="w-3.5 h-3.5" />
                {v.label}
              </button>
            ))}
          </div>
        </div>

        {/* Right: Time range */}
        <div className="flex items-center gap-2">
          {[
            { id: '7d' as TimeRange, label: lang === 'zh' ? '7天' : '7D' },
            { id: '30d' as TimeRange, label: lang === 'zh' ? '30天' : '30D' },
          ].map(r => (
            <button
              key={r.id}
              onClick={() => setTimeRange(r.id)}
              className={`px-3 py-1.5 rounded-md text-xs font-medium transition-all ${
                timeRange === r.id
                  ? 'bg-blue-50 text-blue-600 border border-blue-200'
                  : 'bg-white text-gray-400 border border-gray-200 hover:text-gray-600'
              }`}
            >
              {r.label}
            </button>
          ))}
          <button className="flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium bg-white text-gray-500 border border-gray-200 hover:text-gray-700 transition-all">
            <Download className="w-3.5 h-3.5" />
            {lang === 'zh' ? '导出' : 'Export'}
          </button>
        </div>
      </motion.div>

      {/* Chart View */}
      <AnimatePresence mode="wait">
        {viewMode === 'chart' && (
          <motion.div
            key="chart"
            className={`rounded-xl border p-6 ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -10 }}
          >
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-sm font-semibold text-gray-700">
                {lang === 'zh' ? 'Token 总消耗' : 'Total Token Consumption'} | {formatNumber(s?.total_tokens ?? 0)} {lang === 'zh' ? '个 Token' : 'tokens'}
              </h2>
              {/* Legend */}
              <div className="flex items-center gap-4 text-xs">
                <div className="flex items-center gap-1.5">
                  <span className="w-2.5 h-2.5 rounded-full" style={{ background: COLORS.total }} />
                  <span className="text-gray-500">{lang === 'zh' ? 'Token 总消耗' : 'Total'}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  {modelsInData.map((m, i) => (
                    <span key={m} className="flex items-center gap-1.5">
                      <span className="w-2.5 h-2.5 rounded-sm" style={{ background: colorFor(m, i) }} />
                      <span className="text-gray-500">{m}</span>
                    </span>
                  ))}
                </div>
              </div>
            </div>

            <div className="h-80">
              {chartData.length > 0 ? (
                <ResponsiveContainer width="100%" height="100%">
                  <ComposedChart data={chartData} barGap={2} barSize={40}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#f0f0f0" vertical={false} />
                    <XAxis dataKey="date" stroke={COLORS.text} fontSize={11} tickLine={false} axisLine={{ stroke: '#e5e7eb' }} />
                    <YAxis stroke={COLORS.text} fontSize={11} tickLine={false} axisLine={false} tickFormatter={(v: number) => v >= 1000000 ? (v/1000000).toFixed(0) + 'M' : v >= 1000 ? (v/1000).toFixed(0) + 'K' : v.toString()} />
                    <Tooltip content={<CustomTooltip />} />
                    {modelsInData.map((m, i) => (
                      <Bar key={m} dataKey={m} stackId="a" fill={colorFor(m, i)} radius={i === modelsInData.length - 1 ? [4, 4, 0, 0] : [0, 0, 0, 0]} name={m} />
                    ))}
                    <Line dataKey="total" stroke={COLORS.total} strokeWidth={2} dot={{ r: 4, fill: COLORS.total, strokeWidth: 0 }} name={lang === 'zh' ? 'Token 总消耗' : 'Total'} type="monotone" />
                  </ComposedChart>
                </ResponsiveContainer>
              ) : (
                <div className="h-full flex items-center justify-center text-gray-300">
                  <div className="text-center">
                    <BarChart3 className="w-10 h-10 mx-auto mb-2" />
                    <p className="text-sm">{t('noData')}</p>
                  </div>
                </div>
              )}
            </div>
          </motion.div>
        )}

        {/* List View */}
        {viewMode === 'list' && (
          <motion.div
            key="list"
            className={`rounded-xl border overflow-hidden ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`}
            initial={{ opacity: 0, y: 20 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -10 }}
          >
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className={isDark ? 'bg-gray-700/50' : 'bg-gray-50/80'}>
                    <th className="text-left py-3 px-4 font-medium text-gray-500 text-xs">{t('time')}</th>
                    <th className="text-left py-3 px-4 font-medium text-gray-500 text-xs">{t('model')}</th>
                    <th className="text-right py-3 px-4 font-medium text-gray-500 text-xs">{lang === 'zh' ? '总 Token 数' : 'Total Tokens'}</th>
                    <th className="text-right py-3 px-4 font-medium text-gray-500 text-xs">{lang === 'zh' ? '输入(命中缓存)' : 'Input (Cached)'}</th>
                    <th className="text-right py-3 px-4 font-medium text-gray-500 text-xs">{lang === 'zh' ? '输入(未命中)' : 'Input (Miss)'}</th>
                    <th className="text-right py-3 px-4 font-medium text-gray-500 text-xs">{lang === 'zh' ? '输出Token' : 'Output Tokens'}</th>
                  </tr>
                </thead>
                <tbody>
                  {pagedRows.length > 0 ? pagedRows.map((row) => (
                    <tr key={`${row.date}-${row.model}`} className={`border-t transition-colors ${isDark ? 'border-gray-700 hover:bg-gray-700/50' : 'border-gray-50 hover:bg-blue-50/30'}`}>
                      <td className="py-2.5 px-4 text-gray-600 font-mono text-xs">{row.date}</td>
                      <td className="py-2.5 px-4">
                        <span className="px-2 py-0.5 rounded text-xs font-mono bg-blue-50 text-blue-600">
                          {row.model}
                        </span>
                      </td>
                      <td className="py-2.5 px-4 text-right font-mono font-medium text-gray-800">{formatNumber(row.total)}</td>
                      <td className="py-2.5 px-4 text-right font-mono text-emerald-600">{formatNumber(row.inputCached)}</td>
                      <td className="py-2.5 px-4 text-right font-mono text-gray-500">{formatNumber(row.inputMiss)}</td>
                      <td className="py-2.5 px-4 text-right font-mono text-gray-600">{formatNumber(row.output)}</td>
                    </tr>
                  )) : (
                    <tr>
                      <td colSpan={6} className="py-12 text-center text-gray-300">
                        {loading ? t('loading') : t('noData')}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>

            {/* Pagination */}
            {tableRows.length > PAGE_SIZE && (
              <div className="flex items-center justify-between px-4 py-3 border-t border-gray-100 bg-gray-50/50">
                <span className="text-xs text-gray-400">{lang === 'zh' ? `共${tableRows.length}条` : `${tableRows.length} total`}</span>
                <div className="flex items-center gap-1">
                  <button
                    onClick={() => setPage(Math.max(0, page - 1))}
                    disabled={page === 0}
                    className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 disabled:opacity-30 disabled:cursor-not-allowed"
                  >
                    <ChevronLeft className="w-4 h-4" />
                  </button>
                  {Array.from({ length: totalPages }, (_, i) => (
                    <button
                      key={i}
                      onClick={() => setPage(i)}
                      className={`w-7 h-7 rounded-md text-xs font-medium transition-all ${
                        page === i ? 'bg-blue-500 text-white' : 'text-gray-400 hover:text-gray-600 hover:bg-gray-100'
                      }`}
                    >
                      {i + 1}
                    </button>
                  ))}
                  <button
                    onClick={() => setPage(Math.min(totalPages - 1, page + 1))}
                    disabled={page >= totalPages - 1}
                    className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 disabled:opacity-30 disabled:cursor-not-allowed"
                  >
                    <ChevronRight className="w-4 h-4" />
                  </button>
                </div>
              </div>
            )}
          </motion.div>
        )}
      </AnimatePresence>

      {/* Model breakdown + API endpoints */}
      <motion.div className="grid grid-cols-1 lg:grid-cols-2 gap-4" initial={{ opacity: 0, y: 20 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.4 }}>
        {/* Model stats */}
        <div className={`rounded-xl border p-5 ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`}>
          <h2 className={`text-sm font-semibold mb-4 flex items-center gap-2 ${isDark ? 'text-gray-200' : 'text-gray-700'}`}>
            <Clock className="w-4 h-4 text-blue-400" />
            {t('modelUsage')}
          </h2>
          {stats && stats.by_model?.length > 0 ? (
            <div className="space-y-3">
              {stats.by_model.map((m) => (
                <div key={m.model} className="flex items-center gap-3">
                  <span className={`w-2 h-2 rounded-full ${m.model.includes('pro') ? 'bg-blue-500' : 'bg-cyan-400'}`} />
                  <span className={`text-sm flex-1 font-mono ${isDark ? 'text-gray-300' : 'text-gray-600'}`}>{m.model}</span>
                  <span className={`text-sm font-mono font-medium ${isDark ? 'text-white' : 'text-gray-800'}`}>{formatTokens(m.total_tokens)}</span>
                  <span className="text-xs text-gray-400">{m.request_count}{t('requests')}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-gray-300 text-sm">{t('noData')}</p>
          )}
        </div>

        {/* API Endpoints */}
        <div className={`rounded-xl border p-5 ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`}>
          <h2 className={`text-sm font-semibold mb-4 flex items-center gap-2 ${isDark ? 'text-gray-200' : 'text-gray-700'}`}>
            <Zap className="w-4 h-4 text-purple-400" />
            {t('apiEndpoints')}
          </h2>
          <div className="space-y-2.5">
            {[
              { method: 'POST', path: '/v1/chat/completions', desc: 'OpenAI Chat' },
              { method: 'POST', path: '/v1/messages', desc: 'Anthropic Messages' },
              { method: 'GET', path: '/v1/models', desc: lang === 'zh' ? '模型列表' : 'Models' },
              { method: 'GET', path: '/admin/api/stats', desc: lang === 'zh' ? '用量统计' : 'Stats' },
            ].map((ep) => (
              <div key={ep.path} className={`flex items-center gap-3 p-2.5 rounded-lg transition-colors ${isDark ? 'hover:bg-gray-700' : 'hover:bg-gray-50'}`}>
                <span className={`px-2 py-0.5 rounded text-xs font-mono font-bold ${
                  ep.method === 'GET' ? 'bg-emerald-50 text-emerald-600' : 'bg-blue-50 text-blue-600'
                }`}>
                  {ep.method}
                </span>
                <code className={`text-xs font-mono flex-1 ${isDark ? 'text-gray-300' : 'text-gray-600'}`}>{ep.path}</code>
                <span className="text-xs text-gray-400">{ep.desc}</span>
              </div>
            ))}
          </div>
        </div>
      </motion.div>

      {/* Empty state */}
      {!loading && stats && stats.total.request_count === 0 && (
        <motion.div className={`rounded-xl border p-10 text-center ${isDark ? 'bg-gray-800 border-gray-700' : 'bg-white border-gray-100'}`} initial={{ opacity: 0 }} animate={{ opacity: 1 }}>
          <BarChart3 className="w-10 h-10 text-gray-200 mx-auto mb-3" />
          <p className="text-gray-400">{t('noData')}</p>
          <p className="text-gray-300 text-xs mt-1">{t('noDataHint')}</p>
        </motion.div>
      )}
    </div>
  )
}
