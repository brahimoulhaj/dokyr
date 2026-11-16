import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { api, projectsQuery, s3ConnectionsQuery } from '../lib/api'
import { databasesQuery, type DatabaseAccepted, type DatabaseOperation } from '../lib/databases'
import { submitDatabaseOperation } from '../lib/database-operations'
import { useT } from '../lib/i18n'
import { DatabaseSelect } from '../routes/databases'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { Sheet, SheetActions } from './sheet'
import { Badge, Button, ErrorNote, Field, Panel } from './ui'

type OrphanBackup = {
  id: string; instance_id: string; instance_name: string; project_id: string; engine: string; version: string
  database_name: string; scope: 'database' | 'instance'; s3_connection_id: string; size_bytes: number
  ready: boolean; status: string; error: string; created_at: string
}
export const orphanBackupsQuery = { queryKey: ['database-backups', 'orphaned'], queryFn: () => api<OrphanBackup[]>('/database-backups/orphaned') }

const engineLabel = (engine: string) => engine === 'postgres' ? 'PostgreSQL' : 'MariaDB'

/** Backups whose instance was deleted: restore them elsewhere or delete them. Hidden when there are none. */
export function DeletedInstanceBackups() {
  const all = useT()
  const t = all.databases, tc = all.common
  const qc = useQueryClient()
  const navigate = useNavigate()
  const list = useQuery(orphanBackupsQuery)
  const storage = useQuery(s3ConnectionsQuery)
  const projects = useQuery(projectsQuery)
  const instances = useQuery(databasesQuery)
  const [restoring, setRestoring] = useState<OrphanBackup | null>(null)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [project, setProject] = useState('')
  const [target, setTarget] = useState('')
  const instanceRestore = restoring?.scope === 'instance'
  // Same engine and running: the server checks this again before restoring.
  const targets = (instances.data ?? []).filter((d) => d.engine === restoring?.engine && d.status === 'running' && d.runtime === 'running')
  const selectedTarget = target || targets[0]?.id || ''
  const selectedProject = project || (projects.data?.some((p) => p.id === restoring?.project_id) ? restoring?.project_id : projects.data?.[0]?.id) || ''
  const restore = useMutation({
    mutationFn: () => {
      const b = restoring!
      const path = `/database-backups/orphaned/${b.id}/restore`
      if (b.scope === 'instance') {
        return submitDatabaseOperation(() => api<DatabaseAccepted>(path, { method: 'POST', body: { name, project_id: selectedProject, cpu_limit: 1, memory_mb: 512 } }), t.operationRunning.create(name))
      }
      return submitDatabaseOperation(() => api<{ operation: DatabaseOperation }>(path, { method: 'POST', body: { name, target_id: selectedTarget } }), t.operationRunning.restore(name))
    },
    // Both answers carry the operation; its instance is the new or the target one.
    onSuccess: (result) => {
      setRestoring(null)
      qc.invalidateQueries({ queryKey: databasesQuery.queryKey })
      navigate({ to: '/databases/$databaseId', params: { databaseId: result.operation.instance_id } })
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => api(`/database-backups/orphaned/${id}`, { method: 'DELETE' }),
    onSettled: () => qc.invalidateQueries({ queryKey: orphanBackupsQuery.queryKey }),
  })
  if (!list.data?.length) return null
  return <>
    <Panel title={t.deletedBackups}>
      <ul className="divide-y divide-line">{list.data.map((b) => <li key={b.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2 text-sm"><span>{b.instance_name}</span><span className="text-muted">·</span>{b.scope === 'instance' ? <span>{t.wholeInstanceShort}</span> : <code>{b.database_name}</code>}<Badge tone={b.status === 'failed' ? 'danger' : 'muted'}>{b.status}</Badge></div>
          <p className="text-xs text-muted">{new Date(b.created_at).toLocaleString()} · {projects.data?.find((p) => p.id === b.project_id)?.name ?? b.project_id} · {engineLabel(b.engine)} {b.version} · {storage.data?.find((s) => s.id === b.s3_connection_id)?.name ?? t.backupStorage} · {(b.size_bytes / 1024 / 1024).toFixed(2)} MiB</p>
          {b.error && <p className="text-xs text-danger">{b.error}</p>}
          {remove.error && remove.variables === b.id && <p className="text-xs text-danger">{remove.error.message}</p>}
        </div>
        <ActionsMenu label={t.actions} ariaLabel={`${t.actions} · ${b.id}`} items={[
          { label: b.scope === 'instance' ? t.restoreInstance : t.restoreBackup, disabled: !b.ready || b.status !== 'succeeded', onSelect: () => { restore.reset(); setName(''); setTarget(''); setProject(''); setRestoring(b) } },
          { label: remove.isPending && remove.variables === b.id ? tc.working : t.deleteBackup, danger: true, disabled: remove.isPending, onSelect: () => setDeleting(b.id) },
        ]} />
      </li>)}</ul>
    </Panel>
    <ConfirmModal open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null) }} title={t.deleteBackupTitle} message={t.deleteBackupMessage} confirm={t.confirmDeleteBackup} onConfirm={() => { if (deleting) remove.mutate(deleting) }} />
    <Sheet open={!!restoring} onOpenChange={(open) => { if (!open) setRestoring(null) }} title={instanceRestore ? t.restoreInstance : t.restoreBackup} description={instanceRestore ? t.orphanInstanceHint : t.orphanRestoreHint}>
      <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); restore.mutate() }}>
        {instanceRestore
          ? <>
            <DatabaseSelect label={t.project} value={selectedProject} onChange={setProject}>{projects.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}</DatabaseSelect>
            <Field name="orphan-instance-name" autoFocus label={t.newInstanceName} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9\-]*" maxLength={48} required />
          </>
          : targets.length === 0
            ? <p className="text-sm text-muted">{t.noRestoreTarget}</p>
            : <>
              <DatabaseSelect label={t.restoreInto} value={selectedTarget} onChange={setTarget}>{targets.map((d) => <option key={d.id} value={d.id}>{d.name} · {engineLabel(d.engine)} {d.version}</option>)}</DatabaseSelect>
              <Field name="orphan-database-name" autoFocus label={t.newDatabase} value={name} onChange={(e) => setName(e.target.value)} pattern="[a-z][a-z0-9_]*" maxLength={48} required />
            </>}
        {restore.error && <ErrorNote>{restore.error.message}</ErrorNote>}
        <SheetActions><Button disabled={restore.isPending || !name || (instanceRestore ? !selectedProject : !selectedTarget)}>{instanceRestore ? t.restoreInstance : t.restoreBackup}</Button></SheetActions>
      </form>
    </Sheet>
  </>
}
