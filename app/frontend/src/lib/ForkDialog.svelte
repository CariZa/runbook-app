<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import type { Step } from './types'

  // "Generate from selected" (2c, right card): a new runbook from the checked steps.
  interface Props {
    source: string // ref of the runbook being split
    folders: string[]
    folder: string // default: the source's folder
    steps: Step[]
    selected: string[]
    onCreate: (ref: string, stepIds: string[], keepOutputs: boolean, linkParent: boolean) => void
    onCancel: () => void
  }
  let { source, folders, folder: sourceFolder, steps, selected, onCreate, onCancel }: Props = $props()

  const sourceName = $derived(source.slice(source.lastIndexOf('/') + 1))
  let folder = $state('')

  let name = $state('')
  const picked = new SvelteSet<string>()
  let keepOutputs = $state(true)
  let linkParent = $state(true)
  let dialogEl = $state<HTMLDialogElement>()
  let nameEl = $state<HTMLInputElement>()

  $effect(() => {
    name = `${sourceName}-copy`
    folder = sourceFolder
    for (const id of selected) picked.add(id)
    dialogEl?.showModal()
    nameEl?.select()
  })

  const ids = $derived(steps.filter((s) => picked.has(s.id)).map((s) => s.id))
  const valid = $derived(/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name) && ids.length > 0)

  function create() {
    if (valid) onCreate(folder ? `${folder}/${name}` : name, ids, keepOutputs, linkParent)
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
  <h2>Generate from selected · {ids.length} of {steps.length}</h2>
  <label class="field">
    New runbook name
    <input bind:this={nameEl} bind:value={name} onkeydown={(e) => e.key === 'Enter' && create()} />
  </label>
  <label class="field">
    Folder
    <select bind:value={folder}>
      <option value="">(top level)</option>
      {#each folders as f (f)}<option value={f}>{f}</option>{/each}
    </select>
  </label>

  <div class="label">Carrying over</div>
  <ul>
    {#each steps as s (s.id)}
      <li>
        <label>
          <input type="checkbox" checked={picked.has(s.id)} onchange={() => (picked.has(s.id) ? picked.delete(s.id) : picked.add(s.id))} />
          {s.title}
        </label>
      </li>
    {/each}
  </ul>

  <label class="opt"><input type="checkbox" bind:checked={keepOutputs} /> keep last outputs as reference</label>
  <label class="opt"><input type="checkbox" bind:checked={linkParent} /> link back to parent runbook ({source})</label>

  <div class="actions">
    <span class="where">→ ~/runbooks/{folder ? `${folder}/` : ''}{name}/</span>
    <button onclick={onCancel}>Cancel</button>
    <button class="primary" onclick={create} disabled={!valid}>Create</button>
  </div>
</dialog>

<style>
  dialog {
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--surface);
    color: var(--fg);
    padding: 14px 16px;
    width: min(480px, 92vw);
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
  .field {
    display: flex;
    flex-direction: column;
    gap: 3px;
    color: var(--muted);
    margin-bottom: 10px;
  }
  .field input {
    font-family: var(--mono);
  }
  .label {
    color: var(--muted);
    margin-bottom: 3px;
  }
  ul {
    list-style: none;
    margin: 0 0 10px;
    padding: 6px 8px;
    max-height: 200px;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: 4px;
  }
  li label,
  .opt {
    display: flex;
    gap: 6px;
    align-items: center;
    padding: 1px 0;
  }
  .actions {
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
  }
</style>
