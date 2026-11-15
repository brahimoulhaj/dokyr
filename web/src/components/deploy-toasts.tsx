import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Popover } from '@base-ui/react/popover'
import { useNavigate } from '@tanstack/react-router'
import { chime } from '@yusufceng1z/ding'
import { Toaster, toast } from '@yusufceng1z/ding-react'
import { activeDeploymentsQuery, api, type ActiveDeployment, type Deployment } from '../lib/api'
import { deploymentVersion, duration } from '../lib/format'
import { useT } from '../lib/i18n'
import { useTheme } from '../lib/theme'
import { takeUpdated } from '../lib/toasts'
import { Button } from './ui'

// ding's built-in tones: success rises, error falls, info is one note; loading stays silent.
// Browsers hold audio until the first click or key press on the page.
toast.configure({ sound: true, volume: 0.5 })

/** A deployment that ended, kept in the stack for a while: how it went, and when it ended. */
type Ended = { d: ActiveDeployment; done: Deployment }

// How long an ended deployment's card stays; one that failed stays until it is dismissed.
const endedMs: Partial<Record<Deployment['status'], number>> = { succeeded: 8_000, cancelled: 5_000 }

/**
 * Toasts for deployments on any app, wherever the user is in the panel: one card per
 * deployment in a stack, so several in parallel are all in view. A card turns into success or
 * failure when its deployment ends and the others stay where they are. It follows the same
 * list the overview alert polls, so an end is noticed within one poll.
 */
export function DeployToasts() {
  const t = useT()
  const theme = useTheme()
  const navigate = useNavigate()
  const active = useQuery({ ...activeDeploymentsQuery, refetchInterval: 5_000 })
  // Deployments seen in progress, and the ones that ended since.
  const seen = useRef(new Map<string, ActiveDeployment>())
  const [ended, setEnded] = useState<Ended[]>([])
  // The deployment whose Cancel was clicked, and the button the confirmation points at.
  const [confirm, setConfirm] = useState<{ d: ActiveDeployment; anchor: Element | null } | null>(null)

  // A panel update reloads the page into the new version; say it worked once it is up.
  useEffect(() => {
    const v = takeUpdated()
    if (v) toast.success(t.settingsUpdates.updatedToast(v))
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, on load
  }, [])

  const dismiss = (id: string) => setEnded((list) => list.filter((e) => e.d.id !== id))
  const view = (d: ActiveDeployment) => {
    navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId: d.project_id, appId: d.app_id }, search: { tab: 'deployments' } })
  }

  useEffect(() => {
    if (!active.data) return
    const now = new Set(active.data.map((d) => d.id))
    for (const d of active.data) seen.current.set(d.id, d)
    for (const [id, d] of seen.current) {
      if (now.has(id)) continue
      seen.current.delete(id)
      setConfirm((c) => (c?.d.id === id ? null : c))
      api<Deployment>(`/deployments/${id}`)
        .then((done) => {
          chime(done.status === 'succeeded' ? 'success' : done.status === 'cancelled' ? 'info' : 'error', 0.5)
          setEnded((list) => [...list.filter((e) => e.d.id !== id), { d, done }])
          const ms = endedMs[done.status]
          if (ms) setTimeout(() => dismiss(id), ms)
        })
        .catch(() => undefined)
    }
  }, [active.data])

  return (
    <>
      <DeployStack
        active={active.data ?? []}
        ended={ended}
        onView={view}
        onDismiss={dismiss}
        onCancel={(d, anchor) => setConfirm({ d, anchor })}
      />
      <Toaster position="bottom-center" theme={theme.mode === 'dark' ? 'light' : 'dark'} labels={{ region: t.overview.toasts, dismiss: t.overview.dismiss }} />
      <CancelConfirm target={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

/**
 * One card per deployment, oldest at the top, in ding's look (a solid card, the theme's
 * inverse). A running one spins, a queued one shows its place in the queue, and an ended one
 * says how it went.
 */
function DeployStack({
  active,
  ended,
  onView,
  onDismiss,
  onCancel,
}: {
  active: ActiveDeployment[]
  ended: Ended[]
  onView: (d: ActiveDeployment) => void
  onDismiss: (id: string) => void
  onCancel: (d: ActiveDeployment, anchor: Element | null) => void
}) {
  const t = useT()
  const cards = [
    ...active.map((d) => ({ d, end: null as Deployment | null })),
    ...ended.filter((e) => !active.some((d) => d.id === e.d.id)).map((e) => ({ d: e.d, end: e.done })),
  ].sort((a, b) => a.d.created_at.localeCompare(b.d.created_at))
  if (cards.length === 0) return null
  // The queue runs in the order of the list; the running ones are not in line.
  let place = 0
  return (
    <ol
      aria-label={t.overview.toasts}
      aria-live="polite"
      className="pointer-events-none fixed bottom-4 left-1/2 z-40 flex w-[26rem] max-w-[calc(100vw-2rem)] -translate-x-1/2 flex-col gap-2 transition-[bottom] duration-200 [body:has(.tst-slot)_&]:bottom-24"
    >
      {cards.map(({ d, end }) => {
        const running = !end && d.status === 'running'
        const version = deploymentVersion(d).replace(/^—$/, '')
        const n = !end && !running ? ++place : 0
        const ok = end?.status === 'succeeded'
        const title = !end
          ? running
            ? t.overview.deployingApp(d.app_name)
            : t.overview.queuedApp(d.app_name)
          : ok
            ? t.overview.deployedApp(d.app_name)
            : end.status === 'failed'
              ? t.overview.failedApp(d.app_name)
              : end.status === 'rolled_back'
                ? t.overview.rolledBackApp(d.app_name)
                : t.overview.cancelledApp(d.app_name)
        const sub = !end ? version : ok ? duration(end.started_at, end.finished_at) : end.error || version
        return (
          <li
            key={d.id}
            className="pointer-events-auto flex animate-[stack-in_220ms_cubic-bezier(.2,.8,.3,1)] items-center gap-3 bg-fg py-2.5 ps-4 pe-2.5 text-bg shadow-lg shadow-black/40 motion-reduce:animate-none"
          >
            <span aria-hidden className="flex size-5 shrink-0 items-center justify-center">
              {end ? (
                <EndIcon status={end.status} />
              ) : running ? (
                <span className="size-4 animate-spin rounded-full border-2 border-bg/25 border-t-bg motion-reduce:animate-none" />
              ) : (
                <span className="font-pixel text-xs text-bg/60">{n}</span>
              )}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">{title}</span>
              {sub && <span className="line-clamp-2 text-xs break-words text-bg/60">{sub}</span>}
            </span>
            {!end && (
              <button
                type="button"
                onClick={(e) => onCancel(d, e.currentTarget)}
                className="shrink-0 px-2.5 py-1 text-xs text-bg/70 hover:bg-bg/10 hover:text-bg"
              >
                {t.app.cancelDeployment}
              </button>
            )}
            <button type="button" onClick={() => onView(d)} className="shrink-0 bg-bg px-3 py-1 text-xs font-medium text-fg hover:opacity-85">
              {t.overview.viewDeployment}
            </button>
            {end && (
              <button
                type="button"
                onClick={() => onDismiss(d.id)}
                aria-label={t.overview.dismiss}
                title={t.overview.dismiss}
                className="flex size-6 shrink-0 items-center justify-center text-bg/60 hover:bg-bg/10 hover:text-bg"
              >
                <svg viewBox="0 0 16 16" className="size-3" fill="none" stroke="currentColor" strokeWidth="1.75" strokeLinecap="round" aria-hidden>
                  <path d="M4 4l8 8M12 4l-8 8" />
                </svg>
              </button>
            )}
          </li>
        )
      })}
    </ol>
  )
}

function EndIcon({ status }: { status: Deployment['status'] }) {
  const ok = status === 'succeeded'
  const cancelled = status === 'cancelled'
  return (
    <svg
      viewBox="0 0 16 16"
      className={ok ? 'size-4 text-[#12a36b]' : cancelled ? 'size-4 text-bg/60' : 'size-4 text-[#e5484d]'}
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      {ok ? <path d="M3 8.5l3.2 3.2L13 4.8" /> : cancelled ? <path d="M4 8h8" /> : <path d="M4 4l8 8M12 4l-8 8" />}
    </svg>
  )
}

/** The confirmation above a deploy toast's Cancel button. */
function CancelConfirm({ target, onClose }: { target: { d: ActiveDeployment; anchor: Element | null } | null; onClose: () => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const cancel = useMutation({
    mutationFn: (id: string) => api<Deployment>(`/deployments/${id}/cancel`, { method: 'POST' }),
    onSuccess: () => {
      onClose()
      // Notice the end sooner than the next poll, so the toast turns to cancelled.
      setTimeout(() => queryClient.invalidateQueries({ queryKey: activeDeploymentsQuery.queryKey }), 1_000)
    },
  })
  return (
    <Popover.Root
      open={!!target}
      onOpenChange={(open) => {
        if (!open) {
          cancel.reset()
          onClose()
        }
      }}
    >
      <Popover.Portal>
        {/* Above ding's viewport, which sits at the highest z-index. */}
        <Popover.Positioner anchor={target?.anchor} side="top" sideOffset={6} className="z-[2147483647]">
          <Popover.Popup className="flex max-w-[calc(100vw-2rem)] flex-wrap items-center gap-1.5 border border-line bg-surface px-2 py-1.5 text-xs text-fg shadow-md">
            <Popover.Title className="me-1">{t.overview.cancelConfirmTitle}</Popover.Title>
            <Button variant="danger" type="button" className="h-6! px-2! text-[11px]! leading-none" disabled={cancel.isPending} onClick={() => target && cancel.mutate(target.d.id)}>
              {cancel.isPending ? t.common.working : t.overview.cancelYes}
            </Button>
            <Popover.Close render={<Button variant="ghost" type="button" className="h-6! px-2! text-[11px]! leading-none" />}>{t.overview.cancelNo}</Popover.Close>
            {cancel.error && <p className="w-full text-danger">{cancel.error.message}</p>}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  )
}
