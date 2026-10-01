<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { parsePaste } from './library'
  import type { Step } from './types'

  // "Paste steps…" preview (proposed, agreed in chat): paste on the left, what was found on
  // the right, each step with a checkbox; nothing is added until "Add N steps".
  interface Props {
    onAdd: (steps: Step[]) => void
    onCancel: () => void
  }
  let { onAdd, onCancel }: Props = $props()

  let text = $state('')
  let steps = $state<Step[]>([])
  let dropped = $state<string[]>([])
  const excluded = new SvelteSet<number>()
  let dialogEl = $state<HTMLDialogElement>()
  let textEl = $state<HTMLTextAreaElement>()

  $effect(() => {
    dialogEl?.showModal()
    textEl?.focus()
  })

  // Re-parse shortly after typing stops.
  $effect(() => {
    const src = text
    const t = setTimeout(() => {
      parsePaste(src).then((r) => {
        steps = r.steps ?? []
        dropped = r.dropped ?? []
        excluded.clear()
      })
    }, 150)
    return () => clearTimeout(t)
  })

  const chosen = $derived(steps.filter((_, i) => !excluded.has(i)))

  function firstLine(s: string): string {
    return s.split('\n')[0]
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
  <h2>Paste steps</h2>
  <div class="cols">
    <textarea
      bind:this={textEl}
      bind:value={text}
      spellcheck="false"
      placeholder={'Paste Markdown, a numbered list, or a terminal session.\n\n## 1. Check pods\n```sh\nkubectl get pods\n```\n\n$ uptime'}
    ></textarea>

    <div class="found">
      <div class="label">
        {#if steps.length === 0}Nothing found yet{:else}Found {steps.length} step{steps.length === 1 ? '' : 's'}{/if}
      </div>
      <ul>
        {#each steps as s, i (i)}
          <li class:off={excluded.has(i)}>
            <label>
              <input type="checkbox" checked={!excluded.has(i)} onchange={() => (excluded.has(i) ? excluded.delete(i) : excluded.add(i))} />
              <span class="kind" class:note={s.kind === 'note'}>{s.kind}</span>
              <span class="title">{s.title}</span>
            </label>
            {#if s.kind === 'command'}<code>{firstLine(s.command)}{s.command.includes('\n') ? ' …' : ''}</code>{/if}
            {#if s.destructive}<span class="warn">⚠ destructive</span>{/if}
          </li>
        {/each}
      </ul>
      {#if dropped.length}<p class="dropped">Ignored: {dropped.join(', ')}</p>{/if}
    </div>
  </div>

  <footer>
    <span class="hint">added at the end of the runbook · headings, numbered lists, fenced blocks and $ prompts are recognised</span>
    <button onclick={onCancel}>Cancel</button>
    <button class="primary" onclick={() => onAdd($state.snapshot(chosen) as Step[])} disabled={chosen.length === 0}>
      Add {chosen.length} step{chosen.length === 1 ? '' : 's'}
    </button>
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
  .cols {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    height: min(50vh, 420px);
  }
  textarea {
    font-family: var(--mono);
    font-size: 12px;
    resize: none;
    height: 100%;
  }
  .found {
    min-height: 0;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 6px 8px;
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
    font-weight: 600;
  }
  li code {
    display: block;
    margin-left: 84px;
    font-family: var(--mono);
    color: var(--muted-strong);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .warn {
    margin-left: 84px;
    color: var(--warn);
    font-size: 11px;
  }
  .dropped {
    color: var(--muted);
    margin: 8px 0 0;
    font-style: italic;
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
