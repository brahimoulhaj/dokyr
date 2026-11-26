import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { ConfirmDialog } from '../components/confirm-dialog'
import { RoutingBoard } from '../components/routing'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, CheckOption, ErrorNote, Field, PageHeader, Panel } from '../components/ui'
import { api, hostHref, objectStoreQuery, panelSettingsQuery, registrySettingsQuery } from '../lib/api'
import { useT } from '../lib/i18n'

/** The Routing page: everything the proxy sends somewhere. Routes to apps are edited here
 * (and on each app); the hostnames the panel answers on itself are listed for reference. */
export function RoutingPage() {
  const t = useT()
  return (
    <div className="space-y-6">
      <PageHeader title={t.routing.title} />
      <RoutingBoard intro={t.routing.intro} />
      <Redirects />
      <BuiltInHosts />
    </div>
  )
}

/** The panel, the built-in registry and the built-in storage have hostnames of their own.
 * They are not routes to apps, but they are part of what the proxy serves. */
function BuiltInHosts() {
  const t = useT()
  const tr = t.routing
  const panel = useQuery({ ...panelSettingsQuery, retry: false }).data
  const registry = useQuery({ ...registrySettingsQuery, retry: false }).data
  const storage = useQuery({ ...objectStoreQuery, retry: false }).data
  const rows = [
    panel?.domain ? { host: panel.domain, httpOnly: panel.tls_mode === 'http_only', label: tr.panel, to: '/settings' as const } : null,
    registry?.enabled && registry.domain ? { host: registry.domain, httpOnly: registry.tls_mode === 'http_only', label: tr.registry, to: '/registry/builtin' as const } : null,
    storage?.enabled && storage.domain ? { host: storage.domain, httpOnly: storage.tls_mode === 'http_only', label: tr.storage, to: '/storage' as const } : null,
  ].filter((r) => r !== null)
  if (rows.length === 0) return null
  return (
    <Panel title={tr.builtIn}>
      <p className="border-b border-line px-4 py-3 text-xs text-muted">{tr.builtInHint}</p>
      <ul className="divide-y divide-line">
        {rows.map((r) => (
          <li key={r.host} className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3 text-sm">
            <span className="flex min-w-0 flex-wrap items-center gap-3">
              <a href={hostHref(r.host, r.httpOnly)} target="_blank" rel="noreferrer" dir="ltr" className="text-fg [overflow-wrap:anywhere] hover:text-accent">{r.host}</a>
              <span aria-hidden className="text-muted rtl:-scale-x-100">→</span>
              <span className="text-muted">{r.label}</span>
              {r.httpOnly && <Badge tone="muted">{t.common.httpOnly}</Badge>}
            </span>
            <Link to={r.to} className="text-xs text-muted hover:text-fg">{tr.configure}</Link>
          </li>
        ))}
      </ul>
    </Panel>
  )
}

type Redirect = { id: string; hostname: string; target: string; tls_mode: string; created_at: string }

const redirectsKey = ['redirects'] as const

/** Hostnames that only send their visitors to another one (www to the apex, an old name to the new). */
function Redirects() {
  const t = useT()
  const tr = t.routing
  const qc = useQueryClient()
  const [adding, setAdding] = useState(false)
  const redirects = useQuery({ queryKey: redirectsKey, queryFn: () => api<Redirect[]>('/redirects') })
  const remove = useMutation({
    mutationFn: (id: string) => api<void>(`/redirects/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    onSuccess: () => { toast.success(tr.redirectRemoved) },
    onError: (e) => { toast.error(e.message) },
    onSettled: () => { void qc.invalidateQueries({ queryKey: redirectsKey }) },
  })
  return (
    <>
      <Panel title={tr.redirects} actions={<Button type="button" variant="ghost" onClick={() => setAdding(true)}>{tr.redirectAdd}</Button>}>
        <p className="border-b border-line px-4 py-3 text-xs text-muted">{tr.redirectsHint}</p>
        {redirects.error ? (
          <div className="p-4"><ErrorNote>{redirects.error.message}</ErrorNote></div>
        ) : !redirects.data?.length ? (
          <p className="px-4 py-6 text-center text-sm text-muted">{tr.redirectNone}</p>
        ) : (
          <ul className="divide-y divide-line">
            {redirects.data.map((r) => (
              <li key={r.id} className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3 text-sm">
                <span className="flex min-w-0 flex-wrap items-center gap-3">
                  <a href={hostHref(r.hostname, r.tls_mode === 'http_only')} target="_blank" rel="noreferrer" dir="ltr" className="text-fg [overflow-wrap:anywhere] hover:text-accent">{r.hostname}</a>
                  <span aria-hidden className="text-muted rtl:-scale-x-100">→</span>
                  <span dir="ltr" className="text-muted [overflow-wrap:anywhere]">{r.target}</span>
                  {r.tls_mode === 'http_only' && <Badge tone="muted">{t.common.httpOnly}</Badge>}
                </span>
                <ConfirmDialog label={tr.redirectRemove} title={tr.redirectRemoveTitle} message={tr.redirectRemoveMessage(r.hostname)} confirm={tr.redirectRemove}
                  pending={remove.isPending && remove.variables === r.id} disabled={remove.isPending} onConfirm={() => remove.mutate(r.id)} />
              </li>
            ))}
          </ul>
        )}
      </Panel>
      <Sheet open={adding} onOpenChange={setAdding} title={tr.redirectAdd} description={tr.redirectSheet}>
        {adding && <RedirectForm onDone={() => setAdding(false)} />}
      </Sheet>
    </>
  )
}

function RedirectForm({ onDone }: { onDone: () => void }) {
  const t = useT()
  const tr = t.routing
  const qc = useQueryClient()
  const [hostname, setHostname] = useState('')
  const [target, setTarget] = useState('')
  const [httpOnly, setHttpOnly] = useState(false)
  const save = useMutation({
    mutationFn: () => api<Redirect>('/redirects', { method: 'POST', body: { hostname, target, tls_mode: httpOnly ? 'http_only' : 'auto' } }),
    onSuccess: () => {
      toast.success(tr.redirectAdded)
      void qc.invalidateQueries({ queryKey: redirectsKey })
      onDone()
    },
  })
  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }
  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <Field name="redirect-hostname" dir="ltr" required autoFocus placeholder="www.example.com" autoComplete="off" spellCheck={false}
        label={tr.redirectFrom} hint={tr.redirectFromHint} hintTooltip value={hostname} onChange={(e) => setHostname(e.target.value)} />
      <Field name="redirect-target" dir="ltr" required placeholder="example.com" autoComplete="off" spellCheck={false}
        label={tr.redirectTo} hint={tr.redirectToHint} hintTooltip value={target} onChange={(e) => setTarget(e.target.value)} />
      <CheckOption checked={httpOnly} onChange={setHttpOnly} label={tr.redirectHttpOnly} hint={tr.redirectHttpOnlyHint} />
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!hostname.trim() || !target.trim() || save.isPending}>{save.isPending ? t.common.saving : tr.redirectAdd}</Button>
        <Button type="button" variant="ghost" disabled={save.isPending} onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
