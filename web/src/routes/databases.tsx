import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useMemo, useState, type ReactNode } from 'react'
import { DeletedInstanceBackups } from '../components/deleted-instance-backups'
import { ListToolbar, Pager, SearchBox, ToolbarSelect, fold, usePages } from '../components/list-controls'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, cx, Empty, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, projectsQuery, type Project } from '../lib/api'
import { timeAgo } from '../lib/format'
import { databasesQuery, enginesQuery, type DatabaseAccepted, type DatabaseInstance } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'

export function DatabaseStatus({ value }: { value: string }) {
  const t = useT().databases
  const labels: Record<string, string> = { provisioning: t.provisioning, running: t.running, stopped: t.stopped,
    stopping: t.stopping, failed: t.failed, unavailable: t.unavailable, missing: t.missing, created: t.created, exited: t.exited, restarting: t.restarting }
  return <Badge tone={value === 'running' ? 'accent' : ['failed', 'unavailable', 'missing'].includes(value) ? 'danger' : value === 'provisioning' ? 'warn' : 'muted'}>{labels[value] ?? value}</Badge>
}

export function DatabasesPage() {
  const t = useT()
  const dbs = useQuery(databasesQuery)
  const projects = useQuery(projectsQuery)
  const [creating, setCreating] = useState(false)
  return <div className="space-y-6">
    <PageHeader title={t.databases.title} subtitle={t.databases.subtitle} actions={<Button onClick={() => setCreating(true)}>{t.databases.add}</Button>} />
    <Sheet open={creating} onOpenChange={setCreating} title={t.databases.add}>
      {creating && <NewDatabase onDone={() => setCreating(false)} />}
    </Sheet>
    <QueryState loading={dbs.isPending} error={dbs.error} what={t.databases.title} />
    {dbs.data && (dbs.data.length === 0
      ? <Panel><Empty title={t.databases.empty}>{t.databases.emptyHint}</Empty></Panel>
      : <DatabaseList list={dbs.data} projects={projects.data ?? []} />)}
    <DeletedInstanceBackups />
  </div>
}

const engineLabel = (e: string) => (e === 'postgres' ? 'PostgreSQL' : 'MariaDB')
const shownStatus = (d: DatabaseInstance) => (d.status === 'running' && d.runtime !== 'running' ? d.runtime : d.status)
const needsAttention = (d: DatabaseInstance) => ['failed', 'unavailable', 'missing', 'exited'].includes(shownStatus(d)) || d.last_backup_status === 'failed'

function DatabaseList({ list, projects }: { list: DatabaseInstance[]; projects: Project[] }) {
  const t = useT()
  const tl = t.lists
  const [q, setQ] = useState('')
  const [engine, setEngine] = useState('all')
  const [project, setProject] = useState('all')
  const [status, setStatus] = useState('all')
  const [sort, setSort] = useState('name')
  const projectName = (id: string) => projects.find((p) => p.id === id)?.name ?? id
  const rows = useMemo(() => {
    const needle = fold(q.trim())
    const time = (v?: string | null) => (v ? Date.parse(v) : 0)
    return list.filter((d) => (!needle || fold(`${d.name} ${projectName(d.project_id)} ${d.database_name} ${engineLabel(d.engine)}`).includes(needle))
      && (engine === 'all' || d.engine === engine)
      && (project === 'all' || d.project_id === project)
      && (status === 'all' || (status === 'attention' ? needsAttention(d) : status === 'running' ? shownStatus(d) === 'running' : d.desired_state === 'stopped')))
      .sort((a, b) => sort === 'project' ? projectName(a.project_id).localeCompare(projectName(b.project_id)) || a.name.localeCompare(b.name)
        : sort === 'created' ? time(b.created_at) - time(a.created_at)
        : sort === 'backup' ? time(b.last_backup_at) - time(a.last_backup_at)
        : a.name.localeCompare(b.name))
  }, [list, projects, q, engine, project, status, sort])
  const pages = usePages(rows, [q, engine, project, status, sort])
  const projectIds = [...new Set(list.map((d) => d.project_id))]
  return <Panel>
    <ListToolbar>
      <SearchBox label={tl.searchDatabases} value={q} onChange={setQ} />
      <ToolbarSelect label={tl.engine} value={engine} onChange={setEngine} options={[{ value: 'all', label: tl.allEngines }, { value: 'postgres', label: 'PostgreSQL' }, { value: 'mariadb', label: 'MariaDB' }]} />
      {projectIds.length > 1 && <ToolbarSelect label={tl.project} value={project} onChange={setProject}
        options={[{ value: 'all', label: tl.allProjects }, ...projectIds.map((id) => ({ value: id, label: projectName(id) }))]} />}
      <ToolbarSelect label={tl.filter} value={status} onChange={setStatus} options={['all', 'running', 'stopped', 'attention'].map((v) => ({ value: v, label: tl.dbStatus[v] }))} />
      <ToolbarSelect label={tl.sort} value={sort} onChange={setSort} options={['name', 'project', 'created', 'backup'].map((v) => ({ value: v, label: tl.dbSort[v] }))} />
    </ListToolbar>
    {rows.length === 0
      ? <Empty title={tl.noMatch}><button type="button" className="text-accent hover:underline" onClick={() => { setQ(''); setEngine('all'); setProject('all'); setStatus('all') }}>{tl.clear}</button></Empty>
      : <ul className="divide-y divide-line">{pages.shown.map((d) => <DatabaseRow key={d.id} d={d} projectName={projectName(d.project_id)} />)}</ul>}
    <Pager {...pages} />
  </Panel>
}

function DatabaseRow({ d, projectName }: { d: DatabaseInstance; projectName: string }) {
  const t = useT()
  const tl = t.lists
  const cpu = d.cpu_limit ? String(d.cpu_limit) : tl.unlimited
  const mem = d.memory_mb ? `${d.memory_mb} MiB` : tl.unlimited
  const failed = d.last_backup_status === 'failed'
  return <li>
    <Link to="/databases/$databaseId" params={{ databaseId: d.id }}
      className="grid gap-x-6 gap-y-2 px-4 py-3 hover:bg-raised md:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)] md:items-center">
      <span className="min-w-0">
        <span className="block truncate text-sm">{d.name}</span>
        <span className="block truncate text-xs text-muted">{projectName}</span>
      </span>
      <span className="flex min-w-0 flex-wrap items-center gap-2 text-xs">
        <DatabaseStatus value={shownStatus(d)} />
        <span className="text-muted">{engineLabel(d.engine)} {d.version}</span>
        {d.public_port > 0 && <Badge tone="warn">{tl.publicPort(d.public_port)}</Badge>}
      </span>
      <span className="min-w-0 text-xs text-muted">
        <span className="block text-fg">{tl.dbCounts(d.database_count ?? 1, d.user_count ?? 1)}</span>
        <span className="block">{tl.limits(cpu, mem)}</span>
      </span>
      <span className="min-w-0 text-xs">
        <span className="block text-muted">{tl.lastBackup}{(d.backup_policies ?? 0) > 0 && ` · ${tl.scheduled}`}</span>
        {d.last_backup_at
          ? <span className="flex items-center gap-1.5">
              <span className={cx('size-1.5 shrink-0 rounded-full', failed ? 'bg-danger' : d.last_backup_status === 'succeeded' ? 'bg-accent' : 'bg-warn')} />
              <span className={failed ? 'text-danger' : undefined}>{failed ? tl.backupFailed : timeAgo(d.last_backup_at)}</span>
              {failed && <span className="text-muted">{timeAgo(d.last_backup_at)}</span>}
            </span>
          : <span className="text-muted">{tl.noBackups}</span>}
      </span>
    </Link>
  </li>
}

export function DatabaseSelect({ label, value, onChange, children }: { label: string; value: string; onChange: (v: string) => void; children: ReactNode }) {
  return <label className="block space-y-1.5 text-xs text-muted"><span>{label}</span>
    <select required className="h-9 w-full border border-line bg-bg px-3 text-sm text-fg" value={value} onChange={(e) => onChange(e.target.value)}>{children}</select>
  </label>
}

/** engine fixes the engine; onCreated receives the new instance instead of opening its page. */
export function NewDatabase({ onDone, projectId, engine: fixedEngine, defaults, onCreated }: { onDone: () => void; projectId?: string; engine?: string; defaults?: { name?: string; database?: string }; onCreated?: (d: DatabaseAccepted) => void }) {
  const t = useT()
  const projects = useQuery(projectsQuery)
  const engines = useQuery(enginesQuery)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [project, setProject] = useState('')
  const [name, setName] = useState(defaults?.name ?? 'main')
  const [engine, setEngine] = useState(fixedEngine ?? 'postgres')
  const [version, setVersion] = useState('')
  const [database, setDatabase] = useState(defaults?.database ?? 'app')
  const [user, setUser] = useState(defaults?.database ?? 'app')
  const [cpu, setCPU] = useState('1')
  const [memory, setMemory] = useState('512')
  const versions = engines.data?.find((e) => e.id === engine)?.versions ?? []
  const create = useMutation({
    mutationFn: () => submitDatabaseOperation(() => api<DatabaseAccepted>('/databases', { method: 'POST', body: {
      project_id: projectId || project || projects.data?.[0]?.id, name, engine, version: version || versions[0],
      database_name: database, username: user, cpu_limit: Number(cpu), memory_mb: Number(memory),
    } }), t.databases.operationRunning.create(name)),
    onSuccess: (d) => {
      qc.invalidateQueries({ queryKey: databasesQuery.queryKey })
      if (onCreated) { onCreated(d); return }
      onDone()
      navigate({ to: '/databases/$databaseId', params: { databaseId: d.id } })
    },
  })
  return <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); create.mutate() }}>
    <QueryState loading={projects.isPending || engines.isPending} error={projects.error ?? engines.error} what={t.databases.title} />
    {projects.data?.length === 0 ? <p className="text-sm text-muted">{t.databases.noProjects} <Link to="/projects" className="text-accent underline">{t.databases.project}</Link></p> : <>
      {projectId
        ? <Field name="database-project" label={t.databases.project} readOnly value={projects.data?.find((p) => p.id === projectId)?.name ?? projectId} />
        : <DatabaseSelect label={t.databases.project} value={project || projects.data?.[0]?.id || ''} onChange={setProject}>{projects.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</DatabaseSelect>}
      <Field name="instance-name" label={t.common.name} hint={t.databases.nameHint} required autoFocus maxLength={48} pattern="[a-z][a-z0-9-]*" value={name} onChange={(e) => setName(e.target.value)} />
      <div className="grid grid-cols-2 gap-4">
        <DatabaseSelect label={t.databases.engine} value={engine} onChange={(v) => { setEngine(v); setVersion('') }}>{engines.data?.filter((e) => !fixedEngine || e.id === fixedEngine).map((e) => <option key={e.id} value={e.id}>{e.name}</option>)}</DatabaseSelect>
        <DatabaseSelect label={t.databases.version} value={version || versions[0] || ''} onChange={setVersion}>{versions.map((v) => <option key={v} value={v}>{v}</option>)}</DatabaseSelect>
      </div>
      <Field name="database-name" label={t.databases.database} hint={t.databases.sqlHint} required maxLength={48} pattern="[a-z][a-z0-9_]*" value={database} onChange={(e) => setDatabase(e.target.value)} />
      <Field name="database-user" label={t.databases.username} required maxLength={48} pattern="[a-z][a-z0-9_]*" value={user} onChange={(e) => setUser(e.target.value)} />
      <div className="grid grid-cols-2 gap-4">
        <Field name="cpu" label={t.databases.cpu} type="number" min={0} max={1024} step="0.1" required value={cpu} onChange={(e) => setCPU(e.target.value)} />
        <Field name="memory" label={t.databases.memory} type="number" min={0} max={1048576} required value={memory} onChange={(e) => setMemory(e.target.value)} />
      </div>
      <p className="text-xs text-muted">{t.databases.limitHint}</p>
      <p className="text-xs text-muted">{t.databases.privateHint} {t.databases.passwords}</p>
    </>}
    {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
    <SheetActions><Button disabled={create.isPending || !projects.data?.length || !engines.data?.length}>{create.isPending ? t.common.creating : t.common.create}</Button><Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button></SheetActions>
  </form>
}
