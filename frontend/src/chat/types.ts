// Chat API shapes (docs/api.md, "Chat"). Times are Unix milliseconds.
export type ChannelType = 'O' | 'P' | 'D' | 'G'

export interface PublicUser {
  id: string
  username: string
  display_name: string
  active: boolean
  /** version (ms) of the profile picture; 0 without one */
  last_picture_update: number
}

export interface Team {
  id: string
  name: string
  display_name: string
  description: string
  type: 'open' | 'invite'
  is_default: boolean
  archived_at: number
}

export interface TeamWithState extends Team {
  role: string
  has_unread: boolean
  mention_count: number
}

export interface Channel {
  id: string
  team_id: string
  type: ChannelType
  name: string
  display_name: string
  purpose: string
  header: string
  creator_id: string
  is_default: boolean
  revision: number
  total_msg_count: number
  last_post_at: number
  archived_at: number
  created_at: number
  updated_at: number
}

export interface ChannelWithState extends Channel {
  role: string
  msg_count: number
  mention_count: number
  unread: number
  favorite: boolean
}

export interface ChannelInfo extends Channel {
  member_count: number
  role?: string
}

export interface Conversation extends Channel {
  members: PublicUser[]
  mention_count: number
  unread: number
  favorite: boolean
}

export interface Member extends PublicUser {
  role: string
}

export interface ReactionGroup {
  emoji_name: string
  count: number
  user_ids: string[]
}

export interface Post {
  id: string
  channel_id: string
  user_id: string
  root_id: string
  message?: string
  type: string
  pending_post_id?: string
  create_at: number
  update_at: number
  edit_at: number
  delete_at: number
  reply_count: number
  last_reply_at: number
  revision: number
  is_pinned?: boolean
  pinned_by?: string
  pinned_at?: number
  reactions?: ReactionGroup[]
  file_ids?: string[]
  metadata?: { files?: FileInfo[] }
}

/** an uploaded file (Mattermost's FileInfo); post_id once a message carries it */
export interface FileInfo {
  id: string
  user_id: string
  channel_id: string
  post_id?: string
  name: string
  extension: string
  size: number
  mime_type: string
  create_at: number
  has_preview_image: boolean
  width?: number
  height?: number
}

/** a window of history around a message, newest first */
export interface Around {
  items: Post[]
  prev_cursor?: string
  next_cursor?: string
}

export interface SearchChannel {
  id: string
  type: ChannelType
  name: string
  display_name: string
  team_id: string
}

export interface SearchResult {
  posts: Post[]
  has_more: boolean
  channels: Record<string, SearchChannel>
}

/** a message of this browser not yet stored (pending) or that failed to send */
export interface LocalPost extends Post {
  local?: 'pending' | 'failed'
}

export interface Page<T> {
  items: T[]
  next_cursor?: string
}

export interface Changes {
  posts: Post[]
  next_revision: number
  has_more: boolean
}

export type Presence = 'online' | 'away' | 'offline' | 'dnd'

export interface Status {
  user_id: string
  status: Presence
  dnd_end_time?: number
  stale?: boolean
}

/** a server event (Mattermost envelope) */
export interface ServerEvent {
  event: string
  data: Record<string, unknown>
  broadcast: { channel_id?: string; team_id?: string; user_id?: string }
  seq: number
}
