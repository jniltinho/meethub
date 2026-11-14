export type Role = 'owner' | 'admin' | 'member' | 'guest'
export type Permission =
  | 'manage_system'
  | 'manage_owners'
  | 'manage_users'
  | 'read_audits'
  | 'create_team'
  | 'edit_own_profile'

export interface User {
  id: string
  email: string
  username?: string | null
  display_name: string
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
  last_login_at?: string
  /** version (ms) of the profile picture; 0 without one */
  last_picture_update?: number
}

export interface Member extends User {
  role: Role
}

export interface Organization {
  id: string
  slug: string
  name: string
  default_theme: string
}

export interface Preferences {
  theme: string
  language: string
}

export interface Profile {
  user: User
  organization: Organization
  role: Role
  permissions: Permission[]
  preferences: Preferences
  csrf_token: string
}

export interface Bootstrap {
  setup_required: boolean
  default_theme: string
  languages: string[]
  /** the upload limits; absent when the server has no file storage */
  files?: { max_size_bytes: number; max_per_post: number }
}

export interface ThemeInfo {
  id: string
  label: string
  scheme: 'light' | 'dark'
  default?: boolean
}

export interface AuditEvent {
  id: string
  actor_id?: string
  action: string
  target_type: string
  target_id: string
  client_ip: string
  details: Record<string, unknown>
  created_at: string
}
