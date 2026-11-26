import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmDialog, ConfirmModal } from '../components/confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Button, CheckOption, Empty, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { api, meQuery, metaQuery, s3ConnectionsQuery } from '../lib/api'
import { dateTime, formatBytes, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { poll } from '../lib/live'
import { DatabaseSelect } from './databases'

type Status = { last_run_at: string | null; last_success_at: string | null; last_error: string; last_key: string; last_size: number }
type PanelBackup = {
  enabled: boolean
  s3_connection_id: string
  schedule: string
  keep: number
  has_passphrase: boolean
  configured: boolean
  next_run_at: string | null
  status: Status
  running: boolean
}
type BackupObject = { key: string; name: string; size: number; last_modified: string }
type RestoreInfo = { name: string; created_at: string; panel_version: string; schema_version: number; restarting: boolean }

const settingsKey = ['settings', 'panel-backup'] as const
const toastId = 'panel-backup'
const restoreToastId = 'panel-restore'
const restoredKey = 'panel-restored' // session storage: the backup a restore reloaded the page for
const presets = { daily: '0 3 * * *', sixHours: '0 */6 * * *', weekly: '0 3 * * 0' } as const
type Preset = keyof typeof presets | 'custom'

/** Backs up the panel's own database and key to S3, on a schedule or on demand. */
export function PanelBackupSettings() {
  const t = useT()
  const tb = t.settingsBackup
  const qc = useQueryClient()
  // While a backup started here runs, poll as well, in case the change stream is down.
  const started = useRef<string | null>(null)
  const [waiting, setWaiting] = useState(false)
  const settings = useQuery({
    queryKey: settingsKey,
    queryFn: () => api<PanelBackup>('/settings/panel-backup'),
    refetchInterval: waiting ? poll(2000) : false,
  })
  const storage = useQuery(s3ConnectionsQuery)
  const backups = useQuery({
    queryKey: [...settingsKey, 'backups'],
    queryFn: () => api<BackupObject[]>('/settings/panel-backup/backups'),
    enabled: !!settings.data?.s3_connection_id,
    retry: false,
  })
  const [editing, setEditing] = useState(false)
  const [restoring, setRestoring] = useState(false)
  const [restoreTarget, setRestoreTarget] = useState<BackupObject | null>(null)
  const isOwner = useQuery(meQuery).data?.role === 'owner' // rolling the panel back is the owner's alone

  const run = useMutation({
    mutationFn: () => api<void>('/settings/panel-backup/run', { method: 'POST' }),
    onMutate: () => {
      started.current = new Date(Date.now() - 1000).toISOString()
      toast.loading(tb.runningToast, { id: toastId })
    },
    onSuccess: () => setWaiting(true),
    onError: (e) => {
      started.current = null
      toast.error(tb.failedToast, { id: toastId, description: e.message })
    },
  })

  // The run ends when the status shows an attempt at or after the click.
  const status = settings.data?.status
  useEffect(() => {
    const since = started.current
    if (!since || !status?.last_run_at || settings.data?.running || status.last_run_at < since) return
    started.current = null
    setWaiting(false)
    if (status.last_error) toast.error(tb.failedToast, { id: toastId, description: status.last_error })
    else toast.success(tb.doneToast, { id: toastId, description: `${status.last_key.split('/').pop()} · ${formatBytes(status.last_size)}` })
    void qc.invalidateQueries({ queryKey: [...settingsKey, 'backups'] })
  }, [status, settings.data?.running, tb, qc])

  // After a restore the page reloads on the restored panel: say it worked.
  useEffect(() => {
    let name: string | null = null
    try {
      name = sessionStorage.getItem(restoredKey)
      sessionStorage.removeItem(restoredKey)
    } catch { /* storage blocked: no toast */ }
    if (name) toast.success(tb.restoredToast, { id: restoreToastId, description: name })
  }, [tb])

  const data = settings.data
  const connection = storage.data?.find((s) => s.id === data?.s3_connection_id)
  const noStorage = storage.data?.length === 0

  return (
    <>
      <Panel
        title={tb.panelTitle}
        actions={data && (
          <div className="flex flex-wrap gap-2">
            {data.configured && (
              <Button type="button" variant="ghost" onClick={() => setRestoring(true)}>{tb.restore}</Button>
            )}
            <Button type="button" variant="ghost" disabled={noStorage} onClick={() => setEditing(true)}>
              {data.configured ? tb.edit : tb.setUp}
            </Button>
            {data.configured && (
              <Button type="button" disabled={run.isPending || waiting || data.running} onClick={() => run.mutate()}>
                {run.isPending || waiting || data.running ? t.common.working : tb.runNow}
              </Button>
            )}
          </div>
        )}
      >
        {!data ? (
          <div className="p-4"><QueryState loading={settings.isPending} error={settings.error} what={tb.what} /></div>
        ) : !data.configured ? (
          <div className="p-4 text-sm text-muted">{noStorage ? tb.noStorage : tb.notSetUp}</div>
        ) : (
          <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-2">
            <Row label={tb.storage}>{connection ? `${connection.name} · ${connection.bucket}` : '—'}</Row>
            <Row label={tb.schedule}>
              {data.enabled
                ? <><code dir="ltr">{data.schedule}</code> UTC{data.next_run_at && <span className="text-muted"> · {tb.nextRun} {dateTime(data.next_run_at)}</span>}</>
                : tb.scheduleOff}
            </Row>
            <Row label={tb.keep}>{tb.keepValue(data.keep)}</Row>
            <Row label={tb.lastBackup}>
              {data.status.last_success_at
                ? <span title={dateTime(data.status.last_success_at)}>{timeAgo(data.status.last_success_at)} · {formatBytes(data.status.last_size)}</span>
                : tb.never}
            </Row>
            {data.status.last_error && data.status.last_run_at !== data.status.last_success_at && (
              <div className="sm:col-span-2"><ErrorNote>{tb.lastFailed} ({timeAgo(data.status.last_run_at)}): {data.status.last_error}</ErrorNote></div>
            )}
          </dl>
        )}
      </Panel>

      {data?.s3_connection_id && (
        <Panel title={tb.backups}>
          {backups.error ? (
            <div className="p-4"><ErrorNote>{backups.error.message}</ErrorNote></div>
          ) : !backups.data ? (
            <div className="p-4"><QueryState loading={backups.isPending} error={null} what={tb.backupsWhat} /></div>
          ) : backups.data.length === 0 ? (
            <Empty title={tb.noBackups} />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="text-start text-xs text-muted">
                  <tr className="border-b border-line">
                    <th className="px-4 py-2 text-start font-normal">{tb.name}</th>
                    <th className="px-4 py-2 text-start font-normal">{tb.created}</th>
                    <th className="px-4 py-2 text-end font-normal">{tb.size}</th>
                    <th className="px-4 py-2"><span className="sr-only">{tb.actions}</span></th>
                  </tr>
                </thead>
                <tbody>
                  {backups.data.map((b) => (
                    <tr key={b.key} className="border-b border-line last:border-0">
                      <td className="px-4 py-2"><code dir="ltr" className="text-xs">{b.name}</code></td>
                      <td className="px-4 py-2 text-muted" title={dateTime(b.last_modified)}>{timeAgo(b.last_modified)}</td>
                      <td className="px-4 py-2 text-end text-muted">{formatBytes(b.size)}</td>
                      <td className="px-4 py-2 text-end">
                        <ActionsMenu label={tb.actions} ariaLabel={`${tb.actions}: ${b.name}`} items={[
                          { label: tb.download, onSelect: () => { window.location.href = `/api/settings/panel-backup/backups/${encodeURIComponent(b.name)}` } },
                          ...(isOwner ? [{ label: tb.restoreAction, danger: true, onSelect: () => setRestoreTarget(b) }] : []),
                        ]} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </Panel>
      )}

      <OrphanContainers />

      <Sheet open={editing} onOpenChange={setEditing} title={tb.sheetTitle} description={tb.sheetDescription}>
        {data && <BackupForm current={data} onDone={() => setEditing(false)} />}
      </Sheet>
      <Sheet open={restoring} onOpenChange={setRestoring} title={tb.restoreTitle} description={tb.restoreDescription}>
        <RestoreSteps example={backups.data?.[0]?.name} />
      </Sheet>
      <Sheet open={!!restoreTarget} onOpenChange={(open) => { if (!open) setRestoreTarget(null) }} title={tb.restoreSheetTitle} description={tb.restoreSheetDescription}>
        {restoreTarget && <RestoreBackup key={restoreTarget.key} backup={restoreTarget} onDone={() => setRestoreTarget(null)} />}
      </Sheet>
    </>
  )
}

type Orphan = { id: string; name: string; image: string; kind: string; resource_id: string; state: string; running: boolean; created_at: string }

const orphansKey = ['orphans'] as const

/** Containers of apps and databases the panel has forgotten; nothing is shown when there are none. */
function OrphanContainers() {
  const tb = useT().settingsBackup
  const qc = useQueryClient()
  const role = useQuery(meQuery).data?.role
  const orphans = useQuery({
    queryKey: orphansKey,
    queryFn: () => api<Orphan[]>('/orphans'),
    enabled: role === 'owner' || role === 'admin',
  })
  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`/orphans/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => { toast.success(tb.orphanRemoved) },
    onError: (e) => { toast.error(e.message) },
    onSettled: () => { void qc.invalidateQueries({ queryKey: orphansKey }) },
  })
  if (!orphans.data?.length) return null
  return (
    <Panel title={tb.orphansTitle}>
      <p className="border-b border-line px-4 py-3 text-xs text-muted">{tb.orphansIntro}</p>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-start text-xs text-muted">
            <tr className="border-b border-line">
              <th className="px-4 py-2 text-start font-normal">{tb.orphanContainer}</th>
              <th className="px-4 py-2 text-start font-normal">{tb.orphanState}</th>
              <th className="px-4 py-2 text-start font-normal">{tb.created}</th>
              <th className="px-4 py-2"><span className="sr-only">{tb.actions}</span></th>
            </tr>
          </thead>
          <tbody>
            {orphans.data.map((c) => (
              <tr key={c.id} className="border-b border-line last:border-0">
                <td className="max-w-[18rem] px-4 py-2 sm:max-w-md">
                  <code dir="ltr" className="block truncate text-xs" title={c.name}>{c.name}</code>
                  <div className="mt-0.5 truncate text-xs text-muted" title={c.image}>{tb.orphanKind(c.kind)} · <span dir="ltr">{c.image.startsWith('sha256:') ? c.image.slice(0, 19) : c.image}</span></div>
                </td>
                <td className="px-4 py-2 text-muted" dir="ltr">{c.state}</td>
                <td className="px-4 py-2 text-muted" title={dateTime(c.created_at)}>{timeAgo(c.created_at)}</td>
                <td className="px-4 py-2 text-end">
                  <ConfirmDialog label={tb.orphanRemove} title={tb.orphanRemoveTitle} message={tb.orphanRemoveMessage(c.name)} confirm={tb.orphanRemove}
                    pending={remove.isPending && remove.variables === c.id} disabled={remove.isPending} onConfirm={() => remove.mutate(c.id)} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </Panel>
  )
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="min-w-0"><dt className="text-xs text-muted">{label}</dt><dd className="mt-0.5 truncate">{children}</dd></div>
}

function BackupForm({ current, onDone }: { current: PanelBackup; onDone: () => void }) {
  const t = useT()
  const tb = t.settingsBackup
  const qc = useQueryClient()
  const storage = useQuery(s3ConnectionsQuery)
  const weak = useQuery(metaQuery).data?.weak_passwords // development: any length
  const initialPreset = (Object.entries(presets).find(([, cron]) => cron === current.schedule)?.[0] ?? 'custom') as Preset
  const [enabled, setEnabled] = useState(current.configured ? current.enabled : true)
  const [connection, setConnection] = useState(current.s3_connection_id)
  const [preset, setPreset] = useState<Preset>(initialPreset)
  const [cron, setCron] = useState(current.schedule)
  const [keep, setKeep] = useState(String(current.keep))
  const [passphrase, setPassphrase] = useState('')
  const [confirm, setConfirm] = useState('')
  useEffect(() => {
    if (!connection && storage.data?.[0]) setConnection(storage.data[0].id)
  }, [connection, storage.data])

  const save = useMutation({
    mutationFn: () => api<PanelBackup>('/settings/panel-backup', {
      method: 'PUT',
      body: { enabled, s3_connection_id: connection, schedule: preset === 'custom' ? cron.trim() : presets[preset], keep: Number(keep), passphrase },
    }),
    onSuccess: (next) => {
      qc.setQueryData(settingsKey, next)
      void qc.invalidateQueries({ queryKey: [...settingsKey, 'backups'] })
      toast.success(tb.savedToast, { id: toastId })
      onDone()
    },
  })
  const mismatch = passphrase !== '' && confirm !== '' && passphrase !== confirm
  const needsPassphrase = !current.has_passphrase && passphrase === ''

  function submit(e: FormEvent) {
    e.preventDefault()
    if (passphrase !== confirm || needsPassphrase) return
    save.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <CheckOption checked={enabled} onChange={setEnabled} label={tb.enabled} />
      <DatabaseSelect label={tb.storageLabel} value={connection} onChange={setConnection}>
        {storage.data?.map((s) => <option key={s.id} value={s.id}>{s.name} · {s.bucket}</option>)}
      </DatabaseSelect>
      <DatabaseSelect label={tb.scheduleLabel} value={preset} onChange={(v) => setPreset(v as Preset)}>
        <option value="daily">{tb.presetDaily}</option>
        <option value="sixHours">{tb.presetSixHours}</option>
        <option value="weekly">{tb.presetWeekly}</option>
        <option value="custom">{tb.presetCustom}</option>
      </DatabaseSelect>
      {preset === 'custom' && (
        <Field name="backup-cron" dir="ltr" required label={tb.cron} hint={tb.cronHint} hintTooltip value={cron}
          onChange={(e) => setCron(e.target.value)} spellCheck={false} autoComplete="off" />
      )}
      <Field name="backup-keep" type="number" dir="ltr" min={1} max={365} required label={tb.keepLabel} hint={tb.keepHint} hintTooltip
        value={keep} onChange={(e) => setKeep(e.target.value)} />
      <Field name="backup-passphrase" type="password" autoComplete="new-password" minLength={weak ? 1 : 12} required={!current.has_passphrase}
        label={current.has_passphrase ? tb.passphraseNew : tb.passphrase} hint={weak ? tb.passphraseHintWeak : tb.passphraseHint} hintTooltip
        value={passphrase} onChange={(e) => setPassphrase(e.target.value)} />
      {passphrase !== '' && (
        <Field name="backup-passphrase-confirm" type="password" autoComplete="new-password" required label={tb.passphraseConfirm}
          value={confirm} onChange={(e) => setConfirm(e.target.value)} />
      )}
      {mismatch && <ErrorNote>{tb.passphraseMismatch}</ErrorNote>}
      {(passphrase !== '' || !current.has_passphrase) && (
        <p className="border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-fg">
          {tb.passphraseWarning}{current.has_passphrase && <> {tb.passphraseChangeWarning}</>}
        </p>
      )}
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={save.isPending || mismatch || !connection}>{save.isPending ? t.common.saving : t.common.save}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

/**
 * Rolls the panel back to one backup in storage: the passphrase opens and checks it,
 * what it holds and what is lost are shown, then the panel restores it and restarts.
 */
function RestoreBackup({ backup, onDone }: { backup: BackupObject; onDone: () => void }) {
  const t = useT()
  const tb = t.settingsBackup
  const [passphrase, setPassphrase] = useState('')
  const [info, setInfo] = useState<RestoreInfo | null>(null)
  const [done, setDone] = useState<RestoreInfo | null>(null)
  const [confirming, setConfirming] = useState(false)
  const url = `/settings/panel-backup/backups/${encodeURIComponent(backup.name)}`
  const inspect = useMutation({
    mutationFn: () => api<RestoreInfo>(`${url}/inspect`, { method: 'POST', body: { passphrase } }),
    onSuccess: setInfo,
  })
  const restore = useMutation({
    mutationFn: () => api<RestoreInfo>(`${url}/restore`, { method: 'POST', body: { passphrase } }),
    onMutate: () => { toast.loading(tb.restoreWorking, { id: restoreToastId }) },
    onError: (e) => { toast.error(tb.restoreFailedToast, { id: restoreToastId, description: e.message }) },
    onSuccess: (r) => {
      setDone(r)
      if (!r.restarting) return void toast.success(tb.restoreManualRestart, { id: restoreToastId })
      toast.loading(tb.restoreRestarting, { id: restoreToastId })
      try { sessionStorage.setItem(restoredKey, r.name) } catch { /* no toast after the reload */ }
    },
  })

  // The panel goes away, then comes back on the restored database: reload once it answers.
  const restarting = !!done?.restarting
  useEffect(() => {
    if (!restarting) return
    let stop = false
    void (async () => {
      await sleep(4000)
      while (!stop) {
        try {
          if ((await fetch('/api/meta', { cache: 'no-store' })).ok) return window.location.reload()
        } catch { /* still down */ }
        await sleep(1500)
      }
    })()
    return () => { stop = true }
  }, [restarting])

  if (done) {
    return <div className="p-5 text-sm">{done.restarting ? tb.restoreRestarting : tb.restoreManualRestart}</div>
  }
  const error = restore.error ?? inspect.error
  function submit(e: FormEvent) {
    e.preventDefault()
    if (info) setConfirming(true) // the consequences are spelled out in a dialog, not in the form
    else inspect.mutate()
  }
  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <dl className="grid gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
        <div className="min-w-0 sm:col-span-2"><dt className="text-xs text-muted">{tb.name}</dt><dd className="mt-0.5 truncate"><code dir="ltr" className="text-xs">{backup.name}</code></dd></div>
        {info && <Row label={tb.created}>{dateTime(info.created_at)}</Row>}
        {info && <Row label={tb.restoreVersion}><span dir="ltr">{info.panel_version || '—'}</span></Row>}
      </dl>
      <Field name="restore-passphrase" type="password" autoComplete="off" required autoFocus disabled={restore.isPending}
        label={tb.restorePassphrase} hint={tb.restorePassphraseHint} hintTooltip
        value={passphrase} onChange={(e) => { setPassphrase(e.target.value); setInfo(null); inspect.reset() }} />
      {error && <ErrorNote>{error.message}</ErrorNote>}
      <SheetActions>
        {info ? (
          <Button type="submit" variant="danger" disabled={restore.isPending}>
            {restore.isPending ? tb.restoreWorking : tb.restoreAction}
          </Button>
        ) : (
          <Button type="submit" disabled={!passphrase || inspect.isPending}>
            {inspect.isPending ? tb.restoreChecking : tb.restoreCheck}
          </Button>
        )}
        <Button type="button" variant="ghost" disabled={restore.isPending} onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
      {info && (
        <ConfirmModal open={confirming} onOpenChange={setConfirming} title={tb.restoreSheetTitle} confirm={tb.restoreConfirm}
          onConfirm={() => restore.mutate()}
          message={<>
            <span className="block">{tb.restoreLoses(timeAgo(info.created_at))}</span>
            <span className="mt-3 block">{tb.restoreSafety}</span>
          </>} />
      )}
    </form>
  )
}

function RestoreSteps({ example }: { example?: string }) {
  const tb = useT().settingsBackup
  const meta = useQuery(metaQuery)
  const slug = meta.data?.slug ?? 'panel'
  const file = example ?? `${slug}-<time>.tar.gz.age`
  const block = 'block overflow-x-auto whitespace-pre border border-line bg-bg px-3 py-2 text-xs'
  return (
    <div className="space-y-4 p-5 text-sm">
      <p>{tb.restoreStep1}</p>
      <code dir="ltr" className={block}>{`docker cp ${file} ${slug}:/tmp/`}</code>
      <p>{tb.restoreStep2}</p>
      <code dir="ltr" className={block}>{`docker exec -it ${slug} panel restore /tmp/${file}`}</code>
      <p>{tb.restoreStep3}</p>
      <code dir="ltr" className={block}>{`docker restart ${slug}`}</code>
      <p className="text-xs text-muted">{tb.restoreManual}</p>
      <code dir="ltr" className={block}>{`age -d ${file} | tar -xz`}</code>
    </div>
  )
}
