import { nextTick } from 'vue'
import { createRouter, createWebHistory, type RouteLocationNormalized } from 'vue-router'
import type { Permission } from '@/api/types'
import { setUnauthorizedHandler } from '@/api/client'
import { useSession } from '@/stores/session'
import { cancelDialogs } from '@/feedback/dialogs'
import { toast } from '@/feedback/toasts'
import { errorMessage, t } from '@/i18n'

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    permission?: Permission
    title?: string
  }
}

const placeholder = () => import('@/views/PlaceholderView.vue')

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/setup', name: 'setup', component: () => import('@/views/SetupView.vue'), meta: { public: true } },
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('@/layouts/AppLayout.vue'),
      children: [
        { path: '', redirect: '/conversations' },
        { path: 'activity', name: 'activity', component: placeholder, meta: { title: 'Activity' } },
        { path: 'conversations', name: 'conversations', component: placeholder, meta: { title: 'Conversations' } },
        { path: 'teams', name: 'teams', component: placeholder, meta: { title: 'Teams' } },
        { path: 'meetings', name: 'meetings', component: placeholder, meta: { title: 'Meetings' } },
        { path: 'files', name: 'files', component: placeholder, meta: { title: 'Files' } },
        {
          path: 'settings/:section(profile|password|appearance|language)?',
          name: 'settings',
          component: () => import('@/views/SettingsView.vue'),
        },
        { path: 'admin', redirect: '/admin/users' },
        {
          path: 'admin/users',
          name: 'admin-users',
          component: () => import('@/views/AdminUsersView.vue'),
          meta: { permission: 'manage_users' },
        },
        {
          path: 'admin/audit',
          name: 'admin-audit',
          component: () => import('@/views/AdminAuditView.vue'),
          meta: { permission: 'read_audits' },
        },
        ...(import.meta.env.DEV || import.meta.env.MODE === 'test'
          ? [{ path: '__gallery', name: 'gallery', component: () => import('@/views/GalleryView.vue') }]
          : []),
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// safeNext keeps a post-login target only when it is a path of this app (no open redirect).
export function safeNext(next: unknown): string {
  return typeof next === 'string' && next.startsWith('/') && !next.startsWith('//') && !next.includes('\\') ? next : '/'
}

let restored = false

router.beforeEach(async (to: RouteLocationNormalized) => {
  const session = useSession()
  if (!session.bootstrap) {
    try {
      await session.loadBootstrap()
    } catch {
      /* the server is unreachable: pages show their own errors */
    }
  }
  if (session.bootstrap?.setup_required) return to.name === 'setup' ? true : { name: 'setup', query: to.query }
  if (to.name === 'setup') return { name: 'login' }
  if (!session.loggedIn && !restored) {
    restored = true
    await session.loadThemes().catch(() => [])
    await session.loadMe()
  }
  if (to.meta.public) return session.loggedIn && to.name === 'login' ? { path: safeNext(to.query.next) } : true
  if (!session.loggedIn) return { name: 'login', query: to.fullPath !== '/' ? { next: to.fullPath } : {} }
  if (to.meta.permission && !session.can(to.meta.permission)) {
    toast.error(t('You are not allowed to open this page.'))
    return '/'
  }
  return true
})

// route changes move focus to the new page's heading (screen readers and keyboard users)
router.afterEach(async () => {
  await nextTick()
  const h = document.querySelector<HTMLElement>('main h1, [data-page-heading]')
  if (h) {
    h.tabIndex = -1
    h.focus({ preventScroll: true })
  }
})

// a session ended by the server: cancel pending confirmations, tell the user, go to the login page
setUnauthorizedHandler((code) => {
  const session = useSession()
  if (!session.loggedIn) return
  cancelDialogs()
  session.clear()
  toast.warning(errorMessage(code, t('Your session ended.')))
  const here = router.currentRoute.value.fullPath
  void router.push({ name: 'login', query: here !== '/' ? { next: here } : {} })
})
