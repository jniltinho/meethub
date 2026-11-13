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
  display_name: string
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
  last_login_at?: string
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
