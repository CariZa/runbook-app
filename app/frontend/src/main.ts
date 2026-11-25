import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'

export default mount(App, { target: document.getElementById('app')! })

// macOS can capitalise the first letter of what you type ("Capitalize words automatically"
// in Keyboard settings) and autocorrect it. Names, paths, arguments and commands must stay
// exactly as typed, so both are switched off on every text box; only boxes that ask for
// spell-checking (note text) keep the system behaviour.
function typeAsTyped(root: ParentNode) {
  for (const el of root.querySelectorAll<HTMLElement>('input:not([type=checkbox]):not([type=radio]), textarea')) {
    if (el.getAttribute('spellcheck') === 'true') continue
    el.setAttribute('autocapitalize', 'off')
    el.setAttribute('autocorrect', 'off')
    if (!el.hasAttribute('spellcheck')) el.setAttribute('spellcheck', 'false')
  }
}
typeAsTyped(document)
new MutationObserver((changes) => {
  for (const c of changes)
    for (const n of c.addedNodes)
      if (n instanceof HTMLElement) typeAsTyped(n.matches('input, textarea') ? n.parentNode! : n)
}).observe(document.body, { childList: true, subtree: true })
