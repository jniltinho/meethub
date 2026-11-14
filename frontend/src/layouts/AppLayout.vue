<script setup lang="ts">
// The authenticated frame (web-shell spec): team rail (64 px), context sidebar (264 px) that starts with the section
// navigation and ends with Settings and the user, content, and a collapsible side panel (320 px) showing the open
// channel's details, a thread or the search. Under 768 px the sidebar is a drawer that also holds the team tiles and
// the section navigation is a bottom bar.
import { computed, onMounted, provide, ref, watch } from 'vue'
import { sidePanelKey } from '@/chat/sidePanel'
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
  Video,
  WifiOff,
  X,
} from '@lucide/vue'
import ChannelDetails from '@/components/chat/ChannelDetails.vue'
import ChannelFiles from '@/components/chat/ChannelFiles.vue'
import ChatSidebar from '@/components/chat/ChatSidebar.vue'
import QuickThemes from '@/components/chat/QuickThemes.vue'
import SearchPanel from '@/components/chat/SearchPanel.vue'
import TeamRail from '@/components/chat/TeamRail.vue'
import ThreadPanel from '@/components/chat/ThreadPanel.vue'
import UserPicture from '@/components/chat/UserPicture.vue'
import MeetingRoom from '@/components/meetings/MeetingRoom.vue'
import '@/components/meetings/meetings.css'
import { useMeetings } from '@/stores/meetings'
import { useChat } from '@/stores/chat'
import { announcement } from '@/chat/announce'
import mark from '@/assets/brand/mark.svg'
import { useSession } from '@/stores/session'
import { cancelDialogs } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { t } from '@/i18n'

const session = useSession()
const chat = useChat()
const route = useRoute()
// chat routes (and Teams, which lists them) show the chat sidebar
const chatRoute = computed(() => !!route.meta.chat || route.path.startsWith('/teams'))
const threadOpen = computed(() => !!chat.threadRoot)
// the side panel shows a thread, else the search, else the open channel's files, else its details
const panel = computed(() =>
  threadOpen.value ? 'thread' : chat.searchOpen ? 'search' : chat.filesOpen && route.meta.chat ? 'files' : 'details',
)
// a channel or conversation belongs to Conversations, even under /teams/…
function navOn(to: string): boolean {
  if (route.meta.chat) return to === '/conversations'
  return route.path.startsWith(to)
}
const meetings = useMeetings()
onMounted(() => {
  chat.start()
  void meetings.load().catch(() => undefined)
})
watch(panel, (p) => {
  if (p !== 'details') panelOpen.value = true
})
watch(
  () => chat.detailsRequest,
  () => (panelOpen.value = true),
)
// a thread or the search shows the panel even when it was closed, so the toggle must close them too
const panelVisible = computed(() => panelOpen.value || panel.value !== 'details')
function togglePanel() {
  if (panelVisible.value) {
    chat.threadRoot = ''
    chat.searchOpen = false
    chat.filesOpen = false
    panelOpen.value = false
  } else {
    panelOpen.value = true
  }
}
// a channel shows this toggle in its own header, so the bar above it can go away
provide(sidePanelKey, { visible: panelVisible, toggle: togglePanel })
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

const nav = computed(() => [
  { to: '/conversations', label: t('Conversations'), icon: MessagesSquare },
  { to: '/files', label: t('Files'), icon: FolderOpen },
  { to: '/meetings', label: t('Meetings'), icon: Video },
  { to: '/activity', label: t('Activity'), icon: Activity },
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
  if (route.path.startsWith('/teams')) return t('Teams')
  return nav.value.find((r) => route.path.startsWith(r.to))?.label ?? ''
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
    <nav class="rail" :aria-label="t('Teams')">
      <RouterLink to="/conversations" class="rail-logo" :aria-label="t('Home')">
        <img :src="mark" alt="" width="32" height="32" />
      </RouterLink>
      <TeamRail />
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
      <TeamRail class="drawer-teams" />
      <nav class="section-nav" :aria-label="t('Main navigation')">
        <RouterLink v-for="item in nav" :key="item.to" :to="item.to" class="side-item" :class="{ on: navOn(item.to) }">
          <component :is="item.icon" :size="16" aria-hidden="true" />
          {{ item.label }}
        </RouterLink>
      </nav>
      <ChatSidebar v-if="chatRoute" />
      <div v-if="!chatRoute" class="sidebar-section">{{ sectionTitle }}</div>
      <nav v-if="!chatRoute && sidebarLinks.length" :aria-label="sectionTitle">
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
      <p v-else-if="!chatRoute" class="sidebar-empty">{{ t('Nothing here yet.') }}</p>
      <div class="sidebar-foot">
        <UserPicture
          v-if="session.profile"
          :user-id="session.profile.user.id"
          :name="session.profile.user.display_name"
          :size="24"
        />
        <div class="min-w-0 flex-1 truncate" :title="session.profile?.user.email">
          {{ session.profile?.user.display_name }}
        </div>
        <QuickThemes />
        <RouterLink
          to="/settings/profile"
          class="icon-btn"
          :class="{ on: route.path.startsWith('/settings') }"
          :aria-label="t('Settings')"
          :title="t('Settings')"
        >
          <Settings :size="16" aria-hidden="true" />
        </RouterLink>
        <button type="button" class="icon-btn" :aria-label="t('Log out')" :title="t('Log out')" @click="logout">
          <LogOut :size="16" aria-hidden="true" />
        </button>
      </div>
    </aside>
    <div v-if="drawerOpen" class="drawer-scrim md:hidden" aria-hidden="true" @click="drawerOpen = false" />

    <div class="content">
      <header
        class="content-head"
        :class="{ quiet: chat.socketState !== 'reconnecting', 'in-channel': !!route.meta.chat }"
      >
        <button type="button" class="icon-btn md:hidden" :aria-label="t('Open menu')" @click="drawerOpen = true">
          <Menu :size="18" aria-hidden="true" />
        </button>
        <div class="flex-1">
          <p v-if="chat.socketState === 'reconnecting'" class="reconnecting" role="status">
            <WifiOff :size="14" aria-hidden="true" />{{
              t('Reconnecting… messages will catch up when the connection returns.')
            }}
          </p>
        </div>
        <button
          type="button"
          class="icon-btn max-lg:hidden"
          :aria-label="panelVisible ? t('Close side panel') : t('Open side panel')"
          :aria-expanded="panelVisible"
          @click="togglePanel"
        >
          <PanelRightClose v-if="panelVisible" :size="18" aria-hidden="true" />
          <PanelRightOpen v-else :size="18" aria-hidden="true" />
        </button>
      </header>
      <main class="content-main" :class="{ 'chat-main': route.meta.chat }">
        <RouterView />
      </main>
    </div>

    <aside
      v-if="panelVisible"
      class="side-panel"
      :class="panel !== 'details' ? 'thread-open' : 'max-lg:hidden'"
      :aria-label="t('Side panel')"
    >
      <ThreadPanel v-if="panel === 'thread'" :key="chat.threadRoot" />
      <SearchPanel v-else-if="panel === 'search'" />
      <ChannelFiles v-else-if="panel === 'files' && chat.openChannelId" :key="'f' + chat.openChannelId" />
      <ChannelDetails v-else-if="route.meta.chat && chat.openChannelId" :key="chat.openChannelId" />
      <template v-else>
        <div class="side-panel-head">{{ t('Details') }}</div>
        <p class="sidebar-empty">{{ t('Select a conversation or a meeting to see its details.') }}</p>
      </template>
    </aside>

    <nav class="bottom-bar" :aria-label="t('Main navigation')">
      <RouterLink
        v-for="item in nav"
        :key="item.to"
        :to="item.to"
        class="rail-item"
        :class="{ on: navOn(item.to) }"
        :aria-label="item.label"
      >
        <component :is="item.icon" :size="20" aria-hidden="true" />
        <span class="rail-label">{{ item.label }}</span>
      </RouterLink>
      <RouterLink
        to="/settings/profile"
        class="rail-item"
        :class="{ on: route.path.startsWith('/settings') }"
        :aria-label="t('Settings')"
      >
        <Settings :size="20" aria-hidden="true" />
        <span class="rail-label">{{ t('Settings') }}</span>
      </RouterLink>
    </nav>
    <MeetingRoom v-if="meetings.callChannel" :key="meetings.callChannel" />
    <div class="sr-only" aria-live="polite" aria-atomic="true">{{ announcement }}</div>
  </div>
</template>
