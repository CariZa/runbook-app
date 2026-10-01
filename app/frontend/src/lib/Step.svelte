<script lang="ts">
  import { tick, untrack } from 'svelte'
  import StepSettings from './StepSettings.svelte'
  import { argNames, segments } from './args'
  import type { Runbook, SettingField, Step, StepKind, StepRun } from './types'

  // One step row, in every state from screen 2b: idle, running, passed, failed,
  // destructive, editing (4a), note — plus the empty "add step" editor (5a), the settings
  // panel (3c) and the drag handle (5b).
  interface Props {
    step: Step
    number: number
    run?: StepRun
    destructive?: boolean // resolved (step or runbook default)
    tags?: string[] // non-default settings, shown muted in the header
    defaults?: Runbook['defaults']
    runbookCwd?: string
    isNew?: boolean // the 5a add-step editor
    argValues?: Record<string, string> // this step's values for its {{name}} blanks
    argFocus?: string | null // an argument ▶ found empty: highlight and focus it
    onArgChange?: (name: string, value: string) => void
    dragging?: boolean
    reorderable?: boolean
    selected?: boolean
    editing?: boolean
    locked?: boolean // running or queued in the active run: no editing
    runDisabled?: boolean
    runDisabledReason?: string
    queued?: boolean // waiting its turn in Run all
    skipped?: boolean // passed over in the latest Run all
    gated?: boolean // Run all is paused before this destructive step (3b, right card)
    onGate?: (decision: 'approve' | 'skip' | 'stop') => void
    viewing?: boolean // shown in the terminal pane
    onFocus?: () => void
    onToggleSelect?: () => void
    onRun?: () => void
    onStop?: () => void
    onEdit?: () => void
    onSave?: (title: string, body: string, kind: StepKind, andAnother: boolean) => void
    onCancel?: () => void
    onDelete?: () => void
    onDuplicate?: () => void
    onSettingChange?: (field: SettingField, value: boolean | number | string | undefined) => void
    onChooseDir?: () => Promise<string>
    onInsertBelow?: () => void
    onMove?: (delta: -1 | 1) => void
    onDragStart?: () => void
    onDragEnd?: () => void
  }

  let {
    step,
    number,
    run,
    destructive = false,
    tags = [],
    defaults = { destructive: false, continueOnFail: false, timeoutSec: 60 },
    runbookCwd = '',
    isNew = false,
    argValues = {},
    argFocus = null,
    onArgChange,
    dragging = false,
    reorderable = false,
    selected = false,
    editing = false,
    locked = false,
    runDisabled = false,
    runDisabledReason = '',
    queued = false,
    skipped = false,
    gated = false,
    onGate,
    viewing = false,
    onFocus,
    onToggleSelect,
    onRun,
    onStop,
    onEdit,
    onSave,
    onCancel,
    onDelete,
    onDuplicate,
    onSettingChange,
    onChooseDir,
    onInsertBelow,
    onMove,
    onDragStart,
    onDragEnd,
  }: Props = $props()

  // The add-step editor can switch between command and note ("Note only").
  let draftKind = $state<StepKind>('command')
  const isNote = $derived((editing && isNew ? draftKind : step.kind) === 'note')
  let showSettings = $state(false)
  const status = $derived(run?.status ?? 'idle')

  // Failed and running steps show output by default; a manual toggle wins until the next run.
  let outputOverride = $state<boolean | null>(null)
  $effect(() => {
    run?.startedAt
    outputOverride = null
  })
  const outputOpen = $derived(outputOverride ?? (status === 'running' || status === 'failed'))

  let now = $state(Date.now())
  $effect(() => {
    if (status !== 'running') return
    const t = setInterval(() => (now = Date.now()), 250)
    return () => clearInterval(t)
  })
  const elapsedSec = $derived(run && status === 'running' ? Math.max(0, Math.floor((now - run.startedAt) / 1000)) : 0)

  let outputEl = $state<HTMLPreElement>()
  $effect(() => {
    run?.output
    if (status === 'running' && outputEl) outputEl.scrollTop = outputEl.scrollHeight
  })

  // --- editing (4a) ---
  let draftTitle = $state('')
  let draftBody = $state('')
  let titleEl = $state<HTMLInputElement>()
  let bodyEl = $state<HTMLTextAreaElement>()

  $effect(() => {
    if (!editing) return
    untrack(() => {
      draftKind = step.kind
      draftTitle = step.title
      draftBody = step.kind === 'note' ? step.note : step.command
    })
    tick().then(() => titleEl?.focus())
  })

  const canSave = $derived(draftTitle.trim() !== '')

  function save(andAnother = false) {
    if (canSave) onSave?.(draftTitle.trim(), draftBody, isNote ? 'note' : 'command', andAnother)
  }

  function editorKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      onCancel?.()
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault()
      save(isNew) // 5a: ⌘⏎ saves and opens another
    }
  }

  // ⇥ moves between title and command in both directions.
  function titleKeydown(e: KeyboardEvent) {
    if (e.key === 'Tab') {
      e.preventDefault()
      bodyEl?.focus()
    } else editorKeydown(e)
  }
  function bodyKeydown(e: KeyboardEvent) {
    if (e.key === 'Tab') {
      e.preventDefault()
      titleEl?.focus()
    } else editorKeydown(e)
  }

  // After save/cancel, hand focus back to the row so keyboard flow continues.
  let rowEl = $state<HTMLElement>()
  let wasEditing = false
  $effect(() => {
    if (wasEditing && !editing) rowEl?.focus()
    wasEditing = editing
  })

  function rowKeydown(e: KeyboardEvent) {
    if (e.target !== e.currentTarget || editing) return
    const space = e.key === ' ' || e.code === 'Space'
    if (space) {
      e.preventDefault()
      onToggleSelect?.()
    } else if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown') && reorderable) {
      e.preventDefault()
      onMove?.(e.key === 'ArrowUp' ? -1 : 1)
    }
  }

  // Argument boxes (one per {{blank}} in this step's command). ⏎ in a box runs the step.
  const stepArgs = $derived(step.kind === 'command' ? argNames(step.command) : [])
  let argInputs: Record<string, HTMLInputElement> = $state({})
  $effect(() => {
    if (argFocus) tick().then(() => argInputs[argFocus]?.focus())
  })
  function argKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !runDisabled) {
      e.preventDefault()
      onRun?.()
    }
  }

  function dragStart(e: DragEvent) {
    if (!e.dataTransfer || !rowEl) return
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', step.id)
    e.dataTransfer.setDragImage(rowEl, 20, 14)
    onDragStart?.()
  }

  function fmtDuration(ms: number): string {
    return ms < 60_000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.floor(ms / 60_000)}m${Math.round((ms % 60_000) / 1000)}s`
  }
  function fmtTime(ts: number): string {
    return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  function autoRows(text: string): number {
    return Math.min(12, Math.max(2, text.split('\n').length))
  }
</script>

<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<article
  bind:this={rowEl}
  class="step {status}"
  class:note={isNote}
  class:editing
  class:selected
  class:destructive
  class:dragging
  class:gated
  class:viewing
  onfocusin={() => onFocus?.()}
  tabindex={editing ? -1 : 0}
  onkeydown={rowKeydown}
  aria-label="Step {number}: {step.title}"
>
  <div class="gutter">
    {#if reorderable && !editing}
      <span
        class="handle"
        draggable="true"
        role="button"
        tabindex="-1"
        aria-label="Drag to reorder (or ⌥↑ / ⌥↓)"
        title="Drag to reorder · ⌥↑ ⌥↓"
        ondragstart={dragStart}
        ondragend={() => onDragEnd?.()}>⠿</span
      >
    {/if}
    <input
      type="checkbox"
      checked={selected}
      onchange={() => onToggleSelect?.()}
      aria-label="Select step {number}"
      tabindex="-1"
    />
    {#if !isNote}
      {#if status === 'running'}
        <button class="play stop" onclick={() => onStop?.()} title="Stop" aria-label="Stop step">■</button>
      {:else}
        <button
          class="play"
          onclick={() => onRun?.()}
          disabled={runDisabled || editing || isNew}
          title={runDisabled && runDisabledReason ? runDisabledReason : status === 'idle' ? 'Run' : 'Run again'}
          aria-label="Run step">▶</button
        >
      {/if}
    {/if}
  </div>

  <div class="main">
    <header>
      {#if editing}
        <input
          class="title-input"
          bind:this={titleEl}
          bind:value={draftTitle}
          onkeydown={titleKeydown}
          placeholder="Name this step"
          aria-label="Step title"
        />
      {:else}
        <span class="title">{step.title}</span>
        {#if tags.length}<span class="tags" title={tags.join(' · ')}>{tags.join(' · ')}</span>{/if}
        <span class="status">
          {#if destructive}<span class="badge warn" title="Destructive: confirm before run">⚠ confirm</span>{/if}
          {#if gated}<span class="gate-label">⏸ waiting on you</span>
          {:else if queued}<span class="muted-label">queued</span>{/if}
          {#if skipped}<span class="muted-label">skipped</span>{/if}
          {#if status === 'running'}
            <span class="running-label">running… {elapsedSec}s</span>
          {:else if run && !queued}
            <span class="exit" class:bad={status === 'failed'}>
              {#if run.endReason === 'stopped'}stopped{:else if run.endReason === 'timeout'}timed out{:else if run.endReason === 'error'}error{:else if run.endReason === 'shell-exited'}exit {run.exitCode} · shell ended{:else}exit {run.exitCode}{/if}
            </span>
            <span class="meta">· {fmtDuration(run.durationMs ?? 0)} · {fmtTime(run.startedAt)}</span>
          {/if}
        </span>
        <button class="link edit" onclick={() => onDuplicate?.()} disabled={locked} title="Duplicate this step below">
          Duplicate
        </button>
        <button
          class="link edit gear"
          onclick={() => (showSettings = !showSettings)}
          disabled={locked}
          title={locked ? 'Locked while running' : 'Step settings'}
          aria-expanded={showSettings}
          aria-label="Step settings">⚙</button
        >
        <button class="link edit" onclick={() => onEdit?.()} disabled={locked} title={locked ? 'Locked while running' : 'Edit'}>
          Edit
        </button>
      {/if}
    </header>

    {#if editing}
      {#if isNew}
        <label class="note-only">
          <input type="checkbox" checked={draftKind === 'note'} onchange={(e) => (draftKind = e.currentTarget.checked ? 'note' : 'command')} />
          Note only — no command, just text
        </label>
      {/if}
      <label class="field-label" for="body-{step.id}">{isNote ? 'Note' : 'Command'}</label>
      <textarea
        id="body-{step.id}"
        class:mono={!isNote}
        bind:this={bodyEl}
        bind:value={draftBody}
        onkeydown={bodyKeydown}
        rows={autoRows(draftBody)}
        spellcheck={isNote}
        placeholder={isNote ? 'Text for this note' : 'paste or type a command'}
      ></textarea>
      {#if !isNote}
        <p class="arg-tip">Tip: <code>{'{{'}disk{'}}'}</code> makes a blank you fill in before running, e.g. <code
            >gcloud compute disks delete {'{{'}disk{'}}'}</code
          ></p>
      {/if}
      <div class="editor-bar">
        {#if isNew}
          <span class="hint">⇥ title ⇄ {isNote ? 'note' : 'command'} · ⌘⏎ saves and adds another · esc cancels</span>
          <span class="spacer"></span>
          <button onclick={() => onCancel?.()}>Cancel</button>
          <button class="primary" onclick={() => save(false)} disabled={!canSave}>Add step</button>
        {:else}
          <span class="hint">⇥ title ⇄ {isNote ? 'note' : 'command'} · ⌘⏎ saves · esc cancels</span>
          <span class="spacer"></span>
          <button class="link" onclick={() => (showSettings = !showSettings)} aria-expanded={showSettings}>⚙ Settings</button>
          <button class="link danger" onclick={() => onDelete?.()}>Delete step</button>
          <button onclick={() => onCancel?.()}>Cancel</button>
          <button class="primary" onclick={() => save(false)} disabled={!canSave}>Save</button>
        {/if}
      </div>
    {:else if isNote}
      <p class="note-text">{step.note}</p>
    {:else}
      <pre class="command">{#each segments(step.command, argValues) as seg, i (i)}{#if 'text' in seg}{seg.text}{:else if seg.value !== null}<span
              class="arg-val"
              title={`{{${seg.arg}}}`}>{seg.value}</span
            >{:else}<span class="arg-missing" title="Fill in {seg.arg} below">{`{{${seg.arg}}}`}</span>{/if}{/each}</pre>
      {#if stepArgs.length > 0}
        <div class="args" role="group" aria-label="Arguments for step {number}">
          {#each stepArgs as n (n)}
            <label for="arg-{step.id}-{n}"><code>{n}</code></label>
            <input
              id="arg-{step.id}-{n}"
              bind:this={argInputs[n]}
              class:missing={argFocus === n && !(argValues[n] ?? '').trim()}
              value={argValues[n] ?? ''}
              oninput={(e) => onArgChange?.(n, e.currentTarget.value)}
              onkeydown={argKeydown}
              placeholder="value for {n} · ⏎ runs"
              disabled={locked}
              spellcheck="false"
              autocomplete="off"
            />
          {/each}
        </div>
      {/if}
      {#if gated}
        <div class="gate" role="group" aria-label="Destructive step gate">
          <span class="gate-hint">Run all paused before this destructive step. Approving resumes the rest automatically.</span>
          <span class="spacer"></span>
          <button onclick={() => onGate?.('skip')}>Skip</button>
          <button class="gate-stop" onclick={() => onGate?.('stop')}>Stop run</button>
          <button class="primary" onclick={() => onGate?.('approve')}>Approve</button>
        </div>
      {/if}
      {#if run && !gated}
        <button class="link output-toggle" onclick={() => (outputOverride = !outputOpen)} aria-expanded={outputOpen}>
          {outputOpen ? '▾' : '▸'} output
        </button>
        {#if outputOpen}
          <pre class="output" bind:this={outputEl}>{run.output}{#if status === 'running'}<span class="cursor">▌</span>{/if}{#if !run.output && status !== 'running'}<span class="empty">(no output)</span>{/if}</pre>
        {/if}
      {/if}
    {/if}
    {#if showSettings && !isNew}
      <StepSettings
        {step}
        {defaults}
        {runbookCwd}
        {locked}
        onChange={(f, v) => onSettingChange?.(f, v)}
        onChooseDir={() => onChooseDir?.() ?? Promise.resolve('')}
        onInsertBelow={() => {
          showSettings = false
          onInsertBelow?.()
        }}
        onClose={() => (showSettings = false)}
      />
    {/if}
  </div>
</article>

<style>
  .step {
    display: grid;
    grid-template-columns: 52px 1fr;
    border: 1px solid var(--border);
    border-left: 3px solid var(--border);
    border-radius: 6px;
    background: var(--surface);
    padding: 6px 10px 6px 6px;
    outline: none;
  }
  .step:focus-visible {
    box-shadow: 0 0 0 2px var(--focus);
  }
  .step.viewing {
    box-shadow: inset 0 0 0 1px var(--accent);
  }
  .step.selected {
    background: var(--selected-bg);
  }
  .step.running {
    border-left-color: var(--accent);
  }
  .step.passed {
    border-left-color: var(--ok);
  }
  .step.failed {
    border-left-color: var(--bad);
  }
  .step.destructive:not(.running):not(.passed):not(.failed) {
    border-left-color: var(--warn);
  }
  .step.editing {
    border-color: var(--accent);
    border-left-color: var(--accent);
  }
  .step.note {
    background: var(--note-bg);
    border-style: dashed;
    border-left-style: solid;
  }

  .gutter {
    display: flex;
    align-items: flex-start;
    gap: 4px;
    padding-top: 1px;
  }
  .gutter input {
    margin: 4px 2px 0;
  }
  .gutter {
    position: relative;
  }
  .handle {
    position: absolute;
    left: -12px;
    top: 2px;
    cursor: grab;
    color: var(--muted);
    visibility: hidden;
    user-select: none;
  }
  .step:hover .handle,
  .step:focus-within .handle {
    visibility: visible;
  }
  .step.dragging {
    opacity: 0.5;
  }
  .title {
    flex: 0 1 auto;
    min-width: 6ch;
  }
  /* Tags give way before the title does. */
  .tags {
    flex: 0 100 auto;
    min-width: 0;
    font-size: 11px;
    color: var(--muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .note-only {
    display: flex;
    gap: 6px;
    align-items: center;
    font-size: 12px;
    margin-top: 6px;
  }
  .gear {
    padding: 0 2px;
    font-size: 13px;
  }
  .play {
    width: 22px;
    height: 20px;
    padding: 0;
    font-size: 10px;
    line-height: 1;
  }
  .play.stop {
    color: var(--bad);
  }

  .main {
    min-width: 0;
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 6px;
    min-height: 22px;
  }
  /* Only the title (then the tags) give way when the row is narrow. */
  header > .status,
  header > button {
    flex-shrink: 0;
  }
  .title {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .status {
    margin-left: auto;
    display: flex;
    gap: 6px;
    align-items: baseline;
    white-space: nowrap;
    font-size: 12px;
  }
  .badge.warn {
    color: var(--warn);
    font-weight: 600;
  }
  .running-label {
    color: var(--accent);
  }
  .muted-label {
    color: var(--muted);
  }
  .gate-label {
    color: var(--warn);
    font-weight: 600;
  }
  .step.gated {
    border-color: var(--warn);
    border-left-color: var(--warn);
    box-shadow: 0 0 0 1px var(--warn);
  }
  .gate {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 6px;
    padding-top: 6px;
    border-top: 1px dashed var(--border);
  }
  .gate-hint {
    font-size: 11px;
    color: var(--muted);
  }
  .gate-stop {
    color: var(--bad);
  }
  .exit {
    color: var(--ok);
    font-family: var(--mono);
  }
  .exit.bad {
    color: var(--bad);
  }
  .meta {
    color: var(--muted);
  }
  .edit {
    visibility: hidden;
  }
  .step:hover .edit,
  .step:focus-within .edit {
    visibility: visible;
  }

  .command,
  .output {
    font-family: var(--mono);
    font-size: 12px;
    margin: 3px 0 0;
    white-space: pre-wrap;
    word-break: break-all;
  }
  /* Commands sit in their own box so they stand apart from titles and output. */
  .command {
    color: var(--command);
    background: var(--command-bg);
    border: 1px solid var(--command-border);
    border-radius: 4px;
    padding: 6px 9px;
    margin-top: 5px;
    line-height: 1.5;
  }
  .arg-val {
    color: var(--ok);
    border-bottom: 1px dotted currentColor;
  }
  .arg-missing {
    color: var(--warn);
    background: color-mix(in srgb, var(--warn) 15%, transparent);
    border-radius: 3px;
    padding: 0 2px;
  }
  .args {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: 4px 8px;
    align-items: center;
    margin-top: 6px;
    font-size: 12px;
  }
  .args code {
    font-family: var(--mono);
    color: var(--accent);
  }
  .args input {
    font-family: var(--mono);
    font-size: 12px;
    width: 100%;
  }
  .args input.missing {
    border-color: var(--warn);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--warn) 35%, transparent);
  }
  .arg-tip {
    margin: 3px 0 0;
    font-size: 11px;
    color: var(--muted);
  }
  .arg-tip code {
    font-family: var(--mono);
  }
  .output-toggle {
    font-size: 11px;
    margin-top: 3px;
    padding: 0;
  }
  .output {
    background: var(--term-bg);
    color: var(--term-fg);
    border-radius: 4px;
    padding: 6px 8px;
    max-height: 240px;
    overflow: auto;
  }
  .step.failed .output {
    box-shadow: inset 2px 0 0 var(--bad);
  }
  .cursor {
    color: var(--accent);
  }
  .empty {
    color: var(--muted);
  }

  .note-text {
    margin: 2px 0 0;
    color: var(--muted-strong);
    font-style: italic;
  }

  .title-input {
    flex: 1;
    font: inherit;
    font-weight: 600;
  }
  .field-label {
    display: block;
    font-size: 11px;
    color: var(--muted);
    margin: 6px 0 2px;
  }
  textarea {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    font: inherit;
  }
  textarea.mono {
    font-family: var(--mono);
    font-size: 12px;
  }
  .editor-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 6px;
  }
  .editor-bar button {
    white-space: nowrap;
  }
  .hint {
    font-size: 11px;
    color: var(--muted);
  }
  .spacer {
    flex: 1;
  }
</style>
