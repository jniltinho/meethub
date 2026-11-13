<script setup lang="ts">
// The authenticated frame: navigation rail (64 px), context sidebar (264 px), content and a collapsible side panel
// (320 px). Under 768 px the sidebar is a drawer and the rail a bottom bar.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  Activity,
  FolderOpen,
  LogOut,
  Menu,
  MessagesSquare,
  PanelRightClose,
  PanelRightOpen,
  Settings,
  Shield,
  Users,
  Video,
  X,
} from '@lucide/vue'
import mark from '@/assets/brand/mark.svg'
import { useSession } from '@/stores/session'
import { cancelDialogs } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const session = useSession()
const route = useRoute()
const router = useRouter()

const PANEL_KEY = 'meethub.sidePanel'
const panelOpen = ref(readPanel())
function readPanel(): boolean {
  try {
    return sessionStorage.getItem(PANEL_KEY) !== 'closed'
  } catch {
    return true
  }
}
watch(panelOpen, (open) => {
  try {
    sessionStorage.setItem(PANEL_KEY, open ? 'open' : 'closed')
  } catch {
    /* storage unavailable */
  }
})
const drawerOpen = ref(false)
watch(
  () => route.fullPath,
  () => (drawerOpen.value = false),
)

const rail = computed(() => [
  { to: '/activity', label: t('Activity'), icon: Activity },
  { to: '/conversations', label: t('Conversations'), icon: MessagesSquare },
  { to: '/teams', label: t('Teams'), icon: Users },
  { to: '/meetings', label: t('Meetings'), icon: Video },
  { to: '/files', label: t('Files'), icon: FolderOpen },
  ...(session.can('manage_users') || session.can('read_audits')
    ? [{ to: '/admin', label: t('Administration'), icon: Shield }]
    : []),
])

const sidebarLinks = computed(() => {
  if (route.path.startsWith('/settings'))
    return [
      { to: '/settings/profile', label: t('Profile') },
      { to: '/settings/password', label: t('Password') },
      { to: '/settings/appearance', label: t('Appearance') },
      { to: '/settings/language', label: t('Language') },
    ]
  if (route.path.startsWith('/admin'))
    return [
      ...(session.can('manage_users') ? [{ to: '/admin/users', label: t('Users') }] : []),
      ...(session.can('read_audits') ? [{ to: '/admin/audit', label: t('Audit log') }] : []),
    ]
  return []
})

const sectionTitle = computed(() => {
  if (route.path.startsWith('/settings')) return t('Settings')
  if (route.path.startsWith('/admin')) return t('Administration')
  return rail.value.find((r) => route.path.startsWith(r.to))?.label ?? ''
})

async function logout() {
  try {
    await session.logout()
  } catch {
    toast.error(t('Could not log out. Check the connection and try again.'))
    return
  }
  cancelDialogs()
  await router.push({ name: 'login' })
}
</script>

<template>
  <div class="shell">
    <nav class="rail" :aria-label="t('Main navigation')">
      <RouterLink to="/conversations" class="rail-logo" :aria-label="t('Home')">
        <img :src="mark" alt="" width="32" height="32" />
      </RouterLink>
      <RouterLink
        v-for="item in rail"
        :key="item.to"
        :to="item.to"
        class="rail-item"
        :class="{ on: route.path.startsWith(item.to) }"
        :aria-label="item.label"
        :title="item.label"
      >
        <component :is="item.icon" :size="20" aria-hidden="true" />
        <span class="rail-label">{{ item.label }}</span>
      </RouterLink>
      <div class="rail-spacer" />
      <RouterLink
        to="/settings/profile"
        class="rail-item"
        :class="{ on: route.path.startsWith('/settings') }"
        :aria-label="t('Settings')"
        :title="t('Settings')"
      >
        <Settings :size="20" aria-hidden="true" />
        <span class="rail-label">{{ t('Settings') }}</span>
      </RouterLink>
    </nav>

    <aside class="sidebar" :class="{ open: drawerOpen }" :aria-label="t('Sidebar')">
      <div class="sidebar-head">
        <div class="min-w-0">
          <div class="sidebar-label">{{ t('Workspace') }}</div>
          <div class="truncate font-semibold">{{ session.profile?.organization.name }}</div>
        </div>
        <button type="button" class="icon-btn md:hidden" :aria-label="t('Close menu')" @click="drawerOpen = false">
          <X :size="18" aria-hidden="true" />
        </button>
      </div>
      <div class="sidebar-section">{{ sectionTitle }}</div>
      <nav v-if="sidebarLinks.length" :aria-label="sectionTitle">
        <RouterLink
          v-for="l in sidebarLinks"
          :key="l.to"
          :to="l.to"
          class="side-item"
          :class="{ on: route.path === l.to }"
        >
          {{ l.label }}
        </RouterLink>
      </nav>
      <p v-else class="sidebar-empty">{{ t('Nothing here yet.') }}</p>
      <div class="sidebar-foot">
        <div class="min-w-0">
          <div class="truncate">{{ session.profile?.user.display_name }}</div>
          <div class="sidebar-label truncate">{{ session.profile?.user.email }}</div>
        </div>
        <button type="button" class="icon-btn" :aria-label="t('Log out')" :title="t('Log out')" @click="logout">
          <LogOut :size="16" aria-hidden="true" />
        </button>
      </div>
    </aside>
    <div v-if="drawerOpen" class="drawer-scrim md:hidden" aria-hidden="true" @click="drawerOpen = false" />

    <div class="content">
      <header class="content-head">
        <button type="button" class="icon-btn md:hidden" :aria-label="t('Open menu')" @click="drawerOpen = true">
          <Menu :size="18" aria-hidden="true" />
        </button>
        <div class="flex-1" />
        <button
          type="button"
          class="icon-btn max-lg:hidden"
          :aria-label="panelOpen ? t('Close side panel') : t('Open side panel')"
          :aria-expanded="panelOpen"
          @click="panelOpen = !panelOpen"
        >
          <PanelRightClose v-if="panelOpen" :size="18" aria-hidden="true" />
          <PanelRightOpen v-else :size="18" aria-hidden="true" />
        </button>
      </header>
      <main class="content-main">
        <RouterView />
      </main>
    </div>

    <aside v-if="panelOpen" class="side-panel max-lg:hidden" :aria-label="t('Side panel')">
      <div class="side-panel-head">{{ t('Details') }}</div>
      <p class="sidebar-empty">{{ t('Select a conversation or a meeting to see its details.') }}</p>
    </aside>
  </div>
</template>
