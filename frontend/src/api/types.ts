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
  /** two-factor authentication is on */
  two_factor?: boolean
}

/** the first login step of a person with two-factor authentication: no session yet */
export interface TwoFactorChallenge {
  two_factor_required: true
  ticket: string
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
  /** '' = the default, mentions */
  desktop_notifications?: string
  email_notifications?: string
}

export interface Profile {
  user: User
  organization: Organization
  role: Role
  permissions: Permission[]
  preferences: Preferences
  csrf_token: string
  /** the server can send notification e-mails (SMTP configured) */
  email_notifications_available?: boolean
}

export interface Bootstrap {
  setup_required: boolean
  default_theme: string
  languages: string[]
  /** false: only single sign-on is offered */
  password_login?: boolean
  /** single sign-on is configured: the provider's name for the button */
  sso?: { name: string }
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
