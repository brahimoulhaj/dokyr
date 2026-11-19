# Dokyr website

Landing page for dokyr.com, built with Astro 7 (static output, no client framework).
It reuses the panel's look: dark base, JetBrains Mono, Silkscreen for headings, one green accent.

Needs Node 22.23.4+ (see `.nvmrc`, 24 is used).

```sh
pnpm install
pnpm dev       # http://localhost:4321
pnpm build     # static site in dist/
```

Copy (features, templates, FAQ, install command) lives in `src/data/site.ts`; sections are in
`src/components/`. Keep claims in step with `../Roadmap.md`.

In the repository’s **Settings → Pages**, set **Source** to **GitHub Actions**.
The [website workflow](../.github/workflows/website.yml) publishes after website changes
reach `main`. It uses the Pages URL and base path reported by GitHub, including a custom
domain when one is configured.
