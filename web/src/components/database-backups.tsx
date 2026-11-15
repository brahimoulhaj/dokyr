import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState } from 'react'
import { api, s3ConnectionsQuery } from '../lib/api'
import { databaseCatalogQuery, type DatabaseInstance, type DatabaseOperation } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'
import { DatabaseSelect } from '../routes/databases'
import { DatabaseBackupPolicies } from './database-backup-policies'
import { Sheet } from './sheet'
import { Badge, Button, Empty, ErrorNote, Field, Panel, QueryState } from './ui'

type Backup = { policy_id: string; id: string; database_name: string; s3_connection_id: string; object_key: string; size_bytes: number; ready: boolean; status: string; error: string; created_at: string }
export function DatabaseBackups({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT().databases
  const qc = useQueryClient()
  const history = useQuery({ queryKey: ['database-backups', d.id], queryFn: () => api<Backup[]>(`/databases/${d.id}/backups`), refetchInterval: 2000 })
  const storage = useQuery(s3ConnectionsQuery)
  const catalog = useQuery({ ...databaseCatalogQuery(d.id), enabled: d.runtime === 'running' })
  const [creating, setCreating] = useState(false)
  const [restoring, setRestoring] = useState<Backup | null>(null)
  const [database, setDatabase] = useState(d.database_name)
  const [connection, setConnection] = useState('')
  const [name, setName] = useState('')
  const ready = d.status === 'running' && d.runtime === 'running' && d.desired_state === 'running'
  const selectedStorage = connection || storage.data?.[0]?.id || ''
  const submit = useMutation({
    mutationFn: (action: 'backup' | 'restore') => submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(`/databases/${d.id}/${action === 'backup' ? 'backups' : 'restore'}`, { method: 'POST', body: action === 'backup' ? { database, s3_connection_id: selectedStorage } : { backup_id: restoring?.id, name } }), t.operationRunning[action](d.name)),
    onSuccess: () => { setCreating(false); setRestoring(null); qc.invalidateQueries({ queryKey: ['database-backups', d.id] }) },
  })
  return <div className="space-y-4">
    <p className="text-xs leading-relaxed text-muted">{t.backupHint}</p>
    <DatabaseBackupPolicies d={d} busy={busy} />
    <QueryState loading={storage.isPending} error={storage.error} what={t.backupStorage} />
    {storage.data?.length === 0 && <p className="border border-line p-4 text-sm text-muted">{t.noBackupStorage} <Link className="text-accent hover:underline" to="/settings" search={{ tab: 'storage' }}>{t.backupStorage} ↗</Link></p>}
    {!ready && <p className="text-xs text-muted">{t.requiresRunning}</p>}
    <Panel title={t.backupsTab} actions={<Button variant="ghost" disabled={!ready || busy || !storage.data?.length} onClick={() => { submit.reset(); setCreating(true) }}>{t.backupNow}</Button>}>
      <QueryState loading={history.isPending} error={history.error} what={t.backupsTab} />
      {history.data?.length === 0 && <Empty title={t.noBackups} />}
      <ul className="divide-y divide-line">{history.data?.map((b) => <li key={b.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="min-w-0 space-y-1"><div className="flex flex-wrap items-center gap-2 text-sm"><code>{b.database_name}</code><span className="text-xs text-muted">{b.policy_id ? t.scheduledBackup : t.manualBackup}</span><Badge tone={b.status === 'failed' ? 'danger' : b.status === 'succeeded' ? 'muted' : 'warn'}>{b.status}</Badge></div>
          <p className="text-xs text-muted">{new Date(b.created_at).toLocaleString()} · {storage.data?.find((s) => s.id === b.s3_connection_id)?.name ?? t.backupStorage} · {(b.size_bytes / 1024 / 1024).toFixed(2)} MiB</p>
          {b.error && <p className="text-xs text-danger">{b.error}</p>}
        </div>
        <Button variant="ghost" disabled={!ready || busy || !b.ready || b.status !== 'succeeded'} onClick={() => { submit.reset(); setName(''); setRestoring(b) }}>{t.restoreBackup}</Button>
      </li>)}</ul>
    </Panel>
    <Sheet open={creating} onOpenChange={setCreating} title={t.backupNow}>
      <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); submit.mutate('backup') }}>
        <QueryState loading={catalog.isPending} error={catalog.error} what={t.chooseDatabase} />
        <DatabaseSelect label={t.chooseDatabase} value={database} onChange={setDatabase}>{(catalog.data?.databases ?? [{ id: d.id, name: d.database_name }]).map((db) => <option key={db.id} value={db.name}>{db.name}</option>)}</DatabaseSelect>
        <DatabaseSelect label={t.backupStorage} value={selectedStorage} onChange={setConnection}>{storage.data?.map((s) => <option key={s.id} value={s.id}>{s.name} · {s.bucket}</option>)}</DatabaseSelect>
        {submit.error && <ErrorNote>{submit.error.message}</ErrorNote>}
        <Button disabled={busy || !ready || submit.isPending || !selectedStorage}>{t.backupNow}</Button>
      </form>
    </Sheet>
    <Sheet open={!!restoring} onOpenChange={(open) => { if (!open) setRestoring(null) }} title={t.restoreBackup}>
      <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); submit.mutate('restore') }}>
        <p className="text-sm text-muted">{t.restoreHint}</p>
        <Field name="restore-name" autoFocus label={t.newDatabase} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9_]*" maxLength={48} required />
        {submit.error && <ErrorNote>{submit.error.message}</ErrorNote>}
        <Button disabled={busy || !ready || submit.isPending || !name}>{t.restoreBackup}</Button>
      </form>
    </Sheet>
  </div>
}
