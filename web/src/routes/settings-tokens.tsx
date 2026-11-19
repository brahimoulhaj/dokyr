import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { Sheet, SheetActions } from '../components/sheet'
import { Button, ConfirmButton, Empty, ErrorNote, Field, HelpTip, Panel, QueryState } from '../components/ui'
import { api, tokensQuery, type ApiToken } from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { rich, useT } from '../lib/i18n'

/** API tokens: long-lived bearer credentials for scripts and CI. `bare` drops the section
 * title where the page already names it. */
export function ApiTokens({ bare = false }: { bare?: boolean }) {
  const queryClient = useQueryClient()
  const t = useT()
  const tokens = useQuery(tokensQuery)
  const [open, setOpen] = useState(false)
  const [created, setCreated] = useState<ApiToken | null>(null)
  const close = () => {
    setOpen(false)
    queryClient.invalidateQueries({ queryKey: tokensQuery.queryKey }) // picks up "last used" if it was tried meanwhile
  }

  return (
    <Panel
      title={bare ? undefined : t.settingsTokens.title}
      actions={
        <span className="flex items-center gap-2">
          {!bare && <TokensHelp />}
          <Button variant="ghost" className="h-7 px-2 text-xs" onClick={() => { setCreated(null); setOpen(true) }}>
            {t.settingsTokens.createToken}
          </Button>
        </span>
      }
    >
      <Sheet open={open} onOpenChange={(o) => !o && close()} title={t.settingsTokens.createToken}>
        {created ? <NewSecret token={created} onDone={close} /> : <CreateToken onCreated={setCreated} onCancel={close} />}
      </Sheet>
      {!tokens.data && (
        <div className="p-4">
          <QueryState loading={tokens.isPending} error={tokens.error} what={t.settingsTokens.what} />
        </div>
      )}
      {tokens.data &&
        (tokens.data.length === 0 ? (
          <Empty title={t.settingsTokens.empty} />
        ) : (
          <ul className="divide-y divide-line">
            {tokens.data.map((token) => (
              <TokenRow key={token.id} token={token} />
            ))}
          </ul>
        ))}
    </Panel>
  )
}

/** What a token is for and how to send it, behind a ? next to the title. */
export function TokensHelp() {
  const t = useT()
  return (
    <HelpTip label={t.settingsTokens.title}>
      {rich(t.settingsTokens.intro, { header: () => <code dir="ltr">Authorization: Bearer &lt;token&gt;</code> })}
    </HelpTip>
  )
}

function CreateToken({ onCreated, onCancel }: { onCreated: (t: ApiToken) => void; onCancel: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const [name, setName] = useState('')
  const create = useMutation({
    mutationFn: () => api<ApiToken>('/tokens', { method: 'POST', body: { name } }),
    onSuccess: (token) => {
      setName('')
      queryClient.invalidateQueries({ queryKey: tokensQuery.queryKey })
      onCreated(token)
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <Field
        label={t.common.name}
        name="token-name"
        placeholder="github-actions"
        maxLength={64}
        value={name}
        onChange={(e) => setName(e.target.value)}
        autoComplete="off"
      />
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!name.trim() || create.isPending}>
          {create.isPending ? t.common.creating : t.settingsTokens.createToken}
        </Button>
        <Button type="button" variant="ghost" onClick={onCancel}>
          {t.common.cancel}
        </Button>
      </SheetActions>
    </form>
  )
}

/** The secret of a token just created. It is shown here once and never again. */
function NewSecret({ token, onDone }: { token: ApiToken; onDone: () => void }) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const secret = token.token ?? ''

  async function copy() {
    try {
      await navigator.clipboard.writeText(secret)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="space-y-5 p-5">
      <p className="text-sm">
        {rich(t.settingsTokens.created, { name: () => <span className="text-accent">{token.name}</span> })}
      </p>
      <div className="flex flex-col gap-2 sm:flex-row">
        <input
          readOnly
          value={secret}
          aria-label={t.settingsTokens.newTokenLabel}
          dir="ltr"
          onFocus={(e) => e.target.select()}
          className="block h-9 min-w-0 flex-1 border border-accent bg-bg px-3 text-sm text-fg focus:outline-none"
        />
        <Button type="button" variant="ghost" onClick={copy}>
          {copied ? t.common.copied : t.common.copy}
        </Button>
      </div>
      <pre dir="ltr" className="overflow-x-auto border border-line bg-bg p-3 text-xs text-muted">
        curl -H "Authorization: Bearer {secret}" {location.origin}/api/projects
      </pre>
      <SheetActions>
        <Button type="button" onClick={onDone}>
          {t.common.done}
        </Button>
      </SheetActions>
    </div>
  )
}

function TokenRow({ token }: { token: ApiToken }) {
  const queryClient = useQueryClient()
  const t = useT()
  const revoke = useMutation({
    mutationFn: () => api<void>(`/tokens/${token.id}`, { method: 'DELETE' }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: tokensQuery.queryKey }),
  })
  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm">{token.name}</p>
        <p className="text-xs text-muted">
          <span title={dateTime(token.created_at)}>{t.settingsTokens.createdAgo(timeAgo(token.created_at))}</span>
          {' · '}
          <span title={dateTime(token.last_used_at)}>
            {token.last_used_at ? t.settingsTokens.lastUsed(timeAgo(token.last_used_at)) : t.settingsTokens.neverUsed}
          </span>
        </p>
        {revoke.error && <p className="text-xs text-danger">{revoke.error.message}</p>}
      </div>
      <ConfirmButton label={t.settingsTokens.revoke} confirm={t.settingsTokens.yesRevoke} pending={revoke.isPending} onConfirm={() => revoke.mutate()} />
    </li>
  )
}
