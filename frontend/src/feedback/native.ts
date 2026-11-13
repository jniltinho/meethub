// Replaces the browser's validation bubbles: an invalid field gets the theme's invalid state and a message under it.
import { t } from '@/i18n'

function messageFor(el: HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement): string {
  const v = el.validity
  if (v.valueMissing) return t('This field is required.')
  if (v.typeMismatch && el.type === 'email') return t('Enter a valid e-mail address.')
  if (v.tooShort) return t('Too short.')
  return t('Invalid value.')
}

function clear(el: Element): void {
  el.classList.remove('invalid')
  el.removeAttribute('aria-invalid')
  const id = el.getAttribute('aria-describedby')
  if (id?.startsWith('err-')) {
    document.getElementById(id)?.remove()
    el.removeAttribute('aria-describedby')
  }
}

let seq = 0

export function installNativeReplacements(root: Document = document): void {
  root.addEventListener(
    'invalid',
    (e) => {
      const el = e.target as HTMLInputElement
      e.preventDefault() // no browser bubble
      clear(el)
      el.classList.add('invalid')
      el.setAttribute('aria-invalid', 'true')
      const msg = root.createElement('div')
      msg.id = `err-${++seq}`
      msg.className = 'field-error'
      msg.textContent = messageFor(el)
      el.setAttribute('aria-describedby', msg.id)
      el.insertAdjacentElement('afterend', msg)
      const form = el.form
      if (form && form.querySelector('.invalid') === el) el.focus()
    },
    true,
  )
  root.addEventListener('input', (e) => {
    const el = e.target as Element
    if (el instanceof HTMLElement && el.classList.contains('invalid')) clear(el)
  })
}
