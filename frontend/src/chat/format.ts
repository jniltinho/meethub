import { locale, t } from '@/i18n'

export function formatTime(ms: number): string {
  return new Intl.DateTimeFormat(locale.value, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).format(ms)
}

export function dayKey(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
}

/** formatDay names a day for the timeline separators, always with its date: "Today, Jan 21", "Yesterday, Jan 20",
 *  or the weekday and date (with the year when it is not this one). */
export function formatDay(ms: number, now = Date.now()): string {
  const date = new Intl.DateTimeFormat(locale.value, { day: 'numeric', month: 'short' }).format(ms)
  if (dayKey(ms) === dayKey(now)) return `${t('Today')}, ${date}`
  if (dayKey(ms) === dayKey(now - 86400000)) return `${t('Yesterday')}, ${date}`
  const sameYear = new Date(ms).getFullYear() === new Date(now).getFullYear()
  return new Intl.DateTimeFormat(locale.value, {
    weekday: 'long',
    day: 'numeric',
    month: 'long',
    ...(sameYear ? {} : { year: 'numeric' }),
  }).format(ms)
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  return ((parts[0]?.[0] ?? '?') + (parts.length > 1 ? (parts.at(-1)?.[0] ?? '') : '')).toUpperCase()
}
