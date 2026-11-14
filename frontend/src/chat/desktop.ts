// Desktop notifications (notifications spec, design D1): the browser's Notification, decided in the app. A direct
// message or a message that mentions the person (any message of their conversations with the preference "all"),
// when the app is not the visible tab or another conversation is open; permission is asked only from Settings.

export type DesktopPreference = 'all' | 'mentions' | 'none'

export interface Incoming {
  /** the message's author is the person themself */
  mine: boolean
  /** a direct or group conversation */
  conversation: boolean
  /** the person is among the message's mentions */
  mentioned: boolean
  channelId: string
}

export interface Viewing {
  hidden: boolean
  openChannelId: string
}

/** shouldNotify is the rule of D1 */
export function shouldNotify(pref: DesktopPreference, m: Incoming, v: Viewing): boolean {
  if (pref === 'none' || m.mine) return false
  if (pref === 'mentions' && !m.conversation && !m.mentioned) return false
  return v.hidden || v.openChannelId !== m.channelId
}

/** the preference as stored ('' is the default) */
export function desktopPreference(stored: string | undefined): DesktopPreference {
  return stored === 'all' || stored === 'none' ? stored : 'mentions'
}

export type Permission = 'granted' | 'denied' | 'default' | 'unsupported'

export function permission(): Permission {
  return typeof Notification === 'undefined' ? 'unsupported' : Notification.permission
}
