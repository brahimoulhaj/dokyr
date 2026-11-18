import { Dialog } from '@base-ui/react/dialog'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useLocation, useNavigate, useParams } from '@tanstack/react-router'
import { useEffect, useId, useRef, useState } from 'react'
import type { Icon } from '@phosphor-icons/react'
import { MagnifyingGlass } from '@phosphor-icons/react/dist/csr/MagnifyingGlass'
import { Plus } from '@phosphor-icons/react/dist/csr/Plus'
import { ArrowRight } from '@phosphor-icons/react/dist/csr/ArrowRight'
import { FolderSimple } from '@phosphor-icons/react/dist/csr/FolderSimple'
import { Cube } from '@phosphor-icons/react/dist/csr/Cube'
import { Database } from '@phosphor-icons/react/dist/csr/Database'
import { Globe } from '@phosphor-icons/react/dist/csr/Globe'
import { Package } from '@phosphor-icons/react/dist/csr/Package'
import { GitBranch } from '@phosphor-icons/react/dist/csr/GitBranch'
import { HardDrives } from '@phosphor-icons/react/dist/csr/HardDrives'
import { GearSix } from '@phosphor-icons/react/dist/csr/GearSix'
import { SquaresFour } from '@phosphor-icons/react/dist/csr/SquaresFour'
import { ClockCounterClockwise } from '@phosphor-icons/react/dist/csr/ClockCounterClockwise'
import { Palette } from '@phosphor-icons/react/dist/csr/Palette'
import { Translate } from '@phosphor-icons/react/dist/csr/Translate'
import { SignOut } from '@phosphor-icons/react/dist/csr/SignOut'
import { Check } from '@phosphor-icons/react/dist/csr/Check'
import { TerminalWindow } from '@phosphor-icons/react/dist/csr/TerminalWindow'
import { Timer } from '@phosphor-icons/react/dist/csr/Timer'
import { Gift } from '@phosphor-icons/react/dist/csr/Gift'
import { api, projectsQuery } from '../lib/api'
import { useCan } from '../lib/roles'
import { languages, setLang, useLang, useT } from '../lib/i18n'
import { setTheme, themes, useTheme } from '../lib/theme'
import { useLogout } from '../lib/logout'
import { NewProject } from '../routes/projects'
import { NewDatabase } from '../routes/databases'
import { AddExternal } from '../routes/registries'
import { fold, useDebounced } from './list-controls'
import { Sheet } from './sheet'
import { openWhatsNew } from './whats-new'
import { cx } from './ui'

type ResourceKind = 'project' | 'app' | 'database' | 'domain' | 'registry' | 'git' | 'storage'
type Resource = { kind: ResourceKind; id: string; name: string; detail: string; project_id: string; project_name: string }
type Creation = { kind: 'project' | 'database' | 'registry' }
type Entry = { id: string; label: string; detail?: string; icon: Icon; group: string; keywords?: string; current?: boolean; disabled?: boolean; run: () => void }
const icons: Record<ResourceKind, Icon> = { project: FolderSimple, app: Cube, database: Database, domain: Globe, registry: Package, git: GitBranch, storage: HardDrives }

/** onOpenTerminal is set only for users allowed to open the host terminal. */
export function CommandPalette({ onOpenTerminal }: { onOpenTerminal?: () => void }) {
  const t = useT()
  const c = t.command
  const theme = useTheme()
  const lang = useLang()
  const logout = useLogout()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { projectId } = useParams({ strict: false }) as { projectId?: string }
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [mode, setMode] = useState<'project' | 'theme' | 'language' | null>(null)
  const choosingProject = mode === 'project'
  const [selected, setSelected] = useState(0)
  const [creation, setCreation] = useState<Creation | null>(null)
  const input = useRef<HTMLInputElement>(null)
  const pendingEnter = useRef(false)
  const listId = useId()
  const debounced = useDebounced(query.trim(), 150)
  const can = useCan()
  const resources = useQuery({
    queryKey: ['command-search', debounced],
    queryFn: () => api<{ items: Resource[]; truncated: boolean }>(`/search?q=${encodeURIComponent(debounced)}`),
    enabled: open && mode === null,
    // Keep the last results on screen while the next search runs, so the list doesn't flash empty.
    placeholderData: keepPreviousData,
    staleTime: 0,
    retry: false,
    gcTime: 60_000,
  })
  const projects = useQuery({ ...projectsQuery, enabled: open && choosingProject })

  function changeOpen(value: boolean) {
    setOpen(value)
    if (value) {
      setQuery('')
      setSelected(0)
      pendingEnter.current = false
      setMode(null)
      logout.reset()
    }
  }

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k' && !event.isComposing) {
        // Leave an open editor/form's shortcuts alone.
        if (!open && document.querySelector('[role="dialog"]')) return
        event.preventDefault()
        if (!event.repeat) changeOpen(!open)
      }
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open])

  useEffect(() => { setOpen(false); setCreation(null) }, [pathname])

  function create(value: Creation) {
    setOpen(false)
    setCreation(value)
  }
  function chooseProject() {
    chooseMode('project')
  }
  function chooseMode(value: 'project' | 'theme' | 'language' | null) {
    setMode(value)
    setQuery('')
    setSelected(0)
    pendingEnter.current = false
    input.current?.focus()
  }

  function visit(resource: Resource) {
    setOpen(false)
    switch (resource.kind) {
      case 'project': return navigate({ to: '/projects/$projectId', params: { projectId: resource.id } })
      case 'app': return navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId: resource.project_id, appId: resource.id } })
      case 'database': return navigate({ to: '/databases/$databaseId', params: { databaseId: resource.id } })
      case 'domain': return navigate({ to: '/domains' })
      case 'registry': return navigate({ to: '/registry' })
      case 'git': return navigate({ to: '/git' })
      case 'storage': return navigate({ to: '/settings', search: { tab: 'storage' } })
    }
  }

  const labels: Record<ResourceKind, string> = {
    project: t.shell.projects, app: t.project.apps, database: t.shell.databases,
    domain: t.domains.title, registry: t.shell.registry, git: t.settingsGit.title, storage: t.settingsStorage.title,
  }
  const local: Entry[] = [
    { id: 'new-app', label: c.newApp, icon: Plus, group: c.actions, keywords: 'create new app application', run: chooseProject },
    { id: 'new-project', label: c.newProject, icon: Plus, group: c.actions, keywords: 'create new project', run: () => create({ kind: 'project' }) },
    { id: 'new-database', label: c.newDatabase, icon: Plus, group: c.actions, keywords: 'create new database postgres mariadb', run: () => create({ kind: 'database' }) },
    { id: 'new-registry', label: c.registry, icon: Plus, group: c.actions, keywords: 'add connect registry', run: () => create({ kind: 'registry' }) },
    ...(onOpenTerminal ? [{ id: 'host-terminal', label: t.shell.hostTerminal, icon: TerminalWindow, group: c.actions, keywords: 'open terminal shell console ssh host server root command line', run: () => { setOpen(false); onOpenTerminal() } }] : []),
    { id: 'choose-theme', label: t.shell.chooseTheme, detail: theme.name, icon: Palette, group: c.preferences, keywords: 'theme switch appearance dark light', run: () => chooseMode('theme') },
    { id: 'choose-language', label: t.shell.chooseLanguage, detail: languages.find((l) => l.code === lang)?.name, icon: Translate, group: c.preferences, keywords: 'language switch locale translate', run: () => chooseMode('language') },
    { id: 'whats-new', label: t.shell.whatsNew, icon: Gift, group: c.preferences, keywords: 'whats new changelog release notes version changes news', run: () => { setOpen(false); openWhatsNew() } },
    { id: 'sign-out', label: logout.isPending ? t.shell.signingOut : t.shell.signOut, icon: SignOut, group: c.preferences, keywords: 'logout log out sign out disconnect', disabled: logout.isPending, run: () => { if (!logout.isPending) logout.mutate() } },
    ...([
      ['overview', t.shell.overview, SquaresFour, '/'],
      ['projects', t.shell.projects, FolderSimple, '/projects'],
      ['databases', t.shell.databases, Database, '/databases'],
      ['activity', t.shell.activity, ClockCounterClockwise, '/activity'],
      ['domains', t.domains.title, Globe, '/domains'],
      ['registry', t.shell.registry, Package, '/registry'],
      ['storage', t.storage.title, HardDrives, '/storage'],
      ['settings', t.common.settings, GearSix, '/settings'],
      ['git', t.settingsGit.title, GitBranch, '/git'],
      ['tokens', t.settingsTokens.title, GearSix, '/tokens'],
      ...(!can.admin ? [] : [['backup', t.settingsBackup.title, ClockCounterClockwise, '/backups'] as const, ['cron', t.cronJobs.title, Timer, '/cron'] as const]),
    ] as const).map(([id, label, icon, to]) => ({ id, label, icon, group: c.pages, keywords: id, run: () => { setOpen(false); void navigate({ to }) } })),
    ...([
      ['storage', t.settingsStorage.title, HardDrives],
      ['builds', t.settingsBuilds.title, GearSix], ['notifications', t.settingsNotifications.title, GearSix],
    ] as const).map(([tab, label, icon]) => ({ id: `settings-${tab}`, label, icon, group: c.pages, keywords: tab, run: () => { setOpen(false); void navigate({ to: '/settings', search: { tab } }) } })),
  ]
  const terms = fold(query).trim().split(/\s+/).filter(Boolean)
  const matches = (value: string) => terms.every((term) => fold(value).includes(term))
  const searching = query.trim() !== debounced || resources.isFetching
  const entries: Entry[] = choosingProject
    ? (projects.data ?? []).filter((p) => matches(`${p.name} ${p.description}`))
      .sort((a, b) => Number(b.id === projectId) - Number(a.id === projectId))
      .map((p) => ({ id: p.id, label: p.name, detail: p.description, icon: FolderSimple, group: t.shell.projects, run: () => { setOpen(false); void navigate({ to: '/projects/$projectId/new', params: { projectId: p.id } }) } }))
    : mode === 'theme'
      ? themes.filter((item) => matches(`${item.name} ${item.mode} ${t.theme[item.mode]}`)).map((item) => ({
        id: item.id, label: item.name, detail: t.theme[item.mode], icon: Palette, group: t.shell.theme, current: item.id === theme.id,
        run: () => { setTheme(item.id); setOpen(false) },
      }))
    : mode === 'language'
      ? languages.filter((item) => matches(`${item.name} ${item.english} ${item.code}`)).map((item) => ({
        id: item.code, label: item.name, detail: item.english, icon: Translate, group: t.shell.language, current: item.code === lang,
        run: () => { setLang(item.code); setOpen(false) },
      }))
    : [
      ...local.filter((item) => item.group !== c.pages && matches(`${item.label} ${item.keywords ?? ''}`)),
      ...(resources.data?.items ?? []).map((r) => ({
        id: `${r.kind}-${r.id}`, label: r.name, detail: [r.project_name !== r.name ? r.project_name : '', r.detail].filter(Boolean).join(' · '),
        icon: icons[r.kind], group: labels[r.kind], run: () => { void visit(r) },
      })),
      ...local.filter((item) => item.group === c.pages && matches(`${item.label} ${item.keywords ?? ''}`)),
    ]
  // Keep each group together while retaining the server's ranking within it.
  const groups = [...new Set(entries.map((entry) => entry.group))]
  const ordered = groups.flatMap((group) => entries.filter((entry) => entry.group === group))
  const activeIndex = Math.min(selected, ordered.length - 1)
  const activeId = activeIndex >= 0 ? `${listId}-${ordered[activeIndex].id}` : undefined
  // New results reorder the list: go back to the top so Enter never runs a different row than the one shown.
  useEffect(() => { setSelected(0) }, [resources.data])
  useEffect(() => {
    if (searching || !pendingEnter.current) return
    pendingEnter.current = false
    const entry = ordered[0]
    if (entry && !entry.disabled) entry.run()
  })
  useEffect(() => {
    if (open && activeId) document.getElementById(activeId)?.scrollIntoView({ block: 'nearest' })
  }, [open, activeId])
  const error = choosingProject ? projects.error : mode === null ? resources.error : null
  const loading = choosingProject ? projects.isPending : mode === null && resources.isPending
  const modeTitle = choosingProject ? c.chooseProject : mode === 'theme' ? t.shell.chooseTheme : mode === 'language' ? t.shell.chooseLanguage : c.placeholder
  const hasMore = resources.data?.truncated ?? false
  const done = () => setCreation(null)
  const title = creation?.kind === 'project' ? t.projects.newProject : creation?.kind === 'database' ? t.databases.add : t.registries.addExternal

  return <>
    <Dialog.Root open={open} onOpenChange={changeOpen}>
      <Dialog.Trigger aria-label={c.title} aria-keyshortcuts="Meta+K Control+K" title={`${c.title} (⌘K / Ctrl+K)`}
        className="flex h-8 min-w-8 items-center gap-2 border border-line px-2 text-xs text-muted hover:border-line-strong hover:text-fg sm:w-56 md:w-72">
        <MagnifyingGlass size={15} aria-hidden className="shrink-0" />
        <span className="hidden min-w-0 flex-1 truncate text-start sm:block">{c.search}</span>
        <kbd dir="ltr" className="hidden border border-line bg-bg px-1 text-[10px] sm:block">{navigator.platform.includes('Mac') ? '⌘ K' : 'Ctrl K'}</kbd>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-bg/70 backdrop-blur-[2px]" />
        <Dialog.Popup initialFocus={input} className="fixed start-1/2 top-[min(15vh,7rem)] z-50 flex max-h-[min(75dvh,36rem)] w-[calc(100%-2rem)] max-w-xl -translate-x-1/2 flex-col border border-line-strong bg-surface text-fg shadow-lg outline-none rtl:translate-x-1/2">
          <Dialog.Title className="sr-only">{c.title}</Dialog.Title>
          <Dialog.Description className="sr-only">{modeTitle}</Dialog.Description>
          {mode && <div className="flex items-center gap-3 border-b border-line px-4 py-2 text-xs text-muted">
            <button type="button" className="text-accent hover:underline" onClick={() => chooseMode(null)}>{c.back}</button>
            <span>{modeTitle}</span>
          </div>}
          <div className="flex shrink-0 items-center gap-3 border-b border-line px-4">
            <MagnifyingGlass size={18} aria-hidden className="shrink-0 text-accent" />
            <input ref={input} value={query} onChange={(e) => { setQuery(e.target.value); setSelected(0); pendingEnter.current = false }}
              placeholder={modeTitle} maxLength={64}
              aria-label={modeTitle}
              role="combobox" aria-expanded="true" aria-controls={listId} aria-autocomplete="list" aria-activedescendant={activeId}
              autoComplete="off" spellCheck={false} className="h-13 min-w-0 flex-1 bg-transparent text-sm outline-none! placeholder:text-muted"
              onKeyDown={(e) => {
                if (e.nativeEvent.isComposing) return
                if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(e.key) && ordered.length) {
                  if ((e.key === 'Home' || e.key === 'End') && query) return
                  e.preventDefault()
                  setSelected(e.key === 'Home' ? 0 : e.key === 'End' ? ordered.length - 1 : (activeIndex + (e.key === 'ArrowDown' ? 1 : -1) + ordered.length) % ordered.length)
                }
                if (e.key === 'Enter') {
                  e.preventDefault()
                  // Results for what was typed haven't arrived yet: run the top row once they do.
                  if (mode === null && searching) { pendingEnter.current = true; return }
                  const entry = ordered[activeIndex]
                  if (!entry?.disabled) entry?.run()
                }
              }} />
            <Dialog.Close aria-label={t.common.close} className="shrink-0 border border-line px-1.5 py-0.5 text-[10px] text-muted hover:text-fg">Esc</Dialog.Close>
          </div>
          <div className="min-h-0 overflow-y-auto overscroll-contain p-1.5">
            <div role="listbox" id={listId} aria-label={c.resources} aria-busy={loading || (mode === null && searching)}>
              {groups.map((group) => <div role="group" aria-label={group} key={group} className="mb-1">
                <p aria-hidden className="px-2.5 pb-1 pt-2 text-[10px] font-medium tracking-wide text-muted uppercase">{group}</p>
                {ordered.filter((entry) => entry.group === group).map((entry) => {
                  const id = `${listId}-${entry.id}`
                  const active = id === activeId
                  return <div role="option" aria-selected={active} aria-disabled={entry.disabled || undefined} id={id} key={entry.id}
                    onPointerMove={() => setSelected(ordered.indexOf(entry))}
                    onPointerDown={(e) => e.preventDefault()} onClick={() => !entry.disabled && entry.run()}
                    className={cx('flex cursor-pointer items-center gap-3 border px-2.5 py-2', active ? 'border-line bg-raised' : 'border-transparent hover:bg-raised', entry.disabled && 'cursor-wait opacity-50')}>
                    <entry.icon size={17} aria-hidden className={cx('shrink-0', active ? 'text-accent' : 'text-muted')} />
                    <span className="min-w-0 flex-1"><span className="block truncate text-sm">{entry.label}</span>
                      {entry.detail && <span className="block truncate text-[11px] text-muted">{entry.detail}</span>}</span>
                    {entry.current ? <><span className="text-[10px] text-accent">{t.theme.current}</span><Check size={14} aria-hidden className="shrink-0 text-accent" /></> : active && <ArrowRight size={14} aria-hidden className="shrink-0 text-muted rtl:rotate-180" />}
                  </div>
                })}
              </div>)}
            </div>
            <div role="status" className="text-xs text-muted">
              {loading && <p className="px-3 py-4">{c.loading}</p>}
              {!loading && !error && ordered.length === 0 && <p className="px-3 py-6 text-center">{c.empty}</p>}
              {choosingProject && !loading && projects.data?.length === 0 && <button type="button" onClick={() => create({ kind: 'project' })} className="mb-3 px-3 text-accent hover:underline">{c.newProject}</button>}
              {error && <p className="px-3 py-3 text-danger">{c.error} <button type="button" className="underline" onClick={() => { void (choosingProject ? projects.refetch() : resources.refetch()) }}>{c.retry}</button></p>}
              {logout.error && <p role="alert" className="px-3 py-3 text-danger">{logout.error.message}</p>}
            </div>
          </div>
          <div className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-1 border-t border-line px-4 py-2 text-[10px] text-muted">
            <span><kbd>↑ ↓</kbd> {c.navigate}</span><span><kbd>↵</kbd> {c.select}</span><span><kbd>Esc</kbd> {c.close}</span>
            {mode === null && !searching && hasMore && <span className="basis-full">{c.more}</span>}
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
    <Sheet open={creation !== null} onOpenChange={(value) => !value && done()} title={title}>
      {creation?.kind === 'project' && <NewProject onDone={done} />}
      {creation?.kind === 'database' && <NewDatabase projectId={projectId} onDone={done} />}
      {creation?.kind === 'registry' && <AddExternal onDone={done} />}
    </Sheet>
  </>
}
