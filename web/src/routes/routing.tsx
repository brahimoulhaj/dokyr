import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { RoutingBoard } from '../components/routing'
import { Badge, PageHeader, Panel } from '../components/ui'
import { hostHref, objectStoreQuery, panelSettingsQuery, registrySettingsQuery } from '../lib/api'
import { useT } from '../lib/i18n'

/** The Routing page: everything the proxy sends somewhere. Routes to apps are edited here
 * (and on each app); the hostnames the panel answers on itself are listed for reference. */
export function RoutingPage() {
  const t = useT()
  return (
    <div className="space-y-6">
      <PageHeader title={t.routing.title} />
      <RoutingBoard intro={t.routing.intro} />
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
