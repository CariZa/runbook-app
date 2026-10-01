<script lang="ts">
  import type { Snippet } from 'svelte'

  // Generic modal confirm: delete step, and running a destructive step (3b, left card).
  interface Props {
    title: string
    confirmLabel?: string
    danger?: boolean
    focusCancel?: boolean // start on Cancel so a stray ⏎ does nothing
    children?: Snippet
    onConfirm: () => void
    onCancel: () => void
  }
  let { title, confirmLabel = 'Confirm', danger = false, focusCancel = false, children, onConfirm, onCancel }: Props = $props()

  let dialogEl = $state<HTMLDialogElement>()
  let confirmEl = $state<HTMLButtonElement>()
  let cancelEl = $state<HTMLButtonElement>()

  $effect(() => {
    dialogEl?.showModal()
    ;(focusCancel ? cancelEl : confirmEl)?.focus()
  })
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
      e.stopPropagation()
      onCancel()
    }
  }}
>
  <h2>{title}</h2>
  {#if children}<div class="body">{@render children()}</div>{/if}
  <div class="actions">
    <button bind:this={cancelEl} onclick={onCancel}>Cancel</button>
    <button bind:this={confirmEl} class="primary" class:danger onclick={onConfirm}>{confirmLabel}</button>
  </div>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(460px, 90vw);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
  }
  dialog::backdrop {
    background: rgba(0, 0, 0, 0.35);
  }
  h2 {
    font-size: 14px;
    margin: 0 0 8px;
  }
  .body {
    font-size: 12px;
    margin-bottom: 12px;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }
  button.danger {
    background: var(--bad);
    border-color: var(--bad);
  }
</style>
