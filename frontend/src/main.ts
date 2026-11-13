import { createApp } from 'vue'
import { createPinia } from 'pinia'
import './style.css'
import App from './App.vue'
import { router } from './router'
import { installNativeReplacements } from './feedback/native'
import { locale } from './i18n'

document.documentElement.lang = locale.value

// --view-h (style.css): the window's height in CSS px after the large-screen zoom steps (as in ad-manager)
const viewport = document.createElement('div')
viewport.setAttribute('aria-hidden', 'true')
viewport.className = 'view-probe'
document.body.append(viewport)
new ResizeObserver(() => document.documentElement.style.setProperty('--view-h', `${viewport.offsetHeight}px`)).observe(
  viewport,
)

installNativeReplacements()
createApp(App).use(createPinia()).use(router).mount('#app')
