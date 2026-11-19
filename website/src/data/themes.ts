export const themes = [
  { id: 'midnight', name: 'Midnight', mode: 'dark' },
  { id: 'paper', name: 'Paper', mode: 'light' },
  { id: 'white', name: 'White', mode: 'light' },
  { id: 'tokyo-night', name: 'Tokyo Night', mode: 'dark' },
  { id: 'catppuccin', name: 'Catppuccin', mode: 'dark' },
  { id: 'catppuccin-latte', name: 'Catppuccin Latte', mode: 'light' },
  { id: 'everforest', name: 'Everforest', mode: 'dark' },
  { id: 'gruvbox', name: 'Gruvbox', mode: 'dark' },
  { id: 'nord', name: 'Nord', mode: 'dark' },
  { id: 'rose-pine', name: 'Rosé Pine', mode: 'dark' },
  { id: 'kanagawa', name: 'Kanagawa', mode: 'dark' },
  { id: 'flexoki-light', name: 'Flexoki Light', mode: 'light' },
] as const;

export type ThemeId = (typeof themes)[number]['id'];
