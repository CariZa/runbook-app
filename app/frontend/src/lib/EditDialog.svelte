<script lang="ts">
  // "Edit steps…": the whole runbook as text. Saving overwrites runbook.md with what's in
  // the box; text that doesn't parse is refused and the dialog stays open with the reason.
  interface Props {
    name: string
    source: string // the runbook as it is now
    onSave: (text: string) => Promise<void> // rejects with the reason when the text is refused
    onCancel: () => void
  }
  let { name, source, onSave, onCancel }: Props = $props()

  // svelte-ignore state_referenced_locally
  let text = $state(source)
  let error = $state('')
  let saving = $state(false)
  let dialogEl = $state<HTMLDialogElement>()
  let textEl = $state<HTMLTextAreaElement>()

  $effect(() => {
    dialogEl?.showModal()
    textEl?.focus()
    textEl?.select() // so a paste replaces everything
  })

  const changed = $derived(text !== source)

  async function save() {
    if (saving || !changed) return
    saving = true
    error = ''
    try {
      await onSave(text)
    } catch (err) {
      error = `${err}`
    } finally {
      saving = false
    }
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
    } else if (e.key === 'Enter' && e.metaKey) {
      e.preventDefault()
      save()
    }
  }}
>
  <h2>Edit steps · {name}</h2>
  <textarea bind:this={textEl} bind:value={text} spellcheck="false" aria-label="Runbook source"></textarea>
  {#if error}<p class="error">Not saved: {error}</p>{/if}
  <footer>
    <span class="hint">this is runbook.md · saving replaces the whole runbook · unchanged steps keep their last output</span>
    <button onclick={onCancel}>Cancel</button>
    <button class="primary" onclick={save} disabled={saving || !changed} title="⌘⏎">Replace runbook</button>
  </footer>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(860px, 94vw);
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
  textarea {
    display: block;
    width: 100%;
    height: min(60vh, 520px);
    font-family: var(--mono);
    font-size: 12px;
    resize: none;
    white-space: pre;
    overflow: auto;
  }
  .error {
    margin: 8px 0 0;
    color: var(--bad);
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
  }
  .hint {
    flex: 1;
    color: var(--muted);
    font-size: 11px;
  }
</style>
