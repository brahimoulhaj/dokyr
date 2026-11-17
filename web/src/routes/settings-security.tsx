import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useSearch } from '@tanstack/react-router'
import { Button, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { PasswordConfirmDialog, type PasswordAction } from '../components/password-confirm-dialog'
import { api } from '../lib/api'
import { dateTime } from '../lib/format'
import { useT } from '../lib/i18n'
import { passkeysSupported, registerPasskey } from '../lib/passkeys'

type Security = {
  passkeys: { id: string; name: string; rp_id: string; created_at: string; last_used_at: string | null }[]
  github_identity: { login: string } | null
  github_client_id: string
  github_configured: boolean
  callback_url: string
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
  const text = t.security
  const qc = useQueryClient()
  const search = useSearch({ strict: false }) as { auth_error?: string }
  const [confirmation, setConfirmation] = useState<PasswordAction | null>(null)
  const [name, setName] = useState('')
  const [clientID, setClientID] = useState(data.github_client_id)
  const [secret, setSecret] = useState('')
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
    const { url } = await api<{ url: string }>('/auth/github/start', { method: 'POST', body: { link: true, password } })
    location.assign(url)
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
        </div> : <Button type="button" variant="ghost" disabled={!data.github_configured || !!data.origin_error} onClick={() => confirm({ title: text.linkGitHub, confirm: text.linkGitHub, onConfirm: linkGitHub })}>{text.linkGitHub}</Button>}
        <details className="border-t border-line pt-3" open={!data.github_configured}>
          <summary className="cursor-pointer text-sm">{text.configureGitHub}</summary>
          <div className="mt-3 space-y-3">
            <p className="text-xs text-muted">{text.setupHint} <a href="https://github.com/settings/developers" target="_blank" rel="noreferrer" className="text-accent hover:underline">{text.oauthApps}</a></p>
            {data.callback_url && <Field label={text.callback} name="github-callback" value={data.callback_url} readOnly dir="ltr" onFocus={(e) => e.target.select()} />}
            <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); confirm({ title: text.configureGitHub, confirm: t.common.save, detail: text.configHint, onConfirm: async (password) => { await api('/auth/github/config', { method: 'PUT', body: { client_id: clientID, client_secret: secret, password } }); setSecret('') } }) }}>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="Client ID" name="github-client-id" dir="ltr" autoComplete="off" maxLength={200} value={clientID} onChange={(e) => setClientID(e.target.value)} />
                <Field label="Client secret" name="github-client-secret" type="password" autoComplete="new-password" maxLength={500} value={secret} onChange={(e) => setSecret(e.target.value)} hint={data.github_configured ? text.secretHint : undefined} />
              </div>
              <p className="text-xs text-muted">{text.configHint}</p>
              <div className="flex gap-2">
                <Button type="submit" disabled={!clientID.trim() || (!secret && !data.github_configured)}>{t.common.save}</Button>
                {data.github_configured && <Button type="button" variant="danger" onClick={() => confirm({ title: text.disableGitHub, confirm: text.disableGitHub, tone: 'danger', onConfirm: async (password) => { await api('/auth/github/config', { method: 'PUT', body: { client_id: '', client_secret: '', password } }); setClientID(''); setSecret('') } })}>{text.disableGitHub}</Button>}
              </div>
            </form>
          </div>
        </details>
      </div>
    </Panel>
    {confirmation && <PasswordConfirmDialog action={confirmation} onClose={() => setConfirmation(null)} onComplete={completed} />}
  </div>
}
