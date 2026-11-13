// Applies the theme before the first paint (external file: the CSP allows no inline script).
// Precedence: the theme remembered while logged in, else the organization default rendered by the server
// (data-default-theme), switching meethub-dark to meethub-light when the browser prefers a light scheme.
;(function () {
  var root = document.documentElement
  var theme = null
  try {
    theme = localStorage.getItem('meethub.theme')
  } catch {
    theme = null
  }
  if (!theme) {
    var def = root.getAttribute('data-default-theme')
    if (!def || def.charAt(0) === '%') def = 'meethub-dark'
    theme = def
    if (def === 'meethub-dark' && window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches) {
      theme = 'meethub-light'
    }
  }
  root.setAttribute('data-theme', theme)
})()
