import { EventsOn } from '../../wailsjs/runtime/runtime'

// Per-app zoom, the way Mac apps do it: ⌘+ / ⌘− / ⌘0, remembered between launches.
// Scaling the page root scales text and layout together, so nothing gets out of proportion.

const STEPS = [0.7, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 1.75, 2]
const KEY = 'zoom'

function nearest(scale: number): number {
  return STEPS.reduce((best, s) => (Math.abs(s - scale) < Math.abs(best - scale) ? s : best), STEPS[0])
}

export function loadZoom(): number {
  try {
    const v = parseFloat(localStorage.getItem(KEY) ?? '1')
    return Number.isFinite(v) ? nearest(v) : 1
  } catch {
    return 1
  }
}

/** Applies the zoom to the window and remembers it. Returns the scale actually used. */
export function applyZoom(scale: number): number {
  const s = nearest(scale)
  document.documentElement.style.zoom = String(s)
  try {
    localStorage.setItem(KEY, String(s))
  } catch {
    // not remembered: fine
  }
  return s
}

export type ZoomAction = 'in' | 'out' | 'reset'

/** The next scale for an action, clamped to the ends of the range. */
export function stepZoom(current: number, action: ZoomAction): number {
  if (action === 'reset') return 1
  const i = STEPS.indexOf(nearest(current))
  return STEPS[Math.min(STEPS.length - 1, Math.max(0, i + (action === 'in' ? 1 : -1)))]
}

/** The View menu's Zoom In / Zoom Out / Actual Size (menu.go). */
export function onZoomEvent(handler: (action: ZoomAction) => void): () => void {
  return EventsOn('zoom', handler)
}

/** ⌘+ / ⌘− / ⌘0 (and their numeric-keypad and shifted forms). */
export function zoomKey(e: KeyboardEvent): ZoomAction | null {
  if (!e.metaKey && !e.ctrlKey) return null
  switch (e.key) {
    case '+':
    case '=':
      return 'in'
    case '-':
    case '_':
      return 'out'
    case '0':
      return 'reset'
    default:
      return null
  }
}
