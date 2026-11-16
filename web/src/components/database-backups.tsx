import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api, s3ConnectionsQuery } from '../lib/api'
import { WHOLE_INSTANCE, databaseCatalogQuery, databasesQuery, type DatabaseAccepted, type DatabaseInstance, type DatabaseOperation } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'
import { DatabaseSelect } from '../routes/databases'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { DatabaseBackupPolicies } from './database-backup-policies'
import { Sheet } from './sheet'
import { Badge, Button, Empty, ErrorNote, Field, Panel, QueryState } from './ui'
import { poll } from '../lib/live'

type Backup = { policy_id: string; id: string; database_name: string; scope: 'database' | 'instance'; s3_connection_id: string; object_key: string; size_bytes: number; ready: boolean; status: string; error: string; created_at: string }
export function DatabaseBackups({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const all = useT()
  const t = all.databases, tc = all.common
  const qc = useQueryClient()
  const navigate = useNavigate()
  const history = useQuery({ queryKey: ['database-backups', d.id], queryFn: () => api<Backup[]>(`/databases/${d.id}/backups`), refetchInterval: poll(2000) })
  const storage = useQuery(s3ConnectionsQuery)
  const catalog = useQuery({ ...databaseCatalogQuery(d.id), enabled: d.runtime === 'running' })
  const [creating, setCreating] = useState(false)
  const [restoring, setRestoring] = useState<Backup | null>(null)
  const [database, setDatabase] = useState(WHOLE_INSTANCE)
  const [connection, setConnection] = useState('')
  const [name, setName] = useState('')
  const ready = d.status === 'running' && d.runtime === 'running' && d.desired_state === 'running'
  const selectedStorage = connection || storage.data?.[0]?.id || ''
  const submit = useMutation({
    mutationFn: (action: 'backup' | 'restore') => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/${action === 'backup' ? 'backups' : 'restore'}`, {
      method: 'POST',
      body: action === 'backup'
        ? (database === WHOLE_INSTANCE ? { scope: 'instance', s3_connection_id: selectedStorage } : { database, s3_connection_id: selectedStorage })
        : { backup_id: restoring?.id, name },
    }), t.operationRunning[action](d.name)),
    onSuccess: () => { setCreating(false); setRestoring(null); qc.invalidateQueries({ queryKey: ['database-backups', d.id] }) },
  })
  // A whole-instance backup restores into a brand-new instance in the same project.
  const restoreInstance = useMutation({
    mutationFn: () => submitDatabaseOperation(() => api<DatabaseAccepted>(`/databases/${d.id}/restore-instance`, { method: 'POST', body: { backup_id: restoring?.id, name, project_id: d.project_id, cpu_limit: d.cpu_limit, memory_mb: d.memory_mb } }), t.operationRunning.create(name)),
    onSuccess: (created) => {
      setRestoring(null)
      qc.invalidateQueries({ queryKey: databasesQuery.queryKey })
      navigate({ to: '/databases/$databaseId', params: { databaseId: created.id } })
    },
  })
  // Deleting removes the S3 objects; finished backups only, never one in progress.
  const remove = useMutation({
    mutationFn: (id: string) => api(`/databases/${d.id}/backups/${id}`, { method: 'DELETE' }),
    onSettled: () => qc.invalidateQueries({ queryKey: ['database-backups', d.id] }),
  })
  const [deleting, setDeleting] = useState<string | null>(null)
  const instanceRestore = restoring?.scope === 'instance'
  const restoreMutation = instanceRestore ? restoreInstance : submit
  return <div className="space-y-4">
    <DatabaseBackupPolicies d={d} busy={busy} />
    <QueryState loading={storage.isPending} error={storage.error} what={t.backupStorage} />
    {storage.data?.length === 0 && <p className="border border-line p-4 text-sm text-muted">{t.noBackupStorage} <Link className="text-accent hover:underline" to="/settings" search={{ tab: 'storage' }}>{t.backupStorage} ↗</Link></p>}
    {!ready && <p className="text-xs text-muted">{t.requiresRunning}</p>}
    <Panel title={t.backupsTab} actions={<Button variant="ghost" disabled={!ready || busy || !storage.data?.length} onClick={() => { submit.reset(); setCreating(true) }}>{t.backupNow}</Button>}>
      <QueryState loading={history.isPending} error={history.error} what={t.backupsTab} />
      {history.data?.length === 0 && <Empty title={t.noBackups} />}
      <ul className="divide-y divide-line">{history.data?.map((b) => <li key={b.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="min-w-0 space-y-1"><div className="flex flex-wrap items-center gap-2 text-sm">{b.scope === 'instance' ? <span>{t.wholeInstanceShort}</span> : <code>{b.database_name}</code>}<span className="text-xs text-muted">{b.policy_id ? t.scheduledBackup : t.manualBackup}</span><Badge tone={b.status === 'failed' ? 'danger' : b.status === 'succeeded' ? 'muted' : 'warn'}>{b.status}</Badge></div>
          <p className="text-xs text-muted">{new Date(b.created_at).toLocaleString()} · {storage.data?.find((s) => s.id === b.s3_connection_id)?.name ?? t.backupStorage} · {(b.size_bytes / 1024 / 1024).toFixed(2)} MiB</p>
          {b.error && <p className="text-xs text-danger">{b.error}</p>}
          {remove.error && remove.variables === b.id && <p className="text-xs text-danger">{remove.error.message}</p>}
        </div>
        <ActionsMenu label={t.actions} ariaLabel={`${t.actions} · ${b.id}`} items={[
          { label: b.scope === 'instance' ? t.restoreInstance : t.restoreBackup, disabled: busy || !b.ready || b.status !== 'succeeded' || (b.scope !== 'instance' && !ready), onSelect: () => { submit.reset(); restoreInstance.reset(); setName(''); setRestoring(b) } },
          { label: remove.isPending && remove.variables === b.id ? tc.working : t.deleteBackup, danger: true, disabled: busy || remove.isPending || (b.status !== 'succeeded' && b.status !== 'failed'), onSelect: () => setDeleting(b.id) },
        ]} />
      </li>)}</ul>
    </Panel>
    <ConfirmModal open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null) }} title={t.deleteBackupTitle} message={t.deleteBackupMessage} confirm={t.confirmDeleteBackup} onConfirm={() => { if (deleting) remove.mutate(deleting) }} />
    <Sheet open={creating} onOpenChange={setCreating} title={t.backupNow} description={t.backupHint}>
      <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); submit.mutate('backup') }}>
        <QueryState loading={catalog.isPending} error={catalog.error} what={t.chooseDatabase} />
        <DatabaseSelect label={t.backupTarget} value={database} onChange={setDatabase}>
          <option value={WHOLE_INSTANCE}>{t.wholeInstance}</option>
          {(catalog.data?.databases ?? [{ id: d.id, name: d.database_name }]).map((db) => <option key={db.id} value={db.name}>{db.name}</option>)}
        </DatabaseSelect>
        <DatabaseSelect label={t.backupStorage} value={selectedStorage} onChange={setConnection}>{storage.data?.map((s) => <option key={s.id} value={s.id}>{s.name} · {s.bucket}</option>)}</DatabaseSelect>
        {submit.error && <ErrorNote>{submit.error.message}</ErrorNote>}
        <Button disabled={busy || !ready || submit.isPending || !selectedStorage}>{t.backupNow}</Button>
      </form>
    </Sheet>
    <Sheet open={!!restoring} onOpenChange={(open) => { if (!open) setRestoring(null) }} title={instanceRestore ? t.restoreInstance : t.restoreBackup} description={instanceRestore ? t.restoreInstanceHint : t.restoreHint}>
      <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); if (instanceRestore) restoreInstance.mutate(); else submit.mutate('restore') }}>
        {instanceRestore
          ? <Field name="restore-instance-name" autoFocus label={t.newInstanceName} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9\-]*" maxLength={48} required />
          : <Field name="restore-name" autoFocus label={t.newDatabase} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9_]*" maxLength={48} required />}
        {restoreMutation.error && <ErrorNote>{restoreMutation.error.message}</ErrorNote>}
        <Button disabled={busy || (!instanceRestore && !ready) || restoreMutation.isPending || !name}>{instanceRestore ? t.restoreInstance : t.restoreBackup}</Button>
      </form>
    </Sheet>
  </div>
}
