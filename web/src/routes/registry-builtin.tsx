import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Sheet } from '../components/sheet'
import { Link, Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { BuiltinForm, PushHelp, Repositories, StateBadge, Tokens, TurnOff } from '../components/builtin-registry'
import { Button, cx, PageHeader, QueryState } from '../components/ui'
import { registrySettingsQuery } from '../lib/api'
import { useT } from '../lib/i18n'

export const builtinTabs = ['push', 'tokens', 'repositories'] as const
export type BuiltinTab = (typeof builtinTabs)[number]

/** The built-in registry on its own page: how to push, tokens, repositories, settings. */
export function BuiltinRegistryPage() {
  const t = useT()
  const tb = t.builtinRegistry
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: string }
  const tab: BuiltinTab = builtinTabs.includes(search.tab as BuiltinTab) ? (search.tab as BuiltinTab) : 'push'
  const settings = useQuery({ ...registrySettingsQuery, retry: false })
  const [editing, setEditing] = useState(false)
  const rs = settings.data
  const on = !!(rs?.enabled && rs.domain)
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
        title={<><span dir="ltr" className="normal-case">{rs.domain}</span><StateBadge state={rs.state} /></>}
        actions={<Button variant="ghost" onClick={() => setEditing(true)}>{tb.tabs.settings}</Button>}
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
      {tab === 'push' && <PushHelp domain={rs.domain} />}
      {tab === 'tokens' && <Tokens />}
      {tab === 'repositories' && <Repositories domain={rs.domain} />}
      <Sheet open={editing} onOpenChange={setEditing} title={tb.tabs.settings} description={tb.domainHint}>
        {editing && (
          <>
            <BuiltinForm saved={rs} mode="edit" onDone={() => setEditing(false)} />
            <TurnOff saved={rs} onDone={() => navigate({ to: '/registry' })} />
          </>
        )}
      </Sheet>
    </div>
  )
}
