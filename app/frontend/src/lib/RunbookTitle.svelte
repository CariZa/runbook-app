<script lang="ts">
  import { tick } from 'svelte'

  // Runbook title in the window chrome (2b): click selects, double-click edits,
  // ⏎ saves, esc cancels.
  interface Props {
    name: string
    locked?: boolean
    onRename?: (name: string) => void
  }
  let { name, locked = false, onRename }: Props = $props()

  let selected = $state(false)
  let editing = $state(false)
  let draft = $state('')
  let inputEl = $state<HTMLInputElement>()

  async function startEdit() {
    if (locked) return
    draft = name
    editing = true
    await tick()
    inputEl?.select()
  }

  function commit() {
    const next = draft.trim()
    if (next && next !== name) onRename?.(next)
    editing = false
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      commit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      editing = false
    }
  }
</script>

{#if editing}
  <span class="edit">
    <input bind:this={inputEl} bind:value={draft} onkeydown={keydown} aria-label="Runbook name" />
    <button class="primary" onmousedown={(e) => e.preventDefault()} onclick={commit}>Save</button>
    <span class="hint">esc cancels</span>
  </span>
{:else}
  <button
    class="name"
    class:selected
    onclick={() => (selected = true)}
    ondblclick={startEdit}
    onblur={() => (selected = false)}
    title={locked ? 'Locked while running' : 'Double-click to rename'}
  >
    {name}
  </button>
{/if}

<style>
  .name {
    font: inherit;
    font-weight: 600;
    font-size: 14px;
    border: 1px solid transparent;
    background: none;
    padding: 1px 6px;
    border-radius: 4px;
    cursor: default;
  }
  .name.selected {
    border-color: var(--accent);
    background: var(--selected-bg);
  }
  .edit {
    display: inline-flex;
    gap: 6px;
    align-items: center;
  }
  input {
    font: inherit;
    font-weight: 600;
    font-size: 14px;
    width: 22ch;
  }
  .hint {
    font-size: 11px;
    color: var(--muted);
  }
</style>
