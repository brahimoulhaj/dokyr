import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { ActionsMenu } from '../components/actions-menu'
import { ConfirmModal } from '../components/confirm-dialog'
import { PasswordConfirmDialog } from '../components/password-confirm-dialog'
import { Sheet, SheetActions } from '../components/sheet'
import { Badge, Button, CheckOption, cx, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { api, inviteMailQuery, invitesQuery, meQuery, usersQuery, type Invite, type Role, type User } from '../lib/api'
import { dateTime, timeAgo } from '../lib/format'
import { rich, useT } from '../lib/i18n'

type MemberRole = Exclude<Role, 'owner'>
const memberRoles: MemberRole[] = ['maintainer', 'admin']

/** The people who can sign in, their roles, and the invites not accepted yet. Owner only. */
export function UsersSettings() {
  const t = useT()
  const users = useQuery(usersQuery)
  const invites = useQuery(invitesQuery)
  const { data: me } = useQuery(meQuery)
  const [inviting, setInviting] = useState(false)
  // A link just made by "New link" on a pending invite; the sheet shows it.
  const [renewed, setRenewed] = useState<Invite | null>(null)
  const open = inviting || renewed !== null
  const close = () => {
    setInviting(false)
    setRenewed(null)
  }

  return (
    <>
      <Panel title={t.users.title} actions={<Button type="button" className="h-8 px-3 text-xs" onClick={() => setInviting(true)}>{t.users.invite}</Button>}>
        {!users.data && (
          <div className="p-4">
            <QueryState loading={users.isPending} error={users.error} what={t.users.what} />
          </div>
        )}
        {users.data && (
          <ul className="divide-y divide-line">
            {users.data.map((user) => (
              <UserRow key={user.id} user={user} self={user.id === me?.id} />
            ))}
          </ul>
        )}
      </Panel>
      {invites.error && <ErrorNote>{invites.error.message}</ErrorNote>}
      {invites.data && invites.data.length > 0 && (
        <Panel title={t.users.pending}>
          <ul className="divide-y divide-line">
            {invites.data.map((invite) => (
              <InviteRow key={invite.id} invite={invite} onRenewed={setRenewed} />
            ))}
          </ul>
        </Panel>
      )}
      <Sheet open={open} onOpenChange={(o) => !o && close()} title={renewed ? t.users.linkTitle : t.users.inviteTitle}>
        {open && <InviteForm created={renewed} onDone={close} />}
      </Sheet>
    </>
  )
}

function UserRow({ user, self }: { user: User; self: boolean }) {
  const queryClient = useQueryClient()
  const t = useT()
  const [removing, setRemoving] = useState(false)
  const [handing, setHanding] = useState(false)
  const done = () => queryClient.invalidateQueries({ queryKey: usersQuery.queryKey })
  const setRole = useMutation({
    mutationFn: (role: MemberRole) => api<User>(`/users/${user.id}`, { method: 'PATCH', body: { role } }),
    onSuccess: done,
  })
  const remove = useMutation({ mutationFn: () => api<void>(`/users/${user.id}`, { method: 'DELETE' }), onSuccess: done })
  const error = setRole.error ?? remove.error
  const other = user.role === 'admin' ? 'maintainer' : 'admin'

  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <span dir="ltr" className="truncate">{user.email}</span>
          <Badge tone={user.role === 'owner' ? 'accent' : 'muted'}>{t.users.roles[user.role] ?? user.role}</Badge>
          {self && <span className="text-xs text-muted">{t.users.you}</span>}
        </p>
        <p className="text-xs text-muted" title={dateTime(user.created_at)}>{t.users.joined(timeAgo(user.created_at))}</p>
        {error && <p className="text-xs text-danger">{error.message}</p>}
      </div>
      {user.role !== 'owner' && (
        <ActionsMenu
          label={t.users.actions}
          ariaLabel={`${t.users.actions}: ${user.email}`}
          disabled={setRole.isPending || remove.isPending}
          items={[
            { label: t.users.makeRole(t.users.roles[other]), onSelect: () => setRole.mutate(other) },
            { label: t.users.makeOwner, danger: true, onSelect: () => setHanding(true) },
            { label: t.users.remove, danger: true, onSelect: () => setRemoving(true) },
          ]}
        />
      )}
      <ConfirmModal open={removing} onOpenChange={setRemoving} title={t.users.removeTitle} message={t.users.removeMessage(user.email)} confirm={t.users.yesRemove} onConfirm={() => remove.mutate()} />
      {handing && (
        <PasswordConfirmDialog
          action={{
            title: t.users.makeOwnerTitle,
            confirm: t.users.makeOwnerConfirm,
            detail: t.users.makeOwnerDetail(user.email),
            tone: 'danger',
            onConfirm: (password) => api<User>(`/users/${user.id}/make-owner`, { method: 'POST', body: { password } }),
          }}
          onClose={() => setHanding(false)}
          onComplete={() => {
            setHanding(false)
            // The page is the owner's: once handed over, what this account may see changes.
            void queryClient.invalidateQueries({ queryKey: meQuery.queryKey })
            void done()
          }}
        />
      )}
    </li>
  )
}

function InviteRow({ invite, onRenewed }: { invite: Invite; onRenewed: (i: Invite) => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const [revoking, setRevoking] = useState(false)
  const done = () => queryClient.invalidateQueries({ queryKey: invitesQuery.queryKey })
  // Inviting the same email again replaces the invite, so the old link stops working.
  const renew = useMutation({
    mutationFn: () => api<Invite>('/invites', { method: 'POST', body: { email: invite.email, role: invite.role } }),
    onSuccess: (i) => {
      done()
      onRenewed(i)
    },
  })
  const revoke = useMutation({ mutationFn: () => api<void>(`/invites/${invite.id}`, { method: 'DELETE' }), onSuccess: done })
  const error = renew.error ?? revoke.error

  return (
    <li className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-4 py-3">
      <div className="min-w-0">
        <p className="flex flex-wrap items-center gap-2 text-sm">
          <span dir="ltr" className="truncate">{invite.email}</span>
          <Badge tone="muted">{t.users.roles[invite.role] ?? invite.role}</Badge>
          {invite.expired && <Badge tone="warn">{t.users.expired}</Badge>}
        </p>
        {!invite.expired && <p className="text-xs text-muted">{t.users.expires(dateTime(invite.expires_at))}</p>}
        {error && <p className="text-xs text-danger">{error.message}</p>}
      </div>
      <ActionsMenu
        label={t.users.actions}
        ariaLabel={`${t.users.actions}: ${invite.email}`}
        disabled={renew.isPending || revoke.isPending}
        items={[
          { label: t.users.newLink, onSelect: () => renew.mutate() },
          { label: t.users.revoke, danger: true, onSelect: () => setRevoking(true) },
        ]}
      />
      <ConfirmModal open={revoking} onOpenChange={setRevoking} title={t.users.revokeTitle} message={t.users.revokeMessage(invite.email)} confirm={t.users.yesRevoke} onConfirm={() => revoke.mutate()} />
    </li>
  )
}

/** The invite sheet: the form, then the link it made. `created` skips straight to a link. */
function InviteForm({ created, onDone }: { created: Invite | null; onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<MemberRole>('maintainer')
  const [send, setSend] = useState(false)
  const mail = useQuery(inviteMailQuery).data
  const create = useMutation({
    mutationFn: () => api<Invite>('/invites', { method: 'POST', body: { email, role, send: send && !!mail?.available } }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: invitesQuery.queryKey }),
  })
  const invite = created ?? create.data
  if (invite) return <InviteLink invite={invite} onDone={onDone} />

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate()
  }

  return (
    <form onSubmit={submit} className="space-y-4 p-5">
      <Field label={t.common.email} name="invite-email" type="email" dir="ltr" required autoFocus autoComplete="off" value={email} onChange={(e) => setEmail(e.target.value)} />
      <fieldset className="space-y-2">
        <legend className="mb-1.5 text-xs text-muted">{t.users.role}</legend>
        {memberRoles.map((r) => (
          <label key={r} className={cx('flex cursor-pointer gap-3 border p-3', role === r ? 'border-accent' : 'border-line hover:border-line-strong')}>
            <input type="radio" name="invite-role" value={r} checked={role === r} onChange={() => setRole(r)} className="mt-0.5 accent-(--color-accent)" />
            <span className="min-w-0">
              <span className="block text-sm">{t.users.roles[r]}</span>
              <span className="mt-0.5 block text-xs text-muted">{t.users.roleHints[r]}</span>
            </span>
          </label>
        ))}
      </fieldset>
      {mail?.available ? (
        <CheckOption checked={send} onChange={setSend} label={t.users.sendEmail} hint={t.users.sendEmailHint(mail.channel)} />
      ) : mail && (
        <p className="text-xs text-muted">{t.users.sendEmailUnavailable}</p>
      )}
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!email.trim() || create.isPending}>
          {create.isPending ? t.common.creating : t.users.createInvite}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </SheetActions>
    </form>
  )
}

function InviteLink({ invite, onDone }: { invite: Invite; onDone: () => void }) {
  const t = useT()
  const [copied, setCopied] = useState(false)
  const link = `${location.origin}/invite?token=${invite.token ?? ''}`

  async function copy() {
    try {
      await navigator.clipboard.writeText(link)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="space-y-4 p-5">
      {invite.emailed && <p className="text-sm">{t.users.emailed(invite.email)}</p>}
      {invite.email_error && <p className="text-sm text-warn">{t.users.emailFailed(invite.email_error)}</p>}
      <p className="text-sm">
        {rich(t.users.linkReady, {
          email: () => <span dir="ltr" className="text-accent">{invite.email}</span>,
          date: () => <span>{dateTime(invite.expires_at)}</span>,
        })}
      </p>
      <textarea
        readOnly
        rows={3}
        value={link}
        aria-label={t.users.linkLabel}
        dir="ltr"
        onFocus={(e) => e.target.select()}
        className="block w-full resize-none border border-accent bg-bg p-3 text-xs break-all text-fg focus:outline-none"
      />
      <SheetActions>
        <Button type="button" onClick={copy}>
          {copied ? t.common.copied : t.common.copy}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.done}
        </Button>
      </SheetActions>
    </div>
  )
}
