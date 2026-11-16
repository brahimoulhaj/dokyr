import { ResourceMetrics } from '../components/resource-metrics'
import { Tooltip } from '@base-ui/react/tooltip'
import { MountSettings } from '../components/mounts'
import { EnvironmentSettings } from '../components/environment'
import { keepPreviousData, useMutation, useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { lazy, Suspense, useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { DeployHookPanel } from '../components/deploy-hook'
import { DeploymentTimeline } from '../components/deployment-timeline'
import { HealthCheckPanel } from '../components/health-check'
import { LogViewer } from '../components/logviewer'
import { Badge, Button, CheckOption, ConfirmButton, cx, Empty, ErrorNote, Field, HelpTip, PageHeader, Panel, QueryState } from '../components/ui'
import {
  addDeployment,
  api,
  appQuery,
  appsQuery,
  deploymentsKey,
  deploymentsPerPage,
  deploymentsQuery,
  appRoutesQuery,
  isActive,
  projectQuery,
  registrySettingsQuery,
  type App,
  type AppInput,
  type Deployment,
  type Page,
} from '../lib/api'
import { SourceFields, sourceChanged, sourceForm, sourceInput } from '../components/source'
import { rich, useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { appSource, appStatus, commitSubject, dateTime, deploymentVersion, duration, repoName, timeAgo, type Tone } from '../lib/format'
import { ContextBreadcrumb } from '../components/context-breadcrumb'

const ContainerTerminal = lazy(() => import('../components/container-terminal'))
export const appTabs = ['overview', 'deployments', 'logs', 'metrics', 'terminal', 'settings'] as const
export type AppTab = (typeof appTabs)[number]

export function AppPage() {
  const t = useT()
  const { projectId, appId } = useParams({ strict: false }) as { projectId: string; appId: string }
  const raw = (useSearch({ strict: false }) as { tab?: string }).tab
  const tab: AppTab = appTabs.includes(raw as AppTab) ? (raw as AppTab) : 'overview'
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const project = useQuery(projectQuery(projectId))
  const routes = useQuery(appRoutesQuery(appId))
  const mainUrl = routes.data?.[0]?.url
  const deployments = useQuery({
    ...deploymentsQuery(appId),
    refetchInterval: (q) => (q.state.data?.items.some(isActive) ? 1500 : false),
  })
  const latest = deployments.data?.items[0]
  const deploying = !!latest && isActive(latest)
  const app = useQuery({ ...appQuery(appId), refetchInterval: deploying ? 2000 : 10_000 })

  // A deployment that just finished changes the app (current deployment, state).
  useEffect(() => {
    queryClient.invalidateQueries({ queryKey: appQuery(appId).queryKey, exact: true })
  }, [latest?.id, latest?.status, appId, queryClient])

  // The deployment opened in the list: undefined = the latest (it follows new ones), null = none.
  const [selected, setSelected] = useState<string | null | undefined>(undefined)
  const [page, setPage] = useState(1)
  const [terminalAppId, setTerminalAppId] = useState<string | null>(null)
  useEffect(() => {
    if (tab === 'terminal') setTerminalAppId(appId)
  }, [tab, appId])

  const goto = (t: AppTab) =>
    navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId, appId }, search: { tab: t } })

  const deploy = useMutation({
    mutationFn: () => api<Deployment>(`/apps/${appId}/deploy`, { method: 'POST' }),
    onSuccess: (d) => {
      queryClient.setQueryData(deploymentsQuery(appId).queryKey, addDeployment(d))
      setSelected(undefined)
      setPage(1)
      goto('deployments')
    },
  })
  const action = useMutation({
    mutationFn: (name: 'start' | 'stop' | 'restart') => {
      const label = app.data?.name ?? ''
      const m = t.app.actionToast[name]
      return withToast(() => api<App>(`/apps/${appId}/${name}`, { method: 'POST' }), {
        loading: m.run(label),
        success: m.done(label),
        error: m.fail(label),
      })
    },
    onSuccess: (a) => queryClient.setQueryData(appQuery(appId).queryKey, a),
  })

  const crumbs = <ContextBreadcrumb projectId={projectId} projectName={project.data?.name} appId={appId} appName={app.data?.name} appTab={tab} />

  if (!app.data) {
    return (
      <div className="space-y-4">
        {crumbs}
        <QueryState loading={app.isPending} error={app.error} what={t.app.whatApp} />
      </div>
    )
  }
  const a = app.data
  const status = appStatus(a, deploying)
  const deployed = !!a.current_deployment_id
  const busy = deploying || deploy.isPending || action.isPending
  const actionError = deploy.error

  return (
    <div className="space-y-6">
      <PageHeader
        crumbs={crumbs}
        title={
          <>
            {a.name} <Badge tone={status.tone}>{status.label}</Badge>
          </>
        }
        subtitle={
          <>
            <span className="block">{appSource(a)}</span>
            {mainUrl && (
              <a
                href={mainUrl}
                target="_blank"
                rel="noopener noreferrer"
                title={t.app.primaryDomain}
                className="mt-1 inline-flex max-w-full items-center gap-1.5 text-accent hover:underline"
                dir="ltr"
              >
                <span className="[overflow-wrap:anywhere]">{mainUrl.replace(/^https?:\/\//, '')}</span>
                <span aria-hidden="true">↗</span>
              </a>
            )}
          </>
        }
        actions={
          <>
            {deployed && a.desired_state === 'running' && (
              <>
                <Button variant="ghost" disabled={busy} onClick={() => action.mutate('restart')}>
                  {t.app.restart}
                </Button>
                <Button variant="ghost" disabled={busy} onClick={() => action.mutate('stop')}>
                  {t.app.stop}
                </Button>
              </>
            )}
            {deployed && a.desired_state === 'stopped' && (
              <Button variant="ghost" disabled={busy} onClick={() => action.mutate('start')}>
                {t.app.start}
              </Button>
            )}
            <Button disabled={busy} onClick={() => deploy.mutate()}>
              {deploying ? t.app.deploying : deployed ? t.app.redeploy : t.app.deploy}
            </Button>
          </>
        }
      />

      {actionError && <ErrorNote>{actionError.message}</ErrorNote>}

      <nav className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line" aria-label={t.app.sections}>
        {appTabs.map((name) => (
          <Link
            key={name}
            to="/projects/$projectId/apps/$appId"
            params={{ projectId, appId }}
            search={{ tab: name }}
            className={cx(
              'border-b-2 px-3 py-1.5 text-sm whitespace-nowrap capitalize',
              name === tab ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg',
            )}
            aria-current={name === tab ? 'page' : undefined}
          >
            {name === 'terminal' ? t.terminal.title : t.app.tabs[name]}
          </Link>
        ))}
      </nav>

      {tab === 'overview' && <Overview app={a} latest={latest} onOpenDeployments={() => goto('deployments')} />}
      {tab === 'deployments' && (
        <Deployments
          appId={a.id}
          first={deployments}
          page={page}
          onPage={setPage}
          currentId={a.current_deployment_id}
          open={selected === undefined ? (latest?.id ?? null) : selected}
          onOpen={setSelected}
          onRolledBack={(id) => {
            setPage(1)
            setSelected(id)
          }}
          git={a.source_type === 'git'}
        />
      )}
      {tab === 'logs' && <RuntimeLogs app={a} />}
      {tab === 'metrics' && <ResourceMetrics kind="apps" id={a.id} />}
      {(tab === 'terminal' || terminalAppId === a.id) && (
        <div hidden={tab !== 'terminal'}>
          <Suspense fallback={<p className="p-4 text-sm text-muted">{t.terminal.connecting}</p>}>
            <ContainerTerminal key={a.id} app={a} visible={tab === 'terminal'} />
          </Suspense>
        </div>
      )}
      {tab === 'settings' && <Settings app={a} />}
    </div>
  )
}

// ---- overview ----

function Overview({ app, latest, onOpenDeployments }: { app: App; latest?: Deployment; onOpenDeployments: () => void }) {
  const t = useT()
  const routes = useQuery(appRoutesQuery(app.id))
  const current = useQuery({
    queryKey: ['deployments', app.current_deployment_id] as const,
    queryFn: () => api<Deployment>(`/deployments/${app.current_deployment_id}`),
    enabled: !!app.current_deployment_id,
  }).data
  // A Git deployment does not record the settings it was built with; only the port is compared.
  const pending = current && ((app.source_type === 'image' && current.image !== app.image) || current.port !== app.port)

  return (
    <div className="space-y-6">
      {!app.current_deployment_id && !(latest && isActive(latest)) && (
        <Panel>
          <Empty title={t.app.neverDeployed}>
            {app.source_type === 'git'
              ? rich(t.app.pressDeployGit, {
                  repo: () => (
                    <span className="text-fg" dir="ltr">
                      {repoName(app.git_repository)}
                    </span>
                  ),
                  branch: () => (
                    <span className="text-fg" dir="ltr">
                      {app.git_branch}
                    </span>
                  ),
                })
              : rich(t.app.pressDeployImage, {
                  image: () => (
                    <span className="text-fg" dir="ltr">
                      {app.image}
                    </span>
                  ),
                })}
            {latest?.status === 'failed' && ` ${t.app.lastAttemptFailed}`}
          </Empty>
        </Panel>
      )}
      {pending && (
        <p className="border border-warn/60 px-3 py-2 text-xs text-warn">
          {t.app.settingsChanged(deploymentVersion(current), current.port)}
        </p>
      )}
      {latest?.status === 'failed' && app.current_deployment_id && (
        <p className="border border-danger/60 px-3 py-2 text-xs text-danger">
          {rich(t.app.deployFailed, {
            err: () => latest.error,
            a: (c) => (
              <button className="underline" onClick={onOpenDeployments}>
                {c}
              </button>
            ),
          })}
        </p>
      )}
      <Panel title={t.app.summary}>
        <dl className="grid gap-px bg-line sm:grid-cols-2">
          {app.source_type === 'git' ? (
            <>
              <Fact label={t.app.repository}>
                <a href={app.git_repository} target="_blank" rel="noreferrer" className="text-accent hover:underline" dir="ltr">
                  {repoName(app.git_repository)}
                </a>{' '}
                · <span dir="ltr">{app.git_branch}</span>
              </Fact>
              <Fact label={t.source.builder}>
                {app.builder === 'railpack' ? 'Railpack' : app.builder === 'auto' ? t.source.builderAuto : 'Dockerfile'}
                {app.builder !== 'railpack' && (
                  <>
                    {' · '}
                    <span dir="ltr">{[app.build_context, app.dockerfile_path].filter(Boolean).join('/')}</span>
                  </>
                )}
              </Fact>
            </>
          ) : (
            <Fact label={t.app.image}>
              <span dir="ltr">{app.image}</span>
            </Fact>
          )}
          {app.start_command && (
            <Fact label={t.app.startCommand}>
              <code dir="ltr" className="[overflow-wrap:anywhere]">{app.start_command}</code>
            </Fact>
          )}
          <Fact label={t.app.containerPort}>{app.port}</Fact>
          <Fact label={t.app.strategy}>
            <span title={app.strategy === 'recreate' ? t.app.recreateNote : undefined}>
              {app.strategy === 'recreate' ? t.app.strategyRecreate : t.app.strategyRolling}
            </span>
          </Fact>
          <Fact label={t.app.limits}>
            {app.cpu_limit > 0 ? t.app.cpuValue(app.cpu_limit) : t.app.cpuUnlimited} ·{' '}
            {app.memory_limit_mb > 0 ? t.app.memoryValue(app.memory_limit_mb) : t.app.memoryUnlimited}
          </Fact>
          <Fact label={t.app.container}>{app.runtime_state === 'none' ? '—' : app.runtime_state}</Fact>
          <Fact label={t.app.runningVersion}>
            {current ? (
              <span title={dateTime(current.finished_at)}>
                {t.app.deployedAgo(deploymentVersion(current), timeAgo(current.finished_at))}
              </span>
            ) : (
              '—'
            )}
          </Fact>
          <Fact label={t.app.tabs.domains}>
            {routes.data?.length ? (
              <span className="flex flex-col items-start">
                {routes.data.map((r) => (
                  <a key={r.id} href={r.url} target="_blank" rel="noopener noreferrer" className="text-accent hover:underline [overflow-wrap:anywhere]" dir="ltr">
                    {r.url.replace(/^https?:\/\//, '')}
                  </a>
                ))}
              </span>
            ) : (
              <span className="text-muted">{t.app.noDomains}</span>
            )}
            <Link to="/projects/$projectId" params={{ projectId: app.project_id }} search={{ tab: 'domains' }} className="mt-1 block text-xs text-muted hover:text-fg">
              {t.app.manageDomains}
            </Link>
          </Fact>
        </dl>
      </Panel>
    </div>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="bg-surface px-4 py-3">
      <dt className="text-xs text-muted">{label}</dt>
      <dd className="mt-0.5 text-sm [overflow-wrap:anywhere]">{children}</dd>
    </div>
  )
}

// ---- deployments ----

const deployTone: Record<Deployment['status'], Tone> = {
  queued: 'muted',
  running: 'warn',
  succeeded: 'accent',
  failed: 'danger',
  cancelled: 'muted',
  rolled_back: 'warn',
}

function Deployments(props: {
  appId: string
  /** Page 1, which the page header polls already. */
  first: UseQueryResult<Page<Deployment>>
  page: number
  onPage: (page: number) => void
  currentId: string
  /** The deployment whose timeline and log are open; null = none. */
  open: string | null
  onOpen: (id: string | null) => void
  onRolledBack: (id: string) => void
  git: boolean
}) {
  const t = useT()
  const other = useQuery({
    ...deploymentsQuery(props.appId, props.page),
    enabled: props.page > 1,
    refetchInterval: (q) => (q.state.data?.items.some(isActive) ? 1500 : false),
    placeholderData: keepPreviousData,
  })
  const q = props.page > 1 ? other : props.first
  const list = q.data?.items
  const total = props.first.data?.total ?? q.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / deploymentsPerPage))
  // Only the first page can hold a running deployment; it alone decides "busy".
  const busy = !!props.first.data?.items.some(isActive)

  return (
    <Panel title={t.app.tabs.deployments}>
      <QueryState loading={q.isPending} error={q.error} what={t.app.whatDeployments} />
      {list &&
        (list.length === 0 && props.page === 1 ? (
          <Empty title={t.app.noDeployments}>{t.app.noDeploymentsHint}</Empty>
        ) : (
          <ul className="divide-y divide-line">
            {list.map((d) => {
              const open = d.id === props.open
              return (
                <li key={d.id}>
                  <button
                    type="button"
                    onClick={() => props.onOpen(open ? null : d.id)}
                    aria-expanded={open}
                    className={cx(
                      'flex w-full flex-wrap items-center gap-x-4 gap-y-1 px-4 py-2.5 text-start hover:bg-raised',
                      open && 'bg-raised',
                    )}
                  >
                    <span className="w-24 shrink-0">
                      <Badge tone={deployTone[d.status]}>{t.app.deployStatus[d.status]}</Badge>
                    </span>
                    <span className="min-w-0 flex-1 truncate text-sm">
                      <DeploymentLabel d={d} />
                      {d.status === 'running' && d.phase && <span className="ms-2 text-xs text-warn">{t.app.phase[d.phase]}…</span>}
                      {d.rollback_of && <span className="ms-2 text-xs text-muted">↩ {t.app.rollbackOf}</span>}
                      {d.id === props.currentId && <span className="ms-2 text-xs text-accent">{t.app.live}</span>}
                    </span>
                    <span className="shrink-0 text-xs text-muted" title={dateTime(d.created_at)}>
                      {t.app.took(timeAgo(d.created_at), duration(d.started_at, isActive(d) ? null : d.finished_at))}
                    </span>
                    <span aria-hidden className={cx('shrink-0 text-xs text-muted transition-transform', open && 'rotate-90')}>
                      ›
                    </span>
                  </button>
                  {open && <DeploymentDetails deployment={d} busy={busy} git={props.git} onRolledBack={props.onRolledBack} />}
                </li>
              )
            })}
          </ul>
        ))}
      {pages > 1 && (
        <nav className="flex items-center justify-between gap-2 border-t border-line px-4 py-2 text-xs text-muted" aria-label={t.app.pagination}>
          <Button variant="ghost" className="h-7 px-3" disabled={props.page <= 1} onClick={() => props.onPage(props.page - 1)}>
            {t.app.newer}
          </Button>
          <span className="tabular-nums">{t.app.pageOf(props.page, pages)}</span>
          <Button variant="ghost" className="h-7 px-3" disabled={props.page >= pages} onClick={() => props.onPage(props.page + 1)}>
            {t.app.older}
          </Button>
        </nav>
      )}
    </Panel>
  )
}

/** DeploymentLabel names a deployment in the list: a Git commit by the first line of its
 * message (the whole message in a tooltip), anything else by its version. */
function DeploymentLabel({ d }: { d: Deployment }) {
  const subject = commitSubject(d.commit_message)
  if (!subject) return <>{deploymentVersion(d)}</>
  return (
    <Tooltip.Provider delay={300}>
      <Tooltip.Root>
        <Tooltip.Trigger render={<span />}>
          {subject}
          <span className="ms-2 font-mono text-xs text-muted" dir="ltr">
            {d.commit_sha.slice(0, 7)}
          </span>
        </Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Positioner side="bottom" align="start" sideOffset={6} className="z-50">
            <Tooltip.Popup className="max-h-80 max-w-[min(32rem,calc(100vw-2rem))] overflow-auto border border-line bg-raised px-3 py-2 text-xs leading-relaxed whitespace-pre-wrap text-fg shadow-sm [overflow-wrap:anywhere]">
              {d.commit_message}
              <span className="mt-2 block font-mono text-muted" dir="ltr">
                {d.commit_sha}
              </span>
            </Tooltip.Popup>
          </Tooltip.Positioner>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  )
}

/** DeploymentDetails is what opens under a deployment's row: its actions, timeline and log. */
function DeploymentDetails(props: { deployment: Deployment; busy: boolean; git: boolean; onRolledBack: (id: string) => void }) {
  const { deployment } = props
  const t = useT()
  const queryClient = useQueryClient()
  const all = deploymentsKey(deployment.app_id)
  const cancel = useMutation({
    mutationFn: () => api<Deployment>(`/deployments/${deployment.id}/cancel`, { method: 'POST' }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: all }),
  })
  // The rollback is a new deployment: it goes on top of the first page and opens.
  const rollback = useMutation({
    mutationFn: () => api<Deployment>(`/deployments/${deployment.id}/rollback`, { method: 'POST' }),
    onSuccess: (d) => {
      queryClient.setQueryData(deploymentsQuery(deployment.app_id).queryKey, addDeployment(d))
      props.onRolledBack(d.id)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: all }),
  })
  const actions = isActive(deployment) || deployment.can_roll_back
  return (
    <div className="border-t border-line bg-bg">
      {actions && (
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-line px-4 py-2">
          <p className="min-w-0 flex-1 text-xs text-muted">
            {deployment.can_roll_back && (
              <>
                {t.app.rollBackHint}
                {props.git && ` ${t.app.rollBackGitNote}`}
              </>
            )}
          </p>
          <div className="flex items-center gap-2">
            {isActive(deployment) && (
              <Button
                variant="ghost"
                type="button"
                title={t.app.cancelDeploymentHint}
                disabled={cancel.isPending}
                onClick={() => cancel.mutate()}
              >
                {t.app.cancelDeployment}
              </Button>
            )}
            {deployment.can_roll_back && (
              <ConfirmButton
                variant="ghost"
                label={t.app.rollBack}
                confirm={t.app.rollBackConfirm}
                disabled={props.busy}
                pending={rollback.isPending}
                onConfirm={() => rollback.mutate()}
              />
            )}
          </div>
        </div>
      )}
      {cancel.error && <p className="border-b border-line px-4 py-2 text-xs text-danger">{cancel.error.message}</p>}
      {rollback.error && <p className="border-b border-line px-4 py-2 text-xs text-danger">{rollback.error.message}</p>}
      {deployment.error && (
        <p className={cx('border-b border-line px-4 py-2 text-xs', deployment.status === 'cancelled' ? 'text-muted' : 'text-danger')}>
          {deployment.error}
        </p>
      )}
      <DeploymentTimeline deployment={deployment} />
      {/* The key reopens the stream when another deployment is opened. */}
      <LogViewer key={deployment.id} url={`/api/deployments/${deployment.id}/log/stream`} />
    </div>
  )
}

// ---- runtime logs ----

function RuntimeLogs({ app }: { app: App }) {
  const t = useT()
  if (!app.current_deployment_id) {
    return (
      <Panel>
        <Empty title={t.app.noContainer}>{t.app.noContainerHint}</Empty>
      </Panel>
    )
  }
  // A new deployment, or a stopped container starting again, is a new stream.
  const run = `${app.current_deployment_id}:${app.runtime_state === 'running'}`
  return (
    <Panel title={t.app.containerOutput}>
      <LogViewer
        key={run}
        url={`/api/apps/${app.id}/logs/stream`}
        refused={t.app.containerGone}
      />
    </Panel>
  )
}

// ---- settings ----

export const settingsSections = ['general', 'source', 'resources', 'environment', 'volumes', 'health', 'webhook', 'danger'] as const
export type SettingsSection = (typeof settingsSections)[number]

/** App settings, grouped by family: a vertical menu (a strip on phones) and one section. */
function Settings({ app }: { app: App }) {
  const t = useT()
  const raw = (useSearch({ strict: false }) as { section?: string }).section
  const section: SettingsSection = settingsSections.includes(raw as SettingsSection) ? (raw as SettingsSection) : 'general'
  return (
    <div className="flex flex-col gap-6 md:flex-row md:items-start">
      <nav
        aria-label={t.app.settingsSections.label}
        className="flex shrink-0 gap-1 overflow-x-auto border-b border-line md:sticky md:top-0 md:w-48 md:flex-col md:overflow-visible md:border-b-0"
      >
        {settingsSections.map((name) => (
          <Link
            key={name}
            to="/projects/$projectId/apps/$appId"
            params={{ projectId: app.project_id, appId: app.id }}
            search={{ tab: 'settings', section: name }}
            aria-current={name === section ? 'page' : undefined}
            className={cx(
              'border-b-2 px-3 py-1.5 text-start text-sm whitespace-nowrap md:border-b-0 md:border-s-2',
              name === section ? 'border-accent text-fg md:bg-raised' : 'border-transparent text-muted hover:text-fg',
              name === 'danger' && name !== section && 'hover:text-danger',
            )}
          >
            {t.app.settingsSections[name]}
          </Link>
        ))}
      </nav>
      <div className="min-w-0 flex-1 space-y-6">
        {section === 'general' && <GeneralSettings key={app.id} app={app} />}
        {section === 'source' && <SourceSettings key={app.id} app={app} />}
        {section === 'resources' && <ResourceSettings key={app.id} app={app} />}
        {section === 'environment' && <EnvironmentSettings key={app.id} app={app} />}
        {section === 'volumes' && <MountSettings key={`mounts-${app.id}`} app={app} />}
        {section === 'health' && <HealthCheckPanel app={app} />}
        {section === 'webhook' && <DeployHookPanel app={app} />}
        {section === 'danger' && <DangerZone app={app} />}
      </div>
    </div>
  )
}

/** Saves part of an app's settings: the API keeps every field the body leaves out. */
function useSaveApp(app: App) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: Partial<AppInput>) => api<App>(`/apps/${app.id}`, { method: 'PATCH', body }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: appQuery(app.id).queryKey, exact: true })
      queryClient.invalidateQueries({ queryKey: appsQuery(app.project_id).queryKey })
    },
  })
}

/** A section's panel and form: the title says it applies on the next deploy. */
function SectionForm(props: {
  title: string
  save: ReturnType<typeof useSaveApp>
  dirty: boolean
  onSubmit: () => void
  children: ReactNode
}) {
  const t = useT()
  function submit(e: FormEvent) {
    e.preventDefault()
    props.onSubmit()
  }
  return (
    <Panel title={props.title} actions={<HelpTip label={props.title}>{t.app.appliesNextDeploy}</HelpTip>}>
      <form onSubmit={submit} className="space-y-4 p-4">
        {props.children}
        {props.save.error && <ErrorNote>{props.save.error.message}</ErrorNote>}
        {props.save.isSuccess && !props.dirty && <p className="text-xs text-accent">{t.common.saved}</p>}
        <Button type="submit" disabled={!props.dirty || props.save.isPending}>
          {props.save.isPending ? t.common.saving : t.app.saveSettings}
        </Button>
      </form>
    </Panel>
  )
}

function GeneralSettings({ app }: { app: App }) {
  const t = useT()
  const save = useSaveApp(app)
  const [form, setForm] = useState({ name: app.name, port: String(app.port), start: app.start_command })
  const set = (key: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value })
  const body = { name: form.name, port: Number(form.port), start_command: form.start.trim() }
  const dirty = body.name !== app.name || body.port !== app.port || body.start_command !== app.start_command
  return (
    <SectionForm title={t.app.settingsSections.general} save={save} dirty={dirty} onSubmit={() => save.mutate(body)}>
      <Field
        label={t.common.name}
        name="name"
        required
        pattern="[a-z0-9]([a-z0-9\-]{0,38}[a-z0-9])?"
        title={t.app.nameRule}
        dir="ltr"
        value={form.name}
        onChange={set('name')}
      />
      <div className="max-w-48">
        <Field label={t.app.containerPort} name="port" dir="ltr" type="number" required min={1} max={65535} value={form.port} onChange={set('port')} />
      </div>
      <Field
        label={t.app.startCommand}
        name="start_command"
        dir="ltr"
        className="font-mono"
        maxLength={4096}
        placeholder={t.app.startCommandDefault}
        hint={t.app.startCommandHint}
        value={form.start}
        onChange={set('start')}
      />
    </SectionForm>
  )
}

function SourceSettings({ app }: { app: App }) {
  const t = useT()
  const save = useSaveApp(app)
  const [source, setSource] = useState(sourceForm(app))
  return (
    <SectionForm
      title={t.app.settingsSections.source}
      save={save}
      dirty={sourceChanged(source, app)}
      onSubmit={() => save.mutate(sourceInput(source))}
    >
      <SourceFields form={source} onChange={setSource} />
      <PushDeploy app={app} />
    </SectionForm>
  )
}

/** Deploy on push, for an image app on the built-in registry: saved at once. */
function PushDeploy({ app }: { app: App }) {
  const t = useT()
  const queryClient = useQueryClient()
  const builtin = useQuery({ ...registrySettingsQuery, retry: false })
  const set = useMutation({
    mutationFn: (on: boolean) => api<App>(`/apps/${app.id}/push-deploy`, { method: 'PUT', body: { on } }),
    onSuccess: (a) => queryClient.setQueryData(appQuery(app.id).queryKey, (old: App | undefined) => (old ? { ...old, push_deploy: a.push_deploy } : old)),
  })
  const domain = builtin.data?.enabled ? builtin.data.domain : ''
  if (app.source_type !== 'image' || !domain || !app.image.startsWith(domain + '/')) return null
  return (
    <div className="space-y-1">
      <CheckOption checked={app.push_deploy} disabled={set.isPending} onChange={(on) => set.mutate(on)} label={t.app.pushDeploy} hint={t.app.pushDeployHint} />
      {set.error && <ErrorNote>{set.error.message}</ErrorNote>}
    </div>
  )
}

function ResourceSettings({ app }: { app: App }) {
  const t = useT()
  const save = useSaveApp(app)
  const [form, setForm] = useState({ cpu: String(app.cpu_limit), memory: String(app.memory_limit_mb) })
  const set = (key: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value })
  const body = { cpu_limit: Number(form.cpu), memory_limit_mb: Number(form.memory) }
  const dirty = body.cpu_limit !== app.cpu_limit || body.memory_limit_mb !== app.memory_limit_mb
  return (
    <SectionForm title={t.app.settingsSections.resources} save={save} dirty={dirty} onSubmit={() => save.mutate(body)}>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t.app.cpuLimit} name="cpu" dir="ltr" type="number" min={0} max={256} step="0.05" hintTooltip hint={t.app.zeroUnlimited} value={form.cpu} onChange={set('cpu')} />
        <Field label={t.app.memoryLimit} name="memory" dir="ltr" type="number" min={0} step="1" hintTooltip hint={t.app.zeroUnlimited} value={form.memory} onChange={set('memory')} />
      </div>
    </SectionForm>
  )
}

function DangerZone({ app }: { app: App }) {
  const t = useT()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const remove = useMutation({
    mutationFn: () => api<void>(`/apps/${app.id}`, { method: 'DELETE' }),
    onSuccess: () => {
      queryClient.removeQueries({ queryKey: appQuery(app.id).queryKey })
      queryClient.invalidateQueries({ queryKey: projectQuery(app.project_id).queryKey })
      navigate({ to: '/projects/$projectId', params: { projectId: app.project_id } })
    },
  })
  return (
    <Panel title={t.app.deleteApp}>
      <div className="flex flex-wrap items-center justify-between gap-3 p-4">
        <p className="max-w-md text-xs text-muted">{t.app.deleteAppHint}</p>
        <ConfirmButton label={t.app.deleteApp} confirm={t.common.yesDelete} pending={remove.isPending} onConfirm={() => remove.mutate()} />
      </div>
      {remove.error && (
        <div className="px-4 pb-4">
          <ErrorNote>{remove.error.message}</ErrorNote>
        </div>
      )}
    </Panel>
  )
}
