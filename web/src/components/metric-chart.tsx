import { CartesianGrid, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { locale, useT } from '../lib/i18n'
import { cx } from './ui'

// Loaded on demand (Recharts is large), only when a Metrics tab opens.

export type MetricPoint = { ts: number; cpu: number; cpu_max: number; memory: number; memory_max: number; memory_limit: number; net_rx: number; net_tx: number; disk_read: number; disk_write: number }
export type MetricField = Exclude<keyof MetricPoint, 'ts'>
export type MetricSeries = { range: string; step: number; from: number; to: number; points: MetricPoint[] }

const colors = ['var(--color-accent)', 'var(--color-muted)']
const dots = ['bg-accent', 'bg-muted']

export default function MetricChart({ data, title, format, axis, lines, peak, limit }: {
  data: MetricSeries; title: string; format: (n: number) => string; axis?: (n: number) => string
  lines: { field: MetricField; label: string }[]; peak?: MetricField; limit?: number
}) {
  const t = useT().metrics.usage
  const { points, from, to, step } = data
  // A gap longer than two buckets (container stopped, panel down) breaks the line.
  const rows: (Partial<MetricPoint> & { ts: number })[] = []
  points.forEach((p, i) => {
    if (i > 0 && p.ts - points[i - 1].ts > 2 * step) rows.push({ ts: points[i - 1].ts + step })
    rows.push(p)
  })
  const first = lines[0].field
  const latest = points[points.length - 1]
  const average = points.reduce((s, p) => s + p[first], 0) / points.length
  const top = Math.max(...points.map((p) => p[peak ?? first]))
  // The limit line only helps when usage comes near it; otherwise it flattens the chart.
  const showLimit = !!limit && limit <= top * 2
  const long = to - from > 86_400
  // Five evenly spaced time ticks across the whole window, not just where data exists.
  const ticks = [0, 1, 2, 3, 4].map((i) => Math.round(from + ((to - from) * i) / 4))
  const time = (ts: number, full = false) => new Date(ts * 1000).toLocaleString(locale(), long
    ? { month: 'short', day: 'numeric', ...(full || step < 3600 ? { hour: '2-digit', minute: '2-digit' } : {}) }
    : { hour: '2-digit', minute: '2-digit', ...(full && step < 60 ? { second: '2-digit' } : {}) })
  return <figure className="min-w-0 space-y-2 bg-surface p-4">
    <figcaption className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-1">
      <span className="text-sm">{title}</span>
      <span className="text-xs text-muted tabular-nums">{t.latest} {format(latest[first])} · {t.average} {format(average)} · {t.peak} {format(top)}{limit ? ` · ${t.limit} ${format(limit)}` : ''}</span>
    </figcaption>
    <div className="h-36" role="img" aria-label={`${title} · ${t.ranges[data.range as keyof typeof t.ranges] ?? data.range}`}>
      <ResponsiveContainer width="100%" height="100%">
        <LineChart data={rows} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid stroke="var(--color-line)" strokeDasharray="3 4" vertical={false} />
          <XAxis dataKey="ts" type="number" domain={[from, to]} ticks={ticks} tickFormatter={(v: number) => time(v)} interval="preserveStartEnd"
            stroke="var(--color-line)" tick={{ fill: 'var(--color-muted)', fontSize: 10 }} tickLine={false} />
          <YAxis tickFormatter={(v: number) => (axis ?? format)(v)} width={80} stroke="var(--color-line)" tick={{ fill: 'var(--color-muted)', fontSize: 10 }}
            tickLine={false} axisLine={false} tickCount={4} domain={[0, 'auto']} allowDecimals />
          {showLimit && <ReferenceLine y={limit} stroke="var(--color-danger)" strokeDasharray="4 4" ifOverflow="extendDomain" />}
          <Tooltip isAnimationActive={false} cursor={{ stroke: 'var(--color-muted)', strokeDasharray: '2 3' }}
            content={({ active, label, payload }) => active && payload?.length ? <div className="space-y-0.5 border border-line bg-surface px-2 py-1 text-xs shadow-sm">
              <div className="text-muted">{time(Number(label), true)}</div>
              {lines.map((l, i) => {
                const value = payload.find((p) => p.dataKey === l.field)?.value
                return <div key={l.field} className="flex items-center gap-1.5 tabular-nums"><span className={cx('size-1.5', dots[i])} />{l.label} {typeof value === 'number' ? format(value) : '—'}</div>
              })}
            </div> : null} />
          {lines.map((l, i) => <Line key={l.field} dataKey={l.field} name={l.label} type="monotone" stroke={colors[i]} strokeWidth={1.5} dot={false}
            activeDot={{ r: 3, strokeWidth: 0, fill: colors[i] }} isAnimationActive={false} connectNulls={false} />)}
        </LineChart>
      </ResponsiveContainer>
    </div>
    {lines.length > 1 && <div className="flex gap-3 text-xs text-muted">{lines.map((l, i) => <span key={l.field} className="flex items-center gap-1.5"><span className={cx('size-1.5', dots[i])} />{l.label}</span>)}</div>}
  </figure>
}
