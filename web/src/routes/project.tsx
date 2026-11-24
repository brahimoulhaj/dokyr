import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { Badge, Button, CheckOption, ConfirmButton, cx, Empty, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, appQuery, appsQuery, isWildcard, projectQuery, projectsQuery, routesQuery, type App, type Project, type Route } from '../lib/api'
import { appSource, appStatus } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet } from '../components/sheet'
import { EnvironmentEditor } from '../components/environment'
import { projectDatabasesQuery } from '../lib/databases'
import { DatabaseStatus, NewDatabase } from './databases'
import { RoutingBoard } from '../components/routing'
import { ContextBreadcrumb } from '../components/context-breadcrumb'
import { poll } from '../lib/live'
import { useCan } from '../lib/roles'
import { confirmWithPasskey, ownPasskeyQuery } from '../lib/passkeys'
import { toast } from '@yusufceng1z/ding-react'
import { X } from '@phosphor-icons/react'
import { ConfirmModal } from '../components/confirm-dialog'
import { PasswordConfirmDialog } from '../components/password-confirm-dialog'

export const projectTabs = ['apps', 'databases', 'routing', 'environment', 'settings'] as const
export type ProjectTab = (typeof projectTabs)[number]

export function ProjectPage() {
  const { projectId } = useParams({ strict: false }) as { projectId: string }
  const project = useQuery(projectQuery(projectId))
  const apps = useQuery(appsQuery(projectId))
  // Routes belong to apps; the project's are those of its apps.
  const routes = (useQuery(routesQuery).data ?? []).filter((r) => r.project_id === projectId)
  const front = mainRoute(routes)
  const databases = useQuery(projectDatabasesQuery(projectId))
  const [addingDatabase, setAddingDatabase] = useState(false)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: string }
  const tab: ProjectTab = projectTabs.includes(search.tab as ProjectTab) ? (search.tab as ProjectTab) : 'apps'
  const t = useT()

  if (!project.data) {
    return (
      <div className="space-y-4">
        <ContextBreadcrumb projectId={projectId} />
        <QueryState loading={project.isPending} error={project.error} what={t.project.what} />
      </div>
    )
  }
  const p = project.data

  return (
    <div className="space-y-6">
      <PageHeader
        crumbs={<ContextBreadcrumb projectId={p.id} projectName={p.name} projectTab={tab} />}
        title={p.name}
        subtitle={
          (p.description || front) && (
            <>
              {p.description && <span className="block">{p.description}</span>}
              {front && <RouteLink route={front} />}
            </>
          )
        }
        actions={tab === 'apps' ? <Button onClick={() => navigate({ to: '/projects/$projectId/new', params: { projectId: p.id } })}>{t.project.newApp}</Button>
          : tab === 'databases' ? <Button onClick={() => setAddingDatabase(true)}>{t.databases.add}</Button> : undefined}
      />

      <nav className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line" aria-label={p.name}>
        {projectTabs.map((name) => (
          <Link
            key={name}
            to="/projects/$projectId"
            params={{ projectId: p.id }}
            search={{ tab: name }}
            className={cx('border-b-2 px-3 py-1.5 text-sm whitespace-nowrap', name === tab ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg')}
            aria-current={name === tab ? 'page' : undefined}
          >
            {{ apps: t.project.apps, databases: t.databases.title, routing: t.routing.title, environment: t.project.envTab, settings: t.common.settings }[name]}
            {name === 'apps' && apps.data && <span className="ms-1.5 text-xs text-muted">{apps.data.length}</span>}
            {name === 'databases' && databases.data && <span className="ms-1.5 text-xs text-muted">{databases.data.length}</span>}
          </Link>
        ))}
      </nav>

      {tab === 'apps' && (
        <Panel>
          <QueryState loading={apps.isPending} error={apps.error} what={t.project.appsWhat} />
          {apps.data &&
            (apps.data.length === 0 ? (
              <Empty title={t.project.emptyTitle}>{t.project.emptyHint}</Empty>
            ) : (
              <AppList projectId={p.id} apps={apps.data} routes={routes} />
            ))}
        </Panel>
      )}
      {tab === 'databases' && <div className="space-y-4">
        <p className="text-sm text-muted">{t.project.databasesIntro}</p>
        <Panel>
          <QueryState loading={databases.isPending} error={databases.error} what={t.databases.title} />
          {databases.data?.length === 0 && <Empty title={t.databases.empty}>{t.databases.emptyHint}</Empty>}
          {databases.data && databases.data.length > 0 && <ul className="divide-y divide-line">{databases.data.map((d) => <li key={d.id}>
            <Link to="/databases/$databaseId" params={{ databaseId: d.id }} search={{ tab: 'access' }} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 hover:bg-raised">
              <div className="min-w-0"><span className="block break-words text-sm">{d.name}</span>
                <span className="text-xs text-muted">{d.engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'} {d.version}</span>
                <p className="mt-1 text-xs text-muted">{t.databases.databasesList}: <span dir="ltr" className="break-words">{d.database_names.join(', ')}</span></p>
              </div>
              <DatabaseStatus value={d.status === 'running' && d.runtime !== 'running' ? d.runtime : d.status} />
            </Link>
          </li>)}</ul>}
        </Panel>
      </div>}
      {tab === 'routing' && <RoutingBoard projectId={p.id} intro={t.routing.projectIntro} />}
      {tab === 'environment' && <ProjectEnvironment projectId={p.id} />}
      {tab === 'settings' && (
        <Panel title={t.project.settingsTitle}>
          <ProjectSettings key={p.updated_at} project={p} />
        </Panel>
      )}

      <Sheet open={addingDatabase} onOpenChange={setAddingDatabase} title={t.databases.add}>
        {addingDatabase && <NewDatabase projectId={p.id} onDone={() => setAddingDatabase(false)} />}
      </Sheet>
    </div>
  )
}

type BulkAction = 'deploy' | 'start' | 'stop' | 'restart' | 'delete'

/** The project's apps, with checkboxes to act on several at once. */
function AppList({ projectId, apps, routes }: { projectId: string; apps: App[]; routes: Route[] }) {
  const t = useT()
  const can = useCan()
  const queryClient = useQueryClient()
  const [picked, setPicked] = useState<ReadonlySet<string>>(new Set())
  // An app deleted meanwhile leaves the selection with the list.
  const chosen = apps.filter((a) => picked.has(a.id))
  const running = chosen.filter((a) => a.current_deployment_id && a.desired_state === 'running')
  // What each action applies to: an app never deployed has nothing to start or stop.
  const targets: Record<BulkAction, App[]> = {
    deploy: chosen,
    restart: running,
    stop: running,
    start: chosen.filter((a) => a.current_deployment_id && a.desired_state === 'stopped'),
    delete: chosen,
  }

  /** Refreshes what the action touched, whether or not some of it failed. */
  async function refresh(name: BulkAction, list: App[]) {
    await queryClient.invalidateQueries({ queryKey: appsQuery(projectId).queryKey, exact: true })
    for (const a of list) {
      if (name === 'delete') queryClient.removeQueries({ queryKey: appQuery(a.id).queryKey })
      else queryClient.invalidateQueries({ queryKey: appQuery(a.id).queryKey })
    }
    if (name === 'delete') {
      queryClient.invalidateQueries({ queryKey: projectQuery(projectId).queryKey, exact: true })
      queryClient.invalidateQueries({ queryKey: routesQuery.queryKey })
    }
  }
  /** Says how it went; the apps that failed stay selected, to try again. */
  function report(name: BulkAction, total: number, failed: string[]) {
    const m = t.project.bulk[name]
    if (failed.length > 0) return void toast.error(m.fail(failed.length, total), { description: failed.join('\n') })
    toast.success(m.done(total))
    setPicked(new Set())
  }

  const run = useMutation({
    mutationFn: async (name: Exclude<BulkAction, 'delete'>) => {
      const list = targets[name]
      const results = await Promise.allSettled(list.map((a) => api<unknown>(`/apps/${a.id}/${name}`, { method: 'POST' })))
      const failed = results.flatMap((r, i) => (r.status === 'rejected' ? [`${list[i].name}: ${r.reason instanceof Error ? r.reason.message : String(r.reason)}`] : []))
      await refresh(name, list)
      report(name, list.length, failed)
    },
  })
  // Deleting asks for the password; a wrong one fails here, inside the dialog.
  // Without one, a passkey confirmation was made just before and the server holds it.
  async function remove(password?: string) {
    const list = targets.delete
    const out = await api<{ failed: { id: string; name: string; error: string }[] }>(`/projects/${projectId}/apps`, { method: 'DELETE', body: { app_ids: list.map((a) => a.id), password } })
    await refresh('delete', list)
    report('delete', list.length, out.failed.map((f) => `${f.name || f.id}: ${f.error}`))
  }

  const [asking, setAsking] = useState<BulkAction | null>(null)
  const passkey = useQuery({ ...ownPasskeyQuery, enabled: asking === 'delete' })
  const toggle = (id: string, on: boolean) =>
    setPicked((old) => {
      const next = new Set(old)
      if (on) next.add(id)
      else next.delete(id)
      return next
    })
  const all = chosen.length === apps.length
  const busy = run.isPending
  const names = (list: App[]) => (
    <span dir="ltr" className="mt-3 block max-h-40 overflow-y-auto border-s-2 border-line-strong ps-3 text-fg">
      {list.map((a) => <span key={a.id} className="block truncate">{a.name}</span>)}
    </span>
  )
  const act = (name: BulkAction, label: string) => (
    <button
      type="button"
      disabled={busy || targets[name].length === 0}
      onClick={() => setAsking(name)}
      className={cx(
        'h-11 shrink-0 px-3.5 text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-35',
        name === 'delete' ? 'text-danger enabled:hover:bg-danger enabled:hover:text-bg' : 'text-muted enabled:hover:bg-raised enabled:hover:text-fg',
      )}
    >
      {label}
    </button>
  )
  const ask = asking && asking !== 'delete' ? asking : null

  return (
    <>
      <label className="flex cursor-pointer items-center gap-3 border-b border-line px-4 py-2.5 text-xs text-muted">
        <input
          type="checkbox"
          className="accent-[var(--color-accent)]"
          checked={all}
          ref={(el) => {
            if (el) el.indeterminate = chosen.length > 0 && !all
          }}
          onChange={(e) => setPicked(new Set(e.target.checked ? apps.map((a) => a.id) : []))}
        />
        {chosen.length > 0 ? t.project.selected(chosen.length) : t.project.selectAll}
      </label>
      <ul className="divide-y divide-line">
        {apps.map((a) => (
          <AppRow key={a.id} app={a} routes={routes.filter((r) => r.app_id === a.id)} picked={picked.has(a.id)} onPick={(on) => toggle(a.id, on)} />
        ))}
      </ul>

      {chosen.length > 0 && (
        <div className="bulk-bar pointer-events-none fixed inset-x-0 bottom-5 z-30 flex justify-center px-4">
          <div
            role="toolbar"
            aria-label={t.project.bulkActions}
            className="pointer-events-auto flex max-w-full animate-[stack-in_220ms_cubic-bezier(.2,.8,.3,1)] items-stretch border border-line-strong bg-surface shadow-[5px_5px_0_0_rgb(0_0_0/0.3)] motion-reduce:animate-none"
          >
            <span className="flex shrink-0 items-center gap-2.5 bg-accent ps-3 pe-3.5 text-accent-fg" aria-live="polite">
              <span className="font-pixel text-base leading-none tabular-nums">{String(chosen.length).padStart(2, '0')}</span>
              <span className="font-pixel text-[10px] uppercase leading-none">{busy ? t.common.working : t.project.selectedLabel}</span>
            </span>
            <div className="flex min-w-0 items-stretch divide-x divide-line overflow-x-auto">
              {act('deploy', chosen.every((a) => a.current_deployment_id) ? t.app.redeploy : t.app.deploy)}
              {act('restart', t.app.restart)}
              {act('stop', t.app.stop)}
              {act('start', t.app.start)}
              {can.remove && act('delete', t.project.bulkDelete)}
            </div>
            <button
              type="button"
              disabled={busy}
              onClick={() => setPicked(new Set())}
              aria-label={t.project.clearSelection}
              title={t.project.clearSelection}
              className="flex w-11 shrink-0 items-center justify-center border-s border-line-strong text-muted transition-colors hover:bg-raised hover:text-fg disabled:opacity-35"
            >
              <X size={15} aria-hidden />
            </button>
          </div>
        </div>
      )}

      {ask && (
        <ConfirmModal
          open
          onOpenChange={(open) => { if (!open) setAsking(null) }}
          title={t.project.bulkConfirm[ask].title(targets[ask].length)}
          message={<>{t.project.bulkConfirm[ask].hint}{names(targets[ask])}</>}
          confirm={ask === 'deploy' ? (chosen.every((a) => a.current_deployment_id) ? t.app.redeploy : t.app.deploy) : t.app[ask]}
          tone={ask === 'stop' ? 'danger' : 'primary'}
          onConfirm={() => run.mutate(ask)}
        />
      )}
      {asking === 'delete' && (
        <PasswordConfirmDialog
          action={{
            title: t.project.bulkDeleteTitle(targets.delete.length),
            confirm: t.project.bulkDeleteConfirm(targets.delete.length),
            detail: `${t.project.bulkDeleteHint} ${targets.delete.map((a) => a.name).join(', ')}`,
            tone: 'danger',
            onConfirm: remove,
            onPasskey: passkey.data ? async () => { await confirmWithPasskey(); await remove() } : undefined,
          }}
          onClose={() => setAsking(null)}
          onComplete={() => setAsking(null)}
        />
      )}
    </>
  )
}

/** One app in the list. It asks for the app on its own to learn what the container is doing. */
function AppRow({ app, routes, picked, onPick }: { app: App; routes: Route[]; picked: boolean; onPick: (on: boolean) => void }) {
  const live = useQuery({ ...appQuery(app.id), refetchInterval: poll(10_000) })
  const t = useT()
  const status = appStatus(live.data ?? app, false)
  return (
    <li className={cx('flex items-center gap-2 pe-4', picked && 'bg-raised')}>
      <label className="flex cursor-pointer items-center self-stretch ps-4">
        <input type="checkbox" className="accent-[var(--color-accent)]" checked={picked} onChange={(e) => onPick(e.target.checked)} aria-label={t.project.selectApp(app.name)} />
      </label>
      <Link
        to="/projects/$projectId/apps/$appId"
        params={{ projectId: app.project_id, appId: app.id }}
        className="flex min-w-0 flex-1 flex-wrap items-center justify-between gap-x-6 gap-y-1 px-4 py-3 hover:bg-raised"
      >
        <span className="min-w-0">
          <span className="block truncate text-sm">{app.name}</span>
          <span className="block truncate text-xs text-muted">
            <span dir="ltr">{appSource(app)}</span> · {t.project.port(app.port)}
          </span>
          {routes.length > 0 && (
            <span dir="ltr" className="block truncate text-xs text-muted">
              {routes.map((r) => r.url.replace(/^https?:\/\//, '')).join('  ·  ')}
            </span>
          )}
        </span>
        <Badge tone={status.tone}>{status.label}</Badge>
      </Link>
      <Link to="/projects/$projectId/apps/$appId" params={{ projectId: app.project_id, appId: app.id }} search={{ tab: 'terminal' }} className="shrink-0 border border-line px-2 py-1.5 text-xs text-muted hover:border-accent hover:text-accent" aria-label={`${t.terminal.title}: ${app.name}`}>
        <span aria-hidden className="me-1 font-mono">&gt;_</span>{t.terminal.title}
      </Link>
    </li>
  )
}

function ProjectSettings({ project }: { project: Project }) {
  const can = useCan()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState(project.name)
  const [description, setDescription] = useState(project.description)
  const dirty = name !== project.name || description !== project.description
  const t = useT()

  const save = useMutation({
    mutationFn: () => api<Project>(`/projects/${project.id}`, { method: 'PATCH', body: { name, description } }),
    onSuccess: (p) => {
      queryClient.setQueryData(projectQuery(project.id).queryKey, p)
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey, exact: true })
    },
  })
  const remove = useMutation({
    mutationFn: () => api<void>(`/projects/${project.id}`, { method: 'DELETE' }),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: projectQuery(project.id).queryKey })
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey, exact: true })
      navigate({ to: '/projects' })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }

  return (
    <>
      <form onSubmit={submit} className="space-y-5 p-5">
        <Field label={t.common.name} name="project-name" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />
        <Field label={t.common.description} name="project-description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} />
        {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
        <Button type="submit" disabled={!dirty || save.isPending}>
          {save.isPending ? t.common.saving : t.common.save}
        </Button>
      </form>
      {can.remove && <section className="space-y-3 border-t border-danger/30 p-5">
        <h3 className="font-pixel text-xs text-danger uppercase">{t.project.deleteProject}</h3>
        <p className="text-xs text-muted">{project.app_count > 0 ? t.project.deleteBlocked : t.project.deleteWarning}</p>
        <ConfirmButton
          label={t.project.deleteProject}
          confirm={t.common.yesDelete}
          disabled={project.app_count > 0}
          pending={remove.isPending}
          onConfirm={() => remove.mutate()}
        />
        {remove.error && <ErrorNote>{remove.error.message}</ErrorNote>}
      </section>}
    </>
  )
}

type ProjectEnv = { environment: Record<string, string>; environment_order: string[]; inherit: boolean }

/** The project's shared variables, and whether every app inherits them. */
function ProjectEnvironment({ projectId }: { projectId: string }) {
  const t = useT()
  const queryClient = useQueryClient()
  const key = ['projects', projectId, 'environment']
  const env = useQuery({ queryKey: key, queryFn: () => api<ProjectEnv>(`/projects/${projectId}/environment`) })
  const [inherit, setInherit] = useState<boolean | null>(null)
  if (!env.data) return <QueryState loading={env.isPending} error={env.error} what={t.project.envTitle} />
  const on = inherit ?? env.data.inherit
  return (
    <EnvironmentEditor
      key={JSON.stringify(env.data)}
      title={t.project.envTitle}
      hint={t.project.envHint}
      initial={env.data.environment}
      order={env.data.environment_order}
      save={(environment, _meta, environment_order) => api<ProjectEnv>(`/projects/${projectId}/environment`, { method: 'PUT', body: { environment, environment_order, inherit: on } })}
      onSaved={() => {
        setInherit(null)
        queryClient.invalidateQueries({ queryKey: key })
      }}
      extraDirty={on !== env.data.inherit}
      extra={<CheckOption checked={on} onChange={setInherit} label={t.project.envInherit} hint={t.project.envInheritHint} />}
    />
  )
}

/** The address that stands for the project: a route serving "/" (its front door), else
 * its first one. */
function mainRoute(list: Route[]): Route | undefined {
  const exact = list.filter((r) => !isWildcard(r.hostname))
  return exact.find((r) => r.path === '/') ?? exact[0]
}

function RouteLink({ route }: { route: Route }) {
  return (
    <a href={route.url} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex max-w-full items-center gap-1.5 text-accent hover:underline" dir="ltr">
      <span className="[overflow-wrap:anywhere]">{route.url.replace(/^https?:\/\//, '')}</span>
      <span aria-hidden="true">↗</span>
    </a>
  )
}
