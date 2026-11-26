import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { useState } from 'react'
import { api, s3ConnectionsQuery, type App } from '../lib/api'
import { dateTime, formatBytes, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { poll } from '../lib/live'
import { useCan } from '../lib/roles'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { Badge, Button, ErrorNote, Panel } from './ui'

type VolumeBackup = { id: string; volume: string; s3_connection_id: string; size: number; status: string; error: string; created_at: string }
type VolumeBackups = { volumes: string[]; backups: VolumeBackup[] }

/** Backups of the app's named volumes: take one, restore one, delete one. */
export function VolumeBackupsPanel({ app }: { app: App }) {
  const t = useT()
  const tv = t.volumeBackups
  const qc = useQueryClient()
  const can = useCan()
  const key = ['apps', app.id, 'volume-backups'] as const
  const url = `/apps/${app.id}/volume-backups`
  const list = useQuery({
    queryKey: key,
    queryFn: () => api<VolumeBackups>(url),
    // A backup runs in the background: look again while one is running.
    refetchInterval: (q) => (q.state.data?.backups.some((b) => b.status === 'running') ? poll(3000)() : false),
  })
  const storage = useQuery(s3ConnectionsQuery).data ?? []
  const [volume, setVolume] = useState('')
  const [connection, setConnection] = useState('')
  const [restoring, setRestoring] = useState<VolumeBackup | null>(null)
  const [removing, setRemoving] = useState<VolumeBackup | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: key })
  const volumes = list.data?.volumes ?? []
  const chosenVolume = volume || volumes[0] || ''
  const chosenConnection = connection || storage[0]?.id || ''
  const start = useMutation({
    mutationFn: () => api<VolumeBackup>(url, { method: 'POST', body: { volume: chosenVolume, s3_connection_id: chosenConnection } }),
    onSuccess: () => { toast.success(tv.started); void refresh() },
    onError: (e) => { toast.error(e.message) },
  })
  const restore = useMutation({
    mutationFn: (id: string) => api<VolumeBackup>(`/volume-backups/${id}/restore`, { method: 'POST' }),
    onSuccess: () => { toast.success(tv.restoreStarted) },
    onError: (e) => { toast.error(e.message) },
  })
  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`/volume-backups/${id}`, { method: 'DELETE' }),
    onSuccess: () => { toast.success(tv.removed) },
    onError: (e) => { toast.error(e.message) },
    onSettled: () => { void refresh() },
  })
  if (!list.data) return list.error ? <ErrorNote>{list.error.message}</ErrorNote> : null
  const select = 'block h-9 w-full border border-line bg-bg px-3 text-sm text-fg'
  return (
    <Panel title={tv.title}>
      <p className="border-b border-line px-4 py-3 text-xs text-muted">{tv.intro}</p>
      {volumes.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted">{tv.noVolumes}</p>
      ) : storage.length === 0 ? (
        <p className="px-4 py-6 text-center text-sm text-muted">{tv.noStorage}</p>
      ) : (
        <div className="flex flex-wrap items-end gap-3 border-b border-line px-4 py-3">
          <label className="min-w-40 flex-1 space-y-1.5 text-xs text-muted">
            <span>{tv.volume}</span>
            <select dir="ltr" className={select} value={chosenVolume} onChange={(e) => setVolume(e.target.value)}>
              {volumes.map((v) => <option key={v} value={v}>{v}</option>)}
            </select>
          </label>
          <label className="min-w-40 flex-1 space-y-1.5 text-xs text-muted">
            <span>{tv.storage}</span>
            <select className={select} value={chosenConnection} onChange={(e) => setConnection(e.target.value)}>
              {storage.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </label>
          <Button type="button" disabled={start.isPending} onClick={() => start.mutate()}>{start.isPending ? tv.starting : tv.backUp}</Button>
        </div>
      )}
      {list.data.backups.length === 0 ? (
        volumes.length > 0 && storage.length > 0 && <p className="px-4 py-6 text-center text-sm text-muted">{tv.none}</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-xs text-muted">
              <tr className="border-b border-line">
                <th className="px-4 py-2 text-start font-normal">{tv.volume}</th>
                <th className="px-4 py-2 text-start font-normal">{tv.made}</th>
                <th className="px-4 py-2 text-end font-normal">{tv.size}</th>
                <th className="px-4 py-2"><span className="sr-only">{tv.actions}</span></th>
              </tr>
            </thead>
            <tbody>
              {list.data.backups.map((b) => (
                <tr key={b.id} className="border-b border-line last:border-0">
                  <td className="max-w-[16rem] px-4 py-2">
                    <span className="flex flex-wrap items-center gap-2">
                      <code dir="ltr" className="truncate text-xs">{b.volume}</code>
                      <Badge tone={b.status === 'failed' ? 'danger' : b.status === 'running' ? 'warn' : 'muted'}>{tv.status[b.status] ?? b.status}</Badge>
                    </span>
                    {b.error && <p className="mt-0.5 text-xs text-danger [overflow-wrap:anywhere]">{b.error}</p>}
                  </td>
                  <td className="px-4 py-2 text-muted" title={dateTime(b.created_at)}>{timeAgo(b.created_at)}</td>
                  <td className="px-4 py-2 text-end text-muted">{b.status === 'ready' ? formatBytes(b.size) : '—'}</td>
                  <td className="px-4 py-2 text-end">
                    {can.remove && b.status !== 'running' && (
                      <ActionsMenu label={tv.actions} ariaLabel={`${tv.actions}: ${b.volume} ${dateTime(b.created_at)}`} items={[
                        ...(b.status === 'ready' ? [{ label: tv.restore, danger: true, onSelect: () => setRestoring(b) }] : []),
                        { label: tv.remove, danger: true, onSelect: () => setRemoving(b) },
                      ]} />
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {restoring && (
        <ConfirmModal open onOpenChange={(o) => { if (!o) setRestoring(null) }} title={tv.restoreTitle} confirm={tv.restoreConfirm}
          message={tv.restoreMessage(restoring.volume, timeAgo(restoring.created_at))} onConfirm={() => restore.mutate(restoring.id)} />
      )}
      {removing && (
        <ConfirmModal open onOpenChange={(o) => { if (!o) setRemoving(null) }} title={tv.removeTitle} confirm={tv.remove}
          message={tv.removeMessage(removing.volume, timeAgo(removing.created_at))} onConfirm={() => remove.mutate(removing.id)} />
      )}
    </Panel>
  )
}
