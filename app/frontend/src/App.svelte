<script lang="ts">
  import { tick } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import Step from './lib/Step.svelte'
  import RunbookTitle from './lib/RunbookTitle.svelte'
  import ConfirmDialog from './lib/ConfirmDialog.svelte'
  import ForkDialog from './lib/ForkDialog.svelte'
  import RunbookSettings from './lib/RunbookSettings.svelte'
  import ExportSheet from './lib/ExportSheet.svelte'
  import PasteDialog from './lib/PasteDialog.svelte'
  import NotionDialog from './lib/NotionDialog.svelte'
  import AppSettings from './lib/AppSettings.svelte'
  import { loadExportPrefs } from './lib/exportPrefs'
  import { applyZoom, loadZoom, onZoomEvent, stepZoom, zoomKey, type ZoomAction } from './lib/zoom'
  import { missingArgs, resolveCommand } from './lib/args'
  import Sidebar from './lib/Sidebar.svelte'
  import TerminalPane from './lib/TerminalPane.svelte'
  import {
    answerGate,
    chooseDirectory,
    copyText,
    getShellStatus,
    onRunEvent,
    onShellEvent,
    resetShell,
    startRun,
    stopRun,
    type GateDecision,
    type ShellStatus,
  } from './lib/runClient'
  import {
    createRunbook,
    deleteRunbook,
    exportMarkdown,
    forkRunbook,
    listRunbooks,
    createFolder,
    createFromNotion,
    deleteFolder,
    openInBrowser,
    moveFolder,
    moveRunbook,
    renameFolder,
    logEvent,
    openAuditLog,
    openRunbook,
    renameRunbook,
    saveStepArgs,
    saveRunbook,
    saveVersion,
  } from './lib/library'
  import type {
    AuditEntry,
    Runbook,
    SavedOutput,
    SettingField,
    SettingsResult,
    Step as StepT,
    StepKind,
    StepRun,
    Summary,
    Version,
  } from './lib/types'

  // Main shell (2a) over the on-disk library (~/runbooks). Every committed change autosaves
  // runbook.md; versions are only made by Save version.


  // --- library + the open runbook ---
  let library = $state<Summary[]>([])
  let folders = $state<string[]>([])
  let current = $state('') // ref of the open runbook: "name" or "folder/name"
  const currentFolder = $derived(current.includes('/') ? current.slice(0, current.lastIndexOf('/')) : '')
  let runbook = $state<Runbook>(emptyDoc())
  let versions = $state<Version[]>([])
  let loadError = $state('')

  let runs = $state<Record<string, StepRun>>({})
  const selected = new SvelteSet<string>()
  let editingId = $state<string | null>(null)
  let paneStepId = $state<string | null>(null) // step shown in the terminal pane

  function emptyDoc(): Runbook {
    return {
      name: '',
      cwd: '~',
      version: 0,
      parent: '',
      defaults: { destructive: false, continueOnFail: false, timeoutSec: 60 },
      running: { pauseAtDestructive: true, audit: true },
      notion: { pageId: '', url: '' },
      steps: [],
      preamble: '',
      frontMatterRaw: '',
    }
  }

  function stepNumber(id: string | undefined | null): number {
    return runbook.steps.findIndex((s) => s.id === id) + 1
  }

  // Steps are named, not numbered, in the UI; runs.log still records the number.
  function stepName(id: string | undefined | null): string {
    const t = runbook.steps.find((s) => s.id === id)?.title ?? ''
    return quoted(t)
  }
  function quoted(title: string): string {
    const t = title.trim() || 'untitled step'
    return `“${t.length > 44 ? t.slice(0, 43) + '…' : t}”`
  }

  function fromSaved(o: SavedOutput): StepRun {
    return {
      status: o.status,
      output: o.output,
      startedAt: Date.parse(o.startedAt),
      exitCode: o.exit,
      durationMs: o.ms,
      endReason: o.reason,
      command: o.command,
      cwd: o.cwd,
    }
  }

  async function refreshLibrary() {
    const lib = await listRunbooks()
    library = lib.runbooks
    folders = lib.folders
  }

  async function load(name: string) {
    try {
      const o = await openRunbook(name)
      current = name
      runbook = o.doc
      runbook.steps ??= []
      versions = o.versions ?? []
      stepArgs = o.stepArgs ?? {}
      argFocus = null
      runs = Object.fromEntries(Object.entries(o.outputs ?? {}).map(([id, out]) => [id, fromSaved(out)]))
      selected.clear()
      skippedIds.clear()
      editingId = null
      draft = null
      paneStepId = null
      runMessage = ''
      loadError = ''
      try {
        localStorage.setItem('lastRunbook', name)
      } catch {
        // storage unavailable: fine, we just won't remember
      }
    } catch (err) {
      loadError = `Couldn't open ${name}: ${err}`
    }
  }

  // Switching with an editor open would lose the uncommitted edit, so ask first (SPEC §5).
  let pendingSwitch = $state<string | null>(null)
  function requestOpen(name: string) {
    if (name === current || runActive) return
    if (editingId || draft) pendingSwitch = name
    else load(name)
  }

  $effect(() => {
    ;(async () => {
      try {
        await refreshLibrary()
      } catch (err) {
        loadError = `Couldn't read ~/runbooks: ${err}`
        return
      }
      let last = ''
      try {
        last = localStorage.getItem('lastRunbook') ?? ''
      } catch {
        // ignore
      }
      const pick = library.find((r) => r.name === last)?.name ?? library[0]?.name
      if (pick) await load(pick)
    })()
  })

  async function create(folder: string, name: string) {
    const ref = folder ? `${folder}/${name}` : name
    try {
      await createRunbook(ref)
      await refreshLibrary()
      await load(ref)
    } catch (err) {
      runMessage = `Couldn't create ${name}: ${err}`
    }
  }

  // --- folders (one level) ---
  async function newFolder(parent: string, name: string) {
    try {
      await createFolder(parent ? `${parent}/${name}` : name)
      await refreshLibrary()
    } catch (err) {
      runMessage = `Couldn't create folder ${name}: ${err}`
    }
  }

  // Moving or renaming a folder takes the open runbook's ref with it when it's inside.
  async function relocateFolder(ref: string, run: () => Promise<string>, what: string) {
    const openInside = current === ref || current.startsWith(ref + '/')
    try {
      if (openInside) await saveChain
      const to = await run()
      await refreshLibrary()
      if (openInside) await load(to + current.slice(ref.length))
      runMessage = what
    } catch (err) {
      runMessage = `Couldn't move folder: ${err}`
    }
  }

  const moveFolderTo = (ref: string, parent: string) =>
    relocateFolder(ref, () => moveFolder(ref, parent), `Moved ${ref} to ${parent || 'the top level'}.`)

  const renameFolderTo = (ref: string, name: string) =>
    relocateFolder(ref, () => renameFolder(ref, name), `Renamed folder to ${name}.`)

  async function move(ref: string, folder: string) {
    try {
      if (ref === current) await saveChain
      const to = await moveRunbook(ref, folder)
      await refreshLibrary()
      if (ref === current) await load(to)
      runMessage = `Moved ${ref.slice(ref.lastIndexOf('/') + 1)} to ${folder || 'the top level'}.`
    } catch (err) {
      runMessage = `Couldn't move: ${err}`
    }
  }

  let pendingDeleteFolder = $state<string | null>(null)

  async function doDeleteFolder() {
    const name = pendingDeleteFolder
    pendingDeleteFolder = null
    if (!name) return
    const openInside = current.startsWith(name + '/')
    try {
      if (openInside) await saveChain
      await deleteFolder(name)
      await refreshLibrary()
      runMessage = `Moved folder ${name} to the Trash.`
      if (!openInside) return
      const next = library[0]?.name
      if (next) {
        await load(next)
        runMessage = `Moved folder ${name} to the Trash.`
      } else showEmptyLibrary()
    } catch (err) {
      runMessage = `Couldn't delete folder ${name}: ${err}`
    }
  }

  function showEmptyLibrary() {
    current = ''
    runbook = emptyDoc()
    versions = []
    runs = {}
    selected.clear()
    paneStepId = null
  }

  // --- autosave ---
  let saveState = $state<'saved' | 'saving' | 'error'>('saved')
  let saveChain: Promise<void> = Promise.resolve()

  function persist() {
    const name = current
    const doc = $state.snapshot(runbook) as Runbook
    saveState = 'saving'
    saveChain = saveChain
      .then(() => saveRunbook(name, doc))
      .then(() => {
        saveState = 'saved'
      })
      .catch((err) => {
        saveState = 'error'
        runMessage = `Couldn't save: ${err}`
      })
  }

  // UI-originated audit entries (runs are journalled by the backend).
  function logUI(entry: AuditEntry) {
    if (runbook.running.audit) logEvent(current, entry).catch((err) => (runMessage = `Couldn't write runs.log: ${err}`))
  }

  async function openLog(reveal: boolean) {
    try {
      await openAuditLog(current, reveal)
    } catch (err) {
      const msg = `${err}`
      runMessage = msg.charAt(0).toUpperCase() + msg.slice(1) + '.'
    }
  }

  // --- shared shell (single ▶ clicks) ---
  let shell = $state<ShellStatus>({ alive: false, cwd: '', started: '' })
  $effect(() => {
    getShellStatus().then((s) => (shell = s))
    return onShellEvent((s) => (shell = s))
  })

  async function doResetShell() {
    try {
      await resetShell()
      logUI({ event: 'shell.reset' })
    } catch (err) {
      runMessage = `${err}`
    }
  }

  async function changePath() {
    try {
      const dir = await chooseDirectory(runbook.cwd)
      if (dir && dir !== runbook.cwd) {
        runbook.cwd = dir
        persist()
        logUI({ event: 'runbook.path', message: dir })
      }
    } catch (err) {
      runMessage = `${err}`
    }
  }

  // --- runs (Go run loop, via Wails events) ---
  const OUTPUT_CAP = 512 * 1024 // keep the tail of very chatty steps; bounds memory

  let runActive = $state(false)
  const pendingIds = new SvelteSet<string>() // queued or running in the active run
  const skippedIds = new SvelteSet<string>()
  let runMessage = $state('')
  let lastFailedId = ''
  let lastRunSingle = false
  let gateId = $state<string | null>(null) // Run all is paused before this step

  $effect(() =>
    onRunEvent((e) => {
      const id = e.stepId ?? ''
      switch (e.type) {
        case 'step.start':
          runs[id] = { status: 'running', output: '', startedAt: Date.parse(e.ts), command: e.chunk, cwd: e.cwd }
          paneStepId = id
          break
        case 'step.output': {
          const r = runs[id]
          if (!r) break
          const out = r.output + (e.chunk ?? '')
          r.output = out.length > OUTPUT_CAP ? '… (earlier output trimmed)\n' + out.slice(-OUTPUT_CAP) : out
          break
        }
        case 'step.finish': {
          const r = runs[id]
          if (r) {
            r.status = e.reason === 'exit' && e.exit === 0 ? 'passed' : 'failed'
            r.exitCode = e.exit
            r.endReason = e.reason
            r.durationMs = e.ms ?? 0
            if (r.status === 'failed') lastFailedId = id
          }
          pendingIds.delete(id)
          break
        }
        case 'step.skip':
          pendingIds.delete(id)
          skippedIds.add(id)
          break
        case 'step.gate':
          gateId = id
          break
        case 'run.finish':
          endRunLocally()
          refreshLibrary()
          if (e.outcome === 'error') runMessage = `Run could not start: ${e.message}`
          else if (lastRunSingle) runMessage = ''
          else if (!runMessage) {
            runMessage =
              e.outcome === 'completed'
                ? 'Run finished.'
                : e.outcome === 'stopped'
                  ? 'Run stopped.'
                  : `Run stopped: ${stepName(lastFailedId)} failed${e.message ? ` (${e.message})` : ''}.`
          }
          break
      }
    }),
  )

  function decideGate(d: GateDecision) {
    if (!gateId) return
    if (d === 'stop') runMessage = `Run stopped before ${stepName(gateId)}.`
    gateId = null
    answerGate(d)
  }

  // Single-step run of a destructive step: generic confirm first (3b, left card).
  let pendingConfirm = $state<StepT | null>(null)

  // --- {{arguments}}: each step has its own values, typed into boxes under its command ---
  let stepArgs = $state<Record<string, Record<string, string>>>({})
  let argFocus = $state<{ stepId: string; name: string } | null>(null)
  const argSaveTimers: Record<string, ReturnType<typeof setTimeout>> = {}

  function setArg(stepId: string, name: string, value: string) {
    stepArgs[stepId] = { ...(stepArgs[stepId] ?? {}), [name]: value }
    if (argFocus?.stepId === stepId && argFocus.name === name && value.trim()) argFocus = null
    const rb = current
    const snapshot = $state.snapshot(stepArgs[stepId]) as Record<string, string>
    clearTimeout(argSaveTimers[stepId])
    argSaveTimers[stepId] = setTimeout(() => saveStepArgs(rb, stepId, snapshot).catch(() => {}), 400)
  }

  // Refuses (and points at the first empty box) when a step that will run has an empty blank.
  function argsReady(steps: StepT[]): boolean {
    for (const s of steps) {
      const missing = missingArgs(s, stepArgs[s.id])
      if (missing.length === 0) continue
      argFocus = { stepId: s.id, name: missing[0] }
      runMessage = `Fill in ${missing.join(', ')} for ${quoted(s.title)} first.`
      return false
    }
    return true
  }

  function runSingle(step: StepT) {
    if (!argsReady([step])) return
    if (isDestructive(step)) pendingConfirm = step
    else launch([step], true)
  }

  function isDestructive(step: StepT): boolean {
    return step.destructive ?? runbook.defaults.destructive
  }

  function confirmRun() {
    const step = pendingConfirm // read before clearing: the dialog's {@const} goes null with it
    pendingConfirm = null
    if (!step) return
    logUI({ event: 'confirm.approve', stepId: step.id, step: stepNumber(step.id), title: step.title, message: 'dialog' })
    launch([step], true)
  }

  function cancelConfirm() {
    const step = pendingConfirm
    pendingConfirm = null
    if (step) logUI({ event: 'confirm.cancel', stepId: step.id, step: stepNumber(step.id), title: step.title, message: 'dialog' })
  }

  function endRunLocally() {
    runActive = false
    gateId = null
    pendingIds.clear()
  }

  // Stop always frees the UI: if the backend has no run (e.g. it restarted), reset locally.
  async function stop() {
    let backendHadRun = false
    try {
      backendHadRun = await stopRun()
    } catch {
      // fall through to the local reset
    }
    if (!backendHadRun) {
      endRunLocally()
      runMessage = 'Run stopped.'
    }
  }

  async function launch(steps: StepT[], single: boolean) {
    if (runActive || steps.length === 0 || !current) return
    runActive = true
    try {
      lastFailedId = ''
      lastRunSingle = single
      runMessage = ''
      skippedIds.clear()
      pendingIds.clear()
      for (const s of steps) if (s.kind === 'command') pendingIds.add(s.id)
      const mode = single ? 'single' : selected.size > 0 ? 'run selected' : 'run all'
      await startRun(
        current,
        $state.snapshot(runbook) as Runbook,
        steps.map((s) => s.id),
        mode,
        $state.snapshot(stepArgs) as Record<string, Record<string, string>>,
      )
    } catch (err) {
      endRunLocally()
      runMessage = `Run could not start: ${err}`
    }
  }

  const runTargets = $derived(selected.size > 0 ? runbook.steps.filter((s) => selected.has(s.id)) : runbook.steps)
  const runLabel = $derived(
    selected.size > 0 ? `▶ Run selected (${runTargets.filter((s) => s.kind === 'command').length})` : '▶ Run all',
  )

  // --- editing ---
  function newStepId(): string {
    const b = new Uint8Array(4)
    crypto.getRandomValues(b)
    return 's' + [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
  }

  // The 5a add-step editor: a draft step shown at `index` until it's saved.
  let draft = $state<{ step: StepT; index: number } | null>(null)

  function openDraft(index: number) {
    if (runActive) return
    editingId = null
    draft = { step: { id: newStepId(), title: '', kind: 'command', command: '', note: '' }, index }
  }

  const items = $derived(
    draft ? [...runbook.steps.slice(0, draft.index), draft.step, ...runbook.steps.slice(draft.index)] : runbook.steps,
  )

  function addStep(title: string, body: string, kind: StepKind, andAnother: boolean) {
    if (!draft) return
    const step: StepT = { id: draft.step.id, title, kind, command: kind === 'command' ? body : '', note: kind === 'note' ? body : '' }
    const at = draft.index
    runbook.steps.splice(at, 0, step)
    persist()
    logUI({ event: 'step.add', stepId: step.id, step: at + 1, title, command: body })
    if (andAnother) openDraft(at + 1)
    else draft = null
  }

  function fmtSetting(v: unknown): string {
    return v === undefined ? 'inherit' : v === 0 ? 'no timeout' : String(v)
  }

  // Duplicate: a copy directly below, with the same command, settings and argument values,
  // opened in the editor so it can be changed straight away.
  function duplicateStep(step: StepT, index: number) {
    if (runActive) return
    const copy: StepT = { ...($state.snapshot(step) as StepT), id: newStepId(), title: `${step.title} copy` }
    runbook.steps.splice(index + 1, 0, copy)
    const args = stepArgs[step.id]
    if (args) {
      stepArgs[copy.id] = { ...args }
      saveStepArgs(current, copy.id, $state.snapshot(stepArgs[copy.id]) as Record<string, string>).catch(() => {})
    }
    persist()
    logUI({
      event: 'step.add',
      stepId: copy.id,
      step: index + 2,
      title: copy.title,
      command: copy.command || copy.note,
      message: `duplicate of ${step.title}`,
    })
    draft = null
    editingId = copy.id
  }

  function changeSetting(step: StepT, field: SettingField, value: boolean | number | string | undefined) {
    const before = step[field]
    if (before === value) return
    if (value === undefined) delete step[field]
    else (step as unknown as Record<string, unknown>)[field] = value
    persist()
    logUI({
      event: 'step.settings',
      stepId: step.id,
      step: stepNumber(step.id),
      title: step.title,
      message: `${field}: ${fmtSetting(before)} → ${fmtSetting(value)}`,
    })
  }

  const canReorder = $derived(!runActive && editingId === null && draft === null)

  async function moveStep(id: string, to: number) {
    const from = runbook.steps.findIndex((s) => s.id === id)
    if (from < 0 || to < 0 || to >= runbook.steps.length || to === from) return
    const [step] = runbook.steps.splice(from, 1)
    runbook.steps.splice(to, 0, step)
    persist()
    logUI({ event: 'step.move', stepId: id, step: to + 1, title: step.title, message: `${from + 1} → ${to + 1}` })
    await tick()
    document.querySelectorAll<HTMLElement>('.steps .step')[to]?.focus()
  }

  // Drag and drop (5b): a dashed slot shows where the step will land.
  let dragId = $state<string | null>(null)
  let dropIndex = $state<number | null>(null)

  function dragOver(e: DragEvent) {
    if (!dragId) return
    e.preventDefault()
    const rows = [...document.querySelectorAll<HTMLElement>('.steps .step')]
    let i = rows.findIndex((el) => {
      const r = el.getBoundingClientRect()
      return e.clientY < r.top + r.height / 2
    })
    dropIndex = i < 0 ? rows.length : i
  }

  function drop(e: DragEvent) {
    e.preventDefault()
    if (dragId !== null && dropIndex !== null) {
      const from = runbook.steps.findIndex((s) => s.id === dragId)
      moveStep(dragId, dropIndex > from ? dropIndex - 1 : dropIndex)
    }
    dragId = null
    dropIndex = null
  }

  // Short labels for non-default settings, shown in the step header.
  function tagsFor(step: StepT): string[] {
    if (step.kind === 'note') return []
    const t: string[] = []
    if (step.continueOnFail ?? runbook.defaults.continueOnFail) t.push('keeps going')
    if (step.skipInRunAll) t.push('skip in Run all')
    if (step.timeoutSec !== undefined && step.timeoutSec !== runbook.defaults.timeoutSec)
      t.push(step.timeoutSec === 0 ? 'no timeout' : `${step.timeoutSec}s timeout`)
    if (step.cwd) t.push(`in ${step.cwd}`)
    if (step.recordOutput === false) t.push('output not recorded')
    return t
  }

  // Runbook settings (5c).
  let showSettings = $state(false)

  async function applySettings(r: SettingsResult) {
    showSettings = false
    const changes: string[] = []
    const d = runbook.defaults
    if (r.cwd !== runbook.cwd) changes.push(`path → ${r.cwd}`)
    if (r.defaults.destructive !== d.destructive) changes.push(`destructive default ${r.defaults.destructive}`)
    if (r.defaults.continueOnFail !== d.continueOnFail) changes.push(`keep going default ${r.defaults.continueOnFail}`)
    if (r.defaults.timeoutSec !== d.timeoutSec) changes.push(`timeout default ${fmtSetting(r.defaults.timeoutSec)}`)
    if (r.running.pauseAtDestructive !== runbook.running.pauseAtDestructive)
      changes.push(`pause at destructive ${r.running.pauseAtDestructive}`)
    if (r.running.audit !== runbook.running.audit) changes.push(`audit log ${r.running.audit ? 'on' : 'off'}`)
    if (r.resetOverrides) changes.push('step overrides reset')

    if (changes.length > 0) {
      // Log before applying, so turning the audit log off is itself recorded.
      logUI({ event: 'runbook.settings', message: changes.join(', ') })
      runbook.cwd = r.cwd
      runbook.defaults = r.defaults
      runbook.running = r.running
      if (r.resetOverrides)
        for (const s of runbook.steps) {
          delete s.destructive
          delete s.continueOnFail
          delete s.timeoutSec
        }
      if (r.running.audit && changes.includes('audit log on')) logUI({ event: 'runbook.settings', message: 'audit log on' })
      persist()
    }
    if (r.name !== runbook.name) await rename(r.name)
    if (r.folder !== currentFolder) await move(current, r.folder)
  }

  // Delete (moves to the Trash), from settings or the Library's 🗑. Deleting the open
  // runbook opens the next one, or shows the empty library.
  let pendingDeleteRunbook = $state<string | null>(null)

  async function doDeleteRunbook() {
    const name = pendingDeleteRunbook
    pendingDeleteRunbook = null
    if (!name) return
    try {
      if (name === current) await saveChain
      await deleteRunbook(name)
      await refreshLibrary()
      runMessage = `Moved ${name} to the Trash.`
      if (name !== current) return
      const next = library[0]?.name
      if (next) {
        await load(next)
        runMessage = `Moved ${name} to the Trash.`
      } else showEmptyLibrary()
    } catch (err) {
      runMessage = `Couldn't delete ${name}: ${err}`
    }
  }

  async function chooseFolder(current: string): Promise<string> {
    try {
      return await chooseDirectory(current)
    } catch (err) {
      runMessage = `${err}`
      return ''
    }
  }

  function saveStep(step: StepT, title: string, body: string) {
    const before = { title: step.title, body: step.kind === 'note' ? step.note : step.command }
    editingId = null
    if (title === before.title && body === before.body) return
    step.title = title
    if (step.kind === 'note') step.note = body
    else step.command = body
    persist()
    logUI({ event: 'step.edit', stepId: step.id, step: stepNumber(step.id), title, before, after: { title, body } })
  }

  let pendingDelete = $state<StepT | null>(null)

  function deleteStep(step: StepT) {
    if (pendingIds.has(step.id)) return
    logUI({ event: 'step.delete', stepId: step.id, step: stepNumber(step.id), title: step.title, command: step.command || step.note })
    runbook.steps = runbook.steps.filter((s) => s.id !== step.id)
    selected.delete(step.id)
    delete runs[step.id]
    if (paneStepId === step.id) paneStepId = null
    editingId = null
    pendingDelete = null
    persist()
  }

  async function rename(name: string) {
    const old = current
    try {
      await saveChain
      const to = await renameRunbook(old, $state.snapshot(runbook) as Runbook, name)
      await refreshLibrary()
      await load(to)
    } catch (err) {
      runMessage = `Couldn't rename: ${err}`
    }
  }

  // --- versions + fork ---
  async function doSaveVersion() {
    try {
      await saveChain
      const r = await saveVersion(current, $state.snapshot(runbook) as Runbook)
      runbook.version = r.doc.version
      runbook.frontMatterRaw = r.doc.frontMatterRaw
      versions = r.versions ?? []
      runMessage = `Saved v${r.doc.version}.`
    } catch (err) {
      runMessage = `Couldn't save version: ${err}`
    }
  }

  let forking = $state(false)
  async function fork(ref: string, ids: string[], keepOutputs: boolean, linkParent: boolean) {
    forking = false
    const name = ref.slice(ref.lastIndexOf('/') + 1)
    try {
      await saveChain
      await forkRunbook(current, $state.snapshot(runbook) as Runbook, ids, ref, keepOutputs, linkParent)
      await refreshLibrary()
      await load(ref)
      runMessage = `Created ${name} from ${ids.length} steps.`
    } catch (err) {
      runMessage = `Couldn't create ${name}: ${err}`
    }
  }

  // --- export (6a/6b) + paste (§4) ---
  let showExport = $state(false)
  let showPaste = $state(false)
  const selectedIds = $derived(runbook.steps.filter((s) => selected.has(s.id)).map((s) => s.id))

  async function copyExport(markdown: string, steps: number) {
    try {
      await copyText(markdown)
      runMessage = `✓ Markdown copied (${steps} step${steps === 1 ? '' : 's'})`
      logUI({ event: 'runbook.export', message: `copied ${steps} step${steps === 1 ? '' : 's'}` })
    } catch (err) {
      runMessage = `Copy failed: ${err}`
    }
  }

  // ⌘⇧C: selected steps (or all, if none are checked) straight to the clipboard, no sheet.
  async function quickCopy() {
    if (!current) return
    const prefs = loadExportPrefs()
    const ids = selectedIds
    try {
      const md = await exportMarkdown(current, $state.snapshot(runbook) as Runbook, { stepIds: ids, ...prefs })
      await copyExport(md, ids.length || runbook.steps.length)
    } catch (err) {
      runMessage = `Copy failed: ${err}`
    }
  }

  // --- zoom (⌘+ / ⌘− / ⌘0, and the View menu) ---
  let zoom = $state(1)
  $effect(() => {
    zoom = applyZoom(loadZoom())
    return onZoomEvent(doZoom)
  })

  function doZoom(action: ZoomAction) {
    const next = stepZoom(zoom, action)
    if (next === zoom) return
    zoom = applyZoom(next)
    runMessage = `Zoom ${Math.round(zoom * 100)}%`
  }

  function onWindowKeydown(e: KeyboardEvent) {
    const zoomAction = zoomKey(e)
    if (zoomAction) {
      e.preventDefault()
      doZoom(zoomAction)
      return
    }
    const dialogOpen = document.querySelector('dialog[open]') !== null
    if (e.metaKey && !e.shiftKey && e.key === ',') {
      e.preventDefault()
      showAppSettings = true
      return
    }
    if (e.metaKey && e.shiftKey && e.key.toLowerCase() === 'c' && !dialogOpen && !editingId && !draft) {
      e.preventDefault()
      quickCopy()
    }
  }

  // --- app settings (⌘,) ---
  let showAppSettings = $state(false)

  // --- Notion import (read-only) ---
  let showNotion = $state(false)
  // Bumped when the Notion token changes, so an open import dialog re-checks it.
  let notionKey = $state(0)

  async function importFromNotion(ref: string, steps: StepT[], pageId: string, pageUrl: string) {
    showNotion = false
    try {
      await createFromNotion(ref, steps, pageId, pageUrl)
      await refreshLibrary()
      await load(ref)
      runMessage = `Imported ${steps.length} step${steps.length === 1 ? '' : 's'} from Notion.`
    } catch (err) {
      runMessage = `Couldn't import: ${err}`
    }
  }

  function addPasted(steps: StepT[]) {
    showPaste = false
    const at = runbook.steps.length
    for (const st of steps) {
      const step: StepT = { ...st, id: newStepId() }
      runbook.steps.push(step)
      logUI({ event: 'step.add', stepId: step.id, step: runbook.steps.length, title: step.title, command: step.command || step.note, message: 'pasted' })
    }
    persist()
    runMessage = `Added ${steps.length} pasted step${steps.length === 1 ? '' : 's'} (${at + 1}–${at + steps.length}).`
  }

  async function copy(text: string) {
    try {
      await copyText(text)
      runMessage = 'Copied to clipboard.'
    } catch {
      runMessage = 'Copy failed.'
    }
  }

  function fmtTime(iso: string): string {
    return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
</script>

<svelte:window onkeydown={onWindowKeydown} />

<div class="window">
  <div class="chrome">
    {#if currentFolder}<span class="crumb">{currentFolder} /</span>{/if}
    {#if current}<RunbookTitle name={runbook.name} locked={runActive} onRename={rename} />{/if}
    {#if runbook.version}<span class="muted">v{runbook.version}</span>{/if}
    {#if editingId}<span class="tag">editing {stepName(editingId)}</span>{/if}
    {#if draft}<span class="tag">unsaved changes</span>{/if}
    <span class="save-state" class:error={saveState === 'error'}>
      {saveState === 'saving' ? 'saving…' : saveState === 'error' ? 'not saved' : current ? 'saved' : ''}
    </span>
    <span class="spacer"></span>
    {#if current}
      <button onclick={() => (showSettings = true)} disabled={runActive}>⚙ Settings</button>
      <button onclick={() => (showExport = true)} disabled={runbook.steps.length === 0} title="Export as Markdown · ⌘⇧C copies the selected steps directly"
        >↗ Export</button
      >
    {/if}
    {#if runActive}
      <button class="stop-run" onclick={stop}>■ Stop run</button>
    {:else}
      <button
        class="primary"
        onclick={() => {
          // Steps Run all skips don't need their blanks filled.
          const willRun = runTargets.filter((s) => !(s.skipInRunAll ?? false))
          if (argsReady(willRun)) launch(runTargets, false)
        }}
        disabled={editingId !== null || draft !== null || !runTargets.some((s) => s.kind === 'command')}
        title={editingId ? 'Finish editing first' : 'Fresh shell, top to bottom; stops on the first failure unless a step keeps going'}
        >{runLabel}</button
      >
    {/if}
  </div>

  <Sidebar
    {library}
    {folders}
    onCreateFolder={newFolder}
    onDeleteFolder={(f) => (pendingDeleteFolder = f)}
    onRenameFolder={renameFolderTo}
    onMoveFolder={moveFolderTo}
    onMove={move}
    {current}
    {versions}
    auditOn={runbook.running.audit}
    onOpenLog={openLog}
    onOpenSettings={() => (showAppSettings = true)}
    locked={runActive}
    onOpen={requestOpen}
    onCreate={create}
    onDelete={(name) => (pendingDeleteRunbook = name)}
  />

    <main class="center">
      {#if loadError}<p class="load-error">{loadError}</p>{/if}
      {#if !current && library.length === 0 && !loadError}
        <div class="empty-library">
          <p><strong>No runbooks yet.</strong></p>
          <p>Use <strong>+ New</strong> in the Library, then <strong>+ Add step</strong> or <strong>Paste steps…</strong> to fill it in.</p>
        </div>
      {/if}
      {#if current}
      <div class="context">
        <div class="row">
          <span class="k">Path</span>
          <code>{runbook.cwd}</code>
          <button class="link" onclick={changePath} disabled={runActive || !current}>Change</button>
        </div>
        <div class="row">
          <span class="k">Shell</span>
          {#if shell.alive}
            <code>{shell.cwd}</code>
            <span class="muted">· shared by ▶ since {fmtTime(shell.started)}</span>
            <button class="link" onclick={doResetShell} disabled={runActive}>Reset shell</button>
          {:else}
            <span class="muted">starts on the first ▶ and is shared by later ▶ clicks; Run all uses a fresh one</span>
          {/if}
        </div>
      </div>
      {/if}
      {#if current}
        <div class="toolbar">
          <button onclick={() => (showPaste = true)} disabled={runActive || draft !== null} title="Paste Markdown, a numbered list or a terminal session; steps are added at the end"
            >Paste steps…</button
          >
          <button onclick={() => openDraft(runbook.steps.length)} disabled={runActive || draft !== null}>+ Add step</button>
          {#if runbook.notion?.url}
            <button class="link" onclick={() => openInBrowser(runbook.notion.url)} title={runbook.notion.url}>Open in Notion ↗</button>
          {/if}
        </div>
      {/if}
      {#if runbook.preamble}<p class="preamble">{runbook.preamble}</p>{/if}
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="steps" ondragover={dragOver} ondrop={drop}>
        {#each items as step, i (step.id)}
          {#if dragId && dropIndex === i}<div class="drop-slot">drop here</div>{/if}
          {#if draft && step.id === draft.step.id}
            <Step
              {step}
              number={i + 1}
              isNew
              editing
              onSave={addStep}
              onCancel={() => (draft = null)}
            />
          {:else}
          <Step
            {step}
            number={i + 1}
            destructive={isDestructive(step)}
            argValues={stepArgs[step.id] ?? {}}
            argFocus={argFocus?.stepId === step.id ? argFocus.name : null}
            onArgChange={(n, v) => setArg(step.id, n, v)}
            tags={tagsFor(step)}
            defaults={runbook.defaults}
            runbookCwd={runbook.cwd}
            reorderable={canReorder}
            dragging={dragId === step.id}
            onDragStart={() => (dragId = step.id)}
            onDragEnd={() => {
              dragId = null
              dropIndex = null
            }}
            onMove={(delta) => moveStep(step.id, i + delta)}
            onSettingChange={(f, v) => changeSetting(step, f, v)}
            onChooseDir={() => chooseFolder(step.cwd || runbook.cwd)}
            onInsertBelow={() => openDraft(i + 1)}
            run={runs[step.id]}
            selected={selected.has(step.id)}
            editing={editingId === step.id}
            locked={pendingIds.has(step.id)}
            queued={pendingIds.has(step.id) && runs[step.id]?.status !== 'running'}
            skipped={skippedIds.has(step.id)}
            gated={gateId === step.id}
            viewing={paneStepId === step.id}
            onFocus={() => {
              if (runs[step.id]) paneStepId = step.id
            }}
            onGate={decideGate}
            runDisabled={runActive}
            runDisabledReason="A run is in progress"
            onToggleSelect={() => (selected.has(step.id) ? selected.delete(step.id) : selected.add(step.id))}
            onRun={() => runSingle(step)}
            onStop={stop}
            onEdit={() => {
              if (!pendingIds.has(step.id)) {
                draft = null
                editingId = step.id
              }
            }}
            onSave={(title, body) => saveStep(step, title, body)}
            onCancel={() => (editingId = null)}
            onDelete={() => (pendingDelete = step)}
            onDuplicate={() => duplicateStep(step, i)}
          />
        {/if}
        {/each}
        {#if dragId && dropIndex === runbook.steps.length}<div class="drop-slot">drop here</div>{/if}
        {#if current && runbook.steps.length === 0 && !draft}<p class="muted">No steps yet.</p>{/if}
      </div>
      {#if current}
        <div class="add-row">
          <button onclick={() => openDraft(runbook.steps.length)} disabled={runActive || draft !== null}>+ Add step</button>
          <button class="link" onclick={() => (showPaste = true)} disabled={runActive || draft !== null}>Paste steps…</button>
          <button class="link" onclick={() => (showNotion = true)} disabled={runActive || draft !== null}>Import from Notion…</button>
        </div>
      {/if}
    </main>

  <TerminalPane
    steps={runbook.steps}
    {runs}
    stepId={paneStepId}
    onScrub={(id) => (paneStepId = id)}
    onCopy={copy}
    onStop={stop}
  />

  <footer>
    <span>{selected.size} of {runbook.steps.length} selected</span>
    {#if runMessage}<span class="run-message">{runMessage}</span>{/if}
    <span class="spacer"></span>
    <button onclick={doSaveVersion} disabled={!current || runActive} title="Snapshot runbook.md to versions/v{runbook.version + 1}.md"
      >Save version</button
    >
    <button onclick={() => (forking = true)} disabled={selected.size === 0 || runActive}>Generate from selected</button>
  </footer>
</div>

{#if pendingConfirm}
  {@const target = pendingConfirm}
  <ConfirmDialog
    title="Run {quoted(target.title)} — marked destructive"
    confirmLabel="Run it"
    focusCancel
    onConfirm={confirmRun}
    onCancel={cancelConfirm}
  >
    <pre class="confirm-command">{resolveCommand(target.command, stepArgs[target.id] ?? {}) ?? target.command}</pre>
    <div class="confirm-cwd">in <code>{target.cwd || runbook.cwd}</code></div>
  </ConfirmDialog>
{/if}

{#if pendingDelete}
  {@const target = pendingDelete}
  <ConfirmDialog
    title="Delete {quoted(target.title)}?"
    confirmLabel="Delete step"
    danger
    onConfirm={() => pendingDelete && deleteStep(pendingDelete)}
    onCancel={() => (pendingDelete = null)}
  >
    <strong>{target.title}</strong> and its last output will be removed.
  </ConfirmDialog>
{/if}

{#if pendingSwitch}
  {@const target = pendingSwitch}
  <ConfirmDialog
    title={draft ? 'Discard the new step?' : `Discard the edit to ${stepName(editingId)}?`}
    confirmLabel="Discard and switch"
    danger
    focusCancel
    onConfirm={() => {
      pendingSwitch = null
      editingId = null
      draft = null
      load(target)
    }}
    onCancel={() => (pendingSwitch = null)}
  >
    You're editing a step in <strong>{runbook.name}</strong>. Switching to <strong>{target}</strong> loses the unsaved text.
  </ConfirmDialog>
{/if}

{#if showSettings}
  <RunbookSettings
    doc={runbook}
    {folders}
    folder={currentFolder}
    location="~/runbooks/{current}/"
    onSave={applySettings}
    onCancel={() => (showSettings = false)}
    onChooseDir={chooseFolder}
    onDelete={() => {
      showSettings = false
      pendingDeleteRunbook = current
    }}
  />
{/if}

{#if pendingDeleteRunbook}
  {@const name = pendingDeleteRunbook}
  {@const steps = library.find((r) => r.name === name)?.steps ?? 0}
  <ConfirmDialog
    title="Delete {name}?"
    confirmLabel="Move to Trash"
    danger
    focusCancel
    onConfirm={doDeleteRunbook}
    onCancel={() => (pendingDeleteRunbook = null)}
  >
    Moves <code>~/runbooks/{name}/</code> to the Trash: its {steps} step{steps === 1 ? '' : 's'}, saved versions, last
    outputs and audit log. You can drag it back from the Trash to restore it.
  </ConfirmDialog>
{/if}

{#if showExport}
  <ExportSheet
    name={current}
    doc={runbook}
    {selectedIds}
    onCopy={(md, n) => {
      showExport = false
      copyExport(md, n)
    }}
    onSaved={(path) => {
      showExport = false
      runMessage = `Saved ${path}`
    }}
    onClose={() => (showExport = false)}
  />
{/if}

{#if showPaste}
  <PasteDialog onAdd={addPasted} onCancel={() => (showPaste = false)} />
{/if}

{#if showNotion}
  {#key notionKey}
    <NotionDialog
      {folders}
      folder={currentFolder}
      canAddHere={!!current}
      onCreate={importFromNotion}
      onAddHere={(steps) => {
        showNotion = false
        addPasted(steps)
      }}
      onOpenSettings={() => (showAppSettings = true)}
      onCancel={() => (showNotion = false)}
    />
  {/key}
{/if}

{#if showAppSettings}
  <AppSettings onClose={() => (showAppSettings = false)} onChange={() => notionKey++} />
{/if}

{#if pendingDeleteFolder}
  {@const f = pendingDeleteFolder}
  {@const inside = library.filter((r) => r.name.startsWith(f + '/'))}
  {@const subs = folders.filter((x) => x.startsWith(f + '/'))}
  <ConfirmDialog
    title="Delete folder {f}?"
    confirmLabel="Move to Trash"
    danger
    focusCancel
    onConfirm={doDeleteFolder}
    onCancel={() => (pendingDeleteFolder = null)}
  >
    {#if inside.length === 0 && subs.length === 0}
      Moves the empty folder <code>~/runbooks/{f}/</code> to the Trash.
    {:else}
      Moves <code>~/runbooks/{f}/</code> to the Trash with everything in it
      ({inside.length} runbook{inside.length === 1 ? '' : 's'}{subs.length > 0
        ? `, ${subs.length} subfolder${subs.length === 1 ? '' : 's'}`
        : ''}, including versions, outputs and audit logs):
      <ul class="confirm-list">
        {#each subs as x (x)}<li>{x.slice(f.length + 1)}/</li>{/each}
        {#each inside as r (r.name)}<li>{r.name.slice(f.length + 1)}</li>{/each}
      </ul>
      You can drag it back from the Trash to restore it.
    {/if}
  </ConfirmDialog>
{/if}

{#if forking}
  <ForkDialog
    source={current}
    {folders}
    folder={currentFolder}
    steps={runbook.steps}
    selected={runbook.steps.filter((s) => selected.has(s.id)).map((s) => s.id)}
    onCreate={fork}
    onCancel={() => (forking = false)}
  />
{/if}

<style>
  .window {
    display: grid;
    grid-template-columns: 210px minmax(420px, 1fr) minmax(320px, 36%);
    grid-template-rows: auto 1fr auto;
    height: 100vh;
  }
  .chrome,
  footer {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 12px;
    background: var(--chrome-bg);
  }
  .chrome {
    border-bottom: 1px solid var(--border);
    --wails-draggable: drag;
  }
  .chrome :global(button),
  .chrome :global(input) {
    --wails-draggable: no-drag;
  }
  footer {
    border-top: 1px solid var(--border);
    font-size: 12px;
  }
  .tag {
    font-size: 11px;
    color: var(--warn);
    border: 1px solid currentColor;
    border-radius: 10px;
    padding: 0 7px;
  }
  .spacer {
    flex: 1;
  }
  .stop-run {
    color: var(--bad);
    border-color: var(--bad);
  }
  .run-message {
    color: var(--muted-strong);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  footer button {
    flex-shrink: 0;
    white-space: nowrap;
  }
  .center {
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
  }
  main.center {
    padding: 8px 12px;
  }
  .context {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-bottom: 8px;
    font-size: 12px;
  }
  .row {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-width: 0;
  }
  .k {
    width: 36px;
    color: var(--muted);
  }
  .row code {
    font-family: var(--mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .muted {
    color: var(--muted);
  }
  .steps {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .crumb {
    color: var(--muted);
    font-size: 13px;
    margin-right: -6px;
  }
  .save-state {
    font-size: 11px;
    color: var(--muted);
  }
  .save-state.error {
    color: var(--bad);
    font-weight: 600;
  }
  .load-error {
    color: var(--bad);
    margin: 0 0 8px;
  }
  .preamble {
    margin: 0 0 8px;
    color: var(--muted-strong);
    white-space: pre-wrap;
  }
  .drop-slot {
    border: 1px dashed var(--accent);
    border-radius: 6px;
    padding: 6px 12px;
    color: var(--accent);
    font-size: 11px;
  }
  .confirm-list {
    margin: 6px 0;
    padding-left: 18px;
    font-family: var(--mono);
  }
  .empty-library {
    color: var(--muted-strong);
    padding: 24px 4px;
  }
  .empty-library p {
    margin: 0 0 6px;
  }
  .toolbar {
    display: flex;
    gap: 8px;
    margin-bottom: 8px;
  }
  .add-row {
    display: flex;
    gap: 10px;
    align-items: center;
    margin-top: 8px;
  }
  .confirm-command {
    font-family: var(--mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
    background: var(--term-bg);
    color: var(--term-fg);
    border-radius: 4px;
    padding: 6px 8px;
    margin: 0 0 6px;
  }
  .confirm-cwd {
    color: var(--muted);
  }
  .confirm-cwd code {
    font-family: var(--mono);
    color: var(--fg);
  }
</style>
