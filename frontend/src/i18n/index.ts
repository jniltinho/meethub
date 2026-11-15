// Translations: English source strings in code, catalogs per language. English is the default; a missing entry
// falls back to the English text. Placeholders are written {name} and filled from the params object.
import { ref } from 'vue'
import ptBR from './pt-BR'
import es from './es'

export type Language = 'en' | 'pt-BR' | 'es'
export const languages: { id: Language; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'pt-BR', label: 'Português (Brasil)' },
  { id: 'es', label: 'Español' },
]

export type Catalog = Record<string, string>
const catalogs: Record<Exclude<Language, 'en'>, Catalog> = { 'pt-BR': ptBR, es }
const STORAGE_KEY = 'meethub.language'

export const locale = ref<Language>(initialLanguage())

function initialLanguage(): Language {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (isLanguage(saved)) return saved
  } catch {
    /* storage unavailable */
  }
  return 'en'
}

export function isLanguage(v: unknown): v is Language {
  return v === 'en' || v === 'pt-BR' || v === 'es'
}

// setLanguage applies a language now (no reload) and remembers it in this browser.
export function setLanguage(lang: Language): void {
  locale.value = lang
  document.documentElement.lang = lang
  try {
    localStorage.setItem(STORAGE_KEY, lang)
  } catch {
    /* storage unavailable */
  }
}

export function t(text: string, params?: Record<string, string | number>): string {
  const lang = locale.value
  let out = lang === 'en' ? text : (catalogs[lang][text] ?? text)
  if (params) {
    for (const [k, v] of Object.entries(params)) out = out.replaceAll(`{${k}}`, String(v))
  }
  return out
}

// errorMessage translates an API error code, falling back to the server's English message.
export function errorMessage(code: string, fallback: string): string {
  const key = `error.${code}`
  if (locale.value === 'en') return englishErrors[code] ?? fallback
  const tr = catalogs[locale.value][key]
  return tr ?? englishErrors[code] ?? fallback
}

// English texts for API error codes (the keys "error.<code>" in the other catalogs).
export const englishErrors: Record<string, string> = {
  bad_request: 'The request is malformed.',
  invalid_input: 'Some fields are invalid.',
  weak_password: 'The password must have at least 12 characters.',
  invalid_theme: 'Unknown theme.',
  invalid_language: 'Unsupported language.',
  invalid_notifications: 'Unknown notification preference.',
  invalid_token: 'This link is not valid any more.',
  unauthenticated: 'You must log in.',
  session_expired: 'Your session expired. Log in again.',
  invalid_credentials: 'Invalid e-mail or password.',
  forbidden: 'You are not allowed to do this.',
  csrf_failed: 'The request was blocked for security. Reload the page.',
  invalid_setup_token: 'The setup token is not valid.',
  not_found: 'Not found.',
  already_initialized: 'MeetHub is already set up.',
  email_taken: 'This e-mail is already in use.',
  last_owner: 'The organization must keep at least one active owner.',
  rate_limited: 'Too many failed attempts. Try again later.',
  busy: 'The server is busy. Try again shortly.',
  network: 'The server could not be reached.',
  unavailable: 'The service is unavailable. Try again shortly.',
  invalid_username: 'Usernames have 3 to 22 lowercase letters, digits, dots, dashes or underscores.',
  username_taken: 'This username is already in use.',
  name_taken: 'This name is already in use.',
  not_team_member: 'That person is not a member of this team.',
  default_channel: 'The default channel cannot be left or archived.',
  default_team: 'The default team cannot be archived or left by its last admin.',
  channel_archived: 'This channel is archived.',
  direct_channel: 'Direct and group conversations have fixed members.',
  user_inactive: 'That account is disabled.',
  pending_post_conflict: 'This message was already sent with different content.',
  invalid_image: 'The file is not a PNG, JPEG or GIF image of at most 4096 pixels a side.',
  image_too_large: 'The image is larger than 5 MiB.',
  file_too_large: 'The file is larger than the allowed limit.',
  too_many_files: 'Too many files for one message.',
  storage_unavailable: 'The file storage is unavailable. Try again shortly.',
  meetings_unavailable: 'Meetings are not configured on this server.',
  invalid_code: 'The code is not valid.',
  two_factor_expired: 'The login expired. Log in again.',
  two_factor_unavailable: 'Two-factor authentication is not available on this server.',
  two_factor_enabled: 'Two-factor authentication is already on.',
  two_factor_off: 'Two-factor authentication is off.',
  password_login_disabled: 'Password login is turned off. Use single sign-on.',
  recordings_unavailable: 'Recordings are not enabled on this server.',
  recordings_busy: 'The server cannot record another meeting now. Try again later.',
  recording_active: 'This meeting is already being recorded.',
  not_in_meeting: 'Join the meeting to start recording it.',
  ai_not_configured: 'An AI service must be configured first.',
  summaries_off: 'Meeting summaries are off.',
  summary_source_unavailable: 'This recording cannot be summarized.',
  summary_source_expired: 'This recording is too old to be summarized.',
  summary_exists: "This recording's summary is already being written or was written.",
  recording_not_ready: 'The recording is not ready yet.',
  ffmpeg_unsupported: 'The ffmpeg of this server cannot prepare recordings for summaries.',
  ai_unauthorized: 'The AI service refused the key.',
  ai_unavailable: 'The AI service could not be reached.',
  ai_rejected: 'The AI service refused the request or could not read the audio.',
  ai_too_long: 'The AI service refused the request as too long.',
  ai_unsupported: 'The AI service cannot give timed transcription, or a model or path is wrong.',
  settings_read_only: 'Settings cannot be changed on this install.',
  settings_pending: 'A change is already being applied: wait for its result.',
  settings_not_confirmable: 'This change cannot be confirmed any more.',
  settings_rate_limited: 'Too many setting changes. Try again later.',
  settings_file_unusual: 'config.toml has a layout this page cannot edit safely: edit it by hand.',
}

export function formatDateTime(iso: string | Date): string {
  const d = typeof iso === 'string' ? new Date(iso) : iso
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short', hourCycle: 'h23' }).format(d)
}

export function formatNumber(n: number): string {
  return new Intl.NumberFormat(locale.value).format(n)
}
