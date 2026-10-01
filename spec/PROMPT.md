# Kickoff prompt for Claude Code

Paste this as your first message, from inside `runbook-generator/`.

---

I'm building a local runbook app. Everything you need is in `handoff/`:

- `handoff/SPEC.md` — data model, run semantics, screen list, Markdown format, open questions
- `handoff/wireframes.html` — every screen, one self-contained file, open it in a browser

Read both before writing code. Screens are referenced by the badge printed on them (`2a`,
`4a`, `6b`). The wireframes are low-fidelity by intent: take layout, hierarchy, and states
from them — not the hand-drawn styling.

**What it is:** a runbook is an ordered list of shell steps. You run one step or all of
them, output is captured inline per step, and the whole thing is a plain Markdown file on
disk that you can export, paste into a chat, or commit.

**Stack is decided: Wails v2 (Go backend, web frontend).** Don't re-litigate it. Shell
execution, file I/O, and the run loop live in Go; the UI is the frontend layer. Stream
output to the frontend with Wails runtime events (one event per step, chunked), not by
polling. Pick the frontend framework yourself and tell me what you picked — plain
TypeScript or Svelte both fine, React if you prefer; no heavy UI kit.

**Before you write anything, answer these for me:**

1. How you'll model a persistent shell session in Go — `os/exec` with a long-lived
   `bash -i`, or per-step processes with state carried some other way. §2 of the spec
   requires `cd` and exported vars to survive between steps. Say how Stop (SIGTERM then
   SIGKILL) and per-step timeouts work in your model.
2. The four open questions in §7 of the spec — tell me your default assumption for each
   and I'll correct you.

Then stop and wait for my answer.

**Build order once we've agreed:**

1. **Step component with all seven states** (`2b`) against a hard-coded runbook in memory:
   idle, running, passed, failed, destructive, editing, note. No file I/O yet.
2. **The run loop** (§2 of the spec) — one persistent shell session per run, stop-on-fail
   as the default, timeout, Stop. This is the part most likely to be subtly wrong; get it
   right before anything else.
3. **The destructive gate** (`3b`) — generic confirm dialog for a single step, inline
   pause-and-approve for Run all. No custom messages, no type-to-confirm.
4. **The main shell** (`2a`) — sidebar, step list, output pane, selection bar.
5. **Persistence** — read/write `runbook.md` with the front-matter shape in §1, round-trip
   safe: load a file, save it unchanged, get a byte-identical result.
6. **Editing** (`4a` in place), **add** (`5a`), **reorder** (`5b`), **settings** (`5c`).
7. **Export** (`6a`, `6b`) and **paste parsing** (§4) — these share one Markdown dialect
   and must round-trip.

**Ground rules:**

- Stop at the end of each numbered stage and show me what runs. Don't build 1–7 in one go.
- Where the spec is explicit (defaults, the confirm UX, explicit-only versioning), follow
  it exactly — those are decisions, not suggestions.
- Where it's silent, ask rather than invent. Screens that were deliberately left undesigned
  are listed in §3.
- Keep the shell-execution layer a plain Go package with no Wails dependency, so it can be
  tested with `go test` on its own. The Wails bindings are a thin layer over it.
- Markdown parse/serialise is also plain Go — round-trip tests (`load → save → identical
  bytes`) from the start.
