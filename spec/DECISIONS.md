# Decisions

Answers to the questions `SPEC.md` leaves open, agreed 2026-09-21. `SPEC.md` still wins
where it is explicit; this file only fills gaps.

## Stack

- Wails v2, Go backend. Frontend: Svelte 5 + TypeScript + Vite (Wails template), no UI kit.
- Output pane is a plain `<pre>`; ANSI colour codes are stripped or converted, no terminal emulator.

## Shell session (§2)

- One `bash --noprofile --norc` per run, driven over pipes (no pty, not `-i`).
- The environment comes from the user's login shell, captured once at app start
  (`$SHELL -ilc env`), so GUI-launched PATH still finds Homebrew, kubectl, etc.
- Commands reach bash on its stdin; each step runs as `{ source <tmpfile>; } </dev/null`, so
  no step can read the command stream (tested with `cat` and `read`). A sentinel line with a
  per-session nonce and `$?` follows each step. `cd`, exports, functions and activated envs
  persist between steps.
- Output is streamed immediately (only a genuine partial sentinel is held back), batched every
  50ms into Wails events, and capped at the last 512KB per step in the UI.
- The app runs one run at a time; quitting the app stops an active run first.
- A step with its own `cwd` is wrapped in `cd <cwd>` … `cd -`.
- stdout and stderr are merged into one stream per step, sent as chunked Wails events tagged
  with the step id.
- A step that exits the shell (`exit`) is marked failed and ends the run.
- The session runs in its own process group. **Stop** = SIGTERM the group, SIGKILL after 3s,
  end the run.
- **Timeout**, stop-on-fail (default): same as Stop.
- **Timeout** with `continueOnFail: true`: the session uses `set -m`; SIGTERM the step's child
  process groups, SIGKILL after 3s, keep the shell. If the sentinel still doesn't return
  (the step is a pure shell-builtin loop), kill the session and end the run.
- Interactive commands (password prompts, `ssh` without a command, editors) are unsupported;
  they get `/dev/null` as stdin and fail or hit the timeout.

## Open questions (§7)

1. Runs are always local. Non-interactive `ssh host 'cmd'` is just a step.
2. Runbook dirs are git-friendly: `runbook.md` and `versions/` are meant to be committed; a
   per-runbook `.gitignore` excludes `runs.log` and the output cache.
3. Secrets and env vars are strictly ambient; nothing secret is written to `runbook.md`.
4. Exit code is the only pass/fail signal. No expected-output checks.

## Gaps in the spec

1. **Step ids** are not written to `runbook.md`. The id→step mapping lives in the output cache
   next to the file and is re-matched (by title + command) when the file changes outside the app.
2. **Round-trip:** load → save is byte-identical for files already in canonical form, and
   canonicalisation is idempotent. Arbitrary hand-edited input may be normalised on first save
   (renumbered headings, redundant meta fields dropped).
3. **Autosave:** every committed edit (⌘⏎, reorder, delete, settings save, rename) writes
   `runbook.md` immediately. The `unsaved changes` tag shows only while an editor is open with
   uncommitted text. Versions stay explicit (Save version only).
4. **Edit lock:** during a run, the running and queued steps can't be edited, reordered or
   deleted; runbook settings are read-only until the run ends.
5. **Shell:** bash (see above).
6. **Library root:** `~/runbooks/`, fixed for v1.
7. **Delete step** asks for confirmation (generic modal: step number + title).
8. **Notes are selectable** (checkbox + space) and included in export / Generate from selected.
9. **One run at a time.** While any step or run is active, other ▶ buttons are disabled.
10. **Run button in the chrome:** reads "▶ Run all" when nothing is checked and
    "▶ Run selected (N)" when some are. Either way: top to bottom, stop on the first failure
    unless that step has `continueOnFail: true`; `skipInRunAll` steps are passed over (§2).

## Shell sharing

- Single ▶ clicks share one long-lived shell per open runbook, so `cd`/`export` carry between
  clicks. A **Reset shell** button starts a fresh one. The shared shell also resets on Stop,
  when a timeout kills it, on switching runbooks, and on quit. (Replaces the spec's
  "kill the session on run finish" for single-step runs.)
- Run all / Run selected always start a fresh shell in the runbook `cwd`, so a full run is
  reproducible.

## Audit log (runs.log)

One `runs.log` (JSONL) per runbook, the full record of what ran and what changed:

- Spec events plus: `step.start` carries the exact command and effective cwd; `step.edit`
  carries before/after title and command; step output is logged (per step run, capped at
  ~1MB, keeping the tail).
- **Retention:** entries older than 30 days are removed. On opening a runbook (and daily while
  open) the file is rewritten from the first entry inside the window, via temp file + atomic
  rename. The spec's "append-only" becomes "append-only, pruned at 30 days".
- **Redaction** before anything is written to disk (log and output cache), not in the live view:
  1. literal values of env vars with sensitive-looking names (`*TOKEN*`, `*SECRET*`,
     `*PASSWORD*`, `*KEY*`, `*CREDENTIAL*`) from the login env and from exports during a run;
  2. known token formats (AWS keys, GitHub/GitLab/Slack tokens, GCP SA JSON, JWTs,
     `Authorization: Bearer`, `password=` pairs, PEM private keys, k8s Secret `data:`);
  3. a per-step "don't record output" switch.
  Redaction is best-effort; no entropy-based detection (it would eat pod names and digests).
- Stays git-ignored.

## Persistence (stage 5)

- `~/runbooks/<name>/`: `runbook.md`, `runs.log` (mode 0600), `versions/vN.md`,
  `.runbook/state.json`, `.gitignore` (ignores `runs.log` and `.runbook/`). The directory name
  is the runbook's identity; renaming renames the directory.
- **runbook.md:** the front-matter is written back verbatim (comments kept) unless a
  runbook-level field changed, then regenerated canonically. Text before the first step and
  unrecognised content under a step are kept verbatim. Commands containing fences get a longer
  fence. New per-step field: `recordOutput: false` (don't record output).
- **state.json:** step id ↔ (title, sha256 of body) for re-matching after outside edits (never
  command text), plus each step's last output (redacted) and last-run time.
- **Autosave** writes `runbook.md` on every committed change; there is no `runbook.save` audit
  entry per autosave (the edit entries carry the change). **Save version** stamps
  `version: N` into the file and copies it to `versions/vN.md`.
- **Audit events** beyond the spec: `confirm.skip` / `confirm.stop` (gate), `confirm.approve` /
  `confirm.cancel` carry `message: gate|dialog`, `step.delete`, `runbook.rename`,
  `runbook.path`, `shell.reset`, `version.save`. Entries carry a `runId` and the step's display
  number and title at the time.
- **Empty library:** first launch writes `shell-demo` (harmless commands) rather than an empty
  state, which is still undesigned.
- Opening versions and version diff are not built (undesigned).

## Editing (stage 6)

- **Step settings** open from the editor's ⚙ Settings or the ⚙ on a step row, and apply
  immediately (autosave). Each field shows inherited vs overridden; setting a field back to
  its inherited value clears the override. The panel also has **Insert step below**.
- **Add step** is a draft editor at the insertion point (end, or below a step); it becomes a
  step only when saved. **Add step** saves and closes; ⌘⏎ saves and opens another.
- **Reorder** is disabled during a run and while an editor is open. ⌥↑/⌥↓ keeps focus on the
  moved step.
- **Runbook settings** apply on Save. **Reset overrides** clears step overrides of the three
  defaulted fields (destructive, keep going, timeout); per-step skip / folder / record-output
  settings are untouched. Turning the audit log off is itself logged before it takes effect.
- Step rows show short muted tags for non-default settings (e.g. "keeps going",
  "5s timeout", "output not recorded").
- New audit events: `step.add`, `step.move`, `step.settings` (field: before → after),
  `runbook.settings`.

## Export and paste (stage 7)

- Export and paste parsing live in plain Go (`runbook/export.go`, `runbook/paste.go`) with a
  round-trip test: export → paste yields the same steps (titles, kinds, commands, notes, and
  settings when included).
- **Export outputs** come from the on-disk cache, i.e. the redacted copies. Exported step
  settings are written relative to the built-in defaults, so they mean the same thing when
  pasted into a runbook with different defaults. Timestamps go on the output trailer.
- **⌘⇧C** copies the checked steps (all steps if none are checked) using the export sheet's
  last toggles. Copies and saved exports are logged as `runbook.export`.
- **Paste rules (§4) interpretation:** headings win over numbered lists (lists only start steps
  when the text has no `##`/`###` headings). `$ ` and `% ` are always prompts; `> ` is a
  prompt only in text without headings, otherwise it is a note (so exported notes paste back
  as notes). A prompt line directly under a heading becomes that heading's command. Output
  lines after a prompt, second fences, and prose next to a command are dropped and counted.
  A heading or list item with nothing under it becomes a title-only note (a manual step).
- **Paste preview** (agreed in chat): paste on the left, found steps on the right with
  checkboxes and a command/note label, an "Ignored: …" summary, then **Add N steps**. Pasted
  steps are appended at the end.

## Deleting runbooks

- **Delete runbook…** lives in runbook settings (5c), behind a confirm (focus on Cancel). It
  moves the whole directory (versions, cache, runs.log) to `~/.Trash`, adding " 2", " 3"… on
  a name clash, so it can be dragged back. Refused during a run.
- The `shell-demo` runbook is written only on the very first launch (`~/runbooks/.seeded`
  marks it); an empty library afterwards shows a short "No runbooks yet" message.

## Arguments ({{name}} blanks)

- A command may contain `{{name}}` blanks (letters, digits, `_`, `-`). Names label the blank,
  not the value, and are found automatically. Each step shows one box per blank directly
  under its command; **values belong to that step** (two steps using `{{disk}}` have separate
  values). ⏎ in a box runs the step (destructive steps still get the confirm, focus on Cancel).
- No runbook-wide arguments panel for now (tried, removed: too far from the step being run;
  add back when shared values like project/zone are actually needed).
- Values are substituted in Go right before the run and shell-quoted (bare when plainly safe,
  single-quoted otherwise), so a value is always one literal word and can never become
  command syntax. Tested with `$(...)`, backticks, `;`, `&&`, quotes, newlines, globs.
- A run refuses to start while a blank in a step that will run is empty (Run all ignores
  steps it skips); ▶ focuses the first empty box in that step instead.
- Steps show the command with values filled in; the destructive confirm and `runs.log`
  show the exact command that ran. `runbook.md` and exports keep the `{{blanks}}`.
- Last-used values are remembered per step in `.runbook/state.json` (`stepArgs`, never in
  `runbook.md`) and dropped with their step; values of secret-sounding names (token,
  password, secret, key…) are not remembered.
- Not built yet (asked, deferred): defaults stored in the file; "run once per value" lists.

## Sidebar simplifications

- The sidebar no longer lists audit entries; **Audit log** links to **Open runs.log** (default
  text editor) and **Show in Finder**. runs.log is unchanged and remains the full record.
- The stage-1 "Step states (2b)" gallery (a dev view for checking against the wireframe) is
  removed.

## Folders (nested, up to 4 deep)

- Was one level; now a ref is a path of up to `runbook.MaxDepth` (5) segments, so runbooks sit
  at most 4 folders deep. A directory with `runbook.md` is a runbook and is never descended
  into; any other directory is a folder. `List`/`Folders` walk the tree.
- **Folders move and rename** via `MoveFolder`/`RenameFolder` (one rename = a move within the
  same parent). Refused: into itself or a descendant, onto an existing name, into a runbook,
  and any move that would push contents past the depth limit. Guards live in the store, not
  just the UI, and the open runbook's ref follows a folder that moves.
- Sidebar is a recursive tree: 12px indent per level, collapse remembered per folder ref,
  double-click a folder name to rename, hover gives + (runbook), ⊞ (subfolder, disabled at
  the limit) and 🗑.
- Superseded (kept for history): one level of folders: `~/runbooks/<folder>/<name>/runbook.md`. A folder is a top-level
  directory without a runbook.md. A runbook's ref is `name` or `folder/name`; everything
  that stored a runbook name (state, audit, UI) now stores the ref.
- Library shows folders first (alphabetical, collapsible, collapsed state remembered; the open
  runbook's folder always shows), runbooks inside sorted by name with numeric ordering, then
  loose runbooks. **+ Folder**, a hover **+** (new runbook in folder) and 🗑 per folder.
- Moving: drag a runbook onto a folder (or the Library heading for the top level), or the
  **Folder** dropdown in runbook settings. Everything in the runbook's directory moves with
  it. Moves are refused during a run and logged as `runbook.move`.
- Generate from selected has a **Folder** choice (defaults to the source's folder).
- Deleting a folder confirms (listing its runbooks), then moves the whole folder to the
  Trash. Renaming keeps a runbook in its folder. The top bar shows `folder /` before the name.

## Steps are named, not numbered (UI)

- Step rows no longer show `N ·`. Messages, confirms and the terminal pane's scrubber name the
  step (“title”, truncated) instead of its number. `runs.log` still records the step number and
  `runbook.md` still numbers headings (display only, renumbered on write, stripped on paste).
- **Duplicate** (hover a step, next to ⚙ and Edit) inserts a copy directly below with the same
  command, settings and remembered argument values, titled "… copy", and opens it in the editor.
  Logged as `step.add` with `message: duplicate of <title>`.

## Zoom and the menu bar

- macOS has no system-wide font size, so the app does per-app zoom like other Mac apps:
  ⌘+ / ⌘− / ⌘0 and a **View** menu, stepping 70%–200% and remembered in localStorage.
  It scales the page root (`document.documentElement.style.zoom`), so text and layout scale
  together; converting every px to rem was rejected as a large change for a worse result.
- The menu bar also carries the standard **App** and **Edit** menus. Without an Edit menu a
  Wails app on macOS gets no ⌘C / ⌘V / ⌘A in its text fields.

## Notion import (phase 1, read-only)

- **Import from Notion…** (toolbar, next to Paste steps…) takes a page link, reads the page
  through the Notion API and previews the steps it would make. Nothing is ever written back
  to Notion; a push-back phase was deferred until the read path has been used in anger.
- Blocks map onto the existing Markdown dialect and go through the same `ParsePaste` rules:
  `heading_1/2` → a step, `heading_3` → `###`, `code` → a fenced command, paragraphs, quotes,
  callouts, toggles and lists → prose. Images, tables, columns, embeds, sub-pages and
  databases can't become steps and are summarised as “Ignored: 2 images, 1 table”. Nested
  blocks are followed 3 levels deep; results are paginated 100 at a time.
- Database and view links are refused (a runbook comes from one page), as is any non-Notion
  host. The preview has a checkbox per step, and the steps you keep either create a new
  runbook (name suggested from the page title, with a folder choice) or are appended to the
  open one.
- A created runbook records `notion: {pageId, url}` in its front matter, which is what puts
  **Open in Notion ↗** in the toolbar — and what phase 2 would need to push back.

## Settings (⌘,) and the Notion token

- App-level **Settings** — the sidebar's bottom link and ⌘, — is separate from the per-runbook
  ⚙ Settings. Today it holds the Notion integration token: the field is a password field, and
  once saved the token shows only as a masked hint (`ntn_••••••WXYZ`) with **Replace…** and
  **Remove**. The full token is never shown again.
- The token lives in the login Keychain, not in a settings file, so it can't end up in
  `runbook.md`, an export or a backup of `~/runbooks`. It's stored with `-A` (no per-app
  prompt): a per-app ACL would be granted to `/usr/bin/security`, which any process can run,
  so it would cost a prompt on every read without actually restricting anything. Anything
  running as this user can read it, exactly as with a file in the home directory — the
  Keychain buys encryption at rest and keeps it out of the library.
- The import dialog no longer asks for a token; when none is saved it points at Settings.

## Not yet

- No git repo for the app itself until the prototype proves out.
- Notion phase 2 (push a managed section back, with a `last_edited_time` stale check) waits
  until the read path has been used.
- Undesigned screens (empty state, paste preview, version diff): propose before building.
