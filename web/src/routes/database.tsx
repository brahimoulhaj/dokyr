import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { Button, ConfirmButton, ErrorNote, Field, PageHeader, Panel, QueryState, cx } from '../components/ui'
import { api, projectsQuery } from '../lib/api'
import { databaseOperationsQuery, databaseQuery, type DatabaseAccepted, type DatabaseInstance, type DatabaseOperation } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { DatabaseSelect, DatabaseStatus } from './databases'
import { DatabaseBackups } from '../components/database-backups'
import { DatabaseAccess } from '../components/database-access'
import { DatabasePublicAccess, PublicConnection } from '../components/database-public'

export const databaseTabs = ['overview', 'access', 'connection', 'backups', 'logs', 'settings'] as const
export type DatabaseTab = typeof databaseTabs[number]

export function DatabasePage() {
  const { databaseId } = useParams({ from: '/app/databases/$databaseId' })
  const { tab = 'overview' } = useSearch({ from: '/app/databases/$databaseId' })
  const db = useQuery(databaseQuery(databaseId))
  const projects = useQuery(projectsQuery)
  const operations = useQuery(databaseOperationsQuery)
  const t = useT()
  const qc = useQueryClient()
  const action = useMutation({
    mutationFn: (op: 'start' | 'stop' | 'restart') => submitDatabaseOperation(() => api<DatabaseAccepted>(`/databases/${databaseId}/${op}`, { method: 'POST' }), t.databases.operationRunning[op](db.data?.name ?? '')),
    onSettled: () => qc.invalidateQueries({ queryKey: ['databases'] }),
  })
  const d = db.data
  const busy = action.isPending || d?.status === 'provisioning' || d?.status === 'stopping' || operations.data?.some((op) => op.instance_id === databaseId) === true
  const ready = d?.status === 'running' && d.runtime === 'running' && d.desired_state === 'running'
  const active = d && ['running', 'restarting', 'paused'].includes(d.runtime)
  return <div className="space-y-6">
    <PageHeader title={<>{d?.name ?? t.databases.title}{d && <DatabaseStatus value={d.status === 'running' && d.runtime !== 'running' ? d.runtime : d.status} />}</>} crumbs={<Link to="/databases" className="hover:text-fg">{t.databases.title}</Link>}
      subtitle={d && `${d.engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'} ${d.version}`}
      actions={d && <>
        <div className="flex items-center gap-2">
          <Button variant="ghost" disabled={busy || ready} onClick={() => action.mutate('start')}>{t.databases.start}</Button>
          <ConfirmButton variant="ghost" label={t.databases.stop} confirm={t.databases.confirmStop} disabled={busy || (!active && d.desired_state === 'stopped')} pending={action.isPending} onConfirm={() => action.mutate('stop')} />
          <ConfirmButton variant="ghost" label={t.databases.restart} confirm={t.databases.confirmRestart} disabled={busy || !active} pending={action.isPending} onConfirm={() => action.mutate('restart')} />
        </div>
        <div className="border-s border-line ps-3">
          <Button variant="ghost" className="whitespace-nowrap" disabled={busy || !ready} onClick={() => window.open(`/api/databases/${d.id}/adminer/`, '_blank', 'noopener,noreferrer')}>{t.databases.openAdminer}<span aria-hidden="true">↗</span></Button>
        </div>
      </>} />
    <QueryState loading={db.isPending} error={db.error} what={t.databases.title} />
    {action.error && <ErrorNote>{action.error.message}</ErrorNote>}
    {d?.error && <ErrorNote>{d.error}</ErrorNote>}
    {d && <>
      <nav aria-label={t.databases.title} className="flex gap-4 overflow-x-auto border-b border-line">
        {databaseTabs.map((key) => <Link key={key} to="/databases/$databaseId" params={{ databaseId }} search={{ tab: key }} aria-current={tab === key ? 'page' : undefined}
          className={cx('border-b-2 px-1 py-2 text-sm whitespace-nowrap', tab === key ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-fg')}>{key === 'backups' ? t.databases.backupsTab : t.databases[key]}</Link>)}
      </nav>
      {tab === 'overview' && <>
        <Panel title={t.databases.overview}><dl className="grid gap-x-6 gap-y-4 p-4 text-sm sm:grid-cols-2 xl:grid-cols-3">
          <Detail label={t.databases.project}><Link to="/projects/$projectId" params={{ projectId: d.project_id }} className="text-accent hover:underline">{projects.data?.find((p) => p.id === d.project_id)?.name ?? d.project_id}</Link></Detail>
          <Detail label={t.databases.database}>{d.database_name}</Detail><Detail label={t.databases.username}>{d.username}</Detail>
          <Detail label={t.databases.host}>{d.host}</Detail><Detail label={t.databases.runtime}><DatabaseStatus value={d.runtime} /></Detail>
          <Detail label={t.databases.cpu}>{d.cpu_limit || t.databases.unlimited}</Detail><Detail label={t.databases.memory}>{d.memory_mb || t.databases.unlimited}</Detail>
          <Detail label={t.databases.volume}>{d.volume}</Detail>
        </dl></Panel>
        <p className="text-xs text-muted">{t.databases.privateHint}</p>
      </>}
      {tab === 'backups' && <DatabaseBackups d={d} busy={!!busy} />}
      {tab === 'connection' && <Connection key={d.id} d={d} />}
      {tab === 'access' && <DatabaseAccess d={d} busy={busy} />}
      {tab === 'logs' && <DatabaseLogs id={d.id} />}
      {tab === 'settings' && <div className="space-y-5">
        <DatabaseProject key={`${d.id}:${d.project_id}`} d={d} busy={busy} />
        <DatabasePublicAccess d={d} busy={busy} />
        <RemoveDatabase d={d} busy={busy} />
      </div>}
    </>}
  </div>
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="min-w-0"><dt className="mb-1 text-xs text-muted">{label}</dt><dd className="break-all">{children}</dd></div>
}

function CopyValue({ value }: { value: string }) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const [failed, setFailed] = useState(false)
  return <div className="space-y-2"><div className="flex items-start gap-2"><code dir="ltr" className="min-w-0 flex-1 break-all border border-line bg-bg p-3 text-xs select-all">{value}</code>
    <Button variant="ghost" onClick={async () => { try { await navigator.clipboard.writeText(value); setCopied(true); setFailed(false) } catch { setFailed(true) } }}>{copied ? t.common.copied : t.common.copy}</Button>
  </div>{failed && <ErrorNote>{t.databases.copyFailed}</ErrorNote>}</div>
}

function Connection({ d }: { d: DatabaseInstance }) {
  const t = useT()
  // Credentials stay in component memory only, not the shared query cache.
  const [details, setDetails] = useState<Record<string, string> | null>(null)
  const reveal = useMutation({ mutationFn: () => api<Record<string, string>>(`/databases/${d.id}/connection`), onSuccess: setDetails, gcTime: 0 })
  return <div className="space-y-5">
    <Panel title={t.databases.reference}><div className="space-y-3 p-4">
      <p className="text-xs text-muted">{t.databases.referenceHint}</p><CopyValue value={`\${db.${d.name}.URL}`} />
      <p className="text-xs text-muted">{['HOST', 'PORT', 'DATABASE', 'USERNAME', 'PASSWORD'].map((key) => `\${db.${d.name}.${key}}`).join(' · ')}</p>
    </div></Panel>
    <Panel title={t.databases.connection}><div className="space-y-4 p-4">
      <p className="text-xs text-muted">{t.databases.revealHint}</p>
      <Button variant="ghost" disabled={reveal.isPending} onClick={() => { if (details) { setDetails(null); reveal.reset() } else reveal.mutate() }}>{details ? t.databases.hide : t.databases.reveal}</Button>
      {reveal.error && <ErrorNote>{reveal.error.message}</ErrorNote>}
      {details && <div className="space-y-4">{Object.entries(details).map(([key, value]) => <div key={key}><p className="mb-1 text-xs text-muted">{key}</p><CopyValue value={value} /></div>)}</div>}
    </div></Panel>
    <PublicConnection d={d} copy={(value) => <CopyValue value={value} />} />
  </div>
}

function DatabaseLogs({ id }: { id: string }) {
  const t = useT()
  const logs = useQuery({ queryKey: ['database-logs', id], queryFn: () => api<{ text: string; stream: string; time: string }[]>(`/databases/${id}/logs`), refetchInterval: 3000 })
  return <Panel title={t.databases.logs}>
    <div className="p-4"><QueryState loading={logs.isPending} error={logs.error} what={t.databases.logs} />
      {logs.data && <pre dir="ltr" tabIndex={0} aria-label={t.databases.logs} className="max-h-[32rem] overflow-auto whitespace-pre-wrap break-all text-xs leading-relaxed">{logs.data.length ? logs.data.map((l) => `${l.time} ${l.text}`).join('\n') : t.databases.noLogs}</pre>}
    </div>
  </Panel>
}

function DatabaseProject({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT()
  const qc = useQueryClient()
  const projects = useQuery(projectsQuery)
  const [projectId, setProjectId] = useState(d.project_id)
  const save = useMutation({
    mutationFn: () => withToast(() => api<DatabaseInstance>(`/databases/${d.id}`, { method: 'PATCH', body: { project_id: projectId } }), {
      loading: t.databases.projectSaving, success: t.databases.projectSaved, error: t.databases.projectSaveFailed,
    }),
    onSuccess: (next) => {
      qc.setQueryData(databaseQuery(d.id).queryKey, next)
      qc.invalidateQueries({ queryKey: ['databases'] })
    },
  })
  return <Panel title={t.databases.project}><form className="max-w-xl space-y-4 p-4" onSubmit={(e) => { e.preventDefault(); if (projectId !== d.project_id) save.mutate() }}>
    <QueryState loading={projects.isPending} error={projects.error} what={t.databases.project} />
    <fieldset disabled={busy || save.isPending} className="space-y-4">
      <DatabaseSelect label={t.databases.project} value={projectId} onChange={(id) => { setProjectId(id); save.reset() }}>
        {!projects.data?.some((p) => p.id === d.project_id) && <option value={d.project_id}>{d.project_id}</option>}
        {projects.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
      </DatabaseSelect>
      <p className="text-xs text-muted">{t.databases.projectMoveHint}</p>
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <Button disabled={!projects.data?.length || projectId === d.project_id}>{save.isPending ? t.common.saving : t.common.save}</Button>
    </fieldset>
  </form></Panel>
}

function RemoveDatabase({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT()
  const [name, setName] = useState('')
  const qc = useQueryClient()
  const navigate = useNavigate()
  const remove = useMutation({ mutationFn: () => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}`, { method: 'DELETE', body: { name } }), t.databases.operationRunning.delete(d.name)), onSuccess: () => {
    qc.removeQueries({ queryKey: ['databases', d.id] })
    qc.invalidateQueries({ queryKey: ['databases'] })
    navigate({ to: '/databases' })
  } })
  return <Panel title={t.databases.remove}><div className="max-w-xl space-y-4 p-4">
    <p className="text-sm text-muted">{t.databases.deleteHint}</p><code className="block break-all text-xs">{d.volume}</code>
    <form className="space-y-4" onSubmit={(e) => { e.preventDefault(); if (name === d.name) remove.mutate() }}>
      <Field name="confirm-name" label={t.databases.confirmName} placeholder={d.name} autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} />
      {remove.error && <ErrorNote>{remove.error.message}</ErrorNote>}
      <Button variant="danger" disabled={busy || remove.isPending || name !== d.name}>{remove.isPending ? t.common.working : t.databases.remove}</Button>
    </form>
  </div></Panel>
}
