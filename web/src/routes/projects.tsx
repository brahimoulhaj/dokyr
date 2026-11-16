import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { ListToolbar, Pager, SearchBox, ToolbarSelect, fold, usePages } from '../components/list-controls'
import { Button, cx, Empty, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, appProblemsQuery, projectsQuery, type Project } from '../lib/api'
import { timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet } from '../components/sheet'

export function ProjectsPage() {
  const t = useT()
  const projects = useQuery(projectsQuery)
  const [creating, setCreating] = useState(false)

  return (
    <div className="space-y-6">
      <PageHeader
        title={t.projects.title}
        subtitle={t.projects.subtitle}
        actions={<Button onClick={() => setCreating(true)}>{t.projects.newProject}</Button>}
      />
      <Sheet open={creating} onOpenChange={setCreating} title={t.projects.newProject}>
        {creating && <NewProject onDone={() => setCreating(false)} />}
      </Sheet>
      <QueryState loading={projects.isPending} error={projects.error} what={t.projects.what} />
      {projects.data && (projects.data.length === 0
        ? <Panel><Empty title={t.projects.emptyTitle}>{t.projects.emptyHint}</Empty></Panel>
        : <ProjectList projects={projects.data} />)}
    </div>
  )
}

const failedDeploy = (s?: string) => s === 'failed' || s === 'rolled_back'

function ProjectList({ projects }: { projects: Project[] }) {
  const t = useT()
  const tl = t.lists
  const problems = useQuery(appProblemsQuery)
  const [q, setQ] = useState('')
  const [status, setStatus] = useState('all')
  const [sort, setSort] = useState('name')
  const down = (id: string) => problems.data?.filter((p) => p.project_id === id).length ?? 0
  const attention = (p: Project) => down(p.id) > 0 || failedDeploy(p.last_deploy_status)
  const list = useMemo(() => {
    const needle = fold(q.trim())
    const out = projects.filter((p) => (!needle || fold(`${p.name} ${p.description}`).includes(needle))
      && (status === 'all' || (status === 'empty' ? p.app_count === 0 : status === 'attention' ? attention(p) : p.app_count > 0 && !attention(p))))
    const time = (v?: string | null) => (v ? Date.parse(v) : 0)
    return out.sort((a, b) => sort === 'deploy' ? time(b.last_deploy_at) - time(a.last_deploy_at)
      : sort === 'created' ? time(b.created_at) - time(a.created_at)
      : sort === 'apps' ? b.app_count - a.app_count || a.name.localeCompare(b.name)
      : a.name.localeCompare(b.name, undefined, { sensitivity: 'base' }))
  }, [projects, q, status, sort, problems.data])
  const pages = usePages(list, [q, status, sort])
  const filtered = q !== '' || status !== 'all'
  return (
    <Panel>
      <ListToolbar>
        <SearchBox label={tl.searchProjects} value={q} onChange={setQ} />
        <ToolbarSelect label={tl.filter} value={status} onChange={setStatus}
          options={['all', 'attention', 'healthy', 'empty'].map((v) => ({ value: v, label: tl.projectStatus[v] }))} />
        <ToolbarSelect label={tl.sort} value={sort} onChange={setSort}
          options={['name', 'deploy', 'created', 'apps'].map((v) => ({ value: v, label: tl.projectSort[v] }))} />
      </ListToolbar>
      {list.length === 0
        ? <Empty title={tl.noMatch}>{filtered && <button type="button" className="text-accent hover:underline" onClick={() => { setQ(''); setStatus('all') }}>{tl.clear}</button>}</Empty>
        : <ul className="divide-y divide-line">{pages.shown.map((p) => <ProjectRow key={p.id} p={p} down={down(p.id)} />)}</ul>}
      <Pager {...pages} />
    </Panel>
  )
}

function ProjectRow({ p, down }: { p: Project; down: number }) {
  const t = useT()
  const tl = t.lists
  const running = Math.max(0, (p.apps_running ?? 0) - down)
  return (
    <li>
      <Link to="/projects/$projectId" params={{ projectId: p.id }}
        className="grid gap-x-6 gap-y-2 px-4 py-3 hover:bg-raised md:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1.1fr)] md:items-center">
        <span className="min-w-0">
          <span className="block truncate text-sm">{p.name}</span>
          <span className="block truncate text-xs text-muted">{p.description || t.projects.created(timeAgo(p.created_at))}</span>
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted">
          <span className="text-fg">{t.projects.apps(p.app_count)}</span>
          {running > 0 && <Status tone="accent">{tl.running(running)}</Status>}
          {down > 0 && <Status tone="danger">{tl.down(down)}</Status>}
          {(p.apps_stopped ?? 0) > 0 && <Status tone="muted">{tl.stopped(p.apps_stopped!)}</Status>}
          {(p.apps_not_deployed ?? 0) > 0 && <Status tone="muted">{tl.notDeployed(p.apps_not_deployed!)}</Status>}
          <span className="basis-full">{tl.databases(p.databases ?? 0)} · {tl.domains(p.domains ?? 0)}</span>
        </span>
        <span className="min-w-0 text-xs">
          <span className="block text-muted">{tl.lastDeploy}</span>
          {p.last_deploy_at
            ? <>
                <span className="flex min-w-0 items-center gap-1.5">
                  <span className={cx('size-1.5 shrink-0 rounded-full', failedDeploy(p.last_deploy_status) ? 'bg-danger' : p.last_deploy_status === 'succeeded' ? 'bg-accent' : 'bg-warn')} />
                  <span className="truncate">{p.last_deploy_app}</span>
                </span>
                <span className={cx('block truncate', failedDeploy(p.last_deploy_status) ? 'text-danger' : 'text-muted')}>
                  {tl.deployStatus[p.last_deploy_status ?? ''] ?? p.last_deploy_status} · {timeAgo(p.last_deploy_at)}
                </span>
              </>
            : <span className="text-muted">{tl.neverDeployed}</span>}
        </span>
      </Link>
    </li>
  )
}

function Status({ tone, children }: { tone: 'accent' | 'danger' | 'muted'; children: ReactNode }) {
  return <span className="inline-flex items-center gap-1.5">
    <span className={cx('size-1.5 rounded-full', tone === 'accent' ? 'bg-accent' : tone === 'danger' ? 'bg-danger' : 'bg-muted')} />
    <span className={tone === 'danger' ? 'text-danger' : undefined}>{children}</span>
  </span>
}

export function NewProject({ onDone }: { onDone: () => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')

  const create = useMutation({
    mutationFn: () => api<Project>('/projects', { method: 'POST', body: { name, description } }),
    onSuccess: (p) => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      navigate({ to: '/projects/$projectId', params: { projectId: p.id } })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <Field label={t.common.name} name="name" required autoFocus maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />
      <Field label={`${t.common.description} (${t.common.optional})`} name="description" maxLength={500} value={description} onChange={(e) => setDescription(e.target.value)} />
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <div className="flex gap-2">
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t.common.creating : t.projects.createProject}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </div>
    </form>
  )
}
