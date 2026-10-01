// Export sheet toggles (6a), remembered so ⌘⇧C copies with the same choices.
export interface ExportPrefs {
  outputs: boolean
  settings: boolean
  timestamps: boolean
}

const KEY = 'exportPrefs'
const DEFAULTS: ExportPrefs = { outputs: true, settings: false, timestamps: false }

export function loadExportPrefs(): ExportPrefs {
  try {
    return { ...DEFAULTS, ...JSON.parse(localStorage.getItem(KEY) ?? '{}') }
  } catch {
    return { ...DEFAULTS }
  }
}

export function saveExportPrefs(p: ExportPrefs) {
  try {
    localStorage.setItem(KEY, JSON.stringify(p))
  } catch {
    // storage unavailable: the defaults are fine
  }
}
