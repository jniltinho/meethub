// The last channel or conversation each user opened, remembered in this browser (design D9) and validated by the
// caller before use: it may point to a channel the user can no longer read.
export interface LastLocation {
  kind: 'channel' | 'conversation'
  team?: string // team name
  channel: string // channel name, or conversation id
}

const key = (userId: string) => `meethub.chat.last.${userId}`

export function rememberLocation(userId: string, loc: LastLocation): void {
  try {
    localStorage.setItem(key(userId), JSON.stringify(loc))
  } catch {
    /* storage unavailable */
  }
}

export function lastLocation(userId: string): LastLocation | null {
  try {
    const v = JSON.parse(localStorage.getItem(key(userId)) ?? 'null') as LastLocation | null
    if (v && (v.kind === 'channel' || v.kind === 'conversation') && typeof v.channel === 'string') return v
  } catch {
    /* storage unavailable or corrupt */
  }
  return null
}

export function channelPath(team: string, channel: string): string {
  return `/teams/${encodeURIComponent(team)}/channels/${encodeURIComponent(channel)}`
}
