export const repo = 'https://github.com/brahimoulhaj/paas';
export const sitePath = (path: string) => import.meta.env.BASE_URL.replace(/\/$/, '') + path;
export const releases = `${repo}/releases`;
export const installCmd =
  'curl -fsSL https://github.com/brahimoulhaj/paas/releases/latest/download/install.sh | sudo sh';

// Deploy logs are written by the server in English in every language, so they stay here.
export const sourceLogs = {
  git: [
    ['$', 'git push origin main'],
    ['>', 'push received, commit 4f2a91c'],
    ['>', 'builder auto picked railpack (no Dockerfile)'],
    ['>', 'image app-shop:4f2a91c built in 41s'],
    ['>', 'health check GET /up passed (200, 3/3)'],
    ['ok', 'live at https://shop.example.com'],
  ],
  image: [
    ['image', 'ghcr.io/example/api:1.8.2'],
    ['port', '8080'],
    ['limits', '1 CPU, 512 MB'],
    ['>', 'pulled from a private registry'],
    ['>', 'health check TCP :8080 passed'],
    ['ok', 'live at https://api.example.com'],
  ],
  compose: [
    ['services:', ''],
    ['  web:', 'image: ghost:5'],
    ['  db:', 'image: mysql:8'],
    ['>', 'converted into 2 apps and 1 shared volume'],
    ['>', 'database__connection__host = ${app.db.HOST}'],
    ['ok', '2 apps ready to deploy'],
  ],
  template: [
    ['search', 'n8n'],
    ['>', 'volumes, variables and generated secrets'],
    ['>', 'needs PostgreSQL, connect instance "main"'],
    ['>', 'DB_POSTGRESDB_HOST = ${db.main.HOST}'],
    ['>', 'N8N_ENCRYPTION_KEY = (secret)'],
    ['ok', 'ready to deploy'],
  ],
} as const;

export const builders = ['Automatic', 'Dockerfile', 'Railpack', 'Nixpacks', 'Buildpacks', 'Static'];
