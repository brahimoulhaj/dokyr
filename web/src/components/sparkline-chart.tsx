import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { locale } from '../lib/i18n'
import { cx } from './ui'

// Loaded on demand with Recharts; the host card shows the last ten minutes.

export type SparkPoint = { at: string; [key: string]: number | string | null }
export type SparkLine = { key: string; label: string }

const colors = ['var(--color-accent)', 'var(--color-muted)']
const dots = ['bg-accent', 'bg-muted']
const WINDOW = 600_000

export default function SparklineChart({ history, lines, max, format, label }: {
  history: SparkPoint[]; lines: SparkLine[]; max?: number; format: (n: number) => string; label: string
}) {
  const newest = history.length ? Date.parse(history[history.length - 1].at) : Date.now()
  const rows = history.map((p) => ({ ...p, t: Date.parse(p.at) }))
  return <div className="h-10" role="img" aria-label={label}>
    <ResponsiveContainer width="100%" height="100%">
      <AreaChart data={rows} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
        <defs>{lines.map((l, i) => <linearGradient key={l.key} id={`spark-${l.key}`} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={colors[i]} stopOpacity={0.3} />
          <stop offset="100%" stopColor={colors[i]} stopOpacity={0} />
        </linearGradient>)}</defs>
        <XAxis dataKey="t" type="number" domain={[newest - WINDOW, newest]} hide />
        <YAxis domain={[0, max ?? 'auto']} hide />
        <Tooltip isAnimationActive={false} cursor={{ stroke: 'var(--color-muted)', strokeDasharray: '2 3' }} wrapperStyle={{ zIndex: 20 }}
          content={({ active, label: at, payload }) => active && payload?.length ? <div className="space-y-0.5 border border-line bg-surface px-2 py-1 text-xs whitespace-nowrap shadow-sm">
            <div className="text-muted">{new Date(Number(at)).toLocaleTimeString(locale())}</div>
            {lines.map((l, i) => {
              const value = payload.find((p) => p.dataKey === l.key)?.value
              return <div key={l.key} className="flex items-center gap-1.5 tabular-nums" dir="ltr"><span className={cx('size-1.5', dots[i])} />{l.label} {typeof value === 'number' ? format(value) : '—'}</div>
            })}
          </div> : null} />
        {/* Drawn in reverse so the first line stays on top. */}
        {[...lines].reverse().map((l) => {
          const i = lines.indexOf(l)
          return <Area key={l.key} dataKey={l.key} type="monotone" stroke={colors[i]} strokeWidth={1.5} fill={`url(#spark-${l.key})`}
            dot={false} activeDot={{ r: 2.5, strokeWidth: 0, fill: colors[i] }} isAnimationActive={false} connectNulls={false} />
        })}
      </AreaChart>
    </ResponsiveContainer>
  </div>
}
