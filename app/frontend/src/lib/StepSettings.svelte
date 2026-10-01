<script lang="ts">
  import type { Runbook, SettingField, Step } from './types'

  // Per-step settings (3c): each field inherits the runbook default until overridden.
  // Changes apply immediately (autosave). Setting a field back to its inherited value
  // clears the override, so it isn't written to the file.
  interface Props {
    step: Step
    defaults: Runbook['defaults']
    runbookCwd: string
    locked: boolean
    onChange: (field: SettingField, value: boolean | number | string | undefined) => void
    onChooseDir: () => Promise<string>
    onInsertBelow: () => void
    onClose: () => void
  }
  let { step, defaults, runbookCwd, locked, onChange, onChooseDir, onInsertBelow, onClose }: Props = $props()

  const inherited = $derived({
    destructive: defaults.destructive,
    continueOnFail: defaults.continueOnFail,
    skipInRunAll: false,
    recordOutput: true,
  })

  type BoolField = keyof typeof inherited
  const boolRows: { field: BoolField; label: string; hint: string }[] = [
    { field: 'destructive', label: 'Destructive — confirm before run', hint: 'Run all pauses before it; its own ▶ asks first' },
    { field: 'continueOnFail', label: 'Keep going if this step fails', hint: 'Default is to stop the run' },
    { field: 'skipInRunAll', label: 'Skip in Run all', hint: 'Still runnable with its own ▶' },
    { field: 'recordOutput', label: 'Record output to disk', hint: 'Off: output shows live but never reaches runs.log or the cache' },
  ]

  function value(field: BoolField): boolean {
    return step[field] ?? inherited[field]
  }
  function overridden(field: BoolField): boolean {
    return step[field] !== undefined && step[field] !== inherited[field]
  }
  function toggle(field: BoolField, checked: boolean) {
    onChange(field, checked === inherited[field] ? undefined : checked)
  }

  const timeout = $derived(step.timeoutSec ?? defaults.timeoutSec)
  const timeoutOverridden = $derived(step.timeoutSec !== undefined && step.timeoutSec !== defaults.timeoutSec)
  function setTimeoutSec(v: number) {
    if (!Number.isFinite(v) || v < 0) return
    v = Math.floor(v)
    onChange('timeoutSec', v === defaults.timeoutSec ? undefined : v)
  }

  async function chooseDir() {
    const dir = await onChooseDir()
    if (dir) onChange('cwd', dir)
  }

  function fmtTimeout(n: number): string {
    return n === 0 ? 'no timeout' : `${n}s`
  }
</script>

<section class="settings" aria-label="Step settings">
  <header>
    <span class="head">Step settings</span>
    <span class="muted">inherits runbook defaults until you change a field</span>
    <span class="spacer"></span>
    <button class="link" onclick={onClose}>Done</button>
  </header>

  <fieldset disabled={locked}>
    {#each boolRows as r (r.field)}
      <div class="row" class:set={overridden(r.field)}>
        <label title={r.hint}>
          <input type="checkbox" checked={value(r.field)} onchange={(e) => toggle(r.field, e.currentTarget.checked)} />
          {r.label}
        </label>
        {#if overridden(r.field)}
          <button class="link small" onclick={() => onChange(r.field, undefined)}>overridden · reset</button>
        {:else}
          <span class="inh">inherited</span>
        {/if}
      </div>
    {/each}

    <div class="row" class:set={!!step.cwd}>
      <span class="lbl">Folder</span>
      <code class="val">{step.cwd || `inherit (${runbookCwd})`}</code>
      <button class="link small" onclick={chooseDir}>Choose…</button>
      {#if step.cwd}
        <button class="link small" onclick={() => onChange('cwd', undefined)}>reset</button>
      {/if}
    </div>

    <div class="row" class:set={timeoutOverridden}>
      <span class="lbl">Timeout</span>
      <input
        class="secs"
        type="number"
        min="1"
        value={timeout === 0 ? '' : timeout}
        placeholder="none"
        disabled={timeout === 0}
        onchange={(e) => setTimeoutSec(parseInt(e.currentTarget.value, 10))}
        aria-label="Timeout in seconds"
      />
      <span class="muted">s</span>
      <label class="none">
        <input
          type="checkbox"
          checked={timeout === 0}
          onchange={(e) => setTimeoutSec(e.currentTarget.checked ? 0 : defaults.timeoutSec || 60)}
        /> no timeout
      </label>
      {#if timeoutOverridden}
        <button class="link small" onclick={() => onChange('timeoutSec', undefined)}>reset to {fmtTimeout(defaults.timeoutSec)}</button>
      {:else}
        <span class="inh">inherited</span>
      {/if}
    </div>
  </fieldset>

  <footer>
    <button class="link" onclick={onInsertBelow} disabled={locked}>+ Insert step below</button>
  </footer>
</section>

<style>
  .settings {
    margin-top: 6px;
    padding: 6px 8px;
    border: 1px solid var(--border);
    border-radius: 4px;
    background: var(--bg);
    font-size: 12px;
  }
  header,
  footer,
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  header {
    margin-bottom: 4px;
  }
  .head {
    font-weight: 600;
    white-space: nowrap;
  }
  header .muted {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .row {
    min-height: 22px;
  }
  .row label {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .row.set label,
  .row.set .lbl {
    font-weight: 600;
  }
  .lbl {
    min-width: 52px;
  }
  .val {
    font-family: var(--mono);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 50%;
  }
  .secs {
    width: 64px;
  }
  .none {
    font-weight: normal !important;
  }
  .inh,
  .muted {
    color: var(--muted);
    font-size: 11px;
  }
  .row > :last-child {
    margin-left: auto;
  }
  .small {
    font-size: 11px;
  }
  .spacer {
    flex: 1;
  }
  footer {
    margin-top: 4px;
    padding-top: 4px;
    border-top: 1px dashed var(--border);
  }
</style>
