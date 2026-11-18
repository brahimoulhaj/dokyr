import { useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { Link, Outlet, useLocation } from '@tanstack/react-router'
import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { TerminalWindow } from '@phosphor-icons/react'
import { api, hostTerminalQuery, meQuery, metaQuery, updatesQuery, type User } from '../lib/api'
import { PasswordConfirmDialog } from './password-confirm-dialog'
import { useLogout } from '../lib/logout'
import { useT } from '../lib/i18n'
import { useTheme } from '../lib/theme'
import { LanguagePicker } from './langpicker'
import { ThemePicker } from './themepicker'
import { DeployToasts } from './deploy-toasts'
import { QueueMenu } from './queue-menu'
import { UpdateCheck } from './update-check'
import { WhatsNew } from './whats-new'
import { Badge, cx } from './ui'
import { useDatabaseOperations } from '../lib/database-operations'
import { CommandPalette } from './command-palette'
import { useLiveChanges } from '../lib/live'
import { Sidebar } from './sidebar'

const HostTerminalDock = lazy(() => import('./host-terminal'))

// The page itself never scrolls: the navbar and the sidebar stay put and only <main> scrolls.
export function Shell() {
  const { data: meta } = useSuspenseQuery(metaQuery)
  const { data: me } = useSuspenseQuery(meQuery)
  const updates = useQuery(updatesQuery)
  const databaseOperations = useDatabaseOperations()
  useLiveChanges()
  const main = useRef<HTMLElement>(null)
  const { pathname } = useLocation()
  const [picking, setPicking] = useState(false)
  // 'hidden' keeps the shells running under a collapsed dock.
  const [terminal, setTerminal] = useState<'closed' | 'open' | 'hidden'>('closed')
  const t = useT()
  const qc = useQueryClient()
  const mayOpen = !!me && me.role !== 'maintainer'
  const hostTerminal = useQuery({ ...hostTerminalQuery, enabled: mayOpen })
  const canTerminal = mayOpen && hostTerminal.data?.enabled !== false
  // What waits for the password: the host terminal asks for it again before a shell opens.
  const [unlock, setUnlock] = useState<{ run: () => void; user: string } | null>(null)
  async function guard(run: () => void) {
    try {
      const now = await qc.fetchQuery({ ...hostTerminalQuery, staleTime: 0 })
      if (!now.enabled) return
      if (!now.unlocked) { setUnlock({ run, user: now.user || 'root' }); return }
    } catch { /* the terminal reports what is wrong */ }
    run()
  }
  const openTerminal = () => (terminal === 'closed' ? guard(() => setTerminal('open')) : setTerminal('open'))
  useEffect(() => {
    if (!canTerminal) setTerminal('closed')
  }, [canTerminal])

  // The router restores window scroll, which no longer moves; a new page starts at the top.
  useEffect(() => {
    main.current?.scrollTo(0, 0)
  }, [pathname])

  return (
    <div className="flex h-full flex-col">
      <header className="flex h-12 shrink-0 items-center justify-between border-b border-line bg-surface">
        <div className="flex h-full items-center gap-3 px-4 md:w-56 md:justify-between md:border-e md:border-line">
          <Link to="/" className="font-pixel text-lg text-accent">
            {meta.name}
          </Link>
          <div className="flex items-center gap-1">
            {updates.data?.available ? (
              <Link to="/settings" title={t.shell.updateAvailable(updates.data.latest ?? '')}>
                <Badge tone="warn">{t.shell.update}</Badge>
              </Link>
            ) : (
              <span className="text-[10px] text-muted">{meta.version}</span>
            )}
            <UpdateCheck updates={updates.data} />
          </div>
        </div>
        <div className="flex min-w-0 flex-1 items-center justify-end gap-2 px-2 sm:justify-between sm:px-4">
          <CommandPalette onOpenTerminal={canTerminal ? openTerminal : undefined} />
          <div className="flex shrink-0 items-center gap-1 sm:gap-2">
            <button
              type="button"
              onClick={() => setPicking(true)}
              aria-label={t.shell.chooseTheme}
              title={t.shell.theme}
              className="flex size-8 items-center justify-center border border-transparent text-muted hover:border-line hover:text-fg"
            >
              <PaletteIcon />
            </button>
            <WhatsNew version={meta.version} />
            {canTerminal && (
              <button
                type="button"
                onClick={() => (terminal === 'open' ? setTerminal('hidden') : openTerminal())}
                aria-label={t.shell.hostTerminal}
                aria-pressed={terminal === 'open'}
                title={t.shell.hostTerminal}
                className={cx(
                  'relative flex size-8 items-center justify-center border hover:border-line hover:text-fg',
                  terminal === 'open' ? 'border-line text-fg' : 'border-transparent text-muted',
                )}
              >
                <TerminalWindow size={16} aria-hidden />
                {terminal !== 'closed' && <span aria-hidden className="absolute end-1 top-1 size-1.5 rounded-full bg-accent" />}
              </button>
            )}
            <QueueMenu databaseOperations={databaseOperations.operations} />
            <LanguagePicker />
            {me && <UserMenu user={me} onPickTheme={() => setPicking(true)} />}
          </div>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        <aside className="shrink-0 border-b border-line bg-surface md:w-56 md:overflow-y-auto md:border-e md:border-b-0">
          <Sidebar />
        </aside>
        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          <main ref={main} className="min-h-0 min-w-0 flex-1 overflow-y-auto p-4 md:p-8">
            <Outlet />
          </main>
          {terminal !== 'closed' && (
            <Suspense fallback={null}>
              <HostTerminalDock hidden={terminal === 'hidden'} guard={guard} onHide={() => setTerminal('hidden')} onClose={() => setTerminal('closed')} />
            </Suspense>
          )}
        </div>
      </div>
      <DeployToasts databaseOperations={databaseOperations.operations} onDismissDatabase={databaseOperations.dismiss} />
      {unlock && (
        <PasswordConfirmDialog
          action={{
            title: t.shell.hostTerminal,
            confirm: t.shell.openTerminal,
            detail: t.shell.hostTerminalAs(unlock.user),
            onConfirm: (password) => api('/host/terminal/unlock', { method: 'POST', body: { password } }),
          }}
          onClose={() => setUnlock(null)}
          onComplete={() => { unlock.run(); setUnlock(null) }}
        />
      )}
      {picking && <ThemePicker brand={meta.name} onClose={() => setPicking(false)} />}
    </div>
  )
}

function PaletteIcon() {
  return (
    <svg viewBox="0 0 16 16" className="size-4" fill="currentColor" aria-hidden>
      <path d="M8 1.5a6.5 6.5 0 0 0 0 13c.9 0 1.4-.6 1.4-1.3 0-.4-.2-.7-.4-1-.2-.2-.3-.5-.3-.8 0-.7.6-1.2 1.3-1.2h1.5a3 3 0 0 0 3-3C14.5 4 11.6 1.5 8 1.5Zm-3.5 7a1 1 0 1 1 0-2 1 1 0 0 1 0 2Zm1.5-3a1 1 0 1 1 0-2 1 1 0 0 1 0 2Zm4 0a1 1 0 1 1 0-2 1 1 0 0 1 0 2Zm2 3a1 1 0 1 1 0-2 1 1 0 0 1 0 2Z" />
    </svg>
  )
}

function UserMenu({ user, onPickTheme }: { user: User; onPickTheme: () => void }) {
  const [open, setOpen] = useState(false)
  const theme = useTheme()
  const t = useT()
  const root = useRef<HTMLDivElement>(null)
  const logout = useLogout()

  // Close on a click outside or on Escape.
  useEffect(() => {
    if (!open) return
    const onDown = (e: PointerEvent) => {
      if (!root.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('pointerdown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('pointerdown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const initial = user.email.charAt(0).toUpperCase() || '?'

  return (
    <div ref={root} className="relative">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={t.shell.accountMenu}
        title={user.email}
        className={cx(
          'flex size-8 items-center justify-center border bg-raised font-pixel text-sm text-accent',
          open ? 'border-accent' : 'border-line hover:border-line-strong',
        )}
      >
        {initial}
      </button>
      {open && (
        <div role="menu" className="absolute top-full end-0 z-20 mt-2 w-64 border border-line bg-surface shadow-lg">
          <div className="border-b border-line px-4 py-3">
            <p dir="ltr" className="truncate text-start text-sm" title={user.email}>
              {user.email}
            </p>
            <p className="text-xs text-muted">{t.users.roles[user.role] ?? user.role}</p>
          </div>
          <div className="p-1">
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false)
                onPickTheme()
              }}
              className="flex w-full items-center justify-between px-3 py-1.5 text-start text-sm text-muted hover:bg-raised hover:text-fg"
            >
              {t.shell.theme}
              <span className="text-xs">{theme.name}</span>
            </button>
            <Link
              to="/settings"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="block px-3 py-1.5 text-sm text-muted hover:bg-raised hover:text-fg"
            >
              {t.common.settings}
            </Link>
            <button
              type="button"
              role="menuitem"
              onClick={() => logout.mutate()}
              disabled={logout.isPending}
              className="block w-full px-3 py-1.5 text-start text-sm text-muted hover:bg-raised hover:text-danger disabled:opacity-50"
            >
              {logout.isPending ? t.shell.signingOut : t.shell.signOut}
            </button>
            {logout.error && <p role="alert" className="px-3 py-1.5 text-xs text-danger">{logout.error.message}</p>}
          </div>
        </div>
      )}
    </div>
  )
}
