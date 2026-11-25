<script lang="ts">
  import { tick, untrack } from 'svelte'
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
    onRefresh: () => void
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
    onRefresh,
  }: Props = $props()

  const leaf = (ref: string) => ref.slice(ref.lastIndexOf('/') + 1)
  const parentOf = (ref: string) => (ref.includes('/') ? ref.slice(0, ref.lastIndexOf('/')) : '')
  const depth = (ref: string) => (ref === '' ? 0 : ref.split('/').length)
  const byLeaf = (a: string, b: string) => leaf(a).localeCompare(leaf(b), undefined, { numeric: true })

  const subFolders = (ref: string) => folders.filter((f) => parentOf(f) === ref).sort(byLeaf)
  const runbooksIn = (ref: string) =>
    library.filter((r) => r.folder === ref).sort((a, b) => byLeaf(a.name, b.name))

  // Collapsed folders are remembered. Opening a runbook expands the folders it sits in;
  // after that they collapse like any other.
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
    remember()
  }
  function remember() {
    try {
      localStorage.setItem('collapsedFolders', JSON.stringify([...collapsed]))
    } catch {
      // not remembered: fine
    }
  }
  const isOpen = (f: string) => !collapsed.has(f)
  $effect(() => {
    const ref = current
    untrack(() => {
      let changed = false
      for (let f = parentOf(ref); f; f = parentOf(f)) changed = collapsed.delete(f) || changed
      if (changed) remember()
    })
  })

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
  // descendant (the backend refuses too). This is done with plain mouse events, not HTML5
  // drag and drop: in the macOS web view a native drag can't start on the second press of a
  // double-click (so a row you'd just clicked wouldn't lift), and it leaves :hover stuck.
  let drag = $state<{ ref: string; isFolder: boolean } | null>(null)
  let dropTarget = $state<string | null>(null)
  let ghost = $state({ x: 0, y: 0 })
  let navEl = $state<HTMLElement>()
  let press: { ref: string; isFolder: boolean; x: number; y: number } | null = null
  let justDragged = false // swallows the click that follows a drag

  function pressRow(e: MouseEvent, ref: string, isFolder: boolean) {
    if (locked || e.button !== 0) return
    press = { ref, isFolder, x: e.clientX, y: e.clientY }
    window.addEventListener('mousemove', dragMove)
    window.addEventListener('mouseup', dragUp)
    window.addEventListener('keydown', dragKey, true)
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
  function dragMove(e: MouseEvent) {
    if (!press) return
    if (!drag) {
      if (Math.hypot(e.clientX - press.x, e.clientY - press.y) < 5) return
      drag = { ref: press.ref, isFolder: press.isFolder }
    }
    e.preventDefault()
    ghost = { x: e.clientX, y: e.clientY }
    // Scroll a long library when the pointer nears its top or bottom edge.
    if (navEl) {
      const r = navEl.getBoundingClientRect()
      if (e.clientY < r.top + 24) navEl.scrollTop -= 12
      else if (e.clientY > r.bottom - 24) navEl.scrollTop += 12
    }
    // A folder takes drops over its whole block, header and contents; the innermost folder
    // under the pointer decides. Anywhere else in the Library is the top level ("").
    const over = document.elementFromPoint(e.clientX, e.clientY)?.closest('[data-drop]')
    const target = over?.getAttribute('data-drop') ?? null
    dropTarget = target !== null && canDrop(target) ? target : null
  }
  function dragUp() {
    const d = drag
    const target = dropTarget
    endDrag()
    if (!d) return
    justDragged = true
    setTimeout(() => (justDragged = false))
    if (target === null) return
    if (d.isFolder) onMoveFolder(d.ref, target)
    else onMove(d.ref, target)
  }
  function dragKey(e: KeyboardEvent) {
    if (e.key !== 'Escape' || !drag) return
    e.preventDefault()
    e.stopPropagation()
    endDrag()
  }
  function endDrag() {
    press = null
    drag = null
    dropTarget = null
    window.removeEventListener('mousemove', dragMove)
    window.removeEventListener('mouseup', dragUp)
    window.removeEventListener('keydown', dragKey, true)
  }
  function swallowClick(e: MouseEvent) {
    if (!justDragged) return
    e.preventDefault()
    e.stopPropagation()
  }

  function time(iso: string): string {
    const d = new Date(iso)
    const today = new Date().toDateString() === d.toDateString()
    return today
      ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
      : d.toLocaleDateString([], { month: 'short', day: 'numeric' })
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<nav class="sidebar" class:dragging-any={drag !== null} bind:this={navEl} onclickcapture={swallowClick}>
  <section class="library" class:drop={dropTarget === ''} data-drop="">
    <h3>
      Library
      <span class="head-actions">
        <button class="link small" onclick={onRefresh} title="Re-read ~/runbooks from disk" aria-label="Refresh library">↻</button>
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
          onmousedown={(e) => pressRow(e, rb.name, false)}
          onclick={() => onOpen(rb.name)}
          disabled={locked && rb.name !== current}
          title="{rb.steps} steps · edited {time(rb.modified)}{locked ? '' : ' · drag onto a folder, or below the folders for the top level'}"
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
      <li class="folder-row" class:dragging={drag?.ref === f} class:drop={dropTarget === f} data-drop={f}>
        <div class="folder">
          {#if editing?.kind === 'rename' && editing.folder === f}
            {@render nameBox('folder name ⏎', level)}
          {:else}
            <button
              class="fold"
              style="padding-left: {2 + level * 12}px"
              onmousedown={(e) => pressRow(e, f, true)}
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

{#if drag}
  <div class="ghost" style="left: {ghost.x + 12}px; top: {ghost.y + 8}px">
    {drag.isFolder ? '▸ ' : ''}{leaf(drag.ref)}
    <span class="ghost-to">
      {#if dropTarget === null}· not here{:else if dropTarget === ''}→ top level{:else}→ {dropTarget}{/if}
    </span>
  </div>
{/if}

<style>
  .ghost {
    position: fixed;
    z-index: 1000;
    pointer-events: none;
    padding: 2px 8px;
    border: 1px solid var(--accent);
    border-radius: 4px;
    background: var(--surface);
    color: var(--fg);
    font-size: 12px;
    font-weight: 600;
    white-space: nowrap;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.25);
  }
  .ghost-to {
    font-weight: normal;
    color: var(--muted);
  }
  .sidebar.dragging-any {
    cursor: grabbing;
    user-select: none;
    -webkit-user-select: none;
  }
  .sidebar.dragging-any :global(button) {
    cursor: grabbing;
  }
  /* While dragging, only the drop target is marked: no hover highlight on rows passed over. */
  .sidebar.dragging-any .rb:hover:not(.current) {
    background: none;
  }
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
  .library.drop,
  .folder-row.drop {
    outline: 1px dashed var(--accent);
    outline-offset: -1px;
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
