import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from '@yusufceng1z/ding-react'
import { useState, type FormEvent } from 'react'
import { Sheet, SheetActions } from '../components/sheet'
import { Button, ErrorNote, Field, Panel, QueryState } from '../components/ui'
import { api, buildSettingsQuery, type BuildLimits, type BuildSettings } from '../lib/api'
import { useT } from '../lib/i18n'

function useDescribe() {
  const tb = useT().settingsBuilds
  return (l: BuildLimits) =>
    [l.cpus > 0 ? tb.cpuValue(l.cpus) : tb.cpuNoCap, l.memory_mb > 0 ? tb.memoryValue(l.memory_mb) : tb.memoryNoCap] as const
}

/** CPU and memory caps shared by Git builds: the caps in force, edited in a sheet. */
export function BuildSettingsSection() {
  const settings = useQuery(buildSettingsQuery)
  const t = useT()
  const tb = t.settingsBuilds
  const describe = useDescribe()
  const [open, setOpen] = useState(false)
  return (
    <Panel
      title={tb.title}
      actions={settings.data && <Button variant="ghost" className="h-7 px-2 text-xs" onClick={() => setOpen(true)}>{t.common.edit}</Button>}
    >
      {!settings.data ? (
        <div className="p-4">
          <QueryState loading={settings.isPending} error={settings.error} what={tb.what} />
        </div>
      ) : (
        <div className="space-y-1 p-4 text-sm">
          <p>{tb.current(...describe(settings.data))}</p>
          <p className="text-xs text-muted">{tb.defaults(...describe(settings.data.defaults))}</p>
        </div>
      )}
      <Sheet open={open} onOpenChange={setOpen} title={tb.title} description={tb.intro}>
        {open && settings.data && <BuildSettingsForm current={settings.data} onDone={() => setOpen(false)} />}
      </Sheet>
    </Panel>
  )
}

function BuildSettingsForm({ current, onDone }: { current: BuildSettings; onDone: () => void }) {
  const queryClient = useQueryClient()
  const t = useT()
  const tb = t.settingsBuilds
  const [cpus, setCpus] = useState(String(current.cpus))
  const [memory, setMemory] = useState(String(current.memory_mb))
  const describe = useDescribe()

  const onSuccess = (s: BuildSettings) => {
    queryClient.setQueryData(buildSettingsQuery.queryKey, s)
    toast.success(tb.saved)
    onDone()
  }
  const save = useMutation({
    mutationFn: (body: BuildLimits) => api<BuildSettings>('/settings/builds', { method: 'PUT', body }),
    onSuccess,
  })
  const reset = useMutation({
    mutationFn: () => api<BuildSettings>('/settings/builds', { method: 'DELETE' }),
    onSuccess,
  })

  const input: BuildLimits = { cpus: Number(cpus), memory_mb: Number(memory) }
  const dirty = input.cpus !== current.cpus || input.memory_mb !== current.memory_mb
  const atDefaults = current.cpus === current.defaults.cpus && current.memory_mb === current.defaults.memory_mb
  const error = save.error ?? reset.error

  function submit(e: FormEvent) {
    e.preventDefault()
    save.mutate(input)
  }

  return (
    <form onSubmit={submit} className="space-y-5 p-5">
      <div className="space-y-5">
        <Field
          label={tb.cpus}
          name="build-cpus"
          dir="ltr"
          type="number"
          min={0}
          max={current.host_cpus}
          step="0.1"
          hintTooltip
          hint={tb.cpusHint(current.host_cpus)}
          value={cpus}
          onChange={(e) => setCpus(e.target.value)}
        />
        <Field
          label={tb.memory}
          name="build-memory"
          dir="ltr"
          type="number"
          min={0}
          step="1"
          hintTooltip
          hint={tb.memoryHint}
          value={memory}
          onChange={(e) => setMemory(e.target.value)}
        />
      </div>
      <div className="space-y-1 text-xs text-muted">
        <p>{tb.defaults(...describe(current.defaults))}</p>
        <p>{tb.applies}</p>
      </div>
      {error && <ErrorNote>{error.message}</ErrorNote>}
      <SheetActions>
        <Button type="submit" disabled={!dirty || save.isPending}>
          {save.isPending ? t.common.saving : t.common.save}
        </Button>
        {!atDefaults && (
          <Button type="button" variant="ghost" disabled={reset.isPending} onClick={() => reset.mutate()}>
            {tb.reset}
          </Button>
        )}
        <Button type="button" variant="ghost" onClick={onDone}>
          {t.common.cancel}
        </Button>
      </SheetActions>
    </form>
  )
}
