import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { api, projectsQuery, templatesQuery, type App, type Template } from '../lib/api'
import { useT } from '../lib/i18n'
import { FormActions, NameList, NewAppLayout, SummaryRows } from './new-app-layout'
import { SourceToggle, type OtherSource, type SourceForm } from './source'
import { Button, cx, Empty, ErrorNote, Field, QueryState } from './ui'
import { fold } from './list-controls'
import { TemplateIcon } from './template-icon'

const engineNames: Record<string, string> = { postgres: 'PostgreSQL', mariadb: 'MariaDB' }

/**
 * The new app form with a template as the source: pick one, name it, and the app is created
 * with its volumes and variables, not deployed, beside the other apps of a template made of several. A template that needs a database
 * leaves its variables empty: the app page asks to connect one. onSource leaves for another source.
 */
export function NewFromTemplate({ projectId, onDone, onSource }: { projectId: string; onDone: () => void; onSource: (v: SourceForm['source_type'] | OtherSource) => void }) {
  const t = useT()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const templates = useQuery(templatesQuery)
  const [picked, setPicked] = useState('')
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState('')
  // Empty until typed: the name then follows the picked template.
  const [name, setName] = useState('')
  const categories = [...new Set((templates.data ?? []).map((x) => x.category))]
  const categoryLabel = (c: string) => (t.templates.categories as Record<string, string>)[c] ?? c
  const words = fold(query).split(/\s+/).filter(Boolean)
  // Every word typed must be somewhere in the template: its name, what it is, its category or image.
  const shown = (templates.data ?? []).filter((x) => {
    if (category && x.category !== category) return false
    const text = fold([x.name, x.id, t.templates.descriptions[x.id] ?? x.description, categoryLabel(x.category), x.image, x.database?.engine ?? '', ...(x.services ?? []).map((y) => y.image)].join(' '))
    return words.every((w) => text.includes(w))
  })
  // The pick stays while it is in view; otherwise the first one shown is picked.
  const found = shown.find((x) => x.id === picked) ?? shown[0]
  // Lists a panel of another version may leave out.
  const template = found && { ...found, volumes: found.volumes ?? [], variables: found.variables ?? [], services: found.services ?? [] }

  const create = useMutation({
    mutationFn: (tpl: Template) =>
      api<{ app: App }>(`/projects/${projectId}/templates/${tpl.id}`, { method: 'POST', body: { name: name || tpl.id } }),
    onSuccess: ({ app }) => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId, appId: app.id } })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    if (template) create.mutate(template)
  }

  const rows = [
    { label: t.common.name, value: name || template?.id, ltr: true },
    { label: t.source.source, value: template && `${t.templates.template} · ${template.name}` },
    { label: t.source.image, value: template?.image, ltr: true },
    { label: t.project.containerPort, value: template && String(template.port), ltr: true },
    { label: t.databases.title, value: template && (template.database ? t.connectDb.afterCreate(engineNames[template.database.engine] ?? template.database.engine) : t.newApp.none) },
    { label: t.app.settingsSections.volumes, value: template && (template.volumes.length ? <NameList names={template.volumes.map((v) => v.target)} /> : t.newApp.none) },
    { label: t.app.environment, value: template && (template.variables.length ? <NameList names={template.variables} /> : t.newApp.none) },
    // Only for a template of several apps: the others, as they will be named.
    ...(template?.services.length ? [{ label: t.templates.alsoCreates, value: <NameList names={template.services.map((x) => `${name || template.id}-${x.name}`)} /> }] : []),
  ]

  return (
    <NewAppLayout onSubmit={submit} summary={<SummaryRows rows={rows} />}>
      <Field
        label={t.common.name}
        name="name"
        dir="ltr"
        placeholder={template?.id ?? 'web'}
        // Templates with a database name it after the app: it starts with a letter.
        pattern="[a-z]([a-z0-9\-]{0,38}[a-z0-9])?"
        title={t.project.nameTitle}
        value={name}
        onChange={(e) => setName(e.target.value.toLowerCase())}
      />
      <div>
        <SourceToggle value="template" withOthers onChange={(v) => v !== 'template' && onSource(v)} />
      </div>
      <QueryState loading={templates.isPending} error={templates.error} what={t.templates.what} />
      {templates.data && (
        <div className="space-y-3">
          <input
            type="search"
            aria-label={t.templates.search}
            placeholder={t.templates.search}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="block h-9 w-full border border-line bg-bg px-3 text-sm text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none"
          />
          <div role="radiogroup" aria-label={t.templates.category} className="flex flex-wrap gap-1.5">
            {['', ...categories].map((c) => (
              <button key={c} type="button" role="radio" aria-checked={category === c} onClick={() => setCategory(c)}
                className={cx('h-7 border px-2.5 text-xs transition-colors', category === c ? 'border-accent bg-accent text-accent-fg' : 'border-line text-muted hover:border-line-strong hover:text-fg')}>
                {c === '' ? t.templates.all(templates.data.length) : categoryLabel(c)}
              </button>
            ))}
          </div>
          {shown.length === 0 ? (
            <Empty title={t.templates.noMatch} />
          ) : (
            <div role="radiogroup" aria-label={t.templates.pick} className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
              {shown.map((x) => {
                const on = x.id === template?.id
                return (
                  <button
                    key={x.id}
                    type="button"
                    role="radio"
                    aria-checked={on}
                    onClick={() => setPicked(x.id)}
                    className={cx('flex flex-col justify-start gap-2 border p-3 text-start transition-colors', on ? 'border-accent bg-raised' : 'border-line hover:border-line-strong')}
                  >
                    <span className="flex items-center gap-2.5">
                      <TemplateIcon id={x.id} name={x.name} />
                      <span className="min-w-0">
                        <span className="block truncate text-sm">{x.name}</span>
                        <span className="block truncate text-xs text-muted">
                          {[categoryLabel(x.category), x.database && (engineNames[x.database.engine] ?? x.database.engine), x.services?.length && t.templates.apps(1 + x.services.length)].filter(Boolean).join(' · ')}
                        </span>
                      </span>
                    </span>
                    <span className="block text-xs leading-relaxed text-muted">{t.templates.descriptions[x.id] ?? x.description}</span>
                  </button>
                )
              })}
            </div>
          )}
        </div>
      )}
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <FormActions>
        <Button type="submit" disabled={!template || create.isPending}>
          {create.isPending ? t.common.creating : t.templates.create(template?.name ?? '')}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </FormActions>
    </NewAppLayout>
  )
}
