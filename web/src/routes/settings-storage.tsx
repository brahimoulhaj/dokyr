import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Button, CheckOption, ConfirmButton, Empty, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { api, s3ConnectionsQuery, type S3Connection } from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet, SheetActions } from '../components/sheet'
import { useCan } from '../lib/roles'

/** Settings → Storage: S3-compatible connections — add (checked by writing a small file),
 * test again, remove. */
export function StorageSettings() {
  const conns = useQuery(s3ConnectionsQuery)
  const [adding, setAdding] = useState(false)
  const t = useT().settingsStorage
  return (
    <div className="space-y-3">
      <p className="text-sm text-muted">{t.intro}</p>
      <QueryState loading={conns.isPending} error={conns.error} what={t.what} />
      {conns.data && (
        <Panel
          title={t.what}
          actions={
            <Button variant="ghost" onClick={() => setAdding(true)}>
              {t.add}
            </Button>
          }
        >
          {conns.data.length === 0 ? (
            <Empty title={t.empty}>{t.emptyHint}</Empty>
          ) : (
            <ul className="divide-y divide-line">
              {conns.data.map((c) => (
                <S3Row key={c.id} conn={c} />
              ))}
            </ul>
          )}
        </Panel>
      )}
      <Sheet open={adding} onOpenChange={setAdding} title={t.add} description={t.intro}>
        {adding && <AddS3 onDone={() => setAdding(false)} />}
      </Sheet>
    </div>
  )
}

function AddS3({ onDone }: { onDone: () => void }) {
  const queryClient = useQueryClient()
  const tt = useT()
  const t = tt.settingsStorage
  const [form, setForm] = useState({ name: '', endpoint: '', region: '', bucket: '', prefix: '', access_key: '', secret_key: '' })
  const [pathStyle, setPathStyle] = useState(false)
  const set = (key: keyof typeof form) => (e: { target: { value: string } }) => setForm({ ...form, [key]: e.target.value })
  const add = useMutation({
    mutationFn: () => api<S3Connection>('/s3-connections', { method: 'POST', body: { ...form, path_style: pathStyle } }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: s3ConnectionsQuery.queryKey })
      onDone()
    },
  })
  function submit(e: FormEvent) {
    e.preventDefault()
    add.mutate()
  }
  const ready = form.endpoint.trim() && form.bucket.trim() && form.access_key.trim() && form.secret_key
  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <div className="space-y-5">
        <Field label={t.endpoint} name="s3-endpoint" required dir="ltr" placeholder="s3.amazonaws.com" hintTooltip hint={t.endpointHint}
          value={form.endpoint} onChange={set('endpoint')} autoComplete="off" spellCheck={false} />
        <Field label={t.bucket} name="s3-bucket" required dir="ltr" value={form.bucket} onChange={set('bucket')} autoComplete="off" spellCheck={false} />
        <Field label={t.accessKey} name="s3-access-key" required dir="ltr" value={form.access_key} onChange={set('access_key')} autoComplete="off" spellCheck={false} />
        <Field label={t.secretKey} name="s3-secret-key" type="password" required dir="ltr" value={form.secret_key} onChange={set('secret_key')} autoComplete="new-password" />
        <Field label={t.region} name="s3-region" dir="ltr" placeholder="us-east-1" value={form.region} onChange={set('region')} autoComplete="off" spellCheck={false} />
        <Field label={t.prefix} name="s3-prefix" dir="ltr" placeholder="panel/registry" hintTooltip hint={t.prefixHint} value={form.prefix} onChange={set('prefix')} autoComplete="off" spellCheck={false} />
        <Field label={t.name} name="s3-name" maxLength={64} value={form.name} onChange={set('name')} autoComplete="off" />
      </div>
      <CheckOption checked={pathStyle} onChange={setPathStyle} label={t.pathStyle} hint={t.pathStyleHint} />
      {add.error && <ErrorNote>{add.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={add.isPending || !ready}>
          {add.isPending ? t.checking : t.save}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {tt.common.cancel}
        </Button>
      </SheetActions>
    </form>
  )
}

function S3Row({ conn }: { conn: S3Connection }) {
  const can = useCan()
  const queryClient = useQueryClient()
  const tt = useT()
  const t = tt.settingsStorage
  const remove = useMutation({
    mutationFn: () => api<void>(`/s3-connections/${conn.id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: s3ConnectionsQuery.queryKey }),
  })
  const test = useMutation({ mutationFn: () => api<{ ok: boolean }>(`/s3-connections/${conn.id}/test`, { method: 'POST' }) })
  const where = `${conn.use_ssl ? 'https' : 'http'}://${conn.endpoint}/${conn.bucket}${conn.prefix ? '/' + conn.prefix : ''}`
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm">{conn.name}</p>
        <p className="text-xs text-muted">
          <span dir="ltr" className="[overflow-wrap:anywhere]">{where}</span> ·{' '}
          <span title={dateTime(conn.created_at)}>{t.added(timeAgo(conn.created_at))}</span>
        </p>
        {test.isSuccess && <p className="text-xs text-accent">{t.testOk}</p>}
        {test.error && <p className="text-xs text-danger">{test.error.message}</p>}
        {remove.error && <p className="text-xs text-danger">{remove.error.message}</p>}
      </div>
      <div className="flex gap-2">
        <Button variant="ghost" disabled={test.isPending} onClick={() => test.mutate()}>
          {test.isPending ? t.testing : t.test}
        </Button>
        {can.remove && <ConfirmButton label={tt.common.remove} confirm={tt.common.yesRemove} pending={remove.isPending} onConfirm={() => remove.mutate()} />}
      </div>
    </li>
  )
}
