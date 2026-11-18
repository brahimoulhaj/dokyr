import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api, projectsQuery, type ComposeResult } from '../lib/api'
import { useT } from '../lib/i18n'
import { withToast } from '../lib/toasts'
import { FormActions, NewAppLayout } from './new-app-layout'
import { SourceToggle, type OtherSource, type SourceForm } from './source'
import { Badge, Button, cx, ErrorNote } from './ui'
import { useDebounced } from './list-controls'
import { YamlEditor } from './yaml-editor'

/** The line a converter message is about: "line 3: …" from the YAML reader, or the service it names. */
function errorLine(message: string, file: string): number | undefined {
  const at = /\bline (\d+)/.exec(message)
  if (at) return Number(at[1])
  const service = /^service (\S+?):? /.exec(message)
  if (!service) return undefined
  const i = file.split('\n').findIndex((l) => l.trim().replace(/["']/g, '') === service[1] + ':')
  return i < 0 ? undefined : i + 1
}

const example = `services:
  web:
    image: wordpress:6
    ports: ["8080:80"]
    environment:
      WORDPRESS_DB_HOST: db
    volumes: ["site:/var/www/html"]
  db:
    image: mariadb:11.4
    volumes: ["data:/var/lib/mysql"]`

/**
 * The new app form with a Compose file as the source. The file is converted, not run: the
 * summary shows the apps it becomes and what was changed or left out as it is typed. Nothing is deployed. onSource leaves for another source.
 */
export function NewFromCompose({ projectId, onDone, onSource }: { projectId: string; onDone: () => void; onSource: (v: SourceForm['source_type'] | OtherSource) => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const [file, setFile] = useState('')
  const send = (compose: string, dry_run: boolean) => api<ComposeResult>(`/projects/${projectId}/compose`, { method: 'POST', body: { compose, dry_run } })
  // Checked as it is typed, by the converter itself: what it accepts here is what it creates.
  const typed = useDebounced(file, 400)
  const check = useQuery({
    queryKey: ['compose-check', projectId, typed] as const,
    queryFn: () => send(typed, true),
    enabled: typed.trim() !== '',
    retry: false,
    staleTime: 30_000,
    gcTime: 0,
  })
  const settled = typed === file && !check.isFetching
  const problem = settled && file.trim() !== '' && check.error ? check.error.message : ''
  const create = useMutation({
    mutationFn: (n: number) => withToast(() => send(file, false), { loading: t.compose.creating(n), success: (r) => t.compose.created(r.apps.length), error: t.compose.failed }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      onDone()
    },
  })
  const valid = settled && file.trim() !== '' && check.data ? check.data : undefined
  function submit(e: FormEvent) {
    e.preventDefault()
    if (valid) create.mutate(valid.apps.length)
  }

  // The summary is the conversion itself: the apps the file becomes, and what it lost.
  const tones = { linked: 'accent', check: 'warn', skipped: 'muted' } as const
  const notes = (app: string) => {
    const list = valid?.notes.filter((n) => n.app === app) ?? []
    return list.length === 0 ? null : (
      <ul className="space-y-2 pt-2">
        {list.map((n) => (
          <li key={n.kind + n.text} className="space-y-1">
            <Badge tone={tones[n.kind]}>{t.compose.kinds[n.kind]}</Badge>
            <p className="text-xs leading-relaxed [overflow-wrap:anywhere]">{n.text}</p>
          </li>
        ))}
      </ul>
    )
  }
  const summary = !valid ? (
    <p className="px-4 py-3 text-sm text-muted">—</p>
  ) : (
    <ul className="divide-y divide-line">
      {valid.apps.map((a) => (
        <li key={a.name} className="px-4 py-3">
          <span className="block text-sm" dir="ltr">{a.name}</span>
          <span className="block text-xs text-muted [overflow-wrap:anywhere]">
            <span dir="ltr">{a.image}</span>{[t.compose.summary(a.port, a.variables.length, a.volumes.length)].filter(Boolean).map((x) => ` · ${x}`)}
          </span>
          {notes(a.name)}
        </li>
      ))}
      {notes('') && <li className="px-4 pb-3 pt-1">{notes('')}</li>}
    </ul>
  )

  return (
    <NewAppLayout onSubmit={submit} summary={summary}>
      <div>
        <SourceToggle value="compose" withOthers onChange={(v) => v !== 'compose' && onSource(v)} />
      </div>
      <div className="space-y-1.5">
        <label htmlFor="compose-file" className="block text-xs text-muted">{t.compose.file}</label>
        <YamlEditor id="compose-file" autoFocus value={file} onChange={setFile} placeholder={example} errorLine={problem ? errorLine(problem, file) : undefined} />
        <p role="status" className={cx('min-h-4 break-words text-xs', problem ? 'text-danger' : valid ? 'text-accent' : 'text-muted')}>
          {file.trim() === '' ? '' : problem ? problem : valid ? t.compose.valid(valid.apps.length, valid.notes.length) : t.compose.checking}
        </p>
      </div>
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <FormActions>
        <Button type="submit" disabled={!valid || create.isPending}>
          {create.isPending ? t.common.creating : t.compose.create(valid?.apps.length ?? 0)}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </FormActions>
    </NewAppLayout>
  )
}

