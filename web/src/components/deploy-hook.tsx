import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api, appQuery, gitSourcesQuery, type App } from '../lib/api'
import { useT } from '../lib/i18n'
import { Button, CheckOption, ConfirmButton, ErrorNote, HelpTip, Panel, QueryState } from './ui'

type DeployHook = { enabled: boolean; token?: string }

const hookQuery = (appId: string) => ({
  queryKey: ['apps', appId, 'deploy-hook'] as const,
  queryFn: () => api<DeployHook>(`/apps/${appId}/deploy-hook`),
})

/** Whether this browser reaches the panel on an address a Git host cannot. */
function localAddress(host: string) {
  return /^(localhost|127\.|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.|\[?::1\]?)/.test(host) || host.endsWith('.local') || host.endsWith('.test')
}

/** The app's deploy webhook: turn on, copy the URL, replace it, turn off. */
export function DeployHookPanel({ app }: { app: App }) {
  const t = useT()
  const th = t.deployHook
  const queryClient = useQueryClient()
  const hook = useQuery(hookQuery(app.id))
  const [copied, setCopied] = useState(false)
  const done = (h: DeployHook) => {
    queryClient.setQueryData(hookQuery(app.id).queryKey, h)
    queryClient.invalidateQueries({ queryKey: appQuery(app.id).queryKey, exact: true })
    setCopied(false)
  }
  const create = useMutation({ mutationFn: () => api<DeployHook>(`/apps/${app.id}/deploy-hook`, { method: 'POST' }), onSuccess: done })
  const remove = useMutation({ mutationFn: () => api<DeployHook>(`/apps/${app.id}/deploy-hook`, { method: 'DELETE' }), onSuccess: done })
  const url = hook.data?.token ? `${location.origin}/api/hooks/deploy/${hook.data.token}` : ''
  const git = app.source_type === 'git'

  async function copy() {
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Panel title={<span className="inline-flex items-center gap-2">{th.title}<HelpTip label={th.title}>{th.intro}</HelpTip></span>}>
      <div className="space-y-3 p-4">
        {!hook.data ? (
          <QueryState loading={hook.isPending} error={hook.error} what={th.what} />
        ) : !hook.data.enabled ? (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-sm text-muted">{th.off}</p>
            <Button type="button" disabled={create.isPending} onClick={() => create.mutate()}>
              {th.turnOn}
            </Button>
          </div>
        ) : (
          <>
            <div className="space-y-1.5">
              <p className="text-xs text-muted">{th.url}</p>
              <div className="flex flex-col gap-2 sm:flex-row">
                <input
                  readOnly
                  value={url}
                  aria-label={th.url}
                  dir="ltr"
                  onFocus={(e) => e.target.select()}
                  className="block h-9 min-w-0 flex-1 border border-line bg-bg px-3 font-mono text-xs text-fg focus:border-accent focus:outline-none"
                />
                <Button type="button" variant="ghost" onClick={copy}>
                  {copied ? t.common.copied : t.common.copy}
                </Button>
              </div>
            </div>
            {localAddress(location.hostname) && <p className="text-xs text-warn">{th.unreachable}</p>}
            <p className="text-xs text-muted">{git ? th.gitHowTo(app.git_branch) : th.imageHowTo}</p>
            {!git && (
              <pre dir="ltr" className="overflow-x-auto border border-line bg-bg p-3 text-xs text-muted">
                curl -X POST {url} -H &quot;Content-Type: application/json&quot; -d &apos;{'{"tag":"1.4.2"}'}&apos;
              </pre>
            )}
            {git && <p className="text-xs text-muted">{th.queued}</p>}
            <div className="flex flex-wrap items-center gap-2 pt-1">
              <ConfirmButton label={th.newUrl} confirm={th.newUrlConfirm} pending={create.isPending} onConfirm={() => create.mutate()} />
              <ConfirmButton label={th.turnOff} confirm={th.turnOffConfirm} pending={remove.isPending} onConfirm={() => remove.mutate()} />
              <span className="text-xs text-muted">{th.newUrlHint}</span>
            </div>
          </>
        )}
        {(create.error || remove.error) && <ErrorNote>{(create.error ?? remove.error)!.message}</ErrorNote>}
      </div>
    </Panel>
  )
}

/** Deploy on push for a Git app: the GitHub App's own webhook, and the paths a push must touch. */
export function PushDeployPanel({ app }: { app: App }) {
  const t = useT()
  const th = t.deployHook
  const queryClient = useQueryClient()
  const sources = useQuery({ ...gitSourcesQuery, retry: false, enabled: !!app.git_source_id })
  const viaApp = sources.data?.find((s) => s.id === app.git_source_id)?.push_events ?? false
  const refresh = () => queryClient.invalidateQueries({ queryKey: appQuery(app.id).queryKey, exact: true })
  const toggle = useMutation({
    mutationFn: (on: boolean) => api<App>(`/apps/${app.id}/push-deploy`, { method: 'PUT', body: { on } }),
    onSuccess: refresh,
  })
  const saved = app.watch_paths.join('\n')
  const [draft, setDraft] = useState<string | null>(null)
  const text = draft ?? saved
  const save = useMutation({
    mutationFn: () => api<App>(`/apps/${app.id}`, { method: 'PATCH', body: { watch_paths: text.split('\n').map((l) => l.trim()).filter(Boolean) } }),
    onSuccess: () => { setDraft(null); refresh() },
  })
  if (app.source_type !== 'git') return null
  return (
    <Panel title={<span className="inline-flex items-center gap-2">{th.pushTitle}<HelpTip label={th.pushTitle}>{th.pushIntro}</HelpTip></span>}>
      <div className="space-y-4 p-4">
        {viaApp && (
          <div className="space-y-1">
            <CheckOption checked={app.push_deploy} disabled={toggle.isPending} onChange={(on) => toggle.mutate(on)} label={th.appPush(app.git_branch)} hint={th.appPushHint} />
            {toggle.error && <ErrorNote>{toggle.error.message}</ErrorNote>}
          </div>
        )}
        <form onSubmit={(e) => { e.preventDefault(); save.mutate() }} className="space-y-2">
          <label htmlFor="watch-paths" className="block text-xs text-muted">{th.watchPaths}</label>
          <textarea
            id="watch-paths"
            dir="ltr"
            rows={4}
            spellCheck={false}
            autoCapitalize="none"
            placeholder={'services/api/\npackages/shared\n!**/*.md'}
            value={text}
            onChange={(e) => setDraft(e.target.value)}
            className="block w-full border border-line bg-bg px-3 py-2 font-mono text-xs text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none"
          />
          <p className="text-xs text-muted">{th.watchPathsHint}</p>
          {save.error && <ErrorNote>{save.error.message}</ErrorNote>}
          <Button type="submit" disabled={draft === null || text === saved || save.isPending}>{save.isPending ? t.common.working : t.common.save}</Button>
        </form>
      </div>
    </Panel>
  )
}
