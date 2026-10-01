<script lang="ts">
  import { exportMarkdown, saveExport } from './library'
  import { loadExportPrefs, saveExportPrefs } from './exportPrefs'
  import type { Runbook } from './types'

  // Export sheet (6a): live preview of the exact Markdown, toggles above it. Copy is the
  // primary action (paste into Claude or Notion); Save .md is secondary.
  interface Props {
    name: string // runbook directory
    doc: Runbook
    selectedIds: string[]
    onCopy: (markdown: string, steps: number) => void
    onSaved: (path: string) => void
    onClose: () => void
  }
  let { name, doc, selectedIds, onCopy, onSaved, onClose }: Props = $props()

  const prefs = loadExportPrefs()
  let selectedOnly = $state(false)
  let outputs = $state(prefs.outputs)
  let settings = $state(prefs.settings)
  let timestamps = $state(prefs.timestamps)
  let markdown = $state('')
  let error = $state('')
  let dialogEl = $state<HTMLDialogElement>()

  $effect(() => {
    selectedOnly = selectedIds.length > 0 // the selection pre-fills "selected only" (6b)
    dialogEl?.showModal()
  })

  const stepIds = $derived(selectedOnly ? selectedIds : [])
  const count = $derived(selectedOnly ? selectedIds.length : doc.steps.length)

  $effect(() => {
    const opts = { stepIds, outputs, settings, timestamps }
    saveExportPrefs({ outputs, settings, timestamps })
    exportMarkdown(name, $state.snapshot(doc) as Runbook, opts)
      .then((md) => {
        markdown = md
        error = ''
      })
      .catch((err) => (error = `${err}`))
  })

  async function save() {
    try {
      const path = await saveExport(name, markdown, doc.running.audit)
      if (path) onSaved(path)
    } catch (err) {
      error = `${err}`
    }
  }
</script>

<dialog
  bind:this={dialogEl}
  oncancel={(e) => {
    e.preventDefault()
    onClose()
  }}
  onkeydown={(e) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onClose()
    }
  }}
>
  <header>
    <h2>Export · {doc.name}</h2>
    <span class="kind">markdown</span>
  </header>

  <div class="toggles">
    <label class:off={selectedIds.length === 0}>
      <input type="checkbox" bind:checked={selectedOnly} disabled={selectedIds.length === 0} />
      Selected steps only ({selectedIds.length} of {doc.steps.length})
    </label>
    <label><input type="checkbox" bind:checked={outputs} /> Last outputs</label>
    <label><input type="checkbox" bind:checked={settings} /> Step settings</label>
    <label><input type="checkbox" bind:checked={timestamps} disabled={!outputs} /> Timestamps</label>
  </div>

  <pre class="preview">{markdown}</pre>
  {#if error}<p class="error">{error}</p>{/if}
  <p class="hint">plain headings + fenced blocks — pastes back in through Paste steps · outputs are the redacted copies</p>

  <footer>
    <code class="where">~/runbooks/{name}/export.md</code>
    <button onclick={onClose}>Close</button>
    <button onclick={save} disabled={!markdown}>Save .md</button>
    <button class="primary" onclick={() => onCopy(markdown, count)} disabled={!markdown}>Copy</button>
  </footer>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(720px, 94vw);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
    font-size: 12px;
  }
  dialog::backdrop {
    background: rgba(0, 0, 0, 0.35);
  }
  header {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }
  h2 {
    font-size: 14px;
    margin: 0 0 10px;
  }
  .kind {
    color: var(--muted);
    font-family: var(--mono);
    font-size: 11px;
  }
  .toggles {
    display: flex;
    flex-wrap: wrap;
    gap: 6px 16px;
    margin-bottom: 8px;
  }
  .toggles label {
    display: flex;
    align-items: center;
    gap: 5px;
  }
  .toggles label.off {
    color: var(--muted);
  }
  .preview {
    font-family: var(--mono);
    font-size: 12px;
    background: var(--term-bg);
    color: var(--term-fg);
    border-radius: 4px;
    padding: 8px 10px;
    margin: 0;
    height: min(52vh, 460px);
    overflow: auto;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .hint {
    color: var(--muted);
    font-size: 11px;
    margin: 6px 0 0;
  }
  .error {
    color: var(--bad);
    margin: 6px 0 0;
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
  .where {
    flex: 1;
    font-family: var(--mono);
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
