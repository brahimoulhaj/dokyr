import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState, type ReactNode } from 'react'
import { Sheet } from '../components/sheet'
import { Badge, Button, Empty, ErrorNote, Field, PageHeader, Panel, QueryState } from '../components/ui'
import { api, projectsQuery } from '../lib/api'
import { databasesQuery, enginesQuery, type DatabaseAccepted } from '../lib/databases'
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
    {dbs.data && <Panel>{dbs.data.length === 0 ? <Empty title={t.databases.empty}>{t.databases.emptyHint}</Empty> :
      <ul className="divide-y divide-line">{dbs.data.map((d) => <li key={d.id}>
        <Link to="/databases/$databaseId" params={{ databaseId: d.id }} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 hover:bg-raised">
          <div className="min-w-0"><span className="block truncate text-sm">{d.name}</span><span className="text-xs text-muted">{projects.data?.find((p) => p.id === d.project_id)?.name ?? d.project_id} · {d.engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'} {d.version}</span></div>
          <DatabaseStatus value={d.status === 'running' && d.runtime !== 'running' ? d.runtime : d.status} />
        </Link>
      </li>)}</ul>}
    </Panel>}
  </div>
}

export function DatabaseSelect({ label, value, onChange, children }: { label: string; value: string; onChange: (v: string) => void; children: ReactNode }) {
  return <label className="block space-y-1.5 text-xs text-muted"><span>{label}</span>
    <select required className="h-9 w-full border border-line bg-bg px-3 text-sm text-fg" value={value} onChange={(e) => onChange(e.target.value)}>{children}</select>
  </label>
}

export function NewDatabase({ onDone, projectId }: { onDone: () => void; projectId?: string }) {
  const t = useT()
  const projects = useQuery(projectsQuery)
  const engines = useQuery(enginesQuery)
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [project, setProject] = useState('')
  const [name, setName] = useState('main')
  const [engine, setEngine] = useState('postgres')
  const [version, setVersion] = useState('')
  const [database, setDatabase] = useState('app')
  const [user, setUser] = useState('app')
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
        <DatabaseSelect label={t.databases.engine} value={engine} onChange={(v) => { setEngine(v); setVersion('') }}>{engines.data?.map((e) => <option key={e.id} value={e.id}>{e.name}</option>)}</DatabaseSelect>
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
    <div className="flex gap-2"><Button disabled={create.isPending || !projects.data?.length || !engines.data?.length}>{create.isPending ? t.common.creating : t.common.create}</Button><Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button></div>
  </form>
}
