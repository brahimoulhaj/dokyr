import { HelpTip, PageHeader } from '../components/ui'
import { useT } from '../lib/i18n'
import { PanelBackupSettings } from './settings-backup'
import { GitConnections } from './settings-git'
import { ApiTokens, TokensHelp } from './settings-tokens'

// Pages of the main menu that were sections of Settings: each is its section under a page title.

export function GitPage() {
  const t = useT()
  return (
    <div className="space-y-6">
      <PageHeader title={<>{t.settingsGit.title}<HelpTip label={t.settingsGit.title}>{t.settingsGit.intro}</HelpTip></>} />
      <GitConnections bare />
    </div>
  )
}

export function BackupsPage() {
  const t = useT()
  return (
    <div className="space-y-6">
      <PageHeader title={t.settingsBackup.title} />
      <PanelBackupSettings />
    </div>
  )
}

export function TokensPage() {
  const t = useT()
  return (
    <div className="space-y-6">
      <PageHeader title={<>{t.settingsTokens.title}<TokensHelp /></>} />
      <ApiTokens bare />
    </div>
  )
}
