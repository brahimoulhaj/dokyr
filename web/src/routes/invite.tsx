import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { AuthCard, Button, ErrorNote, Field } from '../components/ui'
import { api, meQuery, metaQuery, type Role, type User } from '../lib/api'
import { useT } from '../lib/i18n'

const route = getRouteApi('/invite')

/** Where an invite link lands: the invited person picks a password and is signed in. */
export function InvitePage() {
  const { token } = route.useSearch()
  const { data: meta } = useSuspenseQuery(metaQuery)
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const t = useT()
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [mismatch, setMismatch] = useState(false)

  const invite = useQuery({
    queryKey: ['invite', token],
    queryFn: () => api<{ email: string; role: Role }>('/invites/lookup', { method: 'POST', body: { token } }),
    retry: false,
    staleTime: Infinity,
  })
  const accept = useMutation({
    mutationFn: () => api<User>('/invites/accept', { method: 'POST', body: { token, password } }),
    onSuccess: (user) => {
      queryClient.clear() // another account may have been signed in here
      queryClient.setQueryData(metaQuery.queryKey, meta)
      queryClient.setQueryData(meQuery.queryKey, user)
      navigate({ to: '/' })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    const differs = password !== confirm
    setMismatch(differs)
    if (!differs) accept.mutate()
  }

  if (invite.isPending) return null
  if (!invite.data) {
    return (
      <AuthCard product={meta.name} title={t.users.badInviteTitle} subtitle={t.users.badInviteSubtitle}>
        <p className="text-sm text-muted">{t.users.badInviteBody}</p>
      </AuthCard>
    )
  }

  return (
    <AuthCard product={meta.name} title={t.users.joinTitle} subtitle={t.users.joinSubtitle(t.users.roles[invite.data.role] ?? invite.data.role)}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t.common.email} name="email" type="email" dir="ltr" autoComplete="username" readOnly value={invite.data.email} />
        <Field label={t.common.password} name="password" type="password" dir="ltr" autoComplete="new-password" required autoFocus minLength={meta.weak_passwords ? 1 : 8} hint={meta.weak_passwords ? undefined : t.auth.passwordHint} value={password} onChange={(e) => setPassword(e.target.value)} />
        <Field label={t.auth.confirmPassword} name="confirm" type="password" dir="ltr" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        {mismatch && <ErrorNote>{t.auth.mismatch}</ErrorNote>}
        {accept.error && !mismatch && <ErrorNote>{accept.error.message}</ErrorNote>}
        <Button type="submit" className="w-full" disabled={accept.isPending}>
          {accept.isPending ? t.common.creating : t.users.join}
        </Button>
      </form>
    </AuthCard>
  )
}
