import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { SourceFields, sourceForm, sourceInput } from '../components/source'
import { Badge, Button, ConfirmButton, cx, Empty, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, appQuery, appsQuery, projectQuery, projectsQuery, type App, type Project } from '../lib/api'
import { appSource, appStatus } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet } from '../components/sheet'

export const projectTabs = ['apps', 'settings'] as const
export type ProjectTab = (typeof projectTabs)[number]

export function ProjectPage() {
  const { projectId } = useParams({ strict: false }) as { projectId: string }
  const project = useQuery(projectQuery(projectId))
  const apps = useQuery(appsQuery(projectId))
  const [adding, setAdding] = useState(false)
  const search = useSearch({ strict: false }) as { tab?: string }
  const tab: ProjectTab = projectTabs.includes(search.tab as ProjectTab) ? (search.tab as ProjectTab) : 'apps'
  const t = useT()

  if (!project.data) {
    return (
      <div className="space-y-4">
        <Crumbs />
        <QueryState loading={project.isPending} error={project.error} what={t.project.what} />
      </div>
    )
  }
  const p = project.data

  return (
    <div className="space-y-6">
      <PageHeader
        crumbs={<Crumbs />}
        title={p.name}
        subtitle={p.description || undefined}
        actions={tab === 'apps' && <Button onClick={() => setAdding(true)}>{t.project.newApp}</Button>}
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
            {name === 'apps' ? t.project.apps : t.common.settings}
            {name === 'apps' && apps.data && <span className="ms-1.5 text-xs text-muted">{apps.data.length}</span>}
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
      {tab === 'settings' && (
        <Panel title={t.project.settingsTitle}>
          <ProjectSettings key={p.updated_at} project={p} />
        </Panel>
      )}

      <Sheet open={adding} onOpenChange={setAdding} title={t.project.newApp} description={t.project.portHint}>
        {adding && <NewApp projectId={p.id} onDone={() => setAdding(false)} />}
      </Sheet>
    </div>
  )
}

function Crumbs() {
  const t = useT()
  return (
    <Link to="/projects" className="hover:text-fg">
      {t.project.back}
    </Link>
  )
}

/** One app in the list. It asks for the app on its own to learn what the container is doing. */
function AppRow({ app }: { app: App }) {
  const live = useQuery({ ...appQuery(app.id), refetchInterval: 10_000 })
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

function NewApp({ projectId, onDone }: { projectId: string; onDone: () => void }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [source, setSource] = useState(sourceForm())
  const [port, setPort] = useState('80')
  const t = useT()

  const create = useMutation({
    mutationFn: () =>
      api<App>(`/projects/${projectId}/apps`, { method: 'POST', body: { name, port: Number(port), ...sourceInput(source) } }),
    onSuccess: (a) => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId, appId: a.id } })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate()
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
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <div className="flex gap-2">
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t.common.creating : t.project.createApp}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </div>
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
