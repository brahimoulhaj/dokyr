import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../lib/api'
import { databasePublicQuery, databaseQuery, type DatabaseInstance, type PublicAccess } from '../lib/databases'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { ConfirmDialog } from './confirm-dialog'
import { Badge, ErrorNote, Field, Panel, QueryState } from './ui'

const address = (host: string, port: number) => `${host || 'server'}:${port}`

/** Settings: open, change or close an instance's public port. */
export function DatabasePublicAccess({ d, busy }: { d: DatabaseInstance; busy: boolean }) {
  const t = useT().databases
  const access = useQuery(databasePublicQuery(d.id))
  return <Panel title={t.publicAccess}>
    <div className="max-w-xl space-y-4 p-4">
      <QueryState loading={access.isPending} error={access.error} what={t.publicAccess} />
      {access.data && <PublicForm key={`${access.data.port}:${access.data.allow}`} d={d} busy={busy} access={access.data} />}
    </div>
  </Panel>
}

function PublicForm({ d, busy, access }: { d: DatabaseInstance; busy: boolean; access: PublicAccess }) {
  const t = useT().databases
  const qc = useQueryClient()
  const [port, setPort] = useState(String(access.port || access.suggested_port || ''))
  const [allow, setAllow] = useState(access.allow.split(',').filter(Boolean).join('\n'))
  const open = access.port > 0
  const ready = d.status === 'running' && d.runtime === 'running' && d.desired_state === 'running'
  const save = useMutation({
    mutationFn: (next: number) => withToast(() => api<PublicAccess>(`/databases/${d.id}/public`, { method: 'PUT', body: { port: next, allow } }), {
      loading: t.publicSaving, success: t.publicSaved, error: t.publicSaveFailed,
    }),
    onSuccess: (next) => {
      qc.setQueryData(databasePublicQuery(d.id).queryKey, next)
      qc.invalidateQueries({ queryKey: databaseQuery(d.id).queryKey })
    },
  })
  const changed = !open || Number(port) !== access.port || allow.split(/\s+/).filter(Boolean).join(',') !== access.allow
  const anyone = allow.trim() === ''
  return <div className="space-y-4">
    <div className="flex flex-wrap items-center gap-2 text-sm">
      {open ? <><span>{t.publicOn(address(access.host, access.port))}</span><Badge tone={access.gate === 'running' ? 'muted' : 'danger'}>{access.gate}</Badge></> : <span className="text-muted">{t.publicOff}</span>}
    </div>
    {open && <p className="text-xs text-muted">{t.publicAllowedFrom}: {access.allow ? access.allow.split(',').join(', ') : t.publicAnyone}</p>}
    {open && access.gate !== 'running' && <ErrorNote>{t.publicGateDown}</ErrorNote>}
    {!access.available && <p className="text-xs text-muted">{t.publicUnavailable}</p>}
    {access.available && <>
      <Field name="public-port" type="number" min={1024} max={65535} required label={t.publicPort} value={port} onChange={(e) => setPort(e.target.value)} />
      <div className="space-y-1.5">
        <label htmlFor="public-allow" className="text-xs text-muted">{t.publicAllow}</label>
        <textarea id="public-allow" dir="ltr" rows={3} spellCheck={false} placeholder={t.publicAllowPlaceholder} value={allow} onChange={(e) => setAllow(e.target.value)}
          className="w-full border border-line bg-bg px-3 py-2 font-mono text-sm outline-none placeholder:text-muted/70 focus:border-accent" />
      </div>
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      {!ready && <p className="text-xs text-muted">{t.requiresRunning}</p>}
      <div className="flex flex-wrap gap-2">
        <ConfirmDialog tone="primary" label={open ? t.savePublic : t.openPublic} title={t.openPublicTitle}
          message={anyone ? t.openPublicAnyoneMessage : t.openPublicMessage} confirm={open ? t.savePublic : t.openPublic}
          disabled={busy || !ready || !changed || !Number(port)} pending={save.isPending && save.variables !== 0} onConfirm={() => save.mutate(Number(port))} />
        {open && <ConfirmDialog label={t.closePublic} title={t.closePublicTitle} message={t.closePublicMessage} confirm={t.closePublic}
          disabled={busy} pending={save.isPending && save.variables === 0} onConfirm={() => save.mutate(0)} />}
      </div>
    </>}
  </div>
}

/** Connection tab: how external clients connect, when the public port is open. */
export function PublicConnection({ d, copy }: { d: DatabaseInstance; copy: (value: string) => React.ReactNode }) {
  const t = useT().databases
  const access = useQuery({ ...databasePublicQuery(d.id), enabled: d.public_port > 0 })
  if (!d.public_port || !access.data) return null
  const host = access.data.host || 'server'
  const example = d.engine === 'postgres'
    ? `psql "host=${host} port=${d.public_port} dbname=${d.database_name} user=${d.username} sslmode=require"`
    : `mariadb --host=${host} --port=${d.public_port} --ssl --user=${d.username} -p ${d.database_name}`
  return <Panel title={t.publicConnection}><div className="space-y-4 p-4">
    <div><p className="mb-1 text-xs text-muted">{t.publicExample}</p>{copy(example)}</div>
    <p className="text-xs text-muted">{t.publicCertificateHint} <a className="text-accent hover:underline" href={`/api/databases/${d.id}/public/certificate`} download>{t.downloadCertificate} ↓</a></p>
  </div></Panel>
}
