<script lang="ts">
  import { untrack } from 'svelte'
  import type { Runbook, SettingsResult, Step } from './types'

  // Runbook settings (5c): name, path, step defaults with override counts, run toggles.
  interface Props {
    doc: Runbook
    folders: string[]
    folder: string // the runbook's current folder ("" = top level)
    location: string
    onSave: (r: SettingsResult) => void
    onCancel: () => void
    onChooseDir: (current: string) => Promise<string>
    onDelete: () => void
  }
  let { doc, folders, folder: currentFolder, location, onSave, onCancel, onChooseDir, onDelete }: Props = $props()

  let name = $state('')
  let folder = $state('')
  let cwd = $state('')
  let defaults = $state<Runbook['defaults']>({ destructive: false, continueOnFail: false, timeoutSec: 60 })
  let running = $state<Runbook['running']>({ pauseAtDestructive: true, audit: true })
  let resetOverrides = $state(false)
  let dialogEl = $state<HTMLDialogElement>()

  $effect(() => {
    untrack(() => {
      name = doc.name
      folder = currentFolder
      cwd = doc.cwd
      defaults = { ...doc.defaults }
      running = { ...doc.running }
    })
    dialogEl?.showModal()
  })

  // "N steps override": steps whose explicit value differs from the (draft) default.
  function overrides(pick: (s: Step) => boolean | number | undefined, def: boolean | number): number {
    if (resetOverrides) return 0
    return doc.steps.filter((s) => s.kind === 'command' && pick(s) !== undefined && pick(s) !== def).length
  }
  const counts = $derived({
    destructive: overrides((s) => s.destructive, defaults.destructive),
    continueOnFail: overrides((s) => s.continueOnFail, defaults.continueOnFail),
    timeoutSec: overrides((s) => s.timeoutSec, defaults.timeoutSec),
  })
  const anyOverrides = $derived(
    doc.steps.some((s) => s.destructive !== undefined || s.continueOnFail !== undefined || s.timeoutSec !== undefined),
  )
  const validName = $derived(/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name))

  function plural(n: number): string {
    return n === 0 ? 'no overrides' : n === 1 ? '1 step overrides' : `${n} steps override`
  }

  async function pickDir() {
    const dir = await onChooseDir(cwd)
    if (dir) cwd = dir
  }

  function save() {
    if (validName) onSave({ name, cwd, defaults: { ...defaults }, running: { ...running }, resetOverrides, folder })
  }
</script>

<dialog
  bind:this={dialogEl}
  oncancel={(e) => {
    e.preventDefault()
    onCancel()
  }}
  onkeydown={(e) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      onCancel()
    }
  }}
>
  <h2>{doc.name} · settings</h2>

  <div class="grid">
    <label for="rs-name">Name</label>
    <input id="rs-name" class="mono" bind:value={name} aria-invalid={!validName} />

    <label for="rs-folder">Folder</label>
    <select id="rs-folder" bind:value={folder}>
      <option value="">(top level)</option>
      {#each folders as f (f)}<option value={f}>{f}</option>{/each}
    </select>

    <span class="lbl">Path <span class="sub">where commands run</span></span>
    <div class="path">
      <code>{cwd}</code>
      <button class="link" onclick={pickDir}>Change</button>
    </div>
  </div>

  <h3>Defaults for steps</h3>
  <div class="rows">
    <label class="row">
      <input type="checkbox" bind:checked={defaults.destructive} />
      <span>Destructive — confirm before run</span>
      <span class="count">{plural(counts.destructive)}</span>
    </label>
    <label class="row">
      <input type="checkbox" bind:checked={defaults.continueOnFail} />
      <span>Keep going if a step fails</span>
      <span class="count">{plural(counts.continueOnFail)}</span>
    </label>
    <div class="row">
      <span class="tlabel">Timeout</span>
      <input
        class="secs"
        type="number"
        min="1"
        value={defaults.timeoutSec || ''}
        placeholder="none"
        disabled={defaults.timeoutSec === 0}
        onchange={(e) => {
          const v = parseInt(e.currentTarget.value, 10)
          if (Number.isFinite(v) && v > 0) defaults.timeoutSec = v
        }}
        aria-label="Default timeout in seconds"
      />
      <span class="sub">s</span>
      <label class="inline"
        ><input
          type="checkbox"
          checked={defaults.timeoutSec === 0}
          onchange={(e) => (defaults.timeoutSec = e.currentTarget.checked ? 0 : 60)}
        /> no timeout</label
      >
      <span class="count">{plural(counts.timeoutSec)}</span>
    </div>
  </div>

  <h3>Running</h3>
  <div class="rows">
    <label class="row"><input type="checkbox" bind:checked={running.pauseAtDestructive} /> Pause at destructive steps during Run all</label>
    <label class="row"><input type="checkbox" bind:checked={running.audit} /> Write every run to the audit log</label>
  </div>

  <div class="danger-zone">
    <button class="link danger" onclick={onDelete}>Delete runbook…</button>
    <span class="sub">moves it to the Trash, with its versions, outputs and audit log</span>
  </div>

  <div class="actions">
    <code class="where">{location}</code>
    <button
      class="link danger"
      onclick={() => (resetOverrides = !resetOverrides)}
      disabled={!anyOverrides}
      title="Clear every step's destructive / keep-going / timeout override"
      >{resetOverrides ? 'Undo reset' : 'Reset overrides'}</button
    >
    <button onclick={onCancel}>Cancel</button>
    <button class="primary" onclick={save} disabled={!validName}>Save</button>
  </div>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(520px, 92vw);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
    font-size: 12px;
  }
  dialog::backdrop {
    background: rgba(0, 0, 0, 0.35);
  }
  h2 {
    font-size: 14px;
    margin: 0 0 10px;
  }
  h3 {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
    margin: 12px 0 4px;
  }
  .grid {
    display: grid;
    grid-template-columns: 120px 1fr;
    gap: 6px 10px;
    align-items: center;
  }
  .mono,
  code {
    font-family: var(--mono);
  }
  .path {
    display: flex;
    gap: 8px;
    align-items: baseline;
    min-width: 0;
  }
  .path code {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .sub {
    color: var(--muted);
    font-size: 11px;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .inline {
    display: flex;
    align-items: center;
    gap: 4px;
  }
  .tlabel {
    min-width: 52px;
  }
  .secs {
    width: 64px;
  }
  .count {
    margin-left: auto;
    color: var(--muted);
    font-size: 11px;
  }
  .danger-zone {
    display: flex;
    align-items: baseline;
    gap: 8px;
    margin-top: 14px;
    padding-top: 8px;
    border-top: 1px dashed var(--border);
  }
  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 14px;
  }
  .where {
    flex: 1;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  input[aria-invalid='true'] {
    border-color: var(--bad);
  }
</style>
