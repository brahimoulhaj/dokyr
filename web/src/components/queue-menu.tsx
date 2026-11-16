import { Popover } from '@base-ui/react/popover'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { activeDeploymentsQuery, type ActiveDeployment } from '../lib/api'
import { deploymentVersion, duration, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { cx } from './ui'
import { databaseOperationActive } from '../lib/database-operations'
import type { DatabaseOperation } from '../lib/databases'

function QueueIcon() {
  // Three rows, the first one "playing": a running job and the ones waiting behind it.
  return (
    <svg viewBox="0 0 16 16" className="size-4" fill="none" stroke="currentColor" strokeWidth="1.25" strokeLinecap="round" aria-hidden>
      <path d="M2.5 3.5l2.5 1.5-2.5 1.5z" fill="currentColor" stroke="none" />
      <path d="M7.5 5h6M2.5 10h11M2.5 13h8" />
    </svg>
  )
}

/**
 * Running and queued deployments and database operations, shared with the toast stack.
 * Database entries link to their instance; removals link to the databases list.
 */
export function QueueMenu({ databaseOperations }: { databaseOperations: DatabaseOperation[] }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const active = useQuery({ ...activeDeploymentsQuery, refetchInterval: 5_000 })
  const list = active.data ?? []
  const databases = databaseOperations.filter(databaseOperationActive)
  const count = list.length + databases.length
  const running = list.some((d) => d.status === 'running') || databases.some((o) => o.status === 'running')

  // Elapsed times tick while the list is open.
  const [, tick] = useState(0)
  useEffect(() => {
    if (!open) return
    const id = setInterval(() => tick((n) => n + 1), 1_000)
    return () => clearInterval(id)
  }, [open])

  // Running ones first, then the queue in the order it will run (the list is oldest first).
  const ordered = [
    ...list.map((d) => ({ kind: 'deployment' as const, value: d })),
    ...databases.map((o) => ({ kind: 'database' as const, value: o })),
  ].sort((a, b) => Number(b.value.status === 'running') - Number(a.value.status === 'running') || a.value.created_at.localeCompare(b.value.created_at))
  let waiting = 0

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        aria-label={count ? t.shell.queueCount(count) : t.shell.queue}
        title={t.shell.queue}
        className={cx(
          'relative flex size-8 items-center justify-center border text-muted hover:border-line hover:text-fg',
          open ? 'border-line text-fg' : 'border-transparent',
          running && !open && 'text-warn',
        )}
      >
        <QueueIcon />
        {count > 0 && (
          <span
            aria-hidden
            className={cx(
              'absolute -top-0.5 -end-0.5 flex h-3.5 min-w-3.5 items-center justify-center px-0.5 font-pixel text-[9px] leading-none text-bg',
              running ? 'bg-warn' : 'bg-muted',
            )}
          >
            {count > 9 ? '9+' : count}
          </span>
        )}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="end" sideOffset={8} className="isolate z-50">
          <Popover.Popup className="w-96 max-w-(--available-width) origin-(--transform-origin) border border-line-strong bg-surface text-fg shadow-lg shadow-black/30 transition-[opacity,scale] duration-100 outline-none data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0">
            <div className="flex items-center justify-between border-b border-line px-4 py-2">
              <Popover.Title className="font-pixel text-xs text-muted uppercase">{t.shell.queue}</Popover.Title>
              <span className="text-xs text-muted">{count ? t.shell.queueCount(count) : ''}</span>
            </div>
            {ordered.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-muted">{t.shell.queueEmpty}</p>
            ) : (
              <ul className="max-h-96 divide-y divide-line overflow-y-auto">
                {ordered.map((item) => {
                  const position = item.value.status === 'running' ? 0 : ++waiting
                  return item.kind === 'database'
                    ? <DatabaseQueueItem key={item.value.id} op={item.value} position={position} onOpen={() => setOpen(false)} />
                    : <QueueItem key={item.value.id} d={item.value} position={position} onOpen={() => setOpen(false)} />
                })}
              </ul>
            )}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}

function QueueItem({ d, position, onOpen }: { d: ActiveDeployment; position: number; onOpen: () => void }) {
  const t = useT()
  const isRunning = d.status === 'running'
  const version = deploymentVersion(d).replace(/^—$/, '')
  return (
    <li>
      <Link
        to="/projects/$projectId/apps/$appId"
        params={{ projectId: d.project_id, appId: d.app_id }}
        search={{ tab: 'deployments' }}
        onClick={onOpen}
        className="flex items-center gap-3 px-4 py-2.5 hover:bg-raised focus-visible:bg-raised focus-visible:outline-none"
      >
        <span className="flex w-5 shrink-0 justify-center" aria-hidden>
          {isRunning ? (
            <span className="size-2 animate-pulse rounded-full bg-warn motion-reduce:animate-none" />
          ) : (
            <span className="font-pixel text-xs text-muted">{position}</span>
          )}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-sm">{d.app_name}</span>
          <span className="block truncate text-xs text-muted">
            {[version, isRunning ? t.overview.runningFor(duration(d.started_at, null)) : t.overview.queuedFor(timeAgo(d.created_at))]
              .filter(Boolean)
              .join(' · ')}
          </span>
        </span>
        <span aria-hidden className="shrink-0 text-xs text-muted rtl:-scale-x-100">›</span>
      </Link>
    </li>
  )
}

function DatabaseQueueItem({ op, position, onOpen }: { op: DatabaseOperation; position: number; onOpen: () => void }) {
  const t = useT()
  const content = <>
    <span className="flex w-5 shrink-0 justify-center" aria-hidden>{op.status === 'running' ? <span className="size-2 animate-pulse rounded-full bg-warn motion-reduce:animate-none" /> : <span className="font-pixel text-xs text-muted">{position}</span>}</span>
    <span className="min-w-0 flex-1"><span className="block truncate text-sm">{t.databases.operationRunning[op.action](op.instance_name)}</span>
      <span className="block truncate text-xs text-muted">{op.engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'} {op.version} · {duration(op.started_at ?? op.created_at, null)}</span>
    </span><span aria-hidden className="text-xs text-muted rtl:-scale-x-100">›</span>
  </>
  const className = 'flex items-center gap-3 px-4 py-2.5 hover:bg-raised focus-visible:bg-raised focus-visible:outline-none'
  return <li>{op.action === 'delete'
    ? <Link to="/databases" onClick={onOpen} className={className}>{content}</Link>
    : <Link to="/databases/$databaseId" params={{ databaseId: op.instance_id }} search={{ tab: ['database_create', 'user_create', 'grant_update', 'database_delete', 'user_delete', 'password_rotate'].includes(op.action) ? 'access' : 'logs' }} onClick={onOpen} className={className}>{content}</Link>}</li>
}
