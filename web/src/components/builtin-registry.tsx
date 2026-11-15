import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { Badge, Button, CheckOption, ConfirmButton, Empty, ErrorNote, Field, HelpTip, HttpOnlyChoice, Panel, QueryState } from './ui'
import {
  api,
  panelSettingsQuery,
  registryTokensQuery,
  repositoriesQuery,
  s3ConnectionsQuery,
  type RegistrySettings,
  type RegistryToken,
} from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { Sheet } from './sheet'
import { withToast } from '../lib/toasts'

/** The built-in registry's settings: to turn it on (in a sheet), or to change it once on
 * (its Settings tab). Saving may pull the registry image first, so it runs under a toast. */
export function BuiltinForm({ saved, mode, onDone }: { saved: RegistrySettings; mode: 'enable' | 'edit'; onDone?: () => void }) {
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const conns = useQuery(s3ConnectionsQuery)
  const panel = useQuery(panelSettingsQuery)
  // Suggest registry.<the panel domain's parent> until a domain is chosen.
  const suggested = panel.data?.domain ? 'registry.' + panel.data.domain.replace(/^[^.]+\./, '') : 'registry.example.com'
  const [form, setForm] = useState({ domain: saved.domain || '', s3_connection_id: saved.s3_connection_id, build_cache: saved.build_cache, tls_mode: saved.tls_mode || 'auto' })
  const save = useMutation({
    mutationFn: () =>
      withToast(() => api<RegistrySettings>('/registry/settings', { method: 'PUT', body: { ...form, enabled: true, domain: form.domain.trim() || suggested } }), {
        loading: t.savingToast,
        success: t.savedToast,
        error: t.saveFailed,
      }),
    onSuccess: () => onDone?.(),
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['registry'] }),
  })
  const dirty = mode === 'enable' || form.domain !== saved.domain || form.s3_connection_id !== saved.s3_connection_id || form.build_cache !== saved.build_cache || form.tls_mode !== (saved.tls_mode || 'auto')
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); save.mutate() }} className="space-y-5 p-5">
      <Field label={t.domain} name="registry-domain" dir="ltr" placeholder={suggested} hint={t.domainHint}
        value={form.domain} onChange={(e) => setForm({ ...form, domain: e.target.value })} autoComplete="off" spellCheck={false} />
      <div className="space-y-1.5">
        <HttpOnlyChoice checked={form.tls_mode === 'http_only'} onChange={(v) => setForm({ ...form, tls_mode: v ? 'http_only' : 'auto' })} />
        {form.tls_mode === 'http_only' && <p className="text-xs text-warn">{t.httpOnlyLimit}</p>}
      </div>
      <label className="block space-y-1.5 text-xs text-muted">
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
      <CheckOption checked={form.build_cache} onChange={(v) => setForm({ ...form, build_cache: v })} label={t.buildCache} hint={t.buildCacheHint} />
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <Button type="submit" disabled={!dirty || save.isPending}>
        {mode === 'enable' ? (save.isPending ? t.enabling : t.enableSave) : save.isPending ? t.saving : t.save}
      </Button>
    </form>
  )
}

/** Turns the registry off; its images stay in their storage. */
export function TurnOff({ saved, onDone }: { saved: RegistrySettings; onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT().builtinRegistry
  const off = useMutation({
    mutationFn: () =>
      withToast(() => api<RegistrySettings>('/registry/settings', { method: 'PUT', body: { ...saved, enabled: false } }), {
        loading: t.turningOff,
        success: t.turnedOff,
        error: t.saveFailed,
      }),
    onSuccess: onDone,
    onSettled: () => queryClient.invalidateQueries({ queryKey: ['registry'] }),
  })
  return (
    <section className="space-y-3 border-t border-danger/30 p-5">
      <h3 className="font-pixel text-xs text-danger uppercase">{t.turnOff}</h3>
      <p className="text-xs text-muted">{t.turnOffHint}</p>
      <ConfirmButton label={t.turnOffButton} confirm={t.turnOffConfirm} pending={off.isPending} onConfirm={() => off.mutate()} />
      {off.error && <ErrorNote>{off.error.message}</ErrorNote>}
    </section>
  )
}

/** The container state as a badge. */
export function StateBadge({ state }: { state?: string }) {
  const t = useT().builtinRegistry
  const s = state ?? 'off'
  return <Badge tone={s === 'running' ? 'accent' : s === 'off' ? 'muted' : 'warn'}>{t.states[s] ?? s}</Badge>
}

export function PushHelp({ domain }: { domain: string }) {
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

/** Tokens: the list; creating one happens in a sheet, which then shows the secret once. */
export function Tokens() {
  const tokens = useQuery(registryTokensQuery)
  const tt = useT()
  const t = tt.builtinRegistry
  const [open, setOpen] = useState(false)
  return (
    <Panel title={t.tokens} actions={<Button variant="ghost" onClick={() => setOpen(true)}>{t.create}</Button>}>
      <p className="border-b border-line px-4 py-3 text-sm text-muted">{t.tokensIntro}</p>
      <QueryState loading={tokens.isPending} error={tokens.error} what={t.tokens} />
      {tokens.data &&
        (tokens.data.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted">{t.noTokens}</p>
        ) : (
          <ul className="divide-y divide-line">
            {tokens.data.map((tok) => <TokenRow key={tok.id} token={tok} />)}
          </ul>
        ))}
      <Sheet open={open} onOpenChange={setOpen} title={t.create} description={t.tokensIntro}>
        {/* Remounted on each opening: a fresh form, no secret left from the last one. */}
        {open && <CreateToken onDone={() => setOpen(false)} />}
      </Sheet>
    </Panel>
  )
}

function CreateToken({ onDone }: { onDone: () => void }) {
  const queryClient = useQueryClient()
  const tt = useT()
  const t = tt.builtinRegistry
  const [name, setName] = useState('')
  const [scope, setScope] = useState<'push' | 'pull'>('push')
  const create = useMutation({
    mutationFn: () => api<RegistryToken>('/registry/tokens', { method: 'POST', body: { name, scope } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: registryTokensQuery.queryKey }),
  })
  if (create.data) return <NewToken token={create.data} onDone={onDone} />
  return (
    <form onSubmit={(e: FormEvent) => { e.preventDefault(); create.mutate() }} className="space-y-5 p-5">
      <Field label={t.tokenName} name="registry-token-name" dir="ltr" placeholder="github-actions" maxLength={64}
        value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" spellCheck={false} />
      <label className="block space-y-1.5 text-xs text-muted">
        {t.scope}
        <select value={scope} onChange={(e) => setScope(e.target.value as 'push' | 'pull')} className="block h-9 w-full border border-line bg-bg px-2 text-sm text-fg">
          <option value="push">{t.scopePush}</option>
          <option value="pull">{t.scopePull}</option>
        </select>
      </label>
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <div className="flex gap-2">
        <Button type="submit" disabled={!name.trim() || create.isPending}>{create.isPending ? t.creating : t.create}</Button>
        <Button type="button" variant="ghost" onClick={onDone}>{tt.common.cancel}</Button>
      </div>
    </form>
  )
}

function NewToken({ token, onDone }: { token: RegistryToken; onDone: () => void }) {
  const tt = useT()
  const t = tt.builtinRegistry
  const [copied, setCopied] = useState(false)
  const secret = token.token ?? ''
  return (
    <div className="space-y-4 p-5">
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

export function Repositories({ domain }: { domain: string }) {
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
