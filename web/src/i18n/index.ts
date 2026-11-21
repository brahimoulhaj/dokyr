// All texts of the web app, by language and then by screen. Each file exports en, fr, ar
// and es; en is the source, and the others are typed against it so a missing text fails
// the typecheck.
import * as common from './common'
import * as shell from './shell'
import * as auth from './auth'
import * as security from './security'
import * as theme from './theme'
import * as app from './app'
import * as logviewer from './logviewer'
import * as dns from './dns'
import * as project from './project'
import * as projects from './projects'
import * as overview from './overview'
import * as activity from './activity'
import * as source from './source'
import * as format from './format'
import * as settings from './settings'
import * as settingsGit from './settingsGit'
import * as settingsTokens from './settingsTokens'
import * as settingsUpdates from './settingsUpdates'
import * as settingsBuilds from './settingsBuilds'
import * as settingsBackup from './settingsBackup'
import * as storage from './storage'
import * as settingsStorage from './settingsStorage'
import * as settingsNotifications from './settingsNotifications'
import * as registries from './registries'
import * as builtinRegistry from './builtinRegistry'
import * as deployHook from './deployHook'
import * as healthCheck from './healthCheck'
import * as onboarding from './onboarding'
import * as terminal from './terminal'
import * as metrics from './metrics'
import * as dockerMetrics from './dockerMetrics'
import * as databases from './databases'
import * as lists from './lists'
import * as command from './command'
import * as routing from './routing'
import * as templates from './templates'
import * as compose from './compose'
import * as newApp from './newApp'
import * as connectDb from './connectDb'
import * as users from './users'
import * as cronJobs from './cronJobs'
import * as cli from './cli'

type Code = 'en' | 'fr' | 'ar' | 'es'

const pick = (l: Code) => ({
  databases: databases[l] as typeof databases.en,
  routing: routing[l] as typeof routing.en,
  templates: templates[l] as typeof templates.en,
  compose: compose[l] as typeof compose.en,
  newApp: newApp[l] as typeof newApp.en,
  connectDb: connectDb[l] as typeof connectDb.en,
  users: users[l] as typeof users.en,
  cronJobs: cronJobs[l] as typeof cronJobs.en,
  cli: cli[l] as typeof cli.en,
  lists: lists[l] as typeof lists.en,
  command: command[l] as typeof command.en,
  dockerMetrics: dockerMetrics[l] as typeof dockerMetrics.en,
  metrics: metrics[l] as typeof metrics.en,
  terminal: terminal[l] as typeof terminal.en,
  common: common[l] as typeof common.en,
  shell: shell[l] as typeof shell.en,
  auth: auth[l] as typeof auth.en,
  security: security[l] as typeof security.en,
  theme: theme[l] as typeof theme.en,
  app: app[l] as typeof app.en,
  logviewer: logviewer[l] as typeof logviewer.en,
  dns: dns[l] as typeof dns.en,
  project: project[l] as typeof project.en,
  projects: projects[l] as typeof projects.en,
  overview: overview[l] as typeof overview.en,
  activity: activity[l] as typeof activity.en,
  source: source[l] as typeof source.en,
  format: format[l] as typeof format.en,
  settings: settings[l] as typeof settings.en,
  settingsGit: settingsGit[l] as typeof settingsGit.en,
  settingsTokens: settingsTokens[l] as typeof settingsTokens.en,
  settingsUpdates: settingsUpdates[l] as typeof settingsUpdates.en,
  settingsBuilds: settingsBuilds[l] as typeof settingsBuilds.en,
  settingsBackup: settingsBackup[l] as typeof settingsBackup.en,
  storage: storage[l] as typeof storage.en,
  settingsStorage: settingsStorage[l] as typeof settingsStorage.en,
  settingsNotifications: settingsNotifications[l] as typeof settingsNotifications.en,
  registries: registries[l] as typeof registries.en,
  builtinRegistry: builtinRegistry[l] as typeof builtinRegistry.en,
  deployHook: deployHook[l] as typeof deployHook.en,
  healthCheck: healthCheck[l] as typeof healthCheck.en,
  onboarding: onboarding[l] as typeof onboarding.en,
})

export type Messages = ReturnType<typeof pick>

export const messages: Record<Code, Messages> = { en: pick('en'), fr: pick('fr'), ar: pick('ar'), es: pick('es') }
