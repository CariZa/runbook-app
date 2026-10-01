<script lang="ts">
  import type { Step, StepRun } from './types'

  // Right-hand terminal pane (2a): the transcript of one step's last run, with a scrubber
  // across the steps that have run.
  interface Props {
    steps: Step[]
    runs: Record<string, StepRun>
    stepId: string | null
    onScrub: (id: string) => void
    onCopy: (text: string) => void
    onStop: () => void
  }
  let { steps, runs, stepId, onScrub, onCopy, onStop }: Props = $props()

  // Clear hides transcripts up to now; the steps keep their own last output.
  let clearedAt = $state(0)

  const ran = $derived(steps.filter((s) => runs[s.id]))
  const index = $derived(ran.findIndex((s) => s.id === stepId))
  const step = $derived(index >= 0 ? ran[index] : undefined)
  const run = $derived(step ? runs[step.id] : undefined)
  const visible = $derived(run && run.startedAt > clearedAt ? run : undefined)
  const label = $derived(step ? step.title || 'untitled step' : '')

  const transcript = $derived.by(() => {
    if (!visible || !step) return ''
    const cmd = (visible.command ?? step.command).split('\n')
    const lines = [...(visible.cwd ? [`$ cd ${visible.cwd}`] : []), `$ ${cmd[0]}`, ...cmd.slice(1).map((l) => `> ${l}`)]
    return lines.join('\n') + '\n' + visible.output
  })

  let bodyEl = $state<HTMLPreElement>()
  $effect(() => {
    transcript
    if (visible?.status === 'running' && bodyEl) bodyEl.scrollTop = bodyEl.scrollHeight
  })
</script>

<section class="pane" aria-label="Terminal">
  <header>
    <span class="label">Terminal</span>
    <span class="spacer"></span>
    {#if step}
      <button class="nav" onclick={() => onScrub(ran[index - 1].id)} disabled={index <= 0} aria-label="Previous step">‹</button>
      <span class="which" title={label}>{label}</span>
      <button class="nav" onclick={() => onScrub(ran[index + 1].id)} disabled={index >= ran.length - 1} aria-label="Next step">›</button>
    {/if}
  </header>

  <pre class="body" bind:this={bodyEl}>{#if visible}{transcript}{#if visible.status === 'running'}<span class="streaming">▌ streaming…</span>{:else}<span class="trailer"
          >{visible.endReason === 'stopped'
            ? 'stopped'
            : visible.endReason === 'timeout'
              ? 'timed out'
              : `exit ${visible.exitCode}`} · {((visible.durationMs ?? 0) / 1000).toFixed(1)}s</span
        >{/if}{:else if run}<span class="empty">(cleared)</span>{:else}<span class="empty"
        >Run a step to see its output here. Click a step to show its last output.</span
      >{/if}</pre>

  <footer>
    <button onclick={() => onCopy(transcript)} disabled={!visible}>Copy</button>
    <button onclick={() => (clearedAt = Date.now())} disabled={!visible}>Clear</button>
    <span class="spacer"></span>
    {#if visible?.status === 'running'}<button class="stop" onclick={onStop}>■ Stop</button>{/if}
  </footer>
</section>

<style>
  .pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    background: var(--term-bg);
    color: var(--term-fg);
    border-left: 1px solid var(--border);
  }
  header,
  footer {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 5px 10px;
    font-size: 12px;
  }
  header {
    border-bottom: 1px solid #2c2d31;
  }
  footer {
    border-top: 1px solid #2c2d31;
  }
  .label {
    color: #9a9a9f;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-size: 11px;
  }
  .which {
    color: var(--term-fg);
    max-width: 55%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .spacer {
    flex: 1;
  }
  button {
    background: #2a2b2f;
    border-color: #3a3b40;
    color: var(--term-fg);
  }
  .nav {
    padding: 0 7px;
  }
  .stop {
    color: #ef6a5f;
    border-color: #ef6a5f;
  }
  .body {
    flex: 1;
    margin: 0;
    padding: 8px 10px;
    overflow: auto;
    font-family: var(--mono);
    font-size: 12px;
    line-height: 1.45;
    white-space: pre-wrap;
    word-break: break-all;
  }
  .streaming {
    color: #5b92f0;
  }
  .trailer,
  .empty {
    color: #8d8d92;
  }
</style>
