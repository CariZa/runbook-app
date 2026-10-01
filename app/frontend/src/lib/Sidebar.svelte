<script lang="ts">
  import { tick } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import type { Summary, Version } from './types'

  // Left sidebar (2a): Library as a folder tree (up to MaxDepth-1 folders deep, matching
  // runbook/store.go) / versions / a link to runs.log.
  interface Props {
    library: Summary[]
    folders: string[] // folder refs, e.g. "gcp", "gcp/disks"
    current: string // ref of the open runbook
    versions: Version[]
    auditOn: boolean
    locked: boolean // a run is active: no switching, creating or moving
    maxFolderDepth?: number
    onOpen: (ref: string) => void
    onCreate: (folder: string, name: string) => void
    onCreateFolder: (parent: string, name: string) => void
    onDelete: (ref: string) => void
    onDeleteFolder: (ref: string) => void
    onRenameFolder: (ref: string, name: string) => void
    onMove: (ref: string, folder: string) => void
    onMoveFolder: (ref: string, folder: string) => void
    onOpenLog: (reveal: boolean) => void
    onOpenSettings: () => void
  }
  let {
    library,
    folders,
    current,
    versions,
    auditOn,
    locked,
    maxFolderDepth = 4,
    onOpen,
    onCreate,
    onCreateFolder,
    onDelete,
    onDeleteFolder,
    onRenameFolder,
    onMove,
    onMoveFolder,
    onOpenLog,
    onOpenSettings,
  }: Props = $props()

  const leaf = (ref: string) => ref.slice(ref.lastIndexOf('/') + 1)
  const parentOf = (ref: string) => (ref.includes('/') ? ref.slice(0, ref.lastIndexOf('/')) : '')
  const depth = (ref: string) => (ref === '' ? 0 : ref.split('/').length)
  const byLeaf = (a: string, b: string) => leaf(a).localeCompare(leaf(b), undefined, { numeric: true })

  const subFolders = (ref: string) => folders.filter((f) => parentOf(f) === ref).sort(byLeaf)
  const runbooksIn = (ref: string) =>
    library.filter((r) => r.folder === ref).sort((a, b) => byLeaf(a.name, b.name))

  // Collapsed folders are remembered; a folder holding the open runbook always shows.
  const collapsed = new SvelteSet<string>(loadCollapsed())
  function loadCollapsed(): string[] {
    try {
      return JSON.parse(localStorage.getItem('collapsedFolders') ?? '[]')
    } catch {
      return []
    }
  }
  function toggle(f: string) {
    if (collapsed.has(f)) collapsed.delete(f)
    else collapsed.add(f)
    try {
      localStorage.setItem('collapsedFolders', JSON.stringify([...collapsed]))
    } catch {
      // not remembered: fine
    }
  }
  const isOpen = (f: string) => !collapsed.has(f) || current.startsWith(f + '/')

  // Inline name box, for creating a runbook or folder and for renaming a folder.
  let editing = $state<{ kind: 'runbook' | 'folder' | 'rename'; folder: string } | null>(null)
  let newName = $state('')
  let inputEl = $state<HTMLInputElement>()

  async function startEdit(kind: 'runbook' | 'folder' | 'rename', folder = '') {
    editing = { kind, folder }
    newName = kind === 'rename' ? leaf(folder) : ''
    if (kind !== 'rename' && folder && collapsed.has(folder)) toggle(folder)
    await tick()
    inputEl?.select()
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && newName.trim() && editing) {
      const { kind, folder } = editing
      editing = null
      if (kind === 'folder') onCreateFolder(folder, newName.trim())
      else if (kind === 'rename') onRenameFolder(folder, newName.trim())
      else onCreate(folder, newName.trim())
    } else if (e.key === 'Escape') {
      editing = null
    }
  }

  // Dragging: runbooks and folders both move; a folder can't go into itself or its own
  // descendant (the backend refuses too).
  let drag = $state<{ ref: string; isFolder: boolean } | null>(null)
  let dropTarget = $state<string | null>(null)

  function dragStart(e: DragEvent, ref: string, isFolder: boolean) {
    if (locked || !e.dataTransfer) return
    drag = { ref, isFolder }
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', ref)
  }
  function canDrop(target: string): boolean {
    if (!drag) return false
    if (parentOf(drag.ref) === target) return false
    if (!drag.isFolder) return true
    if (target === drag.ref || target.startsWith(drag.ref + '/')) return false
    return depth(target) + deepestUnder(drag.ref) <= maxFolderDepth
  }
  // How many levels the dragged folder itself needs (1 = just itself).
  function deepestUnder(ref: string): number {
    let deepest = 1
    for (const f of folders) if (f.startsWith(ref + '/')) deepest = Math.max(deepest, depth(f) - depth(ref) + 1)
    return deepest
  }
  function dragOver(e: DragEvent, target: string) {
    if (!canDrop(target)) return
    e.preventDefault()
    dropTarget = target
  }
  function drop(e: DragEvent, target: string) {
    e.preventDefault()
    if (drag && canDrop(target)) {
      if (drag.isFolder) onMoveFolder(drag.ref, target)
      else onMove(drag.ref, target)
    }
    drag = null
    dropTarget = null
  }
  function dragEnd() {
    drag = null
    dropTarget = null
  }

  function time(iso: string): string {
    const d = new Date(iso)
    const today = new Date().toDateString() === d.toDateString()
    return today
      ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }
</script>

<nav class="sidebar">
  <section>
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <h3
      class:drop={dropTarget === ''}
      ondragover={(e) => dragOver(e, '')}
      ondragleave={() => dropTarget === '' && (dropTarget = null)}
      ondrop={(e) => drop(e, '')}
      title={drag ? 'Drop here to move to the top level' : ''}
    >
      Library
      <span class="head-actions">
        <button class="link small" onclick={() => startEdit('folder', '')} disabled={locked}>+ Folder</button>
        <button class="link small" onclick={() => startEdit('runbook', '')} disabled={locked}>+ New</button>
      </span>
    </h3>

    {#snippet nameBox(placeholder: string, level: number)}
      <input
        class="new-name"
        style="margin-left: {level * 12}px; width: calc(100% - {level * 12}px)"
        bind:this={inputEl}
        bind:value={newName}
        onkeydown={keydown}
        onblur={() => (editing = null)}
        {placeholder}
        aria-label={placeholder}
      />
    {/snippet}

    {#snippet runbookRow(rb: Summary, level: number)}
      <li class="lib-row" class:dragging={drag?.ref === rb.name}>
        <button
          class="rb"
          class:current={rb.name === current}
          style="padding-left: {6 + level * 12}px"
          draggable={!locked}
          ondragstart={(e) => dragStart(e, rb.name, false)}
          ondragend={dragEnd}
          onclick={() => onOpen(rb.name)}
          disabled={locked && rb.name !== current}
          title="{rb.steps} steps · edited {time(rb.modified)}{locked ? '' : ' · drag onto a folder to move'}"
        >
          {leaf(rb.name)}
        </button>
        <button
          class="trash"
          onclick={() => onDelete(rb.name)}
          disabled={locked}
          title="Delete {rb.name} (moves it to the Trash)"
          aria-label="Delete {rb.name}">🗑</button
        >
      </li>
    {/snippet}

    {#snippet folderNode(f: string, level: number)}
      <li class="folder-row" class:dragging={drag?.ref === f}>
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div
          class="folder"
          class:drop={dropTarget === f}
          ondragover={(e) => dragOver(e, f)}
          ondragleave={() => dropTarget === f && (dropTarget = null)}
          ondrop={(e) => drop(e, f)}
        >
          {#if editing?.kind === 'rename' && editing.folder === f}
            {@render nameBox('folder name ⏎', level)}
          {:else}
            <button
              class="fold"
              style="padding-left: {2 + level * 12}px"
              draggable={!locked}
              ondragstart={(e) => dragStart(e, f, true)}
              ondragend={dragEnd}
              onclick={() => toggle(f)}
              ondblclick={() => !locked && startEdit('rename', f)}
              aria-expanded={isOpen(f)}
              title="{f} · double-click to rename{locked ? '' : ' · drag to move'}"
            >
              <span class="caret">{isOpen(f) ? '▾' : '▸'}</span>
              <span class="fname">{leaf(f)}</span>
              <span class="count">{runbooksIn(f).length + subFolders(f).length}</span>
            </button>
            <button
              class="folder-act"
              onclick={() => startEdit('runbook', f)}
              disabled={locked}
              title="New runbook in {leaf(f)}"
              aria-label="New runbook in {f}">+</button
            >
            <button
              class="folder-act"
              onclick={() => startEdit('folder', f)}
              disabled={locked || depth(f) >= maxFolderDepth}
              title={depth(f) >= maxFolderDepth ? `Folders only nest ${maxFolderDepth} deep` : `New folder in ${leaf(f)}`}
              aria-label="New folder in {f}">⊞</button
            >
            <button
              class="folder-act trash-f"
              onclick={() => onDeleteFolder(f)}
              disabled={locked}
              title="Delete folder {f} (moves it and its contents to the Trash)"
              aria-label="Delete folder {f}">🗑</button
            >
          {/if}
        </div>
        {#if isOpen(f)}
          <ul>
            {#if editing?.kind === 'folder' && editing.folder === f}<li>{@render nameBox('folder-name ⏎', level + 1)}</li>{/if}
            {#if editing?.kind === 'runbook' && editing.folder === f}<li>{@render nameBox('runbook-name ⏎', level + 1)}</li>{/if}
            {#each subFolders(f) as sub (sub)}{@render folderNode(sub, level + 1)}{/each}
            {#each runbooksIn(f) as rb (rb.name)}{@render runbookRow(rb, level + 1)}{/each}
            {#if runbooksIn(f).length + subFolders(f).length === 0 && editing?.folder !== f}
              <li class="empty" style="padding-left: {6 + (level + 1) * 12}px">Empty. Drag something here, or use + / ⊞.</li>
            {/if}
          </ul>
        {/if}
      </li>
    {/snippet}

    {#if editing?.kind === 'folder' && editing.folder === ''}{@render nameBox('folder-name ⏎', 0)}{/if}
    {#if editing?.kind === 'runbook' && editing.folder === ''}{@render nameBox('runbook-name ⏎', 0)}{/if}
    <ul>
      {#each subFolders('') as f (f)}{@render folderNode(f, 0)}{/each}
      {#each runbooksIn('') as rb (rb.name)}{@render runbookRow(rb, 0)}{/each}
    </ul>
  </section>

  <section>
    <h3>Versions of this runbook</h3>
    {#if versions.length === 0}
      <p class="empty">None yet. Save version snapshots the current file.</p>
    {:else}
      <ul>
        {#each versions as v (v.n)}
          <li class="version" title="versions/v{v.n}.md · opening and diffing versions isn't designed yet">
            v{v.n} · {time(v.saved)}{#if v.parent}<span class="muted"> (from {v.parent})</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section>
    <h3>Audit log</h3>
    {#if !current}
      <p class="empty">Open a runbook to see its log.</p>
    {:else if !auditOn}
      <p class="empty">Audit log is off for this runbook (⚙ Settings).</p>
    {:else}
      <div class="log-links">
        <button class="link" onclick={() => onOpenLog(false)}>Open runs.log</button>
        <span class="muted">·</span>
        <button class="link" onclick={() => onOpenLog(true)}>Show in Finder</button>
      </div>
      <p class="hint">every command as it ran, its output, and every change · last 30 days</p>
    {/if}
  </section>

  <section class="app-settings">
    <button class="link" onclick={onOpenSettings}>Settings ⌘,</button>
  </section>

</nav>

<style>
  .app-settings {
    margin-top: auto;
  }
  .sidebar {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding: 10px;
    background: var(--chrome-bg);
    border-right: 1px solid var(--border);
    min-height: 0;
    overflow: auto;
    font-size: 12px;
  }
  h3 {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin: 0 0 4px;
    font-size: 11px;
    font-weight: 600;
    color: var(--muted);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .rb {
    display: block;
    width: 100%;
    text-align: left;
    border: none;
    background: none;
    padding: 2px 6px;
    border-radius: 4px;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .lib-row {
    position: relative;
  }
  .lib-row.dragging {
    opacity: 0.5;
  }
  li.empty {
    color: var(--muted);
    font-size: 11px;
    padding-top: 1px;
    padding-bottom: 3px;
  }
  .folder-row.dragging > .folder {
    opacity: 0.5;
  }
  .head-actions {
    display: flex;
    gap: 6px;
  }
  h3.drop,
  .folder.drop {
    outline: 1px dashed var(--accent);
    outline-offset: 1px;
    border-radius: 4px;
  }
  .folder {
    position: relative;
    display: flex;
    align-items: center;
  }
  .fold {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    border: none;
    background: none;
    padding: 2px 6px 2px 2px;
    font-size: 12px;
    font-weight: 600;
    text-align: left;
  }
  .caret {
    color: var(--muted);
    width: 10px;
    font-size: 10px;
  }
  .fname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    color: var(--muted);
    font-weight: normal;
    font-size: 11px;
  }
  .folder-act {
    border: none;
    background: none;
    padding: 0 3px;
    font-size: 12px;
    opacity: 0;
    color: var(--muted);
  }
  .trash-f {
    font-size: 11px;
    filter: grayscale(1);
  }
  .folder:hover .folder-act,
  .folder-act:focus-visible {
    opacity: 0.8;
  }
  .folder-act:hover:not(:disabled) {
    opacity: 1;
    filter: none;
  }
  .lib-row .rb {
    padding-right: 22px;
  }
  .trash {
    position: absolute;
    right: 2px;
    top: 50%;
    transform: translateY(-50%);
    border: none;
    background: none;
    padding: 0 3px;
    font-size: 11px;
    opacity: 0;
    filter: grayscale(1);
  }
  .lib-row:hover .trash,
  .trash:focus-visible {
    opacity: 0.7;
  }
  .trash:hover:not(:disabled) {
    opacity: 1;
    filter: none;
  }
  .rb:hover:not(:disabled) {
    background: var(--selected-bg);
  }
  .rb.current {
    background: var(--selected-bg);
    font-weight: 600;
  }
  .new-name {
    width: 100%;
    font-size: 12px;
    margin-bottom: 4px;
  }
  .version {
    padding: 1px 6px;
  }
  .log-links {
    display: flex;
    align-items: baseline;
    gap: 4px;
  }
  .log-links .link {
    padding: 0;
    font-size: 12px;
  }
  .muted {
    color: var(--muted);
  }
  .empty,
  .hint {
    margin: 0;
    color: var(--muted);
    font-size: 11px;
  }
  .hint {
    margin-top: 6px;
    font-style: italic;
  }
  .small {
    font-size: 11px;
    padding: 0 2px;
    text-transform: none;
    letter-spacing: 0;
  }
</style>
