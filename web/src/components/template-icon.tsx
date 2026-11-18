import {
  siBookstack, siClickhouse, siCoder, siDirectus, siDrupal, siForgejo, siFreshrss, siGitea, siGrafana, siHedgedoc, siJellyfin, siJenkins,
  siJoomla, siKeycloak, siListmonk, siMatomo, siMattermost, siMeilisearch, siMetabase, siMongodb, siN8n, siNextcloud, siNodered, siNtfy,
  siOllama, siPrometheus, siQdrant, siRabbitmq, siRedis, siSonarqubeserver, siUmami, siUptimekuma, siVaultwarden, siWikidotjs,
  siWordpress, type SimpleIcon,
} from 'simple-icons'

// The mark of each service of the catalog, by template id (Simple Icons). They are their
// owners' trademarks, shown only to name the service. A template without one gets its initial.
const icons: Record<string, SimpleIcon> = {
  wordpress: siWordpress, joomla: siJoomla, drupal: siDrupal, directus: siDirectus, bookstack: siBookstack, wikijs: siWikidotjs,
  jenkins: siJenkins, gitea: siGitea, forgejo: siForgejo, 'code-server': siCoder, sonarqube: siSonarqubeserver,
  n8n: siN8n, 'node-red': siNodered, 'uptime-kuma': siUptimekuma, grafana: siGrafana, prometheus: siPrometheus, ntfy: siNtfy,
  clickhouse: siClickhouse, umami: siUmami, metabase: siMetabase, matomo: siMatomo,
  redis: siRedis, mongodb: siMongodb, rabbitmq: siRabbitmq, meilisearch: siMeilisearch, qdrant: siQdrant,
  nextcloud: siNextcloud, mattermost: siMattermost, hedgedoc: siHedgedoc, listmonk: siListmonk,
  vaultwarden: siVaultwarden, keycloak: siKeycloak, freshrss: siFreshrss, jellyfin: siJellyfin, ollama: siOllama,
}

/** A template's icon in a square tile, in the text colour so it reads in every theme. */
export function TemplateIcon({ id, name }: { id: string; name: string }) {
  const icon = icons[id]
  return (
    <span aria-hidden className="inline-flex size-9 shrink-0 items-center justify-center border border-line bg-bg text-fg">
      {icon ? (
        <svg viewBox="0 0 24 24" className="size-5" fill="currentColor"><path d={icon.path} /></svg>
      ) : (
        <span className="font-pixel text-sm uppercase">{name.slice(0, 1)}</span>
      )}
    </span>
  )
}
