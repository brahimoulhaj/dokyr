import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, cx, Empty, ErrorNote, Field, HttpOnlyChoice, PageHeader, Panel, QueryState } from '../components/ui'
import { api, objectStoreQuery, type ObjectStore } from '../lib/api'
import { dateTime, formatBytes, timeAgo } from '../lib/format'
import { locale, useT } from '../lib/i18n'
import { poll } from '../lib/live'
import { useCan } from '../lib/roles'

type Bucket = { name: string; created_at: string; size: number; objects: number; truncated: boolean }
type AccessKey = { id: string; name: string; bucket: string; access_key: string; permission: 'read' | 'write'; created_at: string }
type NewAccessKey = AccessKey & { secret_key: string }

const settingsKey = objectStoreQuery.queryKey
const bucketsKey = ['object-store', 'buckets'] as const
const keysKey = ['object-store', 'keys'] as const

export const storageTabs = ['overview', 'buckets', 'keys'] as const
export type StorageTab = (typeof storageTabs)[number]
const toastId = 'object-store'

export function StoragePage() {
  const t = useT()
  const ts = t.storage
  const qc = useQueryClient()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: string }
  const tab: StorageTab = storageTabs.includes(search.tab as StorageTab) ? (search.tab as StorageTab) : 'overview'
  const store = useQuery({
    ...objectStoreQuery,
    // The container's state comes from Docker, not the database: poll until it settles.
    refetchInterval: (q) => (q.state.data?.enabled && q.state.data.state !== 'running' ? poll(3000)() : false),
  })
  const [sheet, setSheet] = useState<'setup' | 'bucket' | 'key' | null>(null)
  const [keyBucket, setKeyBucket] = useState('')
  const [createdKey, setCreatedKey] = useState<NewAccessKey | null>(null)
  const newKey = (bucket = '') => { setKeyBucket(bucket); setCreatedKey(null); setSheet('key') }
  // A key made from a bucket's menu shows up on the Access keys tab: go there once it is seen.
  const closeKey = () => {
    setSheet(null)
    if (createdKey && tab !== 'keys') navigate({ to: '/storage', search: { tab: 'keys' } })
  }
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
          <nav className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line" aria-label={ts.title}>
            {storageTabs.map((name) => (
              <Link
                key={name}
                to="/storage"
                search={{ tab: name }}
                className={cx('border-b-2 px-3 py-1.5 text-sm whitespace-nowrap', name === tab ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg')}
                aria-current={name === tab ? 'page' : undefined}
              >
                {ts.tabs[name]}
              </Link>
            ))}
          </nav>
          {tab === 'overview' && <Panel>
            <dl className="grid gap-x-6 gap-y-3 p-4 text-sm sm:grid-cols-2">
              <Row label={ts.state}><StateBadge state={data.state} /></Row>
              <Row label={ts.endpoint}><code dir="ltr">{data.endpoint}</code></Row>
              <Row label={ts.publicUrl}>
                {data.domain ? <code dir="ltr">{`${data.tls_mode === 'http_only' ? 'http' : 'https'}://${data.domain}`}</code> : <span className="text-muted">{ts.noDomain}</span>}
              </Row>
              <Row label={ts.connection} hint={ts.connectionHint}>
                {data.connection_id
                  ? <Link to="/settings" search={{ tab: 'storage' }} className="text-accent hover:underline">Built-in storage</Link>
                  : <span className="text-muted">{ts.noConnection}</span>}
              </Row>
            </dl>
          </Panel>}
          {tab !== 'overview' && data.state !== 'running' && (
            <Panel><Empty title={ts.notRunning}><StateBadge state={data.state} /></Empty></Panel>
          )}
          {tab === 'buckets' && data.state === 'running' && <Buckets onCreate={() => setSheet('bucket')} onNewKey={newKey} />}
          {tab === 'keys' && data.state === 'running' && <AccessKeys onCreate={() => newKey()} />}
        </>
      )}

      <Sheet open={sheet === 'setup'} onOpenChange={(o) => !o && setSheet(null)} title={ts.setupTitle} description={ts.setupDescription}>
        {data && <SetupForm current={data} onDone={() => setSheet(null)} />}
      </Sheet>
      <Sheet open={sheet === 'bucket'} onOpenChange={(o) => !o && setSheet(null)} title={ts.bucketTitle}>
        <NewBucket onDone={() => setSheet(null)} />
      </Sheet>
      <Sheet open={sheet === 'key'} onOpenChange={(o) => !o && closeKey()}
        title={createdKey ? ts.keyCreatedTitle : ts.keyTitle} description={createdKey ? ts.keyCreatedDescription : ts.keyDescription}>
        {sheet === 'key' && data && (createdKey
          ? <KeyCreated created={createdKey} endpoint={data.domain ? `${data.tls_mode === 'http_only' ? 'http' : 'https'}://${data.domain}` : `http://${data.endpoint}`} onDone={closeKey} />
          : <NewKeyForm bucket={keyBucket} onCreated={setCreatedKey} onCancel={() => setSheet(null)} />)}
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

function Buckets({ onCreate, onNewKey }: { onCreate: () => void; onNewKey: (bucket: string) => void }) {
  const ts = useT().storage
  const can = useCan()
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
                      items={[
                        { label: ts.newKeyFor, onSelect: () => onNewKey(b.name) },
                        ...(can.remove ? [{ label: ts.delete, danger: true, onSelect: () => setDeleting(b.name) }] : []),
                      ]} />
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

function AccessKeys({ onCreate }: { onCreate: () => void }) {
  const ts = useT().storage
  const can = useCan()
  const qc = useQueryClient()
  const keys = useQuery({ queryKey: keysKey, queryFn: () => api<AccessKey[]>('/object-store/keys') })
  const [deleting, setDeleting] = useState<AccessKey | null>(null)
  const remove = useMutation({
    mutationFn: (k: AccessKey) => api<void>(`/object-store/keys/${k.id}`, { method: 'DELETE' }),
    onSuccess: (_, k) => {
      toast.success(ts.keyDeletedToast(k.name), { id: toastId })
      void qc.invalidateQueries({ queryKey: keysKey })
    },
    onError: (e) => toast.error(ts.failedToast, { id: toastId, description: e.message }),
  })
  return (
    <Panel title={ts.keys} actions={<Button type="button" variant="ghost" onClick={onCreate}>{ts.newKey}</Button>}>
      {!keys.data ? (
        <div className="p-4"><QueryState loading={keys.isPending} error={keys.error} what={ts.keysWhat} /></div>
      ) : keys.data.length === 0 ? (
        <Empty title={ts.noKeys} />
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="text-xs text-muted">
              <tr className="border-b border-line">
                <th className="px-4 py-2 text-start font-normal">{ts.name}</th>
                <th className="px-4 py-2 text-start font-normal">{ts.bucket}</th>
                <th className="px-4 py-2 text-start font-normal">{ts.permission}</th>
                <th className="px-4 py-2 text-start font-normal">{ts.accessKey}</th>
                <th className="px-4 py-2 text-start font-normal">{ts.created}</th>
                <th className="px-4 py-2"><span className="sr-only">{ts.actions}</span></th>
              </tr>
            </thead>
            <tbody>
              {keys.data.map((k) => (
                <tr key={k.id} className="border-b border-line last:border-0">
                  <td className="px-4 py-2">{k.name}</td>
                  <td className="px-4 py-2"><code dir="ltr">{k.bucket}</code></td>
                  <td className="px-4 py-2"><Badge tone={k.permission === 'write' ? 'warn' : 'muted'}>{ts.permissions[k.permission] ?? k.permission}</Badge></td>
                  <td className="px-4 py-2 text-muted"><code dir="ltr">{k.access_key}</code></td>
                  <td className="px-4 py-2 text-muted" title={dateTime(k.created_at)}>{timeAgo(k.created_at)}</td>
                  <td className="px-4 py-2 text-end">
                    {can.remove && <ActionsMenu label={ts.actions} ariaLabel={`${ts.actions}: ${k.name}`}
                      items={[{ label: ts.delete, danger: true, onSelect: () => setDeleting(k) }]} />}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmModal open={deleting !== null} onOpenChange={(o) => !o && setDeleting(null)} title={ts.deleteKeyTitle(deleting?.name ?? '')}
        message={ts.deleteKeyMessage} confirm={ts.delete} onConfirm={() => { if (deleting) remove.mutate(deleting); setDeleting(null) }} />
    </Panel>
  )
}

function NewKeyForm({ bucket: initial, onCreated, onCancel }: { bucket: string; onCreated: (k: NewAccessKey) => void; onCancel: () => void }) {
  const t = useT()
  const ts = t.storage
  const qc = useQueryClient()
  const buckets = useQuery({ queryKey: bucketsKey, queryFn: () => api<Bucket[]>('/object-store/buckets') })
  const [name, setName] = useState('')
  const [bucket, setBucket] = useState(initial)
  const [permission, setPermission] = useState<'read' | 'write'>('read')
  const chosen = bucket || buckets.data?.[0]?.name || ''
  const create = useMutation({
    mutationFn: () => api<NewAccessKey>('/object-store/keys', { method: 'POST', body: { name: name.trim(), bucket: chosen, permission } }),
    onSuccess: (k) => {
      toast.success(ts.keyCreatedToast(k.name), { id: toastId })
      void qc.invalidateQueries({ queryKey: keysKey })
      onCreated(k)
    },
  })
  const none = buckets.data?.length === 0
  return (
    <form onSubmit={(e) => { e.preventDefault(); create.mutate() }} className="space-y-5 p-5">
      <Field name="key-name" required maxLength={64} label={ts.keyName} hint={ts.keyNameHint} hintTooltip placeholder="website"
        value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" autoFocus />
      <label className="block space-y-1.5">
        <span className="block text-xs text-muted">{ts.bucket}</span>
        <select required dir="ltr" className="h-9 w-full border border-line bg-bg px-3 text-sm text-fg" value={chosen}
          disabled={!buckets.data || none} onChange={(e) => setBucket(e.target.value)}>
          {buckets.data?.map((b) => <option key={b.name} value={b.name}>{b.name}</option>)}
        </select>
        {none && <span className="block text-xs text-warn">{ts.noBucketsForKey}</span>}
      </label>
      <fieldset className="space-y-2">
        <legend className="mb-1.5 text-xs text-muted">{ts.permission}</legend>
        {(['read', 'write'] as const).map((p) => (
          <label key={p} className={cx('flex cursor-pointer gap-3 border p-3', permission === p ? 'border-accent' : 'border-line hover:border-line-strong')}>
            <input type="radio" name="key-permission" value={p} checked={permission === p} onChange={() => setPermission(p)} className="mt-0.5 accent-(--color-accent)" />
            <span className="min-w-0">
              <span className="block text-sm">{ts.permissions[p]}</span>
              <span className="mt-0.5 block text-xs text-muted">{ts.permissionHints[p]}</span>
            </span>
          </label>
        ))}
      </fieldset>
      {buckets.error && <ErrorNote>{buckets.error.message}</ErrorNote>}
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!name.trim() || !chosen || create.isPending}>{create.isPending ? t.common.working : ts.create}</Button>
        <Button type="button" variant="ghost" onClick={onCancel}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}

/** The key just made, with its secret: shown here once and never again. */
function KeyCreated({ created, endpoint, onDone }: { created: NewAccessKey; endpoint: string; onDone: () => void }) {
  const t = useT()
  const ts = t.storage
  const [copied, setCopied] = useState('')
  const copy = async (what: string, value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(what)
    } catch {
      setCopied('')
    }
  }
  const block = 'block min-w-0 flex-1 overflow-x-auto whitespace-pre border border-line bg-bg px-3 py-2 text-xs'
  const line = (what: string, label: string, value: string, strong = false) => (
    <div className="space-y-1.5">
      <p className="text-xs text-muted">{label}</p>
      <div className="flex gap-2">
        <code dir="ltr" className={cx(block, strong && 'border-accent')}>{value}</code>
        <Button type="button" variant="ghost" onClick={() => copy(what, value)}>{copied === what ? t.common.copied : t.common.copy}</Button>
      </div>
    </div>
  )
  return (
    <div className="space-y-5 p-5 text-sm">
      <p>
        {created.name} · <code dir="ltr">{created.bucket}</code> · {ts.permissions[created.permission]}
      </p>
      {line('access', ts.accessKey, created.access_key)}
      {line('secret', ts.secretKey, created.secret_key, true)}
      {line('endpoint', ts.keyEndpoint, endpoint)}
      <p className="text-xs text-muted">{ts.keyRegion}: <code dir="ltr">us-east-1</code> · {ts.keyStyle}</p>
      <SheetActions>
        <Button type="button" onClick={onDone}>{t.common.done}</Button>
      </SheetActions>
    </div>
  )
}
