// Pure rules of the setup wizard (setup-wizard spec, add-setup-wizard), kept apart from the view so unit tests run
// them: the steps, what blocks them, the checks the browser's own validation does not make, and the lines for
// config.toml shown where the settings page cannot write it.

export const STEPS = ['access', 'server', 'account', 'recordings', 'summaries', 'review', 'finish'] as const
export type Step = (typeof STEPS)[number]
/** the first step an owner sees when the install already has its account */
export const FIRST_OPTIONAL: Step = 'recordings'

export type Level = 'ok' | 'note' | 'warning' | 'problem'
export interface DiagnosisItem {
  id: string
  level: Level
  detail?: string
}

/** a problem (the database, Redis, the file storage) must be fixed before the account is created */
export function blocked(items: DiagnosisItem[]): boolean {
  return items.some((i) => i.level === 'problem')
}

/** canGo: finished steps are reachable again, except the token once the account exists and anything once Finish ran */
export function canGo(to: Step, current: Step, initialized: boolean, running: boolean): boolean {
  const a = STEPS.indexOf(to)
  const b = STEPS.indexOf(current)
  if (running || a >= b) return false
  return !initialized || a >= STEPS.indexOf(FIRST_OPTIONAL)
}

export type PasswordProblem = '' | 'short' | 'mismatch'
export function passwordProblem(password: string, again: string): PasswordProblem {
  if ([...password].length < 12) return 'short'
  return password === again ? '' : 'mismatch'
}

/** the shape of an AI address: http(s), no credentials, query or fragment. Whether a plain http:// host is this
 *  machine or a private one (a name such as "whisper" may be) is the server's to decide, with its own resolver */
export function addressProblem(raw: string): boolean {
  let u: URL
  try {
    u = new URL(raw.trim())
  } catch {
    return true
  }
  if (u.protocol !== 'https:' && u.protocol !== 'http:') return true
  return !!(u.username || u.password || u.search || u.hash)
}

export type Value = string | number | boolean

/** tomlLines writes changes ("table.key" → value) as the lines to add to config.toml, one table after another */
export function tomlLines(changes: Record<string, Value>): string {
  const tables = new Map<string, string[]>()
  for (const key of Object.keys(changes).sort()) {
    const at = key.lastIndexOf('.')
    const v = changes[key]!
    const lines = tables.get(key.slice(0, at)) ?? []
    lines.push(`${key.slice(at + 1)} = ${typeof v === 'string' ? JSON.stringify(v) : String(v)}`)
    tables.set(key.slice(0, at), lines)
  }
  return [...tables].map(([table, lines]) => `[${table}]\n${lines.join('\n')}`).join('\n\n')
}
