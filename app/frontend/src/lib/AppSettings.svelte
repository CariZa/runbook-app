<script lang="ts">
  import { tick } from 'svelte'
  import { forgetNotionToken, notionTokenHint, saveNotionToken } from './library'

  // App-wide settings, as opposed to the per-runbook ⚙ Settings. Today it holds one thing:
  // the Notion integration token used by Import from Notion.
  interface Props {
    onClose: () => void
    onChange?: () => void
  }
  let { onClose, onChange }: Props = $props()

  let hint = $state('')
  let editing = $state(false)
  let token = $state('')
  let busy = $state(false)
  let error = $state('')
  let saved = $state('')
  let dialogEl = $state<HTMLDialogElement>()
  let tokenEl = $state<HTMLInputElement>()

  $effect(() => {
    dialogEl?.showModal()
    notionTokenHint().then(async (h) => {
      hint = h
      editing = h === ''
      await tick()
      tokenEl?.focus()
    })
  })

  async function save() {
    error = ''
    saved = ''
    busy = true
    try {
      await saveNotionToken(token)
      token = ''
      hint = await notionTokenHint()
      editing = false
      saved = 'Token saved.'
      onChange?.()
    } catch (err) {
      error = `${err}`.replace(/^Error:\s*/, '')
    } finally {
      busy = false
    }
  }

  async function remove() {
    error = ''
    saved = ''
    try {
      await forgetNotionToken()
    } catch (err) {
      error = `${err}`.replace(/^Error:\s*/, '')
    }
    hint = ''
    editing = true
    onChange?.()
    await tick()
    tokenEl?.focus()
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
  <h2>Settings</h2>

  <section>
    <h3>Notion</h3>
    <p class="lead">
      Used by <strong>Import from Notion…</strong> to read a page you've shared with your integration. The app only ever
      reads from Notion.
    </p>

    {#if editing}
      <label class="field">
        Integration token
        <input
          bind:this={tokenEl}
          bind:value={token}
          type="password"
          autocomplete="off"
          spellcheck="false"
          placeholder="ntn_…"
          onkeydown={(e) => e.key === 'Enter' && token.trim() && save()}
        />
      </label>
      <ol class="setup">
        <li>Create an internal integration at <code>notion.so/my-integrations</code> and copy its token.</li>
        <li>Open the page you want in Notion, then <strong>Share</strong> it with that integration.</li>
      </ol>
      <div class="row">
        <button class="primary" onclick={save} disabled={busy || !token.trim()}>{busy ? 'Saving…' : 'Save token'}</button>
        {#if hint}<button onclick={() => ((editing = false), (token = ''))}>Cancel</button>{/if}
      </div>
    {:else}
      <div class="row">
        <input class="mono" value={hint} readonly aria-label="Saved Notion token (hidden)" />
        <button onclick={() => ((editing = true), (token = ''))}>Replace…</button>
        <button onclick={remove}>Remove</button>
      </div>
    {/if}

    {#if error}<p class="error">{error}</p>{/if}
    {#if saved}<p class="ok">{saved}</p>{/if}
    <p class="hint">
      Kept in your login Keychain, so it never lands in a runbook file, an export or a backup of <code>~/runbooks</code>.
      Anything running as you can still read it, as with any file in your home folder.
    </p>
  </section>

  <div class="actions">
    <span class="spacer"></span>
    <button class="primary" onclick={onClose}>Done</button>
  </div>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(560px, 94vw);
    box-shadow: 0 10px 30px rgba(0, 0, 0, 0.25);
    font-size: 12px;
  }
  dialog::backdrop {
    background: rgba(0, 0, 0, 0.35);
  }
  h2 {
    font-size: 14px;
    margin: 0 0 12px;
  }
  h3 {
    font-size: 12px;
    margin: 0 0 4px;
  }
  .lead {
    margin: 0 0 8px;
    color: var(--muted-strong);
  }
  .setup {
    margin: 6px 0 8px;
    padding-left: 18px;
    color: var(--muted);
  }
  .setup li {
    margin-bottom: 3px;
  }
  code,
  .mono {
    font-family: var(--mono);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 3px;
    color: var(--muted);
  }
  .field input {
    font-family: var(--mono);
  }
  .row,
  .actions {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .row .mono {
    flex: 1;
    color: var(--muted-strong);
  }
  .actions {
    margin-top: 14px;
  }
  .spacer {
    flex: 1;
  }
  .error {
    color: var(--bad);
    margin: 8px 0 0;
  }
  .ok {
    color: var(--good, var(--accent));
    margin: 8px 0 0;
  }
  .hint {
    color: var(--muted);
    margin: 8px 0 0;
  }
</style>
