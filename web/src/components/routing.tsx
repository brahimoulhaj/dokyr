import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent, type ReactNode } from 'react'
import { api, appsQuery, dnsQuery, hostHref, isWildcard, projectsQuery, routesByHost, routesQuery, type Route } from '../lib/api'
import { useT } from '../lib/i18n'
import { useCan } from '../lib/roles'
import { withToast } from '../lib/toasts'
import { ActionsMenu } from './actions-menu'
import { ConfirmModal } from './confirm-dialog'
import { DnsBadge, DnsRecords, dnsRail, dnsState } from './dns'
import { Sheet, SheetActions } from './sheet'
import { Badge, Button, CheckOption, ConfirmButton, cx, Empty, ErrorNote, Field, HttpOnlyChoice, Panel, QueryState } from './ui'

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
 * Routing as a board: one card per hostname, with the paths it sends to apps. With appId
 * it is that app's view: only the hostnames it answers on, and new routes go to it.
 * Hostnames are typed with the route; there is nothing to add first.
 */
export function RoutingBoard({ appId, intro }: { appId?: string; intro: ReactNode }) {
  const t = useT()
  const tr = t.routing
  const routes = useQuery(routesQuery)
  const [draft, setDraft] = useState<Draft | null>(null)
  const all = routesByHost(routes.data ?? [])
  const hosts = appId ? all.filter((h) => h.routes.some((r) => r.app_id === appId)) : all
  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
        <p className="max-w-2xl text-sm text-muted">{intro}</p>
        <Button onClick={() => setDraft({})}>{tr.add}</Button>
      </div>
      <QueryState loading={routes.isPending} error={routes.error} what={tr.what} />
      {routes.data && hosts.length === 0 && (
        <Panel>
          <Empty title={appId ? tr.appNone : tr.none}>
            <p>{appId ? tr.appNoneHint : tr.noneHint}</p>
            <Button className="mt-5" onClick={() => setDraft({})}>{tr.add}</Button>
          </Empty>
        </Panel>
      )}
      {hosts.map((h) => (
        <HostCard key={h.hostname} hostname={h.hostname} routes={h.routes} appId={appId}
          onRoute={(route) => setDraft(route ? { route } : { hostname: h.hostname })} />
      ))}
      <Sheet open={draft !== null} onOpenChange={(o) => !o && setDraft(null)} title={draft?.route ? tr.edit : tr.add} description={tr.addressHint}>
        {draft && <RouteForm draft={draft} appId={appId} known={all} onDone={() => setDraft(null)} />}
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

/** One hostname: where its DNS points, how it is served, and its paths. */
function HostCard({ hostname, routes, appId, onRoute }: { hostname: string; routes: Route[]; appId?: string; onRoute: (r?: Route) => void }) {
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
    <Panel>
      {/* The hostname in the body font: the pixel font of panel titles has no lowercase. */}
      <div className="relative flex flex-wrap items-start justify-between gap-3 border-b border-line py-3 ps-5 pe-4">
        {/* The rail repeats the DNS state, so a long page can be scanned down its edge. */}
        <span aria-hidden className={cx('absolute inset-y-0 start-0 w-1 transition-colors duration-300', dnsRail[state])} />
        <div className="min-w-0 space-y-2">
          <p className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
            <a href={hostHref(hostname, httpOnly)} target="_blank" rel="noreferrer" dir="ltr"
              className="text-base leading-tight text-fg [overflow-wrap:anywhere] hover:text-accent">{hostname}</a>
            <DnsBadge state={state} />
            {httpOnly && <Badge tone="muted">{t.common.httpOnly}</Badge>}
          </p>
          <DnsRecords dns={dns} />
        </div>
        <ActionsMenu
          label={tr.actions}
          ariaLabel={tr.actionsFor(hostname)}
          disabled={remove.isPending || setMode.isPending}
          items={[
            { label: httpOnly ? t.app.useHttps : t.common.httpOnly, onSelect: () => setMode.mutate(httpOnly ? 'auto' : 'http_only') },
            ...(can.remove ? [{ label: tr.removeHost, danger: true, onSelect: () => setConfirming(true) }] : []),
          ]}
        />
      </div>
      <ul className="divide-y divide-line">
        {routes.map((r) => <RouteRow key={r.id} route={r} own={r.app_id === appId} onEdit={() => onRoute(r)} />)}
      </ul>
      <div className="border-t border-line px-4 py-3">
        <Button variant="ghost" onClick={() => onRoute()}>{tr.add}</Button>
      </div>
      <ConfirmModal
        open={confirming}
        onOpenChange={setConfirming}
        title={tr.removeHostTitle}
        message={tr.removeHostMessage(hostname, routes.length)}
        confirm={t.common.yesRemove}
        onConfirm={() => remove.mutate()}
      />
    </Panel>
  )
}

function RouteRow({ route, own, onEdit }: { route: Route; own: boolean; onEdit: () => void }) {
  const t = useT()
  const tr = t.routing
  const can = useCan()
  const refresh = useRefreshRoutes()
  const address = addressOf(route)
  const remove = useMutation({
    mutationFn: () => withToast(() => api<void>(`/routes/${route.id}`, { method: 'DELETE' }),
      { loading: tr.removing(address), success: tr.removed(address), error: tr.removeFailed }),
    onSettled: refresh,
  })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="flex min-w-0 flex-wrap items-center gap-3 text-sm">
        <code dir="ltr" className="min-w-16 text-fg [overflow-wrap:anywhere]">{route.path}</code>
        <span aria-hidden className="text-muted rtl:-scale-x-100">→</span>
        <span className="min-w-0">
          <Link to="/projects/$projectId/apps/$appId" params={{ projectId: route.project_id, appId: route.app_id }} search={{ tab: 'routing' }}
            className="hover:text-accent hover:underline">{route.app_name}</Link>
          <span className="ms-2 text-xs text-muted">{own ? tr.thisApp : route.project_name}</span>
        </span>
        {route.strip_prefix && <Badge tone="muted">{tr.stripped}</Badge>}
      </div>
      <span className="flex items-center gap-2">
        <Button variant="ghost" onClick={onEdit}>{t.common.edit}</Button>
        {can.remove && <ConfirmButton label={tr.removeRoute} confirm={t.common.yesRemove} pending={remove.isPending} onConfirm={() => remove.mutate()} />}
      </span>
    </li>
  )
}

/** One address field does it: "shop.example.com" or "shop.example.com/api", then the app. */
function RouteForm({ draft, appId, known, onDone }: { draft: Draft; appId?: string; known: { hostname: string; routes: Route[] }[]; onDone: () => void }) {
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
  const target = apps.find((a) => a.id === picked) ?? apps[0]
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
