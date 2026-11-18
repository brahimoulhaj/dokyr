import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearch } from '@tanstack/react-router'
import { Button, CheckOption, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { PasswordConfirmDialog, type PasswordAction } from '../components/password-confirm-dialog'
import { api, hostTerminalQuery, type HostTerminalSettings } from '../lib/api'
import { dateTime } from '../lib/format'
import { useT } from '../lib/i18n'
import { passkeysSupported, registerPasskey } from '../lib/passkeys'
import { ActiveSessions } from './settings-sessions'
import { useCan } from '../lib/roles'

type Security = {
  passkeys: { id: string; name: string; rp_id: string; created_at: string; last_used_at: string | null }[]
  github_identity: { login: string } | null
  github_configured: boolean
  origin_error: string
}
const securityQuery = { queryKey: ['auth', 'security'], queryFn: () => api<Security>('/auth/security') }

export function SecuritySettings() {
  const q = useQuery(securityQuery)
  const t = useT().security
  return q.data ? <SecurityForm data={q.data} /> : <Panel title={t.title}><div className="p-4"><QueryState loading={q.isPending} error={q.error} what={t.title} /></div></Panel>
}

function SecurityForm({ data }: { data: Security }) {
  const t = useT()
  const can = useCan()
  const text = t.security
  const qc = useQueryClient()
  const search = useSearch({ strict: false }) as { auth_error?: string }
  const [confirmation, setConfirmation] = useState<PasswordAction | null>(null)
  const [name, setName] = useState('')
  const [notice, setNotice] = useState('')
  function confirm(action: PasswordAction) {
    setNotice('')
    setConfirmation(action)
  }
  function completed() {
    setConfirmation(null)
    setNotice(text.saved)
    qc.invalidateQueries({ queryKey: securityQuery.queryKey })
    qc.invalidateQueries({ queryKey: ['auth', 'methods'] })
  }
  async function linkGitHub(password: string) {
    const next = await api<{ url: string } | { action_url: string; manifest: string }>('/auth/github/start', { method: 'POST', body: { link: true, password } })
    if ('url' in next) { location.assign(next.url); return }
    const form = document.createElement('form')
    form.method = 'post'
    form.action = next.action_url
    const manifest = document.createElement('input')
    manifest.type = 'hidden'
    manifest.name = 'manifest'
    manifest.value = next.manifest
    form.appendChild(manifest)
    document.body.appendChild(form)
    form.submit()
  }

  return <div className="space-y-4">
    {search.auth_error && <ErrorNote>{text.githubError}</ErrorNote>}
    {notice && <p role="status" className="text-xs text-accent">{notice}</p>}
    {data.origin_error && <ErrorNote>{text.originHint}</ErrorNote>}
    <Panel title={text.passkeys}>
      <div className="space-y-3 p-4">
        <p className="text-sm text-muted">{text.passkeyHint}</p>
        {!passkeysSupported() && <p className="text-xs text-warn">{text.unsupported}</p>}
        <form onSubmit={(e) => { e.preventDefault(); confirm({ title: text.addPasskey, confirm: text.addPasskey, detail: name.trim(), pendingLabel: text.working, onConfirm: async (password) => { await registerPasskey(name.trim(), password); setName('') } }) }} className="flex flex-wrap items-end gap-3">
          <div className="min-w-48 flex-1"><Field label={t.common.name} name="passkey-name" maxLength={64} placeholder={text.passkeyPlaceholder} value={name} onChange={(e) => setName(e.target.value)} /></div>
          <Button type="submit" disabled={!name.trim() || !passkeysSupported() || !!data.origin_error}>{text.addPasskey}</Button>
        </form>
      </div>
      {data.passkeys.length === 0 ? <p className="border-t border-line p-4 text-sm text-muted">{text.noPasskeys}</p> : <ul className="divide-y divide-line border-t border-line">
        {data.passkeys.map((key) => <li key={key.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
          <div><p className="text-sm">{key.name}</p><p className="text-xs text-muted"><span dir="ltr">{key.rp_id}</span> · {key.last_used_at ? `${text.lastUsed}: ${dateTime(key.last_used_at)}` : text.neverUsed}</p></div>
          <Button type="button" variant="danger" onClick={() => confirm({ title: text.removePasskey, confirm: t.common.remove, detail: key.name, tone: 'danger', onConfirm: (password) => api(`/auth/passkeys/${key.id}`, { method: 'DELETE', body: { password } }) })}>{t.common.remove}</Button>
        </li>)}
      </ul>}
    </Panel>
    <Panel title={text.github}>
      <div className="space-y-3 p-4">
        <p className="text-sm text-muted">{text.githubHint}</p>
        {data.github_identity ? <div className="flex flex-wrap items-center justify-between gap-3 border border-line p-3">
          <span className="text-sm" dir="ltr">@{data.github_identity.login}</span>
          <Button type="button" variant="danger" onClick={() => confirm({ title: text.unlink, confirm: text.unlink, detail: `@${data.github_identity!.login}`, tone: 'danger', onConfirm: (password) => api('/auth/github/link', { method: 'DELETE', body: { password } }) })}>{text.unlink}</Button>
        </div> : <Button type="button" variant="ghost" disabled={!!data.origin_error || (!data.github_configured && !can.admin)} onClick={() => confirm({ title: text.linkGitHub, confirm: text.continueGitHub, onConfirm: linkGitHub })}>{text.linkGitHub}</Button>}
        {!data.github_configured && can.admin && <p className="text-xs text-muted">{text.automaticGitHubHint}</p>}
        {data.github_configured && can.admin && <div className="border-t border-line pt-3">
          <Button type="button" variant="danger" className="h-7 px-2 text-xs" onClick={() => confirm({ title: text.disableGitHub, confirm: text.disableGitHub, detail: text.disableGitHubHint, tone: 'danger', onConfirm: (password) => api('/auth/github/config', { method: 'PUT', body: { client_id: '', client_secret: '', password } }) })}>{text.disableGitHub}</Button>
        </div>}
      </div>
    </Panel>
    <ActiveSessions confirm={confirm} />
    {can.admin && <HostTerminalSettingsPanel />}
    {confirmation && <PasswordConfirmDialog action={confirmation} onClose={() => setConfirmation(null)} onComplete={completed} />}
  </div>
}

function HostTerminalSettingsPanel() {
  const t = useT()
  const text = t.security
  const qc = useQueryClient()
  const q = useQuery(hostTerminalQuery)
  const [draft, setDraft] = useState<{ enabled: boolean; user: string } | null>(null)
  const value = draft ?? { enabled: q.data?.enabled ?? true, user: q.data?.user ?? '' }
  const save = useMutation({
    mutationFn: () => api<HostTerminalSettings>('/host/terminal/settings', { method: 'PUT', body: { enabled: value.enabled, user: value.user.trim() } }),
    onSuccess: (saved) => { qc.setQueryData(hostTerminalQuery.queryKey, saved); setDraft(null) },
  })
  if (!q.data) return null
  return <Panel title={text.hostTerminal}>
    <form onSubmit={(e) => { e.preventDefault(); save.mutate() }} className="space-y-4 p-4">
      <CheckOption checked={value.enabled} onChange={(enabled) => setDraft({ ...value, enabled })} label={text.hostTerminalOn} hint={text.hostTerminalOnHint} />
      <div className="max-w-xs">
        <Field label={text.hostTerminalUser} hint={text.hostTerminalUserHint} hintTooltip name="host-terminal-user" dir="ltr" placeholder="root" maxLength={32}
          autoCapitalize="none" autoComplete="off" spellCheck={false} disabled={!value.enabled}
          value={value.user} onChange={(e) => setDraft({ ...value, user: e.target.value })} />
      </div>
      {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
      <Button type="submit" disabled={!draft || save.isPending}>{save.isPending ? t.common.working : t.common.save}</Button>
    </form>
  </Panel>
}
