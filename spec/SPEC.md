# Runbook app — build spec

A local desktop-ish app for writing, running, and sharing ops runbooks. A runbook is an
ordered list of shell steps you can run one at a time or all at once, with output captured
inline. Everything is stored as plain files on disk.

Stack: **Wails v2** — Go backend (shell execution, file I/O, run loop), web frontend.

Wireframes: `wireframes.html` (open in a browser; it is one self-contained file).
Screen ids used below — `2a`, `4a`, `6b` etc. — are the badges printed on each wireframe.

---

## 1. Data model

One runbook = one directory.

```
~/runbooks/db-latency-triage/
  runbook.md        # steps + settings (source of truth, human-editable)
  runs.log          # append-only audit log, JSONL
  versions/
    v1.md
    v2.md
```

### runbook.md

Settings live in **YAML front-matter** — runbook-level at the top of the file, per-step in a
fenced `yaml` meta block directly under the step heading. One portable file that survives
being emailed, committed, or pasted. (If this turns out to fight the editor, the fallback is
a sidecar `steps.json` keyed by step id — but do not do both.)

```markdown
---
name: db-latency-triage
cwd: ~/work/payments-api
version: 3
parent: queue-backlog          # optional, set when forked
defaults:                       # runbook-level step defaults
  destructive: false
  continueOnFail: false
  timeoutSec: 60
running:
  pauseAtDestructive: true
  audit: true
---

## 1. Check pod health

```sh
kubectl get pods -n payments | grep -v Running
```

## 2. Restart the deployment

```yaml meta
destructive: true
skipInRunAll: true
```

```sh
kubectl rollout restart deploy/payments-api
```

## 3. Escalation note

> If pool timeouts persist past 5m, page the DB oncall.
```

### Step fields (screen `3c`)

| field | type | default | meaning |
|---|---|---|---|
| `id` | string | generated | stable across reorder/rename; never shown in UI |
| `title` | string | required | the `##` heading text, minus the number |
| `kind` | `command` \| `note` | `command` | a note has prose, no command, no play button |
| `command` | string | — | may be multi-line |
| `destructive` | bool | `false` | gates the run behind a confirm dialog |
| `continueOnFail` | bool | `false` | **default is to STOP the run on failure** |
| `skipInRunAll` | bool | `false` | still runnable individually |
| `cwd` | path \| `inherit` | `inherit` | inherits the runbook `cwd` |
| `timeoutSec` | number \| null | `60` | kill the process after this |

Resolution order: step value → runbook `defaults` → built-in default above. A step only
writes a field into its meta block when it differs from the inherited value; the settings UI
shows "N steps override" per field (screen `5c`).

Numbers in `## 1.` headings are **display only** — order comes from position in the file.
Renumber on write.

### runs.log (JSONL, one object per line)

```json
{"ts":"2026-09-21T14:03:11Z","event":"step.finish","stepId":"s2","exit":0,"ms":1240}
```

Events: `run.start`, `run.finish`, `step.start`, `step.finish`, `step.skip`,
`confirm.approve`, `confirm.cancel`, `step.edit`, `runbook.save`, `runbook.fork`.

---

## 2. Run semantics

These are the rules the wireframes imply but cannot show. They matter more than the pixels.

**Shell session.** One persistent shell per runbook run, not one per step. `cd`, exported
vars, and activated environments therefore carry between steps — that is intended, it is
what makes a pasted terminal session work as a runbook. The session starts in the runbook
`cwd`; a step with its own `cwd` gets a `cd` emitted before its command and a `cd` back
after. Kill the session on run finish or Stop. In Go this is one long-lived `exec.Cmd` per run
with pipes held open, not a process per step; output is streamed to the frontend as Wails
runtime events, chunked, tagged with the step id.

**Run all / Run selected.** Runs the checked steps top to bottom. A step with
`skipInRunAll: true` is passed over and logged as `step.skip` (it stays runnable via its own
play button).

**On failure** (non-zero exit or timeout): stop the whole run, leave the failed step
expanded with output open, leave later steps untouched in `idle`. Unless the step has
`continueOnFail: true`, in which case mark it failed and carry on.

**Destructive gate.** With `pauseAtDestructive` on, Run all halts *before* the step and
shows the inline gate (`3b`, right card): Approve → runs it and the rest continues
automatically; Skip → logs `step.skip`, continues with the next step; Stop run → ends the
run, remaining steps stay idle. Running a destructive step individually shows the modal
confirm (`3b`, left card). **The confirm dialog is generic** — command text plus cwd, no
per-step custom message, no type-to-confirm. Do not add either.

**Stop** kills the current process (SIGTERM, SIGKILL after 3s) and ends the run.

**Output** is streamed and retained per step: last stdout+stderr, exit code, duration,
timestamp. It survives edits and app restarts (cache next to `runbook.md`, not in the md
itself), and is replaced on the next run of that step.

---

## 3. Screens

| id | screen | notes |
|---|---|---|
| `2a` | Main shell | Sidebar = Library / versions of this runbook / audit log. Centre = step list. Right = terminal pane, per-run, with a step scrubber. Bottom bar = selection count + Save version + Generate from selected. |
| `2b` | Step states | The full set one component must handle: idle, running, passed, failed, destructive, editing, note. Plus the runbook-title inline rename (click selects, double-click edits, ⏎ saves, esc cancels). |
| `2c` | Open + fork | Open dialog with search and per-runbook history; "Generate from selected" creates a new runbook from the checked steps, optionally keeping last outputs and a parent link. |
| `3b` | Confirm + gate | Two run-time treatments described in §2. |
| `3c` | Config shape | Same table as §1. |
| `4a` | Edit a step | **In place** — the step expands where it sits, neighbours stay visible. Title is a field in the header, command is a multi-line editor below. ⇥ moves between them, ⌘⏎ saves, esc cancels. Row: ⚙ Settings · Delete step · Cancel · Save. |
| `5a` | Add a step | Same editor, empty, appended at the end; ⌘⏎ saves and opens another. "Note only" checkbox switches `kind`. Also reachable as "insert below" from a step's ⚙. |
| `5b` | Reorder | Drag handle on hover, lifted card, dashed drop slot, renumber on drop. ⌥↑ / ⌥↓ moves the focused step. Settings travel with the step. |
| `5c` | Runbook settings | Name, cwd, the same field list as a step as *defaults*, override counts, and the two run-level toggles. |
| `6a` | Export sheet | Live Markdown preview + toggles: selected only / last outputs / step settings / timestamps. **Copy** primary, **Save .md** secondary. |
| `6b` | Export entry point | ↗ Export in the window chrome; ⌘⇧C copies selected steps straight to the clipboard with no dialog. |

Screens deliberately **not** designed yet: empty state before any runbook exists,
paste-parsing preview, version diff. Ask before inventing them.

---

## 4. Paste and export (round-trip)

Export (`6a`) and "Paste steps…" (`5a`) use the same Markdown dialect, so output from one
pastes back into the other.

**Export format:** `#` runbook name, then `Path:` line, then per step a `## N. Title`
heading, a ```sh fenced block with the command, and — when "last outputs" is on — a plain
fenced block with output and an `exit N · 1.2s` trailer. Step settings, when included, go in
the ```yaml meta block. Notes export as a blockquote.

**Paste parsing**, in order:
1. A `## ...` / `### ...` heading starts a new step; its text is the title, minus a leading
   `N.` / `N)` / `Step N:`.
2. Failing that, a numbered list item (`1. foo`) starts a step.
3. A fenced block (` ``` ` with any or no language) is that step's command. A second fenced
   block under the same step is treated as example output and discarded on import.
4. Bare lines that look like a shell prompt (`$ `, `% `, `> `) become a command step; strip
   the prompt. Consecutive prompt lines = consecutive steps.
5. Prose that is not any of the above attaches to the current step as a note step if it
   stands alone, otherwise is dropped.

Show the parse result in a preview before committing (screen not yet designed — propose
one rather than importing silently).

---

## 5. Interaction details

- Checkbox on a step = selection for "Run selected" / "Generate from selected" / export,
  **not** enable/disable.
- Unsaved edits mark the runbook dirty (`unsaved changes` tag in the chrome); switching
  runbooks with a dirty buffer must prompt. Autosave is acceptable if versions stay explicit.
- "Save version" snapshots `runbook.md` into `versions/vN.md`. Versions are **explicit
  only** — never auto-versioned on save. Fork sets `parent` in front-matter.
- Keyboard: ⌘⏎ save in an editor · esc cancel · ⇥ title ⇄ command · ⌥↑/⌥↓ move step ·
  ⌘⇧C copy selected as Markdown · space toggles selection on the focused step.

---

## 6. Visual direction

The wireframes are **low-fidelity on purpose** — hand-drawn borders, Kalam/Caveat, marker
annotations in blue. Do not reproduce that styling in the app. Take from them: layout,
hierarchy, what's on screen, and the states. Choose a normal, quiet desktop aesthetic;
monospace for commands and output, system UI for chrome. Dense over airy — this is a tool
for someone mid-incident.

---

## 7. Open questions

1. Are runs ever remote (ssh) or always the local machine?
2. Do multiple people share a runbook directory (git), and if so does `runs.log` get
   committed or ignored?
3. Should secrets/env vars be part of a runbook, or strictly ambient?
4. Is there a per-step "expected output" check worth having, or is exit code enough?
