// Applying themes at runtime, with the precedence of the ui-themes spec:
// user preference > theme remembered in this browser while logged in > organization default (rendered by the server
// into data-default-theme) with meethub-dark → meethub-light when the browser prefers light.
export const THEME_STORAGE_KEY = 'meethub.theme'

export function organizationDefault(): string {
  const def = document.documentElement.getAttribute('data-default-theme')
  const id = def && !def.startsWith('%') ? def : 'meethub-dark'
  if (id === 'meethub-dark' && window.matchMedia?.('(prefers-color-scheme: light)').matches) return 'meethub-light'
  return id
}

export function currentTheme(): string {
  return document.documentElement.getAttribute('data-theme') ?? organizationDefault()
}

// applyTheme switches the theme now; remember=true keeps it for the next page load (logged in).
export function applyTheme(id: string, scheme?: 'light' | 'dark', remember = true): void {
  const root = document.documentElement
  root.setAttribute('data-theme', id)
  if (scheme) root.style.colorScheme = scheme
  if (!remember) return
  try {
    localStorage.setItem(THEME_STORAGE_KEY, id)
  } catch {
    /* storage unavailable */
  }
}

// forgetTheme runs on logout: a shared browser goes back to the organization default.
export function forgetTheme(): void {
  try {
    localStorage.removeItem(THEME_STORAGE_KEY)
  } catch {
    /* storage unavailable */
  }
  applyTheme(organizationDefault(), undefined, false)
}
