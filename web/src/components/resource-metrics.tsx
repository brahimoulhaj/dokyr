import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { lazy, Suspense, useState } from 'react'
import { api } from '../lib/api'
import { formatBytes } from '../lib/format'
import { locale, useT } from '../lib/i18n'
import type { MetricSeries } from './metric-chart'
import { cx, Empty, HelpTip, Panel, QueryState } from './ui'

const MetricChart = lazy(() => import('./metric-chart'))
const ranges = ['1h', '6h', '24h', '7d', '30d'] as const
type Range = (typeof ranges)[number]

/** Usage history of an app or a database instance, read from /api/{apps|databases}/{id}/metrics. */
export function ResourceMetrics({ kind, id }: { kind: 'apps' | 'databases'; id: string }) {
  const t = useT().metrics.usage
  const [range, setRange] = useState<Range>('1h')
  const q = useQuery({
    queryKey: ['resource-metrics', kind, id, range],
    queryFn: () => api<MetricSeries>(`/${kind}/${id}/metrics?range=${range}`),
    refetchInterval: range === '1h' ? 10_000 : 60_000,
    placeholderData: keepPreviousData,
  })
  const number = (n: number, digits: number) => new Intl.NumberFormat(locale(), { maximumFractionDigits: digits }).format(n)
  const cores = (n: number) => t.cores(number(n, n < 0.1 ? 3 : 2))
  const coreAxis = (n: number) => number(n, n < 0.1 ? 3 : 2)
  const bytes = (n: number) => formatBytes(n)
  const rate = (n: number) => formatBytes(n, true)
  const data = q.data
  const limit = data?.points.reduce((m, p) => Math.max(m, p.memory_limit), 0) ?? 0
  return <Panel
    title={<span className="inline-flex items-center gap-2">{t.title}<HelpTip label={t.title}>{t.help}</HelpTip></span>}
    actions={<div role="radiogroup" aria-label={t.range} className="flex border border-line">
      {ranges.map((r) => <button key={r} type="button" role="radio" aria-checked={range === r} title={t.ranges[r]} onClick={() => setRange(r)}
        className={cx('px-2.5 py-1 text-xs', range === r ? 'bg-raised text-fg' : 'text-muted hover:text-fg')}>{r}</button>)}
    </div>}>
    <QueryState loading={q.isPending} error={q.error} what={t.title} />
    {data && (data.points.length === 0 ? <Empty title={t.empty} /> :
      <Suspense fallback={<div className="h-96" />}>
        <div className="grid gap-px bg-line md:grid-cols-2">
          <MetricChart data={data} title={t.cpu} format={cores} axis={coreAxis} lines={[{ field: 'cpu', label: t.cpu }]} peak="cpu_max" />
          <MetricChart data={data} title={t.memory} format={bytes} lines={[{ field: 'memory', label: t.memory }]} peak="memory_max" limit={limit || undefined} />
          <MetricChart data={data} title={t.network} format={rate} lines={[{ field: 'net_rx', label: t.received }, { field: 'net_tx', label: t.sent }]} />
          <MetricChart data={data} title={t.disk} format={rate} lines={[{ field: 'disk_read', label: t.read }, { field: 'disk_write', label: t.written }]} />
        </div>
      </Suspense>)}
  </Panel>
}
