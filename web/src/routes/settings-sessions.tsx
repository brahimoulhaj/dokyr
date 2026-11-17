import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Desktop, DeviceMobile } from '@phosphor-icons/react'
import { Badge, Button, ErrorNote, Panel, QueryState } from '../components/ui'
import type { PasswordAction } from '../components/password-confirm-dialog'
import { api } from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { useT } from '../lib/i18n'
import { useLogout } from '../lib/logout'

type Session = {
  id: string
  ip_address: string
  user_agent: string
  created_at: string
  last_used_at: string
  expires_at: string
  current: boolean
}
const sessionsQuery = { queryKey: ['auth', 'sessions'], queryFn: () => api<Session[]>('/auth/sessions') }

function deviceLabel(agent: string, unknown: string) {
  const browser = /Edg\//.test(agent) ? 'Edge' : /OPR\//.test(agent) ? 'Opera' : /Firefox\//.test(agent) ? 'Firefox' : /Chrome\/|CriOS\//.test(agent) ? 'Chrome / Chromium' : /Safari\//.test(agent) ? 'Safari' : unknown
  const platform = /Android/.test(agent) ? 'Android' : /iPhone|iPad|iPod/.test(agent) ? 'iOS' : /Windows/.test(agent) ? 'Windows' : /Macintosh|Mac OS X/.test(agent) ? 'macOS' : /Linux/.test(agent) ? 'Linux' : ''
  return platform ? `${browser} · ${platform}` : browser
}

export function ActiveSessions({ confirm }: { confirm: (action: PasswordAction) => void }) {
  const t = useT()
  const text = t.security
  const sessions = useQuery(sessionsQuery)
  const qc = useQueryClient()
  const logout = useLogout()
  const others = sessions.data?.filter((session) => !session.current) ?? []
  async function revoke(path: string, password: string) {
    await api(path, { method: 'DELETE', body: { password } })
    await qc.invalidateQueries({ queryKey: sessionsQuery.queryKey })
  }

  return <Panel title={text.sessions} actions={
    <Button type="button" variant="ghost" className="h-7 px-2 text-xs" disabled={!others.length || logout.isPending}
      onClick={() => confirm({ title: text.logoutOthers, confirm: text.logoutOthers, detail: text.logoutOthersHint, tone: 'danger', onConfirm: (password) => revoke('/auth/sessions/others', password) })}>
      {text.logoutOthers}
    </Button>
  }>
    <div className="space-y-3 p-4">
      <p className="text-sm text-muted">{text.sessionsHint}</p>
      {logout.error && <ErrorNote>{logout.error.message}</ErrorNote>}
      {!sessions.data && <QueryState loading={sessions.isPending} error={sessions.error} what={text.sessions} />}
    </div>
    {sessions.data && <ul className="divide-y divide-line border-t border-line">
      {sessions.data.map((session) => {
        const mobile = /Android|iPhone|iPad|iPod/.test(session.user_agent)
        const Icon = mobile ? DeviceMobile : Desktop
        const label = deviceLabel(session.user_agent, text.unknownBrowser)
        return <li key={session.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3 px-4 py-3">
          <div className="flex min-w-0 flex-1 items-start gap-3">
            <Icon size={19} className="mt-0.5 shrink-0 text-muted" aria-hidden />
            <div className="min-w-0 space-y-1.5">
              <div className="flex flex-wrap items-center gap-2"><span className="text-sm" title={session.user_agent || undefined}>{label}</span>{session.current && <Badge>{text.currentSession}</Badge>}</div>
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted">
                <span>{text.ipAddress}: <span dir="ltr">{session.ip_address || text.unknownIP}</span></span>
                <span title={dateTime(session.created_at)}>{text.signedIn}: {dateTime(session.created_at)}</span>
                <span title={dateTime(session.last_used_at)}>{text.lastActive}: {timeAgo(session.last_used_at)}</span>
              </div>
            </div>
          </div>
          <Button type="button" variant={session.current ? 'ghost' : 'danger'} disabled={logout.isPending}
            onClick={() => session.current ? logout.mutate() : confirm({ title: text.logoutSession, confirm: t.shell.signOut, detail: `${label}${session.ip_address ? ` · ${session.ip_address}` : ''}`, tone: 'danger', onConfirm: (password) => revoke(`/auth/sessions/${session.id}`, password) })}>
            {session.current && logout.isPending ? t.shell.signingOut : t.shell.signOut}
          </Button>
        </li>
      })}
    </ul>}
  </Panel>
}
