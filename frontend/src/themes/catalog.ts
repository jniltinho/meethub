// The theme catalog as the picker shows it; kept equal to internal/theme/theme.go and to the [data-theme] rules in
// src/themes/*.css by the lockstep test (tests/unit/themes.test.ts).
export interface ThemeEntry {
  id: string
  label: string
  scheme: 'light' | 'dark'
  swatch: [page: string, surface: string, action: string, rail: string] // picker preview
}

export const themeCatalog: ThemeEntry[] = [
  { id: 'meethub-dark', label: 'MeetHub (dark)', scheme: 'dark', swatch: ['#0c1020', '#151b31', '#3d41cf', '#0b0f1c'] },
  {
    id: 'meethub-light',
    label: 'MeetHub (light)',
    scheme: 'light',
    swatch: ['#f4f5fa', '#ffffff', '#3d41cf', '#1f2340'],
  },
  { id: 'default', label: 'Default', scheme: 'light', swatch: ['#d9e6f3', '#ffffff', '#113058', '#0a1222'] },
  { id: 'admanager', label: 'ADManager', scheme: 'light', swatch: ['#d9e6f3', '#ffffff', '#113058', '#0a1222'] },
  { id: 'breeze', label: 'Breeze', scheme: 'light', swatch: ['#eff0f1', '#ffffff', '#164e72', '#eff0f1'] },
  { id: 'breeze-dark', label: 'Breeze (dark)', scheme: 'dark', swatch: ['#2a2e32', '#1b1e20', '#3daee9', '#2a2e32'] },
  { id: 'violet', label: 'Violet', scheme: 'light', swatch: ['#f6f7fb', '#ffffff', '#6037b0', '#f6f7fb'] },
  { id: 'violet-dark', label: 'Violet (dark)', scheme: 'dark', swatch: ['#0e0d19', '#171624', '#a36eff', '#0e0d19'] },
  { id: 'aurora', label: 'Aurora', scheme: 'light', swatch: ['#f9fafd', '#ffffff', '#4c30ae', '#f9fafd'] },
  { id: 'aurora-dark', label: 'Aurora (dark)', scheme: 'dark', swatch: ['#010105', '#13121f', '#967aff', '#010105'] },
  { id: 'crystal', label: 'Crystal', scheme: 'light', swatch: ['#ececec', '#ffffff', '#00469e', '#e4e4e6'] },
  { id: 'crystal-dark', label: 'Crystal (dark)', scheme: 'dark', swatch: ['#323232', '#1e1e1e', '#0053ac', '#262626'] },
  { id: 'cone', label: 'Cone (dark)', scheme: 'dark', swatch: ['#121212', '#212121', '#ff8800', '#121212'] },
  {
    id: 'classic-blue',
    label: 'Classic blue (XP)',
    scheme: 'light',
    swatch: ['#ece9d8', '#ffffff', '#1a5c1e', '#e3dfcc'],
  },
  {
    id: 'classic-gray',
    label: 'Classic gray (98)',
    scheme: 'light',
    swatch: ['#7fbfbf', '#c0c0c0', '#c0c0c0', '#c0c0c0'],
  },
  { id: 'graphite', label: 'Graphite (dark)', scheme: 'dark', swatch: ['#111217', '#181b1f', '#3461bb', '#111217'] },
  { id: 'grafana', label: 'Grafana (dark)', scheme: 'dark', swatch: ['#111217', '#181b1f', '#3461bb', '#111217'] },
  { id: 'rabbitmq', label: 'RabbitMQ', scheme: 'light', swatch: ['#e7e7e7', '#ffffff', '#b84700', '#e6e6e6'] },
  { id: 'matrix', label: 'Matrix (dark)', scheme: 'dark', swatch: ['#010502', '#07130b', '#00e03a', '#020904'] },
  { id: 'winamp', label: 'Winamp (dark)', scheme: 'dark', swatch: ['#15161d', '#1b1c25', '#f4a51c', '#1d1e27'] },
  { id: 'pink-panther', label: 'Pink Panther', scheme: 'light', swatch: ['#f7eef2', '#ffffff', '#a52d63', '#f4e3eb'] },
  {
    id: 'high-contrast',
    label: 'High contrast (dark)',
    scheme: 'dark',
    swatch: ['#000000', '#000000', '#ffff00', '#000000'],
  },
]
