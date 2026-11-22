import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent, type ReactNode } from 'react'
import { api, appsQuery, dnsQuery, hostHref, isWildcard, projectsQuery, routesByHost, routesQuery, type Route } from '../lib/api'
import { useT } from '../lib/i18n'
import { useCan } from '../lib/roles'
import { withToast } from '../lib/toasts'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { ListToolbar, SearchBox, fold } from './list-controls'
import { DnsBadge, DnsRecords, dnsRail, dnsState } from './dns'
import { Sheet, SheetActions } from './sheet'
import { Badge, Button, CheckOption, cx, Empty, ErrorNote, Field, HttpOnlyChoice, Panel, QueryState } from './ui'

/** What the route form opens with: a route to edit, or a hostname and an app to start from. */
type Draft = { route?: Route; hostname?: string }

/** What someone typed for a route ("https://Shop.example.com/api/") as a hostname and a path. */
function splitAddress(address: string): { host: string; path: string } {
  const bare = address.trim().replace(/^[a-z]+:\/\//i, '')
  const slash = bare.indexOf('/')
  const host = (slash < 0 ? bare : bare.slice(0, slash)).toLowerCase().replace(/\.$/, '')
  const path = slash < 0 ? '/' : '/' + bare.slice(slash + 1).replace(/^\/+|\/+$/g, '')
  return { host, path }
}

const addressOf = (r: { hostname: string; path: string }) => r.hostname + (r.path === '/' ? '' : r.path)

/** Every app of the panel, by project: what a route can send traffic to. */
function useAllApps() {
  const projects = useQuery(projectsQuery)
  const lists = useQueries({ queries: (projects.data ?? []).map((p) => appsQuery(p.id)) })
  const groups = (projects.data ?? []).map((project, i) => ({ project, apps: lists[i]?.data ?? [] })).filter((g) => g.apps.length > 0)
  return { groups, loading: projects.isPending || lists.some((l) => l.isPending) }
}

/**
 * Routing as one list: a line per hostname, then a line per path it sends to an app.
 * Scoped to an app it shows only that app's routes, scoped to a project only those of its
 * apps, and unscoped everything. Routes a scope leaves out are counted under their hostname.
 * Hostnames are typed with the route; there is nothing to add first.
 */
export function RoutingBoard({ appId, projectId, intro }: { appId?: string; projectId?: string; intro: ReactNode }) {
  const t = useT()
  const tr = t.routing
  const routes = useQuery(routesQuery)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [query, setQuery] = useState('')
  const all = routesByHost(routes.data ?? [])
  const scoped = Boolean(appId || projectId)
  const inScope = (r: Route) => (appId ? r.app_id === appId : projectId ? r.project_id === projectId : true)
  const q = fold(query.trim())
  const matches = (r: Route) => q === '' || [addressOf(r), r.app_name, r.project_name].some((s) => fold(s).includes(q))
  const hosts = all
    .map((h) => ({ hostname: h.hostname, total: h.routes.length, own: h.routes.filter(inScope) }))
    .filter((h) => h.own.length > 0)
  const shown = hosts.map((h) => ({ ...h, routes: h.own.filter(matches) })).filter((h) => h.routes.length > 0)
  // Worth a filter only once the list no longer fits a glance.
  const filterable = hosts.reduce((n, h) => n + h.own.length, 0) > 8
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <p className="max-w-2xl text-sm text-muted">{intro}</p>
        <Button onClick={() => setDraft({})}>{tr.add}</Button>
      </div>
      <QueryState loading={routes.isPending} error={routes.error} what={tr.what} />
      {routes.data && hosts.length === 0 && (
        <Panel>
          <Empty title={scoped ? tr.appNone : tr.none}>
            <p>{appId ? tr.appNoneHint : tr.noneHint}</p>
            <Button className="mt-5" onClick={() => setDraft({})}>{tr.add}</Button>
          </Empty>
        </Panel>
      )}
      {hosts.length > 0 && (
        <>
          {filterable && <ListToolbar><SearchBox value={query} onChange={setQuery} label={tr.filter} /></ListToolbar>}
          <Panel>
            {shown.length === 0 && <p className="px-4 py-6 text-center text-sm text-muted">{tr.noMatch}</p>}
            <div className="divide-y divide-line">
              {shown.map((h) => (
                <HostSection key={h.hostname} hostname={h.hostname} routes={h.routes} total={h.total} hidden={h.total - h.own.length}
                  target={appId ? 'none' : projectId ? 'app' : 'full'}
                  onRoute={(route) => setDraft(route ? { route } : { hostname: h.hostname })} />
              ))}
            </div>
          </Panel>
        </>
      )}
      <Sheet open={draft !== null} onOpenChange={(o) => !o && setDraft(null)} title={draft?.route ? tr.edit : tr.add} description={tr.addressHint}>
        {draft && <RouteForm draft={draft} appId={appId} projectId={projectId} known={all} onDone={() => setDraft(null)} />}
      </Sheet>
    </div>
  )
}

function useRefreshRoutes() {
  const queryClient = useQueryClient()
  return () => {
    for (const key of [['routes'], ['apps'], ['projects'], ['dns']]) queryClient.invalidateQueries({ queryKey: key })
  }
}

/** How much of a route's destination a row says: nothing on the app's own page, the app
 * on its project's, the app and its project everywhere else. */
type Target = 'none' | 'app' | 'full'

const rowAction = 'h-7 border border-transparent px-2 text-xs text-muted transition-colors hover:border-line-strong hover:text-fg disabled:opacity-50'

/** One hostname: a line with where its DNS points and how it is served, then its paths.
 * total counts every route of the hostname, hidden those this view leaves out. */
function HostSection({ hostname, routes, total, hidden, target, onRoute }: {
  hostname: string; routes: Route[]; total: number; hidden: number; target: Target; onRoute: (r?: Route) => void
}) {
  const t = useT()
  const tr = t.routing
  const can = useCan()
  const refresh = useRefreshRoutes()
  const dns = useQuery(dnsQuery(hostname))
  const state = dnsState(dns)
  const [confirming, setConfirming] = useState(false)
  const httpOnly = routes[0].tls_mode === 'http_only'
  const host = `/hosts/${encodeURIComponent(hostname)}`
  const remove = useMutation({
    mutationFn: () => withToast(() => api<void>(host, { method: 'DELETE' }),
      { loading: tr.removingHost(hostname), success: tr.removedHost(hostname), error: tr.removeHostFailed }),
    onSettled: refresh,
  })
  const setMode = useMutation({
    mutationFn: (tls_mode: string) => withToast(() => api<unknown>(host, { method: 'PATCH', body: { tls_mode } }),
      { loading: tr.switching(hostname), success: tr.switched(hostname), error: tr.switchFailed }),
    onSettled: refresh, // a 503 still saved it
  })
  return (
    <section>
      <div className="relative flex items-start justify-between gap-x-4 bg-bg/60 py-1.5 ps-5 pe-3">
        {/* The rail repeats the DNS state, so a long page can be scanned down its edge. */}
        <span aria-hidden className={cx('absolute inset-y-0 start-0 w-1 transition-colors duration-300', dnsRail[state])} />
        <div className="flex min-h-8 min-w-0 flex-1 flex-wrap items-center gap-x-3 gap-y-1.5">
          {/* The hostname in the body font: the pixel font of panel titles has no lowercase. */}
          <a href={hostHref(hostname, httpOnly)} target="_blank" rel="noreferrer" dir="ltr"
            className="text-sm font-medium leading-tight text-fg [overflow-wrap:anywhere] hover:text-accent">{hostname}</a>
          <DnsBadge state={state} />
          {httpOnly && <Badge tone="muted">{t.common.httpOnly}</Badge>}
          <DnsRecords dns={dns} />
        </div>
        <ActionsMenu
          label={tr.actions}
          ariaLabel={tr.actionsFor(hostname)}
          disabled={remove.isPending || setMode.isPending}
          items={[
            { label: tr.add, onSelect: () => onRoute() },
            { label: httpOnly ? t.app.useHttps : t.common.httpOnly, onSelect: () => setMode.mutate(httpOnly ? 'auto' : 'http_only') },
            ...(can.remove ? [{ label: tr.removeHost, danger: true, onSelect: () => setConfirming(true) }] : []),
          ]}
        />
      </div>
      <ul>
        {routes.map((r) => <RouteRow key={r.id} route={r} target={target} onEdit={() => onRoute(r)} />)}
        {hidden > 0 && (
          <li className="py-1 ps-5 pe-3 text-xs text-muted">
            <Link to="/routing" className="underline decoration-dotted underline-offset-2 hover:text-fg">{tr.others(hidden)}</Link>
          </li>
        )}
      </ul>
      <ConfirmModal
        open={confirming}
        onOpenChange={setConfirming}
        title={tr.removeHostTitle}
        message={tr.removeHostMessage(hostname, total)}
        confirm={t.common.yesRemove}
        onConfirm={() => remove.mutate()}
      />
    </section>
  )
}

function RouteRow({ route, target, onEdit }: { route: Route; target: Target; onEdit: () => void }) {
  const t = useT()
  const tr = t.routing
  const can = useCan()
  const refresh = useRefreshRoutes()
  const [armed, setArmed] = useState(false)
  const address = addressOf(route)
  const remove = useMutation({
    mutationFn: () => withToast(() => api<void>(`/routes/${route.id}`, { method: 'DELETE' }),
      { loading: tr.removing(address), success: tr.removed(address), error: tr.removeFailed }),
    onSettled: refresh,
  })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-4 py-0.5 ps-5 pe-3 hover:bg-raised">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 text-sm">
        <code dir="ltr" className={cx('text-fg [overflow-wrap:anywhere]', target !== 'none' && 'min-w-28')}>{route.path}</code>
        {target !== 'none' && (
          <>
            <span aria-hidden className="text-muted rtl:-scale-x-100">→</span>
            <span className="min-w-0">
              <Link to="/projects/$projectId/apps/$appId" params={{ projectId: route.project_id, appId: route.app_id }} search={{ tab: 'routing' }}
                className="hover:text-accent hover:underline">{route.app_name}</Link>
              {target === 'full' && <span className="ms-2 text-xs text-muted">{route.project_name}</span>}
            </span>
          </>
        )}
        {route.strip_prefix && <Badge tone="muted">{tr.stripped}</Badge>}
      </div>
      <span className="flex items-center gap-1">
        {armed ? (
          <>
            <button type="button" disabled={remove.isPending} onClick={() => remove.mutate()}
              className="h-7 border border-danger/60 px-2 text-xs text-danger transition-colors hover:bg-danger hover:text-bg disabled:opacity-50">
              {remove.isPending ? t.common.working : t.common.yesRemove}
            </button>
            <button type="button" disabled={remove.isPending} onClick={() => setArmed(false)} className={rowAction}>{t.common.cancel}</button>
          </>
        ) : (
          <>
            <button type="button" onClick={onEdit} aria-label={`${t.common.edit}: ${address}`} className={rowAction}>{t.common.edit}</button>
            {can.remove && (
              <button type="button" onClick={() => setArmed(true)} aria-label={`${tr.removeRoute}: ${address}`}
                className={cx(rowAction, 'hover:border-danger/60 hover:text-danger')}>{tr.removeRoute}</button>
            )}
          </>
        )}
      </span>
    </li>
  )
}

/** One address field does it: "shop.example.com" or "shop.example.com/api", then the app. */
function RouteForm({ draft, appId, projectId, known, onDone }: { draft: Draft; appId?: string; projectId?: string; known: { hostname: string; routes: Route[] }[]; onDone: () => void }) {
  const t = useT()
  const tr = t.routing
  const refresh = useRefreshRoutes()
  const { groups, loading } = useAllApps()
  const { route } = draft
  // On a hostname that already serves "/", the next route is a path of it.
  const start = route ? addressOf(route) : draft.hostname ? draft.hostname + '/' : ''
  const [address, setAddress] = useState(start)
  const [picked, setPicked] = useState(route?.app_id ?? appId ?? '')
  const [strip, setStrip] = useState(route?.strip_prefix ?? false)
  const [httpOnly, setHttpOnly] = useState(false)
  const apps = groups.flatMap((g) => g.apps)
  // On a project's page a new route starts with one of its apps.
  const target = apps.find((a) => a.id === picked) ?? apps.find((a) => a.project_id === projectId) ?? apps[0]
  const { host, path } = splitAddress(address)
  const existing = known.find((h) => h.hostname === host)
  // How a hostname is served is asked once, with its first route.
  const fresh = !route && host !== '' && !existing
  const scheme = (existing ? existing.routes[0].tls_mode === 'http_only' : httpOnly) ? 'http://' : 'https://'
  const save = useMutation({
    mutationFn: () => {
      const body = { hostname: address, app_id: target?.id, strip_prefix: path !== '/' && strip, ...(fresh && httpOnly ? { tls_mode: 'http_only' } : {}) }
      const shown = addressOf({ hostname: host, path })
      return route
        ? withToast(() => api<Route>(`/routes/${route.id}`, { method: 'PATCH', body }), { loading: tr.saving(shown), success: tr.saved(shown), error: tr.saveFailed })
        : withToast(() => api<Route>('/routes', { method: 'POST', body }), { loading: tr.adding(shown), success: tr.added(shown), error: tr.addFailed })
    },
    onSuccess: onDone,
    onSettled: refresh, // a 503 still saved it
  })
  if (!loading && apps.length === 0) return <p className="p-5 text-sm text-muted">{tr.noApps}</p>
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); save.mutate() }} className="space-y-5 p-5">
      <Field label={tr.address} name="route-address" required autoFocus dir="ltr" placeholder="shop.example.com/api" list="route-hostnames"
        value={address} onChange={(e) => setAddress(e.target.value.trim())} autoComplete="off" autoCapitalize="none" spellCheck={false} />
      <datalist id="route-hostnames">
        {known.map((h) => <option key={h.hostname} value={h.hostname} />)}
      </datalist>
      <label className="block space-y-1.5 text-xs text-muted">
        {tr.app}
        <select value={target?.id ?? ''} onChange={(e) => setPicked(e.target.value)} disabled={loading}
          className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg disabled:opacity-60">
          {groups.map((g) => (
            <optgroup key={g.project.id} label={g.project.name}>
              {g.apps.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
            </optgroup>
          ))}
        </select>
      </label>
      {host && target && (
        <p className="flex flex-wrap items-center gap-2 border border-line bg-bg px-3 py-2 text-xs text-muted">
          <code dir="ltr" className="text-fg [overflow-wrap:anywhere]">{isWildcard(host) ? '' : scheme}{addressOf({ hostname: host, path })}</code>
          <span aria-hidden className="rtl:-scale-x-100">→</span>
          <span className="text-fg">{target.name}</span>
        </p>
      )}
      {path !== '/' && <CheckOption checked={strip} onChange={setStrip} label={tr.strip} hint={tr.stripHint} />}
      {fresh && <HttpOnlyChoice checked={httpOnly} onChange={setHttpOnly} />}
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={save.isPending || !host || !target}>{save.isPending ? t.common.saving : route ? t.common.save : tr.add}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{t.common.cancel}</Button>
      </SheetActions>
    </form>
  )
}
