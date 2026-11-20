import { useMutation, useSuspenseQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { AuthCard, Button, ErrorNote } from '../components/ui'
import { api, meQuery, metaQuery } from '../lib/api'
import { useT } from '../lib/i18n'

const route = getRouteApi('/cli/authorize')

/** What the CLI puts in the link it opens: where it listens, and what binds the answer to it. */
export type CliRequest = { port: number; state: string; challenge: string; device: string; code: string }

export function parseCliRequest(s: Record<string, unknown>): CliRequest {
  const text = (v: unknown) => (typeof v === 'string' || typeof v === 'number' ? String(v) : '')
  const port = Number(s.port)
  return { port: Number.isInteger(port) ? port : 0, state: text(s.state), challenge: text(s.challenge), device: text(s.device), code: text(s.code) }
}

const valid = (r: CliRequest) => r.port >= 1024 && r.port <= 65535 && /^[A-Za-z0-9_-]{16,128}$/.test(r.state) && /^[A-Za-z0-9_-]{43}$/.test(r.challenge)

// A sign-in that had to pass through the sign-in page waits here, in this tab only, so
// it survives every way of signing in (GitHub leaves the page and comes back).
const pendingKey = 'cli-authorize'
const pendingTTL = 10 * 60_000

export function holdCliRequest(r: CliRequest) {
  try {
    sessionStorage.setItem(pendingKey, JSON.stringify({ at: Date.now(), request: r }))
  } catch {
    // Without storage the user opens the link again after signing in.
  }
}

export function takeCliRequest(): CliRequest | null {
  try {
    const raw = sessionStorage.getItem(pendingKey)
    if (!raw) return null
    sessionStorage.removeItem(pendingKey)
    const held = JSON.parse(raw) as { at: number; request: Record<string, unknown> }
    if (Date.now() - held.at > pendingTTL) return null
    const request = parseCliRequest(held.request)
    return valid(request) ? request : null
  } catch {
    return null
  }
}

/** Where `dokyr login` sends the browser: the signed-in user approves the device or not. */
export function CliAuthorizePage() {
  const request = route.useSearch()
  const { data: meta } = useSuspenseQuery(metaQuery)
  const { data: me } = useSuspenseQuery(meQuery)
  const t = useT()

  // The panel answers with the address to go back to: the CLI's port on this machine.
  const approve = useMutation({
    mutationFn: () =>
      api<{ redirect: string }>('/cli/authorize', {
        method: 'POST',
        body: { port: request.port, state: request.state, challenge: request.challenge, device: request.device },
      }),
    onSuccess: ({ redirect }) => location.assign(redirect),
  })

  if (!valid(request)) {
    return (
      <AuthCard product={meta.name} title={t.cli.badTitle} subtitle={t.cli.badSubtitle}>
        <p className="text-sm text-muted">{t.cli.badBody}</p>
      </AuthCard>
    )
  }

  function deny() {
    const back = new URLSearchParams({ error: 'access_denied', state: request.state })
    location.assign(`http://127.0.0.1:${request.port}/callback?${back}`)
  }

  const done = approve.isPending || approve.isSuccess
  return (
    <AuthCard product={meta.name} title={t.cli.title} subtitle={t.cli.subtitle}>
      <dl className="space-y-3 text-sm">
        <div>
          <dt className="text-xs text-muted">{t.cli.device}</dt>
          <dd className="[overflow-wrap:anywhere]" dir="ltr">{request.device || '—'}</dd>
        </div>
        {request.code && (
          <div>
            <dt className="text-xs text-muted">{t.cli.code}</dt>
            <dd className="font-pixel text-lg tracking-widest text-accent" dir="ltr">{request.code.slice(0, 16)}</dd>
          </div>
        )}
        <div>
          <dt className="text-xs text-muted">{t.cli.account}</dt>
          <dd className="[overflow-wrap:anywhere]" dir="ltr">{me?.email}</dd>
        </div>
      </dl>
      <p className="mt-5 text-xs text-muted">{t.cli.checkCode}</p>
      <p className="mt-2 mb-5 text-xs text-muted">{t.cli.grants}</p>
      {approve.error && <ErrorNote>{approve.error.message}</ErrorNote>}
      <div className="mt-4 flex gap-3">
        <Button type="button" className="flex-1" disabled={done} onClick={() => approve.mutate()}>
          {done ? t.cli.approving : t.cli.approve}
        </Button>
        <Button type="button" variant="ghost" disabled={done} onClick={deny}>
          {t.cli.deny}
        </Button>
      </div>
    </AuthCard>
  )
}
