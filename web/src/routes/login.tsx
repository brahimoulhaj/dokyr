import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { Fingerprint, GithubLogo } from '@phosphor-icons/react'
import { useState, type FormEvent } from 'react'
import { AuthCard, Button, ErrorNote, Field } from '../components/ui'
import { api, meQuery, metaQuery, type User } from '../lib/api'
import { useT } from '../lib/i18n'
import { loginWithPasskey, passkeysSupported } from '../lib/passkeys'

export function LoginPage() {
  const { data: meta } = useSuspenseQuery(metaQuery)
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const t = useT()
  const search = useSearch({ strict: false }) as { auth_error?: string }
  const methods = useQuery({ queryKey: ['auth', 'methods'], queryFn: () => api<{ passkeys: boolean; github: boolean }>('/auth/methods'), staleTime: 0 })
  function signedIn(user: User) {
    queryClient.setQueryData(meQuery.queryKey, user)
    navigate({ to: '/' })
  }
  const passkey = useMutation({ mutationFn: loginWithPasskey, onSuccess: signedIn })
  const github = useMutation({
    mutationFn: () => api<{ url: string }>('/auth/github/start', { method: 'POST', body: {} }),
    onSuccess: ({ url }) => location.assign(url),
  })
  const busy = passkey.isPending || github.isPending

  const login = useMutation({
    mutationFn: () => api<User>('/auth/login', { method: 'POST', body: { email, password } }),
    onSuccess: signedIn,
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    login.mutate()
  }

  return (
    <AuthCard product={meta.name} title={t.auth.signIn} subtitle={t.auth.signInSubtitle}>
      <form onSubmit={submit} className="space-y-4">
        <Field label={t.common.email} name="email" type="email" dir="ltr" autoComplete="username" required autoFocus value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field label={t.common.password} name="password" dir="ltr" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        {login.error && <ErrorNote>{login.error.message}</ErrorNote>}
        <Button type="submit" className="w-full" disabled={login.isPending || busy}>
          {login.isPending ? t.auth.signingIn : t.auth.signIn}
        </Button>
      </form>
      {(methods.data?.passkeys || methods.data?.github) && <div className="mt-4 space-y-2 border-t border-line pt-4">
        {methods.data.passkeys && <Button type="button" variant="ghost" className="w-full justify-center gap-2" disabled={login.isPending || busy || !passkeysSupported()} onClick={() => passkey.mutate()}>
          <Fingerprint size={16} />{passkey.isPending ? t.auth.signingIn : t.security.signInPasskey}
        </Button>}
        {methods.data.github && <Button type="button" variant="ghost" className="w-full justify-center gap-2" disabled={login.isPending || busy} onClick={() => github.mutate()}>
          <GithubLogo size={16} />{t.security.signInGitHub}
        </Button>}
        {methods.data.passkeys && <p className="text-center text-xs text-muted">{passkeysSupported() ? t.security.enrollHint : t.security.unsupported}</p>}
      </div>}
      {passkey.error && <div className="mt-3"><ErrorNote>{passkey.error instanceof DOMException && passkey.error.name === 'NotAllowedError' ? t.security.cancelled : passkey.error.message}</ErrorNote></div>}
      {github.error && <div className="mt-3"><ErrorNote>{github.error.message}</ErrorNote></div>}
      {search.auth_error && <div className="mt-3"><ErrorNote>{t.security.githubError}</ErrorNote></div>}
    </AuthCard>
  )
}
