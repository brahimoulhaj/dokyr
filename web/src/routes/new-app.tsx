import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams } from '@tanstack/react-router'
import { useState, type FormEvent } from 'react'
import { NewFromCompose } from '../components/compose'
import { ContextBreadcrumb } from '../components/context-breadcrumb'
import { FormActions, NameList, NewAppLayout, SummaryRows } from '../components/new-app-layout'
import { builderLabel, SourceFields, sourceForm, sourceInput, usesDockerfile, type OtherSource, type SourceForm } from '../components/source'
import { NewFromTemplate } from '../components/templates'
import { Button, ErrorNote, Field, HelpTip, PageHeader, QueryState } from '../components/ui'
import { api, gitSourcesQuery, projectQuery, projectsQuery, registriesQuery, type App } from '../lib/api'
import { environmentMap, parseEnvironment } from '../lib/environment'
import { useT } from '../lib/i18n'

/**
 * The page a new app is made on: the form on one side, a summary of what it will create on
 * the other. The source picked in the form decides which form it is: an image or a Git
 * repository here, a Compose file or a template in their own components.
 */
export function NewAppPage() {
  const { projectId } = useParams({ strict: false }) as { projectId: string }
  const project = useQuery(projectQuery(projectId))
  const navigate = useNavigate()
  const t = useT()
  if (!project.data) {
    return (
      <div className="space-y-4">
        <ContextBreadcrumb projectId={projectId} />
        <QueryState loading={project.isPending} error={project.error} what={t.project.what} />
      </div>
    )
  }
  return (
    <div className="space-y-6">
      <PageHeader crumbs={<ContextBreadcrumb projectId={projectId} projectName={project.data.name} />} title={t.project.newApp} />
      <NewApp projectId={projectId} onDone={() => navigate({ to: '/projects/$projectId', params: { projectId }, search: { tab: 'apps' } })} />
    </div>
  )
}

/** onDone leaves the page without an app to open: cancelled, or several apps were created. */
function NewApp({ projectId, onDone }: { projectId: string; onDone: () => void }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [source, setSource] = useState(sourceForm())
  const [port, setPort] = useState('80')
  const [envText, setEnvText] = useState('')
  const [envError, setEnvError] = useState('')
  const [limits, setLimits] = useState({ cpu: '0', memory: '0' })
  const [other, setOther] = useState<OtherSource | null>(null)
  const t = useT()

  const create = useMutation({
    mutationFn: (environment: Record<string, string>) =>
      api<App>(`/projects/${projectId}/apps`, {
        method: 'POST',
        body: { name, port: Number(port), ...sourceInput(source), environment, cpu_limit: Number(limits.cpu) || 0, memory_limit_mb: Number(limits.memory) || 0 },
      }),
    onSuccess: (a) => {
      queryClient.invalidateQueries({ queryKey: projectsQuery.queryKey })
      navigate({ to: '/projects/$projectId/apps/$appId', params: { projectId, appId: a.id } })
    },
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    let environment: Record<string, string>
    try { environment = environmentMap(parseEnvironment(envText)) } catch { setEnvError(t.app.environmentInvalid); return }
    setEnvError('')
    create.mutate(environment)
  }

  // What the form says so far, for the summary card. Variable values stay out of it.
  let variables: string[] | null
  try { variables = Object.keys(environmentMap(parseEnvironment(envText))) } catch { variables = null }
  const registries = useQuery(registriesQuery)
  const gitSources = useQuery(gitSourcesQuery)
  const image = source.source_type === 'image'
  const rows = [
    { label: t.common.name, value: name, ltr: true },
    { label: t.source.source, value: image ? t.source.image : t.source.gitRepository },
    ...(image
      ? [
          { label: t.source.image, value: source.image.trim(), ltr: true },
          { label: t.source.registryAccess, value: registries.data?.find((r) => r.id === source.registry_id)?.name ?? t.source.publicImage },
        ]
      : [
          { label: t.source.repositoryUrl, value: source.git_repository.trim(), ltr: true },
          { label: t.source.branch, value: source.git_branch.trim(), ltr: true },
          { label: t.source.repositoryAccess, value: gitSources.data?.find((g) => g.id === source.git_source_id)?.name ?? t.source.publicByUrl },
          { label: t.source.builder, value: builderLabel(source.builder, t.source.builderAuto) },
          ...(source.build_context.trim() ? [{ label: t.source.buildContext, value: source.build_context.trim(), ltr: true }] : []),
          ...(usesDockerfile(source.builder) ? [{ label: 'Dockerfile', value: source.dockerfile_path.trim(), ltr: true }] : []),
          ...((source.builder === 'nixpacks' || source.builder === 'static') && source.build_command.trim() ? [{ label: t.source.buildCommand, value: source.build_command.trim(), ltr: true }] : []),
          ...(source.builder === 'static' ? [{ label: t.source.outputDir, value: source.output_dir.trim() || '.', ltr: true }] : []),
          ...(source.builder === 'buildpacks' ? [{ label: t.source.builderImage, value: source.build_image.trim() || 'heroku/builder:24', ltr: true }] : []),
        ]),
    { label: t.project.containerPort, value: port, ltr: true },
    { label: t.app.environment, value: variables === null ? <span className="text-danger">{t.app.environmentInvalid}</span> : variables.length === 0 ? t.newApp.none : <NameList names={variables} /> },
    {
      label: t.app.settingsSections.resources,
      value: `${Number(limits.cpu) > 0 ? t.app.cpuValue(Number(limits.cpu)) : t.app.cpuUnlimited} · ${Number(limits.memory) > 0 ? t.app.memoryValue(Number(limits.memory)) : t.app.memoryUnlimited}`,
    },
  ]

  if (other) {
    const onSource = (v: SourceForm['source_type'] | OtherSource) => {
      if (v === 'image' || v === 'git') { setSource({ ...source, source_type: v }); setOther(null) } else setOther(v)
    }
    return other === 'template'
      ? <NewFromTemplate projectId={projectId} onDone={onDone} onSource={onSource} />
      : <NewFromCompose projectId={projectId} onDone={onDone} onSource={onSource} />
  }

  return (
    <NewAppLayout onSubmit={submit} summary={<SummaryRows rows={rows} />}>
      <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
        <Field
          label={t.common.name}
          name="name"
          required
          autoFocus
          dir="ltr"
          placeholder="web"
          pattern="[a-z0-9]([a-z0-9\-]{0,38}[a-z0-9])?"
          title={t.project.nameTitle}
          value={name}
          onChange={(e) => setName(e.target.value.toLowerCase())}
        />
        <Field label={t.project.containerPort} name="port" type="number" required min={1} max={65535} value={port} onChange={(e) => setPort(e.target.value)} />
      </div>
      <SourceFields form={source} onChange={setSource} onOther={setOther} />
      <details className="group border border-line" open={!!envError || undefined}>
        <summary className="flex cursor-pointer items-center justify-between px-3 py-2.5 text-sm text-muted hover:text-fg">
          <span>{t.app.environment} <span className="text-xs">({t.common.optional})</span></span>
          <span aria-hidden className="text-xs transition-transform group-open:rotate-90">›</span>
        </summary>
        <div className="space-y-1.5 border-t border-line p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted">
            <label htmlFor="new-app-env" dir="ltr">.env</label>
            <HelpTip label={t.app.environment}>{t.app.environmentFormat}</HelpTip>
          </div>
          <textarea id="new-app-env" dir="ltr" spellCheck={false} value={envText} onChange={(e) => { setEnvText(e.target.value); setEnvError('') }}
            className="min-h-32 w-full resize-y border border-line bg-bg p-3 font-mono text-xs text-fg placeholder:text-muted/60 focus:border-accent focus:outline-none"
            placeholder={'APP_ENV=production\nDATABASE_URL=${db.main.URL}'} />
          {envError && <ErrorNote>{envError}</ErrorNote>}
        </div>
      </details>
      <details className="group border border-line">
        <summary className="flex cursor-pointer items-center justify-between px-3 py-2.5 text-sm text-muted hover:text-fg">
          <span>{t.app.settingsSections.resources} <span className="text-xs">({t.common.optional})</span></span>
          <span aria-hidden className="text-xs transition-transform group-open:rotate-90">›</span>
        </summary>
        <div className="grid gap-4 border-t border-line p-3 sm:grid-cols-2">
          <Field label={t.app.cpuLimit} name="cpu" dir="ltr" type="number" min={0} max={256} step="0.05" hintTooltip hint={t.app.zeroUnlimited} value={limits.cpu} onChange={(e) => setLimits({ ...limits, cpu: e.target.value })} />
          <Field label={t.app.memoryLimit} name="memory" dir="ltr" type="number" min={0} step="1" hintTooltip hint={t.app.zeroUnlimited} value={limits.memory} onChange={(e) => setLimits({ ...limits, memory: e.target.value })} />
        </div>
      </details>
      {create.error && <ErrorNote>{create.error.message}</ErrorNote>}
      <FormActions>
        <Button type="submit" disabled={create.isPending}>
          {create.isPending ? t.common.creating : t.project.createApp}
        </Button>
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </FormActions>
    </NewAppLayout>
  )
}

