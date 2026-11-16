import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { ListToolbar, Pager, SearchBox, ToolbarSelect, useDebounced } from '../components/list-controls'
import { cx, Empty, PageHeader, Panel, QueryState } from '../components/ui'
import { api, type PanelEvent } from '../lib/api'
import { actionLabel, dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { poll } from '../lib/live'

type EventPage = { page: number; size: number; total: number; items: PanelEvent[] }

export function ActivityPage() {
  const t = useT()
  const tl = t.lists
  const [text, setText] = useState('')
  const [group, setGroup] = useState('all')
  const [actor, setActor] = useState('all')
  const [period, setPeriod] = useState('all')
  const [outcome, setOutcome] = useState('all')
  const [page, setPage] = useState(1)
  const q = useDebounced(text.trim())
  const filters = { q, group, actor, period, outcome }
  const key = JSON.stringify(filters)
  useEffect(() => setPage(1), [key])
  const params = new URLSearchParams({ page: String(page), size: '25' })
  if (q) params.set('q', q)
  if (group !== 'all') params.set('group', group)
  if (actor !== 'all') params.set('actor', actor)
  if (period !== 'all') params.set('period', period)
  if (outcome === 'failures') params.set('failures', '1')
  const events = useQuery({
    queryKey: ['events', 'search', filters, page],
    queryFn: () => api<EventPage>(`/events/search?${params}`),
    placeholderData: keepPreviousData,
    refetchInterval: page === 1 ? poll(15_000) : false, // only the newest page moves
  })
  const filtered = q !== '' || group !== 'all' || actor !== 'all' || period !== 'all' || outcome !== 'all'
  const clear = () => { setText(''); setGroup('all'); setActor('all'); setPeriod('all'); setOutcome('all') }
  return (
    <div className="space-y-6">
      <PageHeader title={t.activity.title} subtitle={t.activity.subtitle} />
      <Panel>
        <ListToolbar>
          <SearchBox label={tl.searchActivity} value={text} onChange={setText} />
          <ToolbarSelect label={tl.filter} value={group} onChange={setGroup} options={['all', 'deploys', 'apps', 'databases', 'projects', 'alerts', 'settings'].map((v) => ({ value: v, label: tl.eventGroups[v] }))} />
          <ToolbarSelect label={tl.filter} value={outcome} onChange={setOutcome} options={['all', 'failures'].map((v) => ({ value: v, label: tl.outcome[v] }))} />
          <ToolbarSelect label={tl.filter} value={actor} onChange={setActor} options={['all', 'people', 'system'].map((v) => ({ value: v, label: tl.eventActors[v] }))} />
          <ToolbarSelect label={tl.filter} value={period} onChange={setPeriod} options={['all', '24h', '7d', '30d'].map((v) => ({ value: v, label: tl.periods[v] }))} />
          {events.data && <span className="ms-auto text-xs text-muted tabular-nums">{tl.matching(events.data.total)}</span>}
        </ListToolbar>
        <QueryState loading={events.isPending} error={events.error} what={t.activity.what} />
        {events.data && (events.data.items.length === 0 && filtered
          ? <Empty title={tl.noMatch}><button type="button" className="text-accent hover:underline" onClick={clear}>{tl.clear}</button></Empty>
          : <div className={cx(events.isPlaceholderData && 'opacity-60 transition-opacity')}><EventList events={events.data.items} /></div>)}
        {events.data && <Pager page={page} pages={Math.max(1, Math.ceil(events.data.total / events.data.size))} setPage={setPage} total={events.data.total} size={events.data.size} />}
      </Panel>
    </div>
  )
}

/** Shared by the activity page and the overview. */
export function EventList({ events }: { events: PanelEvent[] }) {
  const t = useT()
  if (events.length === 0) return <Empty title={t.activity.empty} />
  return (
    <ul className="divide-y divide-line">
      {events.map((e) => (
        <li key={e.id} className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-0.5 px-4 py-2.5">
          <p className="min-w-0 text-sm break-words">
            <span aria-hidden className={cx('me-2 inline-block size-1.5 rounded-full align-middle', tone(e.action) === 'bad' ? 'bg-danger' : tone(e.action) === 'good' ? 'bg-accent' : 'bg-line-strong')} />
            <span className="text-muted">{e.actor || t.activity.system}</span> {actionLabel(e.action)}{' '}
            {e.target_name && <span className={tone(e.action) === 'bad' ? 'text-danger' : 'text-fg'}>{e.action.startsWith('notification.rule_') ? t.settingsNotifications.events[e.target_name] ?? e.target_name : e.target_name}</span>}
            {detail(e) && (
              <span className="text-muted">
                {' '}
                — <span dir="ltr">{detail(e)}</span>
              </span>
            )}
          </p>
          <time className="shrink-0 text-xs text-muted" dateTime={e.created_at} title={dateTime(e.created_at)}>
            {timeAgo(e.created_at)}
          </time>
        </li>
      ))}
    </ul>
  )
}

// Problems in red, good news in green, the rest neutral.
function tone(action: string): 'bad' | 'good' | 'neutral' {
  if (action.endsWith('failed') || ['deployment.rolled_back', 'app.crashed', 'app.health_failing', 'disk.high', 'usage.cpu_high', 'usage.memory_high'].includes(action)) return 'bad'
  if (['deployment.succeeded', 'app.health_recovered', 'usage.cpu_normal', 'usage.memory_normal'].includes(action)) return 'good'
  return 'neutral'
}

// The detail column holds ids for some actions; only show what a person can read.
function detail(e: PanelEvent): string {
  switch (e.action) {
    case 'app.create':
    case 'domain.add':
    case 'domain.remove':
    case 'domain.primary':
    case 'deployment.failed':
    case 'deployment.rolled_back':
    case 'app.health_failing':
    case 'app.crashed':
    case 'backup.failed':
    case 'disk.high':
    case 'usage.cpu_high':
    case 'usage.memory_high':
      return e.detail
    default:
      return ''
  }
}
