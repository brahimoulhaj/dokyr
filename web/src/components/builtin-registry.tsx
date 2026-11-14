import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useEffect, useState, type FormEvent } from 'react'
import { Badge, Button, CheckOption, ConfirmButton, Empty, ErrorNote, Field, HelpTip, Panel, QueryState } from './ui'
import {
  api,
  panelSettingsQuery,
  registrySettingsQuery,
  registryTokensQuery,
  repositoriesQuery,
  s3ConnectionsQuery,
  type RegistrySettings,
  type RegistryToken,
} from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'

/** The built-in registry: settings, then (once on) how to push, tokens and repositories. */
export function BuiltinRegistry() {
  const settings = useQuery(registrySettingsQuery)
  const t = useT().builtinRegistry
  const on = settings.data?.enabled && settings.data.domain
  return (
    <div className="space-y-6">
      <QueryState loading={settings.isPending} error={settings.error} what={t.title} />
      {settings.data && <SettingsForm saved={settings.data} />}
      {on && settings.data && (
        <>
          <PushHelp domain={settings.data.domain} />
          <Tokens />
          <Repositories domain={settings.data.domain} />
        </>
      )}
    </div>
  )
}

function SettingsForm({ saved }: { saved: RegistrySettings }) {
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const conns = useQuery(s3ConnectionsQuery)
  const panel = useQuery(panelSettingsQuery)
  const [form, setForm] = useState(saved)
  useEffect(() => setForm(saved), [saved])
  // Suggest registry.<panel domain> until a domain is chosen.
  const suggested = panel.data?.domain ? 'registry.' + panel.data.domain.replace(/^[^.]+\./, '') : 'registry.example.com'
  const save = useMutation({
    mutationFn: () =>
      withToast(() => api<RegistrySettings>('/registry/settings', { method: 'PUT', body: { ...form, domain: form.domain || (form.enabled ? suggested : '') } }), {
        loading: t.savingToast,
        success: t.savedToast,
        error: t.saveFailed,
      }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['registry'] }),
  })
  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate()
  }
  const dirty = JSON.stringify({ ...form, state: undefined, system_user: undefined }) !== JSON.stringify({ ...saved, state: undefined, system_user: undefined })
  const state = saved.state ?? 'off'
  return (
    <Panel
      title={t.title}
      actions={
        <span className="flex items-center gap-2 text-xs text-muted">
          {t.state} <Badge tone={state === 'running' ? 'accent' : state === 'off' ? 'muted' : 'warn'}>{t.states[state] ?? state}</Badge>
        </span>
      }
    >
      <form onSubmit={submit} className="space-y-4 p-4">
        <p className="text-sm text-muted">{t.intro}</p>
        <CheckOption checked={form.enabled} onChange={(v) => setForm({ ...form, enabled: v })} label={t.enable} hint={t.enableHint} />
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label={t.domain} name="registry-domain" dir="ltr" placeholder={suggested} hintTooltip hint={t.domainHint}
            value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} autoComplete="off" spellCheck={false} />
          <label className="space-y-1.5 text-xs text-muted">
            {t.storage}
            <select value={form.s3_connection_id} onChange={(e) => setForm({ ...form, s3_connection_id: e.target.value })}
              className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg">
              <option value="">{t.storageVolume}</option>
              {conns.data?.map((c) => (
                <option key={c.id} value={c.id}>{t.storageS3(c.name)}</option>
              ))}
            </select>
            <span className="block">
              {t.storageHint}{' '}
              <Link to="/settings" search={{ tab: 'storage' }} className="text-accent hover:underline">{t.addS3}</Link>
            </span>
          </label>
        </div>
        <CheckOption checked={form.build_cache} onChange={(v) => setForm({ ...form, build_cache: v })} label={t.buildCache} hint={t.buildCacheHint} />
        {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
        <Button type="submit" disabled={!dirty || save.isPending}>{save.isPending ? t.saving : t.save}</Button>
      </form>
    </Panel>
  )
}

function PushHelp({ domain }: { domain: string }) {
  const t = useT().builtinRegistry
  return (
    <Panel title={t.how}>
      <div className="space-y-2 p-4">
        <p className="text-sm text-muted">{t.howIntro}</p>
        <pre dir="ltr" className="overflow-x-auto border border-line bg-bg p-3 text-xs">
          {`docker login ${domain} -u <token name>\ndocker tag my-app ${domain}/team/my-app:1.0\ndocker push ${domain}/team/my-app:1.0`}
        </pre>
        <p className="text-xs text-muted">{t.deploy}</p>
      </div>
    </Panel>
  )
}

function Tokens() {
  const tokens = useQuery(registryTokensQuery)
  const queryClient = useQueryClient()
  const tt = useT()
  const t = tt.builtinRegistry
  const [name, setName] = useState('')
  const [scope, setScope] = useState<'push' | 'pull'>('push')
  const [fresh, setFresh] = useState<RegistryToken | null>(null)
  const create = useMutation({
    mutationFn: () => api<RegistryToken>('/registry/tokens', { method: 'POST', body: { name, scope } }),
    onSuccess: (tok) => {
      setFresh(tok)
      setName('')
      queryClient.invalidateQueries({ queryKey: registryTokensQuery.queryKey })
    },
  })
  return (
    <Panel title={t.tokens}>
      <div className="space-y-3 border-b border-line p-4">
        <p className="text-sm text-muted">{t.tokensIntro}</p>
        <form onSubmit={(e) => { e.preventDefault(); create.mutate() }} className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex-1">
            <Field label={t.tokenName} name="registry-token-name" dir="ltr" placeholder="github-actions" maxLength={64}
              value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" spellCheck={false} />
          </div>
          <label className="space-y-1.5 text-xs text-muted">
            {t.scope}
            <select value={scope} onChange={(e) => setScope(e.target.value as 'push' | 'pull')} className="block h-9 border border-line bg-bg px-2 text-sm text-fg">
              <option value="push">{t.scopePush}</option>
              <option value="pull">{t.scopePull}</option>
            </select>
          </label>
          <Button type="submit" disabled={!name.trim() || create.isPending}>{create.isPending ? t.creating : t.create}</Button>
        </form>
        {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      </div>
      {fresh && <NewToken token={fresh} onDone={() => setFresh(null)} />}
      <QueryState loading={tokens.isPending} error={tokens.error} what={t.tokens} />
      {tokens.data &&
        (tokens.data.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">{t.noTokens}</p>
        ) : (
          <ul className="divide-y divide-line">
            {tokens.data.map((tok) => <TokenRow key={tok.id} token={tok} />)}
          </ul>
        ))}
    </Panel>
  )
}

function NewToken({ token, onDone }: { token: RegistryToken; onDone: () => void }) {
  const tt = useT()
  const t = tt.builtinRegistry
  const [copied, setCopied] = useState(false)
  const secret = token.token ?? ''
  return (
    <div className="space-y-3 border-b border-line bg-raised p-4">
      <p className="text-sm">{t.created(token.name)}</p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <input readOnly value={secret} aria-label={t.newToken} dir="ltr" onFocus={(e) => e.target.select()}
          className="block h-9 min-w-0 flex-1 border border-accent bg-bg px-3 text-sm text-fg focus:outline-none" />
        <Button type="button" variant="ghost" onClick={async () => {
          try { await navigator.clipboard.writeText(secret); setCopied(true) } catch { setCopied(false) }
        }}>
          {copied ? tt.common.copied : tt.common.copy}
        </Button>
      </div>
      <Button type="button" onClick={onDone}>{tt.common.done}</Button>
    </div>
  )
}

function TokenRow({ token }: { token: RegistryToken }) {
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const revoke = useMutation({
    mutationFn: () => api<void>(`/registry/tokens/${token.id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: registryTokensQuery.queryKey }),
  })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm" dir="ltr">{token.name}</p>
        <p className="text-xs text-muted">
          {token.scope === 'push' ? t.scopePush : t.scopePull} ·{' '}
          {token.last_used_at ? <span title={dateTime(token.last_used_at)}>{t.used(timeAgo(token.last_used_at))}</span> : t.neverUsed}
        </p>
        {revoke.error && <p className="text-xs text-danger">{revoke.error.message}</p>}
      </div>
      <ConfirmButton label={t.revoke} confirm={t.yesRevoke} pending={revoke.isPending} onConfirm={() => revoke.mutate()} />
    </li>
  )
}

function Repositories({ domain }: { domain: string }) {
  const repos = useQuery(repositoriesQuery)
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const gc = useMutation({
    mutationFn: () => withToast(() => api<{ output: string }>('/registry/gc', { method: 'POST' }), { loading: t.gcToast, success: t.gcDone, error: t.gcFailed }),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['registry'] }),
  })
  return (
    <Panel
      title={t.repositories}
      actions={
        <span className="flex items-center gap-2">
          <HelpTip label={t.gc}>{t.gcHint}</HelpTip>
          <Button variant="ghost" disabled={gc.isPending} onClick={() => gc.mutate()}>{t.gc}</Button>
        </span>
      }
    >
      <QueryState loading={repos.isPending} error={repos.error} what={t.repositories} />
      {repos.data &&
        (repos.data.length === 0 ? (
          <Empty title={t.noRepos} />
        ) : (
          <ul className="divide-y divide-line">
            {repos.data.map((r) => (
              <li key={r.name} className="space-y-2 px-4 py-3">
                <p className="text-sm" dir="ltr">{domain}/{r.name}</p>
                <ul className="flex flex-wrap gap-2">
                  {r.tags.map((tag) => <TagChip key={tag} domain={domain} repo={r.name} tag={tag} />)}
                </ul>
              </li>
            ))}
          </ul>
        ))}
    </Panel>
  )
}

function TagChip({ domain, repo, tag }: { domain: string; repo: string; tag: string }) {
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const [copied, setCopied] = useState(false)
  const image = `${domain}/${repo}:${tag}`
  const remove = useMutation({
    mutationFn: () => api<void>(`/registry/repositories/${repo}?tag=${encodeURIComponent(tag)}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: repositoriesQuery.queryKey }),
  })
  return (
    <li className="flex items-center gap-2 border border-line px-2 py-1 text-xs">
      <button type="button" dir="ltr" title={t.copyImage} className="hover:text-accent" onClick={async () => {
        try { await navigator.clipboard.writeText(image); setCopied(true); setTimeout(() => setCopied(false), 1500) } catch { /* no clipboard */ }
      }}>
        {copied ? t.copied : tag}
      </button>
      <ConfirmButton label={t.deleteTag} confirm={t.yesDelete} pending={remove.isPending} onConfirm={() => remove.mutate()} />
      {remove.error && <span className="text-danger">{remove.error.message}</span>}
    </li>
  )
}
