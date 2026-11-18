import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { DnsBadge, DnsRecords, dnsRail, dnsState } from '../components/dns'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, cx, Empty, ErrorNote, Field, HttpOnlyChoice, PageHeader, Panel, QueryState } from '../components/ui'
import { allDomainsQuery, api, dnsQuery, hostHref, type Domain } from '../lib/api'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'

/** The Domains page: every hostname the panel serves. A domain is added here once; a
 * project then picks it and routes its paths to apps. */
export function DomainsPage() {
  const t = useT()
  const td = t.domains
  const domains = useQuery(allDomainsQuery)
  const [adding, setAdding] = useState(false)
  const empty = domains.data?.length === 0
  return (
    <div className="space-y-6">
      <PageHeader title={td.title} actions={domains.data && !empty && <Button onClick={() => setAdding(true)}>{td.add}</Button>} />
      <QueryState loading={domains.isPending} error={domains.error} what={td.what} />
      {domains.data && (
        <Panel>
          {empty ? (
            <Empty title={td.none}>
              <p>{td.noneHint}</p>
              <Button className="mt-5" onClick={() => setAdding(true)}>{td.add}</Button>
            </Empty>
          ) : (
            <ul className="divide-y divide-line">
              {domains.data.map((d) => <DomainRow key={d.id} domain={d} />)}
            </ul>
          )}
        </Panel>
      )}
      <Sheet open={adding} onOpenChange={setAdding} title={td.add} description={t.app.noDomainsYetHint}>
        {adding && <AddDomain onDone={() => setAdding(false)} />}
      </Sheet>
    </div>
  )
}

function DomainRow({ domain }: { domain: Domain }) {
  const t = useT()
  const td = t.domains
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const dns = useQuery(dnsQuery(domain.id))
  const state = dnsState(dns)
  const [confirming, setConfirming] = useState(false)
  const httpOnly = domain.tls_mode === 'http_only'
  const host = domain.hostname
  const refresh = () => {
    queryClient.invalidateQueries({ queryKey: allDomainsQuery.queryKey })
    queryClient.invalidateQueries({ queryKey: ['projects'] })
  }
  const remove = useMutation({
    mutationFn: () => withToast(() => api<void>(`/domains/${domain.id}`, { method: 'DELETE' }),
      { loading: td.removing(host), success: td.removed(host), error: td.removeFailed }),
    onSettled: refresh,
  })
  const setMode = useMutation({
    mutationFn: (tls_mode: string) => withToast(() => api<Domain>(`/domains/${domain.id}`, { method: 'PATCH', body: { tls_mode } }),
      { loading: td.switching(host), success: td.switched(host), error: td.switchFailed }),
    onSettled: refresh,
  })
  const busy = remove.isPending || setMode.isPending
  return (
    <li className="relative flex flex-wrap items-center justify-between gap-x-6 gap-y-3 py-4 ps-5 pe-4">
      {/* The rail repeats the DNS state, so a long list can be scanned down its edge. */}
      <span aria-hidden className={cx('absolute inset-y-0 start-0 w-1 transition-colors duration-300', dnsRail[state])} />
      <div className="min-w-0 space-y-2">
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
          <a href={hostHref(host, httpOnly)} target="_blank" rel="noreferrer" dir="ltr"
            className="text-base leading-tight text-fg [overflow-wrap:anywhere] hover:text-accent">{host}</a>
          <DnsBadge state={state} />
          {httpOnly && <Badge tone="muted">{t.common.httpOnly}</Badge>}
        </p>
        <DnsRecords dns={dns} />
      </div>
      <div className="flex items-center gap-4">
        <p className="text-end text-xs text-muted">
          {domain.project_id ? (
            <>
              <span className="block">{td.usedBy}{' '}
                <Link to="/projects/$projectId" params={{ projectId: domain.project_id }} search={{ tab: 'domains' }} className="text-fg hover:text-accent">
                  {domain.project_name}
                </Link>
              </span>
              <span className="block">{td.routeCount(domain.routes.length)}</span>
            </>
          ) : td.unused}
        </p>
        <ActionsMenu
          label={td.actions}
          ariaLabel={td.actionsFor(host)}
          disabled={busy}
          items={[
            ...(domain.project_id ? [{
              label: td.openProject,
              onSelect: () => void navigate({ to: '/projects/$projectId', params: { projectId: domain.project_id }, search: { tab: 'domains' } }),
            }] : []),
            { label: httpOnly ? t.app.useHttps : t.common.httpOnly, onSelect: () => setMode.mutate(httpOnly ? 'auto' : 'http_only') },
            { label: td.remove, danger: true, onSelect: () => setConfirming(true) },
          ]}
        />
      </div>
      <ConfirmModal
        open={confirming}
        onOpenChange={setConfirming}
        title={td.removeTitle}
        message={domain.project_id ? td.removeUsed(host, domain.project_name, domain.routes.length) : td.removeUnused(host)}
        confirm={t.common.yesRemove}
        onConfirm={() => remove.mutate()}
      />
    </li>
  )
}

/** The form that adds a domain to the panel. With projectId, that project uses it right
 * away: the shortcut on a project's Domains tab. */
export function AddDomain({ projectId, onDone }: { projectId?: string; onDone: () => void }) {
  const t = useT()
  const td = t.domains
  const queryClient = useQueryClient()
  const [hostname, setHostname] = useState('')
  const [httpOnly, setHttpOnly] = useState(false)
  const add = useMutation({
    mutationFn: () => withToast(
      () => api<Domain>('/domains', { method: 'POST', body: { hostname, tls_mode: httpOnly ? 'http_only' : 'auto', ...(projectId ? { project_id: projectId } : {}) } }),
      { loading: td.addingToast(hostname), success: td.added(hostname), error: td.addFailed }),
    onSuccess: onDone,
    onSettled: () => { // a 503 still saved it
      queryClient.invalidateQueries({ queryKey: allDomainsQuery.queryKey })
      queryClient.invalidateQueries({ queryKey: ['projects'] })
    },
  })
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); add.mutate() }} className="space-y-5 p-5">
      <Field label={t.app.hostname} name="hostname" required autoFocus placeholder="shop.example.com" dir="ltr"
        hint={httpOnly ? t.app.hostnameHintHttp : t.app.hostnameHintHttps}
        value={hostname} onChange={(e) => setHostname(e.target.value.trim().toLowerCase())} />
      <HttpOnlyChoice checked={httpOnly} onChange={setHttpOnly} />
      {add.error && <ErrorNote>{add.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={add.isPending || !hostname}>{add.isPending ? t.app.adding : td.add}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
