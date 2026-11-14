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

const teamKey = (userId: string) => `meethub.chat.teams.${userId}`

function teamChannels(userId: string): Record<string, string> {
  try {
    const v = JSON.parse(localStorage.getItem(teamKey(userId)) ?? '{}') as unknown
    if (v && typeof v === 'object') return v as Record<string, string>
  } catch {
    /* storage unavailable or corrupt */
  }
  return {}
}

/** rememberTeamChannel keeps the last channel opened in each team, for switching teams (team rail). */
export function rememberTeamChannel(userId: string, team: string, channel: string): void {
  try {
    localStorage.setItem(teamKey(userId), JSON.stringify({ ...teamChannels(userId), [team]: channel }))
  } catch {
    /* storage unavailable */
  }
}

export function lastTeamChannel(userId: string, team: string): string {
  const v = teamChannels(userId)[team]
  return typeof v === 'string' ? v : ''
}

export function channelPath(team: string, channel: string): string {
  return `/teams/${encodeURIComponent(team)}/channels/${encodeURIComponent(channel)}`
}
