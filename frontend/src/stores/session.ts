import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, setCsrfToken } from '@/api/client'
import type { Bootstrap, Permission, Profile, ThemeInfo } from '@/api/types'
import { isLanguage, setLanguage } from '@/i18n'
import { applyTheme, forgetTheme } from '@/themes/apply'

export const useSession = defineStore('session', () => {
  const bootstrap = ref<Bootstrap | null>(null)
  const profile = ref<Profile | null>(null)
  const themes = ref<ThemeInfo[]>([])

  const loggedIn = computed(() => profile.value !== null)
  const can = (p: Permission) => profile.value?.permissions.includes(p) ?? false

  async function loadBootstrap(): Promise<Bootstrap> {
    bootstrap.value = await api<Bootstrap>('GET', '/bootstrap')
    return bootstrap.value
  }

  async function loadThemes(): Promise<ThemeInfo[]> {
    if (themes.value.length === 0) themes.value = (await api<{ themes: ThemeInfo[] }>('GET', '/themes')).themes
    return themes.value
  }

  function accept(p: Profile): Profile {
    profile.value = p
    setCsrfToken(p.csrf_token)
    if (p.preferences.theme) {
      const scheme = themes.value.find((t) => t.id === p.preferences.theme)?.scheme
      applyTheme(p.preferences.theme, scheme)
    }
    if (isLanguage(p.preferences.language)) setLanguage(p.preferences.language)
    return p
  }

  // loadMe restores an existing session (page reload); false when not logged in.
  async function loadMe(): Promise<boolean> {
    try {
      accept(await api<Profile>('GET', '/me'))
      return true
    } catch {
      return false
    }
  }

  async function login(email: string, password: string): Promise<Profile> {
    return accept(await api<Profile>('POST', '/auth/login', { email, password }))
  }

  async function setup(input: {
    setup_token: string
    organization_name: string
    display_name: string
    email: string
    password: string
  }): Promise<Profile> {
    const p = accept(await api<Profile>('POST', '/auth/setup', input))
    if (bootstrap.value) bootstrap.value.setup_required = false
    return p
  }

  async function updateMe(patch: { display_name?: string; preferences?: { theme?: string; language?: string } }) {
    return accept(await api<Profile>('PATCH', '/me', patch))
  }

  async function changePassword(current_password: string, new_password: string) {
    return accept(await api<Profile>('POST', '/me/password', { current_password, new_password }))
  }

  // clear forgets the session locally (after logout or when the server ended it).
  function clear(): void {
    profile.value = null
    setCsrfToken('')
    forgetTheme()
  }

  async function logout(): Promise<void> {
    try {
      await api('POST', '/auth/logout')
    } finally {
      clear()
    }
  }

  return {
    bootstrap,
    profile,
    themes,
    loggedIn,
    can,
    loadBootstrap,
    loadThemes,
    loadMe,
    login,
    setup,
    updateMe,
    changePassword,
    logout,
    clear,
  }
})
