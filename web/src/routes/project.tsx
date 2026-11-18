import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { SourceFields, sourceForm, sourceInput } from '../components/source'
import { Badge, Button, CheckOption, ConfirmButton, cx, Empty, HelpTip, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { environmentMap, parseEnvironment } from '../lib/environment'
import { allDomainsQuery, api, appQuery, appsQuery, dnsQuery, domainsQuery, hostHref, isWildcard, projectQuery, projectsQuery, type App, type Domain, type Project, type Route } from '../lib/api'
import { appSource, appStatus } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet, SheetActions } from '../components/sheet'
import { EnvironmentEditor } from '../components/environment'
import { DnsHintText } from '../components/dns'
import { projectDatabasesQuery } from '../lib/databases'
import { DatabaseStatus, NewDatabase } from './databases'
import { ContextBreadcrumb } from '../components/context-breadcrumb'
import { poll } from '../lib/live'
import { withToast } from '../lib/toasts'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { AddDomain } from './domains'

export const projectTabs = ['apps', 'databases', 'domains', 'environment', 'settings'] as const
export type ProjectTab = (typeof projectTabs)[number]

export function ProjectPage() {
  const { projectId } = useParams({ strict: false }) as { projectId: string }
  const project = useQuery(projectQuery(projectId))
  const apps = useQuery(appsQuery(projectId))
  const domains = useQuery(domainsQuery(projectId))
  const databases = useQuery(projectDatabasesQuery(projectId))
  const [adding, setAdding] = useState(false)
  const [addingDatabase, setAddingDatabase] = useState(false)
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
          (p.description || mainDomain(domains.data)) && (
            <>
              {p.description && <span className="block">{p.description}</span>}
              {mainDomain(domains.data) && <DomainLink domain={mainDomain(domains.data)!} />}
            </>
          )
        }
        actions={tab === 'apps' ? <Button onClick={() => setAdding(true)}>{t.project.newApp}</Button>
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
            {{ apps: t.project.apps, databases: t.databases.title, domains: t.project.domainsTab, environment: t.project.envTab, settings: t.common.settings }[name]}
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
              <ul className="divide-y divide-line">
                {apps.data.map((a) => (
                  <AppRow key={a.id} app={a} />
                ))}
              </ul>
            ))}
        </Panel>
      )}
      {tab === 'domains' && <ProjectDomains projectId={p.id} apps={apps.data ?? []} />}
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
      {tab === 'environment' && <ProjectEnvironment projectId={p.id} />}
      {tab === 'settings' && (
        <Panel title={t.project.settingsTitle}>
          <ProjectSettings key={p.updated_at} project={p} />
        </Panel>
      )}

      <Sheet open={adding} onOpenChange={setAdding} title={t.project.newApp} description={t.project.portHint}>
        {adding && <NewApp projectId={p.id} onDone={() => setAdding(false)} />}
      </Sheet>
      <Sheet open={addingDatabase} onOpenChange={setAddingDatabase} title={t.databases.add}>
        {addingDatabase && <NewDatabase projectId={p.id} onDone={() => setAddingDatabase(false)} />}
      </Sheet>
    </div>
  )
}

/** One app in the list. It asks for the app on its own to learn what the container is doing. */
function AppRow({ app }: { app: App }) {
  const live = useQuery({ ...appQuery(app.id), refetchInterval: poll(10_000) })
  const t = useT()
  const status = appStatus(live.data ?? app, false)
  return (
    <li className="flex items-center gap-2 pe-4">
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
        </span>
        <Badge tone={status.tone}>{status.label}</Badge>
      </Link>
      <Link to="/projects/$projectId/apps/$appId" params={{ projectId: app.project_id, appId: app.id }} search={{ tab: 'terminal' }} className="shrink-0 border border-line px-2 py-1.5 text-xs text-muted hover:border-accent hover:text-accent" aria-label={`${t.terminal.title}: ${app.name}`}>
        <span aria-hidden className="me-1 font-mono">&gt;_</span>{t.terminal.title}
      </Link>
    </li>
  )
}

export function NewApp({ projectId, onDone }: { projectId: string; onDone: () => void }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [source, setSource] = useState(sourceForm())
  const [port, setPort] = useState('80')
  const [envText, setEnvText] = useState('')
  const [envError, setEnvError] = useState('')
  const [limits, setLimits] = useState({ cpu: '0', memory: '0' })
  const t = useT()

  const create = useMutation({
    mutationFn: (environment: Record<string, string>) =>
      api<App>(`/projects/${projectId}/apps`, {
        method: 'POST',
        body: { name, port: Number(port), ...sourceInput(source), environment, cpu_limit: Number(limits.cpu) || 0, memory_limit_mb: Number(limits.memory) || 0 },
      }),
    onSuccess: (a) => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId, appId: a.id } })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    let environment: Record<string, string>
    try { environment = environmentMap(parseEnvironment(envText)) } catch { setEnvError(t.app.environmentInvalid); return }
    setEnvError('')
    create.mutate(environment)
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
        <Field
          label={t.common.name}
          name="name"
          required
          autoFocus
          dir="ltr"
          placeholder="web"
          pattern="[a-z0-9]([a-z0-9\-]{0,38}[a-z0-9])?"
          title={t.project.nameTitle}
          value={name}
          onChange={(e) => setName(e.target.value.toLowerCase())}
        />
        <Field label={t.project.containerPort} name="port" type="number" required min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} />
      </div>
      <SourceFields form={source} onChange={setSource} />
      <details className="group border border-line" open={!!envError || undefined}>
        <summary className="flex cursor-pointer items-center justify-between px-3 py-2.5 text-sm text-muted hover:text-fg">
          <span>{t.app.environment} <span className="text-xs">({t.common.optional})</span></span>
          <span aria-hidden className="text-xs transition-transform group-open:rotate-90">›</span>
        </summary>
        <div className="space-y-1.5 border-t border-line p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted">
            <label htmlFor="new-app-env" dir="ltr">.env</label>
            <HelpTip label={t.app.environment}>{t.app.environmentFormat}</HelpTip>
          </div>
          <textarea id="new-app-env" dir="ltr" spellCheck={false} value={envText} onChange={(e) => { setEnvText(e.target.value); setEnvError('') }}
            className="min-h-32 w-full resize-y border border-line bg-bg p-3 font-mono text-xs text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none"
            placeholder={'APP_ENV=production\nDATABASE_URL=${db.main.URL}'} />
          {envError && <ErrorNote>{envError}</ErrorNote>}
        </div>
      </details>
      <details className="group border border-line">
        <summary className="flex cursor-pointer items-center justify-between px-3 py-2.5 text-sm text-muted hover:text-fg">
          <span>{t.app.settingsSections.resources} <span className="text-xs">({t.common.optional})</span></span>
          <span aria-hidden className="text-xs transition-transform group-open:rotate-90">›</span>
        </summary>
        <div className="grid gap-4 border-t border-line p-3 sm:grid-cols-2">
          <Field label={t.app.cpuLimit} name="cpu" dir="ltr" type="number" min={0} max={256} step="0.05" hintTooltip hint={t.app.zeroUnlimited} value={limits.cpu} onChange={(e) => setLimits({ ...limits, cpu: e.target.value })} />
          <Field label={t.app.memoryLimit} name="memory" dir="ltr" type="number" min={0} step="1" hintTooltip hint={t.app.zeroUnlimited} value={limits.memory} onChange={(e) => setLimits({ ...limits, memory: e.target.value })} />
        </div>
      </details>
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t.common.creating : t.project.createApp}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </SheetActions>
    </form>
  )
}

function ProjectSettings({ project }: { project: Project }) {
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
      <section className="space-y-3 border-t border-danger/30 p-5">
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
      </section>
    </>
  )
}

type ProjectEnv = { environment: Record<string, string>; inherit: boolean }

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
      save={(environment) => api<ProjectEnv>(`/projects/${projectId}/environment`, { method: 'PUT', body: { environment, inherit: on } })}
      onSaved={() => {
        setInherit(null)
        queryClient.invalidateQueries({ queryKey: key })
      }}
      extraDirty={on !== env.data.inherit}
      extra={<CheckOption checked={on} onChange={setInherit} label={t.project.envInherit} hint={t.project.envInheritHint} />}
    />
  )
}

// ---- domains ----

type RouteSheet = { domain: Domain; route?: Route } | null

/** The domains this project uses, each with the routes that send its paths to apps.
 * Domains are added on the Domains page; here one is picked from those no project uses. */
function ProjectDomains({ projectId, apps }: { projectId: string; apps: App[] }) {
  const t = useT()
  const queryClient = useQueryClient()
  const domains = useQuery(domainsQuery(projectId))
  const all = useQuery(allDomainsQuery)
  const [adding, setAdding] = useState(false)
  const [routing, setRouting] = useState<RouteSheet>(null)
  const [picked, setPicked] = useState('')
  const free = (all.data ?? []).filter((d) => !d.project_id)
  const choice = free.find((d) => d.id === picked)
  const use = useMutation({
    mutationFn: (d: Domain) => withToast(() => api<Domain>(`/projects/${projectId}/domains`, { method: 'POST', body: { domain_id: d.id } }),
      { loading: t.project.usingToast(d.hostname), success: t.project.usedToast(d.hostname), error: t.project.useFailed }),
    onSuccess: () => setPicked(''),
    onSettled: () => { // a 503 still saved it
      queryClient.invalidateQueries({ queryKey: domainsQuery(projectId).queryKey })
      queryClient.invalidateQueries({ queryKey: allDomainsQuery.queryKey })
    },
  })
  return (
    <div className="space-y-6">
      <form onSubmit={(e: FormEvent) => { e.preventDefault(); if (choice) use.mutate(choice) }} className="flex flex-wrap items-end gap-3">
        <label className="block min-w-56 flex-1 space-y-1.5 text-xs text-muted sm:max-w-sm">
          {t.project.pickDomain}
          <select value={choice?.id ?? ''} onChange={(e) => setPicked(e.target.value)} disabled={free.length === 0 || use.isPending} dir="ltr"
            className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg disabled:opacity-60">
            <option value="">{free.length === 0 ? t.project.noFreeDomains : t.project.pickDomainPlaceholder}</option>
            {free.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
          </select>
        </label>
        <Button type="submit" disabled={!choice || use.isPending}>{use.isPending ? t.app.adding : t.project.useDomain}</Button>
        <Button type="button" variant="ghost" onClick={() => setAdding(true)}>{t.project.addDomain}</Button>
        <Link to="/domains" className="ms-auto self-center text-xs text-muted hover:text-fg">
          {t.project.manageDomains} <span aria-hidden className="inline-block rtl:-scale-x-100">→</span>
        </Link>
      </form>
      <QueryState loading={domains.isPending} error={domains.error} what={t.project.domainsTab} />
      {domains.data?.length === 0 && (
        <Panel>
          <Empty title={t.project.noDomains}>{t.project.noDomainsHint}</Empty>
        </Panel>
      )}
      {domains.data?.map((d) => <DomainCard key={d.id} domain={d} projectId={projectId} onRoute={(route) => setRouting({ domain: d, route })} />)}

      <Sheet open={adding} onOpenChange={setAdding} title={t.project.addDomain} description={t.app.noDomainsYetHint}>
        {adding && <AddDomain projectId={projectId} onDone={() => setAdding(false)} />}
      </Sheet>
      <Sheet open={routing !== null} onOpenChange={(o) => !o && setRouting(null)} title={routing?.route ? t.project.editRoute : t.project.addRoute}
        description={routing?.domain.hostname}>
        {routing && <RouteForm projectId={projectId} apps={apps} domain={routing.domain} route={routing.route} onDone={() => setRouting(null)} />}
      </Sheet>
    </div>
  )
}

function DomainCard({ domain, projectId, onRoute }: { domain: Domain; projectId: string; onRoute: (r?: Route) => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const dns = useQuery(dnsQuery(domain.id))
  const [confirming, setConfirming] = useState(false)
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: domainsQuery(projectId).queryKey })
    queryClient.invalidateQueries({ queryKey: allDomainsQuery.queryKey })
  }
  const httpOnly = domain.tls_mode === 'http_only'
  const host = domain.hostname
  const detach = useMutation({
    mutationFn: () => withToast(() => api<void>(`/projects/${projectId}/domains/${domain.id}`, { method: 'DELETE' }),
      { loading: t.project.detachingToast(host), success: t.project.detachedToast(host), error: t.project.detachFailed }),
    onSettled: () => { refresh(); queryClient.invalidateQueries({ queryKey: ['apps'] }) },
  })
  const setMode = useMutation({
    mutationFn: (tls_mode: string) => withToast(() => api<Domain>(`/domains/${domain.id}`, { method: 'PATCH', body: { tls_mode } }),
      { loading: t.domains.switching(host), success: t.domains.switched(host), error: t.domains.switchFailed }),
    onSettled: refresh,
  })
  const busy = detach.isPending || setMode.isPending
  return (
    <Panel>
      {/* The hostname in the body font: the pixel font of panel titles has no lowercase. */}
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-line px-4 py-3">
        <div className="min-w-0">
          <p className="flex flex-wrap items-center gap-2">
            <a href={hostHref(host, httpOnly)} target="_blank" rel="noreferrer" className="text-sm text-fg [overflow-wrap:anywhere] hover:text-accent" dir="ltr">
              {host}
            </a>
            {httpOnly && <Badge tone="muted">{t.common.httpOnly}</Badge>}
          </p>
          <p className="mt-1 text-xs text-muted"><DnsHintText dns={dns} /></p>
        </div>
        <ActionsMenu
          label={t.domains.actions}
          ariaLabel={t.domains.actionsFor(host)}
          disabled={busy}
          items={[
            { label: httpOnly ? t.app.useHttps : t.common.httpOnly, onSelect: () => setMode.mutate(httpOnly ? 'auto' : 'http_only') },
            { label: t.project.detachDomain, danger: true, onSelect: () => setConfirming(true) },
          ]}
        />
      </div>
      {domain.routes.length === 0 ? (
        <p className="px-4 py-3 text-sm text-muted">{t.project.noRoutes}</p>
      ) : (
        <ul className="divide-y divide-line">
          {domain.routes.map((r) => <RouteRow key={r.id} route={r} onEdit={() => onRoute(r)} onChanged={refresh} />)}
        </ul>
      )}
      <div className="border-t border-line px-4 py-3">
        <Button variant="ghost" onClick={() => onRoute()}>{t.project.addRoute}</Button>
      </div>
      <ConfirmModal
        open={confirming}
        onOpenChange={setConfirming}
        title={t.project.detachTitle}
        message={t.project.detachMessage(host, domain.routes.length)}
        confirm={t.common.yesRemove}
        onConfirm={() => detach.mutate()}
      />
    </Panel>
  )
}

function RouteRow({ route, onEdit, onChanged }: { route: Route; onEdit: () => void; onChanged: () => void }) {
  const t = useT()
  const remove = useMutation({ mutationFn: () => api<void>(`/routes/${route.id}`, { method: 'DELETE' }), onSettled: onChanged })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-center gap-3 text-sm">
        <code dir="ltr" className="min-w-16 text-fg">{route.path}</code>
        <span aria-hidden className="text-muted rtl:-scale-x-100">→</span>
        <span>{route.app_name}</span>
        {route.strip_prefix && <Badge tone="muted">{t.project.stripped}</Badge>}
      </div>
      <span className="flex items-center gap-2">
        <Button variant="ghost" onClick={onEdit}>{t.common.edit}</Button>
        <ConfirmButton label={t.project.removeRoute} confirm={t.common.yesRemove} pending={remove.isPending} onConfirm={() => remove.mutate()} />
      </span>
      {remove.error && <p className="w-full text-xs text-danger">{remove.error.message}</p>}
    </li>
  )
}

function RouteForm({ projectId, apps, domain, route, onDone }: { projectId: string; apps: App[]; domain: Domain; route?: Route; onDone: () => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const [path, setPath] = useState(route?.path ?? (domain.routes.some((r) => r.path === '/') ? '/api' : '/'))
  const [appId, setAppId] = useState(route?.app_id ?? apps[0]?.id ?? '')
  const [strip, setStrip] = useState(route?.strip_prefix ?? false)
  const save = useMutation({
    mutationFn: () => {
      const body = { path, app_id: appId, strip_prefix: strip }
      return route
        ? api<Route>(`/routes/${route.id}`, { method: 'PATCH', body })
        : api<Route>(`/domains/${domain.id}/routes`, { method: 'POST', body })
    },
    onSuccess: onDone,
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: domainsQuery(projectId).queryKey })
      queryClient.invalidateQueries({ queryKey: ['apps'] })
    },
  })
  if (apps.length === 0) return <p className="p-5 text-sm text-muted">{t.project.noApps}</p>
  const root = path.trim() === '/' || path.trim() === ''
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); save.mutate() }} className="space-y-5 p-5">
      <Field label={t.project.routePath} name="route-path" required dir="ltr" placeholder="/api" hint={t.project.routePathHint}
        value={path} onChange={(e) => setPath(e.target.value)} autoComplete="off" spellCheck={false} />
      <label className="block space-y-1.5 text-xs text-muted">
        {t.project.routeApp}
        <select value={appId} onChange={(e) => setAppId(e.target.value)} className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg">
          {apps.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
        </select>
      </label>
      {!root && <CheckOption checked={strip} onChange={setStrip} label={t.project.strip} hint={t.project.stripHint} />}
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={save.isPending || !appId}>{save.isPending ? t.common.saving : route ? t.common.save : t.project.addRoute}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}

/** The domain that stands for the project: the one serving "/" (its front door), else the
 * first one. */
function mainDomain(list: Domain[] | undefined): Domain | undefined {
  const exact = list?.filter((d) => !isWildcard(d.hostname))
  return exact?.find((d) => d.routes.some((r) => r.path === '/')) ?? exact?.[0] ?? list?.[0]
}

function DomainLink({ domain }: { domain: Domain }) {
  const url = hostHref(domain.hostname, domain.tls_mode === 'http_only')
  return (
    <a href={url} target="_blank" rel="noopener noreferrer" className="mt-1 inline-flex max-w-full items-center gap-1.5 text-accent hover:underline" dir="ltr">
      <span className="[overflow-wrap:anywhere]">{domain.hostname}</span>
      {url && <span aria-hidden="true">↗</span>}
    </a>
  )
}
