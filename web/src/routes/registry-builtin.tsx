import { useQuery } from '@tanstack/react-query'
import { Link, Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { BuiltinForm, PushHelp, Repositories, StateBadge, Tokens, TurnOff } from '../components/builtin-registry'
import { cx, PageHeader, Panel, QueryState } from '../components/ui'
import { registrySettingsQuery } from '../lib/api'
import { useT } from '../lib/i18n'

export const builtinTabs = ['images', 'settings'] as const
export type BuiltinTab = (typeof builtinTabs)[number]

/** The built-in registry on its own page: its images (with how to push and pull), and settings (with tokens). */
export function BuiltinRegistryPage() {
  const t = useT()
  const tb = t.builtinRegistry
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: string }
  const tab: BuiltinTab = builtinTabs.includes(search.tab as BuiltinTab) ? (search.tab as BuiltinTab) : 'images'
  const settings = useQuery({ ...registrySettingsQuery, retry: false })
  const rs = settings.data
  const on = !!(rs?.enabled && rs.domain)
  const address = rs?.address || rs?.domain || ''
  const crumbs = <Link to="/registry" className="hover:text-fg">{t.registries.title}</Link>

  if (settings.data && !on) return <Navigate to="/registry" replace /> // turned off: back to the list
  if (!rs || !on) {
    return (
      <div className="space-y-6">
        <PageHeader crumbs={crumbs} title={tb.title} />
        <QueryState loading={settings.isPending} error={settings.error} what={tb.title} />
      </div>
    )
  }
  return (
    <div className="space-y-6">
      <PageHeader
        crumbs={crumbs}
        title={<><span dir="ltr" className="normal-case">{address}</span><StateBadge state={rs.state} /></>}
      />
      <nav className="flex gap-1 overflow-x-auto overflow-y-hidden border-b border-line" aria-label={tb.title}>
        {builtinTabs.map((name) => (
          <Link
            key={name}
            to="/registry/builtin"
            search={{ tab: name }}
            className={cx('border-b-2 px-3 py-1.5 text-sm whitespace-nowrap', name === tab ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg')}
            aria-current={name === tab ? 'page' : undefined}
          >
            {tb.tabs[name]}
          </Link>
        ))}
      </nav>
      {tab === 'images' && (
        <>
          <PushHelp domain={address} />
          <Repositories domain={address} />
        </>
      )}
      {tab === 'settings' && (
        <>
          <Panel title={tb.tabs.settings}>
            <BuiltinForm saved={rs} mode="edit" />
            <TurnOff saved={rs} onDone={() => navigate({ to: '/registry' })} />
          </Panel>
          <Tokens />
        </>
      )}
    </div>
  )
}
