<script lang="ts">
  import { tick } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { fetchNotionPage, hasNotionToken, type NotionPage } from './library'
  import type { Step } from './types'

  // Import from Notion (read-only): paste a page link, see what it found, then create a
  // runbook from the steps you keep. Nothing is written back to Notion.
  interface Props {
    folders: string[]
    folder: string
    onCreate: (ref: string, steps: Step[], pageId: string, pageUrl: string) => void
    onAddHere: (steps: Step[]) => void
    canAddHere: boolean
    onOpenSettings: () => void
    onCancel: () => void
  }
  let { folders, folder: startFolder, onCreate, onAddHere, canAddHere, onOpenSettings, onCancel }: Props = $props()

  let connected = $state(false)
  let link = $state('')
  let page = $state<NotionPage | null>(null)
  let busy = $state(false)
  let error = $state('')
  let name = $state('')
  let folder = $state('')
  let destination = $state<'new' | 'here'>('new')
  const excluded = new SvelteSet<number>()
  let dialogEl = $state<HTMLDialogElement>()
  let firstEl = $state<HTMLInputElement>()

  $effect(() => {
    folder = startFolder
    dialogEl?.showModal()
    hasNotionToken().then(async (yes) => {
      connected = yes
      await tick()
      firstEl?.focus()
    })
  })

  const kept = $derived((page?.steps ?? []).filter((_, i) => !excluded.has(i)))
  const validName = $derived(/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name))

  async function fetchPage() {
    error = ''
    busy = true
    page = null
    try {
      const p = await fetchNotionPage(link)
      page = p
      name = p.suggest
      excluded.clear()
      if ((p.steps ?? []).length === 0) error = 'That page has no headings or code blocks to make steps from.'
    } catch (err) {
      error = `${err}`.replace(/^Error:\s*/, '')
    } finally {
      busy = false
    }
  }

  function go() {
    if (kept.length === 0) return
    if (destination === 'here') onAddHere($state.snapshot(kept) as Step[])
    else if (validName) onCreate(folder ? `${folder}/${name}` : name, $state.snapshot(kept) as Step[], page!.pageId, page!.url)
  }

  const firstLine = (s: string) => s.split('\n')[0]
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
  <h2>Import from Notion</h2>

  {#if !connected}
    <p class="lead">
      Notion isn't connected yet. Add an integration token in <strong>Settings</strong>, then come back here and paste a
      page link.
    </p>
    <div class="actions">
      <span class="hint">read-only · nothing is written back to Notion</span>
      <button onclick={onCancel}>Cancel</button>
      <button class="primary" onclick={onOpenSettings}>Open Settings…</button>
    </div>
  {:else}
    <label class="field">
      Notion page link
      <input
        bind:this={firstEl}
        bind:value={link}
        placeholder="https://www.notion.so/…"
        onkeydown={(e) => e.key === 'Enter' && fetchPage()}
      />
    </label>
    <div class="row">
      <button onclick={fetchPage} disabled={busy || !link.trim()}>{busy ? 'Reading…' : 'Read page'}</button>
      <span class="spacer"></span>
      <button class="link" onclick={onOpenSettings}>Notion settings…</button>
    </div>

    {#if error}<p class="error">{error}</p>{/if}

    {#if page}
      <div class="found">
        <div class="label">
          <strong>{page.title || 'Untitled page'}</strong> · {(page.steps ?? []).length} step{(page.steps ?? []).length === 1
            ? ''
            : 's'}
        </div>
        <ul>
          {#each page.steps ?? [] as s, i (i)}
            <li class:off={excluded.has(i)}>
              <label>
                <input
                  type="checkbox"
                  checked={!excluded.has(i)}
                  onchange={() => (excluded.has(i) ? excluded.delete(i) : excluded.add(i))}
                />
                <span class="kind" class:note={s.kind === 'note'}>{s.kind}</span>
                <span class="title">{s.title}</span>
              </label>
              {#if s.kind === 'command'}<code>{firstLine(s.command)}{s.command.includes('\n') ? ' …' : ''}</code>{/if}
            </li>
          {/each}
        </ul>
        {#if (page.skipped ?? []).length || (page.dropped ?? []).length}
          <p class="dropped">Ignored: {[...(page.skipped ?? []), ...(page.dropped ?? [])].join(', ')}</p>
        {/if}
      </div>

      <div class="dest">
        <label><input type="radio" bind:group={destination} value="new" /> New runbook</label>
        {#if destination === 'new'}
          <input class="mono" bind:value={name} aria-label="New runbook name" aria-invalid={!validName} />
          <select bind:value={folder} aria-label="Folder">
            <option value="">(top level)</option>
            {#each folders as f (f)}<option value={f}>{f}</option>{/each}
          </select>
        {/if}
      </div>
      {#if canAddHere}
        <label class="dest"><input type="radio" bind:group={destination} value="here" /> Add these steps to the open runbook</label>
      {/if}

      <div class="actions">
        <span class="hint">read-only · nothing is written back to Notion</span>
        <button onclick={onCancel}>Cancel</button>
        <button class="primary" onclick={go} disabled={kept.length === 0 || (destination === 'new' && !validName)}>
          {destination === 'here' ? `Add ${kept.length} step${kept.length === 1 ? '' : 's'}` : 'Create runbook'}
        </button>
      </div>
    {:else}
      <div class="actions">
        <span class="hint">the page must be shared with your integration</span>
        <button onclick={onCancel}>Cancel</button>
      </div>
    {/if}
  {/if}
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(620px, 94vw);
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
  .lead {
    margin: 0 0 6px;
    color: var(--muted-strong);
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
    margin-bottom: 8px;
  }
  .field input {
    font-family: var(--mono);
  }
  .row,
  .actions,
  .dest {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .dest {
    margin-top: 8px;
  }
  .dest .mono {
    flex: 1;
  }
  .actions {
    margin-top: 12px;
  }
  .spacer,
  .hint {
    flex: 1;
  }
  .hint {
    color: var(--muted);
    font-size: 11px;
  }
  .error {
    color: var(--bad);
    margin: 8px 0 0;
  }
  .found {
    margin-top: 10px;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 6px 8px;
    max-height: min(40vh, 320px);
    overflow: auto;
  }
  .label {
    color: var(--muted);
    margin-bottom: 4px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    padding: 3px 0;
    border-bottom: 1px solid var(--border);
  }
  li.off {
    opacity: 0.45;
  }
  li label {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .kind {
    font-size: 10px;
    text-transform: uppercase;
    color: var(--accent);
    width: 58px;
  }
  .kind.note {
    color: var(--muted);
  }
  .title {
    font-weight: 500;
  }
  li code {
    display: block;
    margin-left: 84px;
    color: var(--muted-strong);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dropped {
    color: var(--muted);
    margin: 8px 0 0;
    font-style: italic;
  }
  input[aria-invalid='true'] {
    border-color: var(--bad);
  }
</style>
