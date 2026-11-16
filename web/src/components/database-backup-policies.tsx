import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, s3ConnectionsQuery } from '../lib/api'
import { WHOLE_INSTANCE, databaseCatalogQuery, type DatabaseInstance } from '../lib/databases'
import { useT } from '../lib/i18n'
import { DatabaseSelect } from '../routes/databases'
import { Sheet, SheetActions } from './sheet'
import { Badge, Button, Empty, ErrorNote, Field, Panel, QueryState } from './ui'
import { poll } from '../lib/live'

type Frequency = 'daily' | 'weekly' | 'cron'
type Policy = { id: string; database_name: string; s3_connection_id: string; enabled: boolean; frequency: Frequency; cron: string; scope: 'database' | 'instance'; hour: number; minute: number; weekday: number; keep_count: number; next_run_at: string; last_error: string; last_operation_id: string }
export function DatabaseBackupPolicies({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT().databases
  const policies = useQuery({ queryKey: ['backup-policies', d.id], queryFn: () => api<Policy[]>(`/databases/${d.id}/backup-policies`), refetchInterval: poll(5000) })
  const storage = useQuery(s3ConnectionsQuery)
  const [editing, setEditing] = useState<Policy | 'new' | null>(null)
  return <>
    <Panel title={t.backupSchedule} actions={<Button variant="ghost" disabled={busy || !storage.data?.length} onClick={() => setEditing('new')}>{t.configureSchedule}</Button>}>
      <QueryState loading={policies.isPending} error={policies.error} what={t.backupSchedule} />
      {!policies.isPending && policies.data?.length === 0 && <Empty title={t.noSchedules} />}
      <ul className="divide-y divide-line">{policies.data?.map((p) => <li key={p.id} className="flex flex-wrap items-start justify-between gap-3 p-4">
        <div className="min-w-0 space-y-1"><div className="flex items-center gap-2 text-sm">{p.scope === 'instance' ? <span>{t.wholeInstanceShort}</span> : <code>{p.database_name}</code>}<Badge tone="muted">{p.enabled ? t.scheduleEnabled : t.schedulePaused}</Badge></div>
          <p className="text-xs text-muted">{p.frequency === 'cron' ? <>{t.scheduleCron} · <code dir="ltr">{p.cron}</code> UTC</> : <>{p.frequency === 'daily' ? t.scheduleDaily : `${t.scheduleWeekly} · ${t.scheduleWeekdays.split('|')[p.weekday]}`} · {String(p.hour).padStart(2, '0')}:{String(p.minute).padStart(2, '0')} UTC</>} · {t.retentionCount}: {p.keep_count}</p>
          <p className="text-xs text-muted">{storage.data?.find((s) => s.id === p.s3_connection_id)?.name ?? t.backupStorage}{p.enabled && ` · ${t.nextBackup}: ${new Date(p.next_run_at).toLocaleString()}`}</p>
          {p.last_error && <p className="max-w-xl text-xs text-danger">{p.last_error}</p>}
        </div>
        <Button variant="ghost" disabled={busy} onClick={() => setEditing(p)}>{t.configureSchedule}</Button>
      </li>)}</ul>
    </Panel>
    <Sheet open={editing !== null} onOpenChange={(open) => { if (!open) setEditing(null) }} title={t.backupSchedule} description={t.scheduleHint}>
      {editing !== null && <PolicyEditor key={editing === 'new' ? 'new' : editing.id} d={d} busy={busy} policy={editing === 'new' ? undefined : editing} onDone={() => setEditing(null)} />}
    </Sheet>
  </>
}
function PolicyEditor({ d, busy, policy, onDone }: { d: DatabaseInstance; busy: boolean; policy?: Policy; onDone: () => void }) {
  const t = useT().databases
  const qc = useQueryClient()
  const storage = useQuery(s3ConnectionsQuery)
  const catalog = useQuery({ ...databaseCatalogQuery(d.id), enabled: d.runtime === 'running' })
  const [database, setDatabase] = useState(policy ? (policy.scope === 'instance' ? WHOLE_INSTANCE : policy.database_name) : WHOLE_INSTANCE)
  const [connection, setConnection] = useState(policy?.s3_connection_id ?? '')
  const [enabled, setEnabled] = useState(policy?.enabled ?? true)
  const [frequency, setFrequency] = useState<Frequency>(policy?.frequency ?? 'daily')
  const [cron, setCron] = useState(policy?.cron || '0 2 * * *')
  const [time, setTime] = useState(policy ? `${String(policy.hour).padStart(2, '0')}:${String(policy.minute).padStart(2, '0')}` : '02:00')
  const [weekday, setWeekday] = useState(policy?.weekday ?? 0)
  const [keep, setKeep] = useState(policy?.keep_count ?? 7)
  const selectedStorage = connection || storage.data?.[0]?.id || ''
  const save = useMutation({ mutationFn: () => api(`/databases/${d.id}/backup-policies`, { method: 'PUT', body: { database: database === WHOLE_INSTANCE ? '' : database, scope: database === WHOLE_INSTANCE ? 'instance' : 'database', s3_connection_id: selectedStorage, enabled, frequency, hour: Number(time.split(':')[0]), minute: Number(time.split(':')[1]), weekday, keep_count: keep, cron: frequency === 'cron' ? cron : '' } }), onSuccess: () => { qc.invalidateQueries({ queryKey: ['backup-policies', d.id] }); onDone() } })
  const names = Array.from(new Set([d.database_name, ...(policy?.database_name ? [policy.database_name] : []), ...(catalog.data?.databases.map((db) => db.name) ?? [])]))
  return <form className="space-y-5 p-5" onSubmit={(e) => { e.preventDefault(); save.mutate() }}>
    <QueryState loading={storage.isPending} error={storage.error} what={t.backupStorage} />
    <DatabaseSelect label={t.backupTarget} value={database} onChange={setDatabase}><option value={WHOLE_INSTANCE}>{t.wholeInstance}</option>{names.map((name) => <option key={name} value={name}>{name}</option>)}</DatabaseSelect>
    <DatabaseSelect label={t.backupStorage} value={selectedStorage} onChange={setConnection}>{storage.data?.map((s) => <option key={s.id} value={s.id}>{s.name} · {s.bucket}</option>)}</DatabaseSelect>
    <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />{t.scheduleEnabled}</label>
    <DatabaseSelect label={t.backupSchedule} value={frequency} onChange={(v) => setFrequency(v as Frequency)}><option value="daily">{t.scheduleDaily}</option><option value="weekly">{t.scheduleWeekly}</option><option value="cron">{t.scheduleCron}</option></DatabaseSelect>
    {frequency === 'weekly' && <DatabaseSelect label={t.scheduleDay} value={String(weekday)} onChange={(v) => setWeekday(Number(v))}>{t.scheduleWeekdays.split('|').map((day, i) => <option key={i} value={i}>{day}</option>)}</DatabaseSelect>}
    {frequency === 'cron'
      ? <Field name="backup-cron" dir="ltr" required label={t.cronExpression} hint={t.cronHint} value={cron} onChange={(e) => setCron(e.target.value)} spellCheck={false} autoComplete="off" />
      : <Field name="backup-time" type="time" required label={t.scheduleTime} value={time} onChange={(e) => setTime(e.target.value)} />}
    <Field name="backup-retention" type="number" min={1} max={365} required label={t.retentionCount} value={keep} onChange={(e) => setKeep(Number(e.target.value))} />
    <p className="text-xs leading-relaxed text-muted">{t.retentionHint}</p>
    {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
    <SheetActions><Button disabled={busy || save.isPending || !selectedStorage}>{t.saveSchedule}</Button></SheetActions>
  </form>
}
