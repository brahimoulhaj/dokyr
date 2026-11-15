import { Popover } from '@base-ui/react/popover'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { activeDeploymentsQuery, type ActiveDeployment } from '../lib/api'
import { deploymentVersion, duration, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { cx } from './ui'

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
 * The queue button of the navbar: a count of the deployments running or waiting (any app),
 * and a list of them that opens on click. An item goes to its app's Deployments tab.
 * It follows the list the toasts and the overview alert poll.
 */
export function QueueMenu() {
  const t = useT()
  const [open, setOpen] = useState(false)
  const active = useQuery({ ...activeDeploymentsQuery, refetchInterval: 5_000 })
  const list = active.data ?? []
  const running = list.some((d) => d.status === 'running')

  // Elapsed times tick while the list is open.
  const [, tick] = useState(0)
  useEffect(() => {
    if (!open) return
    const id = setInterval(() => tick((n) => n + 1), 1_000)
    return () => clearInterval(id)
  }, [open])

  // Running ones first, then the queue in the order it will run (the list is oldest first).
  const ordered = [...list.filter((d) => d.status === 'running'), ...list.filter((d) => d.status !== 'running')]
  let waiting = 0

  return (
    <Popover.Root open={open} onOpenChange={setOpen}>
      <Popover.Trigger
        aria-label={list.length ? t.shell.queueCount(list.length) : t.shell.queue}
        title={t.shell.queue}
        className={cx(
          'relative flex size-8 items-center justify-center border text-muted hover:border-line hover:text-fg',
          open ? 'border-line text-fg' : 'border-transparent',
          running && !open && 'text-warn',
        )}
      >
        <QueueIcon />
        {list.length > 0 && (
          <span
            aria-hidden
            className={cx(
              'absolute -top-0.5 -end-0.5 flex h-3.5 min-w-3.5 items-center justify-center px-0.5 font-pixel text-[9px] leading-none text-bg',
              running ? 'bg-warn' : 'bg-muted',
            )}
          >
            {list.length > 9 ? '9+' : list.length}
          </span>
        )}
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner side="bottom" align="end" sideOffset={8} className="isolate z-50">
          <Popover.Popup className="w-96 max-w-(--available-width) origin-(--transform-origin) border border-line-strong bg-surface text-fg shadow-lg shadow-black/30 transition-[opacity,scale] duration-100 outline-none data-[ending-style]:scale-95 data-[ending-style]:opacity-0 data-[starting-style]:scale-95 data-[starting-style]:opacity-0">
            <div className="flex items-center justify-between border-b border-line px-4 py-2">
              <Popover.Title className="font-pixel text-xs text-muted uppercase">{t.shell.queue}</Popover.Title>
              <span className="text-xs text-muted">{list.length ? t.shell.queueCount(list.length) : ''}</span>
            </div>
            {ordered.length === 0 ? (
              <p className="px-4 py-6 text-center text-sm text-muted">{t.shell.queueEmpty}</p>
            ) : (
              <ul className="max-h-96 divide-y divide-line overflow-y-auto">
                {ordered.map((d) => (
                  <QueueItem key={d.id} d={d} position={d.status === 'running' ? 0 : ++waiting} onOpen={() => setOpen(false)} />
                ))}
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
