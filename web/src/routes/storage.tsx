import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, Empty, ErrorNote, Field, HttpOnlyChoice, PageHeader, Panel, QueryState } from '../components/ui'
import { api } from '../lib/api'
import { dateTime, formatBytes, timeAgo } from '../lib/format'
import { locale, useT } from '../lib/i18n'
import { poll } from '../lib/live'

type ObjectStore = {
  enabled: boolean
  domain: string
  tls_mode: 'auto' | 'http_only'
  state: string
  endpoint: string
  connection_id: string
  access_key: string
}
type Bucket = { name: string; created_at: string; size: number; objects: number; truncated: boolean }

// Settings-table changes (turning it on, the connection id) refetch it over the change stream.
const settingsKey = ['settings', 'object-store'] as const
const bucketsKey = ['object-store', 'buckets'] as const
const toastId = 'object-store'

export function StoragePage() {
  const t = useT()
  const ts = t.storage
  const qc = useQueryClient()
  const store = useQuery({
    queryKey: settingsKey,
    queryFn: () => api<ObjectStore>('/object-store'),
    // The container's state comes from Docker, not the database: poll until it settles.
    refetchInterval: (q) => (q.state.data?.enabled && q.state.data.state !== 'running' ? poll(3000)() : false),
  })
  const [sheet, setSheet] = useState<'setup' | 'credentials' | 'bucket' | null>(null)
  const [confirmOff, setConfirmOff] = useState(false)

  const turnOff = useMutation({
    mutationFn: () => api<ObjectStore>('/object-store', { method: 'PUT', body: { enabled: false, domain: store.data?.domain ?? '', tls_mode: store.data?.tls_mode } }),
    onSuccess: (next) => {
      qc.setQueryData(settingsKey, next)
      toast.success(ts.stoppedToast, { id: toastId })
    },
    onError: (e) => toast.error(ts.failedToast, { id: toastId, description: e.message }),
  })

  const data = store.data
  return (
    <div className="space-y-6">
      <PageHeader
        title={<>{ts.title} <Badge tone="warn">{ts.experimental}</Badge></>}
        subtitle={ts.intro}
        actions={data && (data.enabled ? (
          <div className="flex flex-wrap gap-2">
            <Button type="button" variant="ghost" onClick={() => setSheet('credentials')}>{ts.credentials}</Button>
            <Button type="button" variant="ghost" onClick={() => setSheet('setup')}>{ts.edit}</Button>
            <Button type="button" variant="danger" disabled={turnOff.isPending} onClick={() => setConfirmOff(true)}>
              {turnOff.isPending ? t.common.working : ts.turnOff}
            </Button>
          </div>
        ) : (
          <Button type="button" onClick={() => setSheet('setup')}>{ts.turnOn}</Button>
        ))}
      />

      {!data ? (
        <QueryState loading={store.isPending} error={store.error} what={ts.what} />
      ) : !data.enabled ? (
        <Panel><Empty title={ts.off} /></Panel>
      ) : (
        <>
          <Panel>
            <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-2">
              <Row label={ts.state}><StateBadge state={data.state} /></Row>
              <Row label={ts.endpoint}><code dir="ltr">{data.endpoint}</code></Row>
              <Row label={ts.publicUrl}>
                {data.domain ? <code dir="ltr">{`${data.tls_mode === 'http_only' ? 'http' : 'https'}://${data.domain}`}</code> : <span className="text-muted">{ts.noDomain}</span>}
              </Row>
              <Row label={ts.accessKey}><code dir="ltr">{data.access_key}</code></Row>
              <Row label={ts.connection} hint={ts.connectionHint}>
                {data.connection_id
                  ? <Link to="/settings" search={{ tab: 'storage' }} className="text-accent hover:underline">Built-in storage</Link>
                  : <span className="text-muted">{ts.noConnection}</span>}
              </Row>
            </dl>
          </Panel>
          {data.state === 'running' && <Buckets onCreate={() => setSheet('bucket')} />}
        </>
      )}

      <Sheet open={sheet === 'setup'} onOpenChange={(o) => !o && setSheet(null)} title={ts.setupTitle} description={ts.setupDescription}>
        {data && <SetupForm current={data} onDone={() => setSheet(null)} />}
      </Sheet>
      <Sheet open={sheet === 'credentials'} onOpenChange={(o) => !o && setSheet(null)} title={ts.credentialsTitle} description={ts.credentialsDescription}>
        {data && <Credentials accessKey={data.access_key} />}
      </Sheet>
      <Sheet open={sheet === 'bucket'} onOpenChange={(o) => !o && setSheet(null)} title={ts.bucketTitle}>
        <NewBucket onDone={() => setSheet(null)} />
      </Sheet>
      <ConfirmModal open={confirmOff} onOpenChange={setConfirmOff} title={ts.turnOffTitle} message={ts.turnOffMessage}
        confirm={ts.turnOff} onConfirm={() => { setConfirmOff(false); turnOff.mutate() }} />
    </div>
  )
}

function Row({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted" title={hint}>{label}</dt>
      <dd className="mt-0.5 truncate">{children}</dd>
    </div>
  )
}

function StateBadge({ state }: { state: string }) {
  const ts = useT().storage
  return <Badge tone={state === 'running' ? 'accent' : state === 'off' ? 'muted' : 'warn'}>{ts.states[state] ?? state}</Badge>
}

function SetupForm({ current, onDone }: { current: ObjectStore; onDone: () => void }) {
  const t = useT()
  const ts = t.storage
  const qc = useQueryClient()
  const [domain, setDomain] = useState(current.domain)
  const [httpOnly, setHttpOnly] = useState(current.tls_mode === 'http_only')
  const save = useMutation({
    mutationFn: () => api<ObjectStore>('/object-store', { method: 'PUT', body: { enabled: true, domain: domain.trim(), tls_mode: httpOnly ? 'http_only' : 'auto' } }),
    onMutate: () => { if (!current.enabled) toast.loading(ts.startingToast, { id: toastId }) },
    onSuccess: (next) => {
      qc.setQueryData(settingsKey, next)
      void qc.invalidateQueries({ queryKey: ['s3-connections'] })
      toast.success(ts.startedToast, { id: toastId })
      onDone()
    },
    onError: (e) => toast.error(ts.failedToast, { id: toastId, description: e.message }),
  })
  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }
  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <Field name="s3-domain" dir="ltr" label={ts.domain} hint={ts.domainHint} hintTooltip placeholder="s3.example.com"
        value={domain} onChange={(e) => setDomain(e.target.value)} autoComplete="off" spellCheck={false} />
      {domain.trim() !== '' && <HttpOnlyChoice checked={httpOnly} onChange={setHttpOnly} />}
      {!current.enabled && <p className="border border-warn/40 bg-warn/10 px-3 py-2 text-xs text-fg">{ts.experimentalWarning}</p>}
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={save.isPending}>{save.isPending ? t.common.working : current.enabled ? ts.save : ts.turnOn}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}

function Credentials({ accessKey }: { accessKey: string }) {
  const t = useT()
  const ts = t.storage
  const reveal = useMutation({ mutationFn: () => api<{ access_key: string; secret_key: string }>('/object-store/credentials') })
  const block = 'block overflow-x-auto whitespace-pre border border-line bg-bg px-3 py-2 text-xs'
  return (
    <div className="space-y-5 p-5 text-sm">
      <div className="space-y-1.5"><p className="text-xs text-muted">{ts.accessKey}</p><code dir="ltr" className={block}>{accessKey}</code></div>
      <div className="space-y-1.5">
        <p className="text-xs text-muted">{ts.secretKey}</p>
        {reveal.data
          ? <code dir="ltr" className={block}>{reveal.data.secret_key}</code>
          : <Button type="button" variant="ghost" disabled={reveal.isPending} onClick={() => reveal.mutate()}>{reveal.isPending ? t.common.working : ts.reveal}</Button>}
      </div>
      {reveal.error && <ErrorNote>{reveal.error.message}</ErrorNote>}
    </div>
  )
}

function Buckets({ onCreate }: { onCreate: () => void }) {
  const ts = useT().storage
  const qc = useQueryClient()
  const buckets = useQuery({ queryKey: bucketsKey, queryFn: () => api<Bucket[]>('/object-store/buckets'), retry: 2 })
  const [deleting, setDeleting] = useState<string | null>(null)
  const remove = useMutation({
    mutationFn: (name: string) => api<void>(`/object-store/buckets/${encodeURIComponent(name)}`, { method: 'DELETE' }),
    onSuccess: (_, name) => {
      toast.success(ts.bucketDeletedToast(name), { id: toastId })
      void qc.invalidateQueries({ queryKey: bucketsKey })
    },
    onError: (e) => toast.error(ts.failedToast, { id: toastId, description: e.message }),
  })
  const count = (b: Bucket) => {
    const n = new Intl.NumberFormat(locale()).format(b.objects)
    return b.truncated ? ts.more(n) : n
  }
  return (
    <Panel title={ts.buckets} actions={<Button type="button" variant="ghost" onClick={onCreate}>{ts.newBucket}</Button>}>
      {!buckets.data ? (
        <div className="p-4"><QueryState loading={buckets.isPending} error={buckets.error} what={ts.bucketsWhat} /></div>
      ) : buckets.data.length === 0 ? (
        <Empty title={ts.noBuckets} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-xs text-muted">
              <tr className="border-b border-line">
                <th className="px-4 py-2 text-start font-normal">{ts.name}</th>
                <th className="px-4 py-2 text-end font-normal">{ts.objects}</th>
                <th className="px-4 py-2 text-end font-normal">{ts.size}</th>
                <th className="px-4 py-2 text-start font-normal">{ts.created}</th>
                <th className="px-4 py-2"><span className="sr-only">{ts.actions}</span></th>
              </tr>
            </thead>
            <tbody>
              {buckets.data.map((b) => (
                <tr key={b.name} className="border-b border-line last:border-0">
                  <td className="px-4 py-2"><code dir="ltr">{b.name}</code></td>
                  <td className="px-4 py-2 text-end text-muted">{count(b)}</td>
                  <td className="px-4 py-2 text-end text-muted">{formatBytes(b.size)}</td>
                  <td className="px-4 py-2 text-muted" title={dateTime(b.created_at)}>{timeAgo(b.created_at)}</td>
                  <td className="px-4 py-2 text-end">
                    <ActionsMenu label={ts.actions} ariaLabel={`${ts.actions}: ${b.name}`}
                      items={[{ label: ts.delete, danger: true, onSelect: () => setDeleting(b.name) }]} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmModal open={deleting !== null} onOpenChange={(o) => !o && setDeleting(null)} title={ts.deleteTitle(deleting ?? '')}
        message={ts.deleteMessage} confirm={ts.delete} onConfirm={() => { if (deleting) remove.mutate(deleting); setDeleting(null) }} />
    </Panel>
  )
}

function NewBucket({ onDone }: { onDone: () => void }) {
  const t = useT()
  const ts = t.storage
  const qc = useQueryClient()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => api<void>('/object-store/buckets', { method: 'POST', body: { name: name.trim() } }),
    onSuccess: () => {
      toast.success(ts.bucketCreatedToast(name.trim()), { id: toastId })
      void qc.invalidateQueries({ queryKey: bucketsKey })
      onDone()
    },
  })
  return (
    <form onSubmit={(e) => { e.preventDefault(); create.mutate() }} className="space-y-5 p-5">
      <Field name="bucket-name" dir="ltr" required label={ts.name} hint={ts.bucketHint} hintTooltip pattern="[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]"
        value={name} onChange={(e) => setName(e.target.value.toLowerCase())} autoComplete="off" spellCheck={false} />
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={create.isPending}>{create.isPending ? t.common.working : ts.create}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
