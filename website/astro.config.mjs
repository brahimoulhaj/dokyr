import { defineConfig } from 'astro/config';

export default defineConfig({
  site: process.env.PAGES_ORIGIN || 'https://dokyr.com',
  base: process.env.PAGES_BASE_PATH || '/',
});
